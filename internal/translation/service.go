package translation

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/models"
)

const (
	providerOpenAI           = "openai"
	providerOpenAICompatible = "openai-compatible"
	providerDeepL            = "deepl"
	providerGoogle           = "google"
	providerAnthropic        = "anthropic"
	providerBedrock          = "bedrock"

	maxTranslationResponseSize = 10 * 1024 * 1024
)

type Service struct {
	cfg        config.TranslationConfig
	httpClient *http.Client
}

func New(cfg config.TranslationConfig) *Service {
	return &Service{
		cfg: cfg,
		httpClient: &http.Client{
			Timeout: 0,
		},
	}
}

func (s *Service) TranslateMovie(ctx context.Context, movie *models.Movie, settingsHash string) ([]models.MovieTranslation, string, error) {
	if s == nil || movie == nil || !s.cfg.Enabled {
		return nil, "", nil
	}

	targetLanguages := s.targetLanguages()
	if len(targetLanguages) == 0 {
		return nil, "", fmt.Errorf("target language is required")
	}

	sourceLang := normalizeLanguage(s.cfg.SourceLanguage)
	if sourceLang == "" {
		sourceLang = sourceLangAuto
	}

	actressTargetLang := s.actressTargetLanguage()

	type pendingText struct {
		text       string
		fieldName  string
		targetLang string
		isActress  bool
		apply      func(string)
	}

	requests := make([]pendingText, 0)
	records := make(map[string]*models.MovieTranslation)
	touchedRecords := make(map[string]bool)

	getRecord := func(lang string) *models.MovieTranslation {
		if rec, ok := records[lang]; ok {
			return rec
		}
		rec := &models.MovieTranslation{
			Language:     lang,
			SourceName:   "translation:" + normalizeProvider(s.cfg.Provider),
			SettingsHash: settingsHash,
		}
		records[lang] = rec
		return rec
	}

	queueField := func(lang, raw string, assignRecord func(string), assignMovie func(string), fieldName string) {
		if sourceLang != sourceLangAuto && sourceLang == lang {
			return
		}
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" {
			return
		}
		touchedRecords[lang] = true
		requests = append(requests, pendingText{
			text:       trimmed,
			fieldName:  fieldName,
			targetLang: lang,
			apply: func(translated string) {
				assignRecord(translated)
				if s.cfg.ApplyToPrimary && lang == targetLanguages[0] {
					assignMovie(translated)
				}
			},
		})
	}

	// Queue metadata fields for each target language — texts captured from ORIGINAL movie
	fields := s.cfg.Fields

	// Pre-pass: build JapaneseName → romanized name map so we can detect when the title
	// is an actress name and substitute the romanized form instead of translating it.
	actressJaNameToRomanized := make(map[string]string)
	if fields.Actresses {
		for _, actress := range movie.Actresses {
			jaName := strings.TrimSpace(actress.JapaneseName)
			if jaName == "" {
				continue
			}
			if lastName, firstName, ok := extractNamesFromDMMActjpgsURL(actress.ThumbURL); ok {
				actressJaNameToRomanized[jaName] = capitalize(lastName) + " " + capitalize(firstName)
			} else if actress.FirstName != "" && isLikelyRomanized(actress.FirstName) {
				name := strings.TrimSpace(actress.FirstName)
				if actress.LastName != "" && isLikelyRomanized(actress.LastName) {
					name = strings.TrimSpace(actress.LastName) + " " + name
				}
				actressJaNameToRomanized[jaName] = name
			}
		}
	}

	romanizedTitle := ""
	if normTitle := strings.TrimSpace(movie.Title); normTitle != "" {
		if romanized, ok := actressJaNameToRomanized[normTitle]; ok && romanized != "" {
			romanizedTitle = romanized
			logging.Debugf("Translation: title %q matches actress JapaneseName → using romanized %q instead of translating", normTitle, romanized)
		}
	}

	for _, lang := range targetLanguages {
		rec := getRecord(lang)
		if fields.Title {
			if romanizedTitle != "" {
				rec.Title = romanizedTitle
				touchedRecords[lang] = true
				if s.cfg.ApplyToPrimary && lang == targetLanguages[0] {
					movie.Title = romanizedTitle
				}
			} else {
				queueField(lang, movie.Title, func(v string) { rec.Title = v }, func(v string) { movie.Title = v }, "title")
			}
		}
		if fields.OriginalTitle {
			queueField(lang, movie.OriginalTitle, func(v string) { rec.OriginalTitle = v }, func(v string) { movie.OriginalTitle = v }, "original_title")
		}
		if fields.Description {
			queueField(lang, movie.Description, func(v string) { rec.Description = v }, func(v string) { movie.Description = v }, "description")
		}
		if fields.Director {
			queueField(lang, movie.Director, func(v string) { rec.Director = v }, func(v string) { movie.Director = v }, "director")
		}
		if fields.Maker {
			queueField(lang, movie.Maker, func(v string) { rec.Maker = v }, func(v string) { movie.Maker = v }, "maker")
		}
		if fields.Label {
			queueField(lang, movie.Label, func(v string) { rec.Label = v }, func(v string) { movie.Label = v }, "label")
		}
		if fields.Series {
			queueField(lang, movie.Series, func(v string) { rec.Series = v }, func(v string) { movie.Series = v }, "series")
		}
		if fields.Genres {
			for i := range movie.Genres {
				idx := i
				queueField(lang, movie.Genres[idx].Name, func(string) {}, func(v string) { movie.Genres[idx].Name = v }, fmt.Sprintf("genre[%d]", i))
			}
		}
	}

	// Queue actress fields for actressTargetLang — separate batch with actress-specific prompt
	if fields.Actresses {
		actressRecord := getRecord(actressTargetLang)
		actressRecord.Actresses = make([]string, len(movie.Actresses))
		for i := range movie.Actresses {
			idx := i
			actress := &movie.Actresses[idx]

			logging.Debugf("Translation: actress[%d] before=%q JapaneseName=%q FirstName=%q LastName=%q ThumbURL=%q", idx, actressDisplayTitle(*actress), actress.JapaneseName, actress.FirstName, actress.LastName, actress.ThumbURL)

			// Priority 1: extract romanized name directly from DMM actjpgs thumbnail URL.
			// This is more reliable than LLM romanization (official DMM naming).
			if lastName, firstName, ok := extractNamesFromDMMActjpgsURL(actress.ThumbURL); ok {
				displayName := capitalize(lastName) + " " + capitalize(firstName)
				logging.Debugf("Translation: actress[%d] URL extraction → %q (LastName=%q FirstName=%q)", idx, displayName, capitalize(lastName), capitalize(firstName))
				actressRecord.Actresses[idx] = displayName
				if s.cfg.ApplyToPrimary {
					actress.LastName = capitalize(lastName)
					actress.FirstName = capitalize(firstName)
				}
				touchedRecords[actressTargetLang] = true
				continue
			}

			// Priority 2: LLM romanization — only if a Japanese source name exists.
			if strings.TrimSpace(actress.JapaneseName) == "" {
				logging.Debugf("Translation: actress[%d] skip — no JapaneseName", idx)
				continue
			}
			rawName := actressDisplayTitle(*actress)
			name := cleanActressNameForTranslation(rawName)
			if name == "" {
				continue
			}
			if sourceLang != sourceLangAuto && sourceLang == actressTargetLang {
				continue
			}
			touchedRecords[actressTargetLang] = true
			requests = append(requests, pendingText{
				text:       name,
				fieldName:  fmt.Sprintf("actress[%d]", i),
				targetLang: actressTargetLang,
				isActress:  true,
				apply: func(translated string) {
					logging.Debugf("Translation: actress[%d] LLM → %q", idx, translated)
					actressRecord.Actresses[idx] = translated
					if s.cfg.ApplyToPrimary {
						replaceActressName(actress, translated)
						logging.Debugf("Translation: actress[%d] after replaceActressName FirstName=%q LastName=%q", idx, actress.FirstName, actress.LastName)
					}
				},
			})
		}
	}

	if len(requests) == 0 && len(touchedRecords) == 0 {
		return nil, "", nil
	}

	// Group requests by (targetLang, isActress) and execute in separate batches.
	// Actress names use a specialized romanization prompt; metadata uses the general prompt.
	type batchKey struct {
		lang      string
		isActress bool
	}
	requestsByKey := make(map[batchKey][]int)
	for i := range requests {
		key := batchKey{lang: requests[i].targetLang, isActress: requests[i].isActress}
		requestsByKey[key] = append(requestsByKey[key], i)
	}

	var warnings []string
	for key, indexes := range requestsByKey {
		texts := make([]string, 0, len(indexes))
		for _, idx := range indexes {
			texts = append(texts, requests[idx].text)
		}

		var translatedTexts []string
		var err error
		if key.isActress {
			translatedTexts, err = s.translateActressNames(ctx, sourceLang, key.lang, texts)
		} else {
			translatedTexts, err = s.translateTexts(ctx, sourceLang, key.lang, texts)
		}
		if err != nil {
			logging.Debugf("Translation: translateTexts failed: %v", err)
			warning := sanitizeTranslationWarning(normalizeProvider(s.cfg.Provider), err)
			return nil, warning, err
		}
		if len(translatedTexts) != len(indexes) {
			logging.Debugf("Translation: count mismatch - got %d, expected %d", len(translatedTexts), len(indexes))
			return nil, "", fmt.Errorf("translation provider returned %d items for %d inputs", len(translatedTexts), len(indexes))
		}

		for i, reqIdx := range indexes {
			raw := translatedTexts[i]
			translated := strings.TrimSpace(raw)
			if translated == "" {
				logging.Debugf("Translation: empty result for %s (original=%q, raw=%q), falling back to original", requests[reqIdx].fieldName, requests[reqIdx].text, raw)
				warnings = append(warnings, fmt.Sprintf("%s: empty translation, kept original", requests[reqIdx].fieldName))
				translated = requests[reqIdx].text
			}
			requests[reqIdx].apply(translated)
		}
	}

	var warning string
	if len(warnings) > 0 {
		warning = fmt.Sprintf("Translation (%s): %s", normalizeProvider(s.cfg.Provider), strings.Join(warnings, "; "))
		logging.Warnf("Translation: %s", warning)
	}

	// Collect output records: metadata languages first, then actress language if distinct
	out := make([]models.MovieTranslation, 0, len(records))
	seen := make(map[string]bool)
	for _, lang := range targetLanguages {
		rec, ok := records[lang]
		if !ok || seen[lang] {
			continue
		}
		if touchedRecords[lang] || movieTranslationHasContent(*rec) {
			out = append(out, *rec)
			seen[lang] = true
		}
	}
	if !seen[actressTargetLang] {
		if rec, ok := records[actressTargetLang]; ok {
			if touchedRecords[actressTargetLang] || movieTranslationHasContent(*rec) {
				out = append(out, *rec)
			}
		}
	}

	if len(out) == 0 {
		return nil, warning, nil
	}
	return out, warning, nil
}

func (s *Service) targetLanguages() []string {
	if len(s.cfg.TargetLanguages) == 0 {
		lang := normalizeLanguage(s.cfg.TargetLanguage)
		if lang == "" {
			return nil
		}
		return []string{lang}
	}

	languages := make([]string, 0, len(s.cfg.TargetLanguages))
	seen := make(map[string]bool, len(s.cfg.TargetLanguages))
	for _, raw := range s.cfg.TargetLanguages {
		lang := normalizeLanguage(raw)
		if lang == "" || seen[lang] {
			continue
		}
		seen[lang] = true
		languages = append(languages, lang)
	}
	return languages
}

func (s *Service) actressTargetLanguage() string {
	lang := normalizeLanguage(s.cfg.ActressTargetLanguage)
	if lang == "" {
		return "en"
	}
	return lang
}

func movieTranslationHasContent(record models.MovieTranslation) bool {
	if record.Title != "" || record.OriginalTitle != "" || record.Description != "" || record.Director != "" || record.Maker != "" || record.Label != "" || record.Series != "" {
		return true
	}
	for _, actress := range record.Actresses {
		if strings.TrimSpace(actress) != "" {
			return true
		}
	}
	return false
}

// cleanActressNameForTranslation strips descriptive extras from actress name strings
// before sending to the LLM. Handles multiple patterns scrapers append to names.
func cleanActressNameForTranslation(name string) string {
	name = strings.TrimSpace(name)

	// "[name]" → "name"
	if strings.HasPrefix(name, "[") {
		if end := strings.LastIndex(name, "]"); end > 0 {
			name = strings.TrimSpace(name[1:end])
		}
	}

	// "name, age, occupation" → "name"
	if idx := strings.Index(name, ","); idx >= 0 {
		name = strings.TrimSpace(name[:idx])
	}

	// "りむ・Hカップ 20歳..." → "りむ"  (middle-dot separates name from extras)
	if idx := strings.Index(name, "・"); idx >= 0 {
		name = strings.TrimSpace(name[:idx])
	}

	// "カレン 25歳 歯科衛生士" → "カレン"  (age suffix and everything after)
	name = strings.TrimSpace(ageOccupationSuffixRE.ReplaceAllString(name, ""))

	// "高身長172cmショート× Gカップ豹変アクメギャル メイちゃん" → "メイちゃん"
	// When multiple space-separated tokens remain and the last ends with a Japanese
	// name honorific, the description precedes the name — use only the last token.
	if tokens := strings.Fields(name); len(tokens) > 1 {
		last := tokens[len(tokens)-1]
		for _, sfx := range []string{"ちゃん", "さん", "くん"} {
			if strings.HasSuffix(last, sfx) {
				name = last
				break
			}
		}
	}

	return name
}

func normalizeProvider(provider string) string {
	return strings.ToLower(strings.TrimSpace(provider))
}

func sanitizeTranslationWarning(provider string, err error) string {
	var te *TranslationError
	if errors.As(err, &te) && te.Kind == TranslationErrorHTTPStatus {
		logging.Warnf("Translation (%s): HTTP %d error", provider, te.StatusCode)
		switch {
		case te.StatusCode == 429:
			return "Translation failed: rate limited, try again later"
		case te.StatusCode == 403:
			return "Translation failed: access denied, check API key"
		case te.StatusCode >= 500:
			return "Translation failed: external service error"
		case te.StatusCode >= 400:
			return "Translation failed: request error"
		}
	}
	if errors.As(err, &te) {
		return "Translation failed: service unavailable"
	}
	return "Translation failed: internal error"
}

func normalizeLanguage(language string) string {
	return strings.ToLower(strings.TrimSpace(language))
}

func actressDisplayTitle(actress models.Actress) string {
	if strings.TrimSpace(actress.JapaneseName) != "" {
		return actress.JapaneseName
	}
	full := strings.TrimSpace(strings.TrimSpace(actress.LastName) + " " + strings.TrimSpace(actress.FirstName))
	return full
}

// ageOccupationSuffixRE matches " N歳..." suffixes (ASCII or full-width digits).
// These are descriptive extras appended to actress names by some scrapers.
var ageOccupationSuffixRE = regexp.MustCompile(`\s+[0-9０-９]+歳.*`)

var longVowelReplacer = strings.NewReplacer(
	"ā", "a", "Ā", "A",
	"ū", "u", "Ū", "U",
	"ō", "o", "Ō", "O",
	"ē", "e", "Ē", "E",
	"ī", "i", "Ī", "I",
)

func normalizeRomanizationToASCII(s string) string {
	return longVowelReplacer.Replace(s)
}

// isLikelyRomanized returns true when s contains only ASCII or Latin Extended
// characters (U+0000–U+024F). Rejects Hangul, CJK, kana, and other scripts
// so we never treat a non-romanized name as a valid romanization substitute.
func isLikelyRomanized(s string) bool {
	for _, r := range s {
		if r > 0x024F {
			return false
		}
	}
	return true
}

func replaceActressName(actress *models.Actress, translated string) {
	translated = strings.TrimSpace(translated)
	if actress == nil || translated == "" {
		return
	}
	// Strip parenthetical noise the LLM may append (e.g. "Kuroki(Mai" → "Kuroki").
	if idx := strings.IndexAny(translated, "([（"); idx >= 0 {
		translated = strings.TrimSpace(translated[:idx])
	}
	if translated == "" {
		return
	}
	translated = normalizeRomanizationToASCII(translated)
	if !isLikelyRomanized(translated) {
		logging.Debugf("Translation: replaceActressName skipping non-Latin result %q", translated)
		return
	}
	// Prompt returns Japanese name order: FamilyName GivenName.
	// parts[0] = family name → LastName; parts[1:] = given name → FirstName.
	// JapaneseName is preserved for <ACTRESS:ja>.
	parts := strings.Fields(translated)
	if len(parts) >= 2 {
		actress.LastName = parts[0]
		actress.FirstName = strings.Join(parts[1:], " ")
	} else {
		actress.FirstName = translated
		actress.LastName = ""
	}
}

// extractNamesFromDMMActjpgsURL parses lastName and firstName from DMM actjpgs thumbnail URLs.
// Format: https://pics.dmm.co.jp/mono/actjpgs/lastname_firstname[N].jpg
func extractNamesFromDMMActjpgsURL(thumbURL string) (lastName, firstName string, ok bool) {
	const prefix = "actjpgs/"
	idx := strings.LastIndex(thumbURL, prefix)
	if idx < 0 {
		return "", "", false
	}
	filename := thumbURL[idx+len(prefix):]
	if q := strings.IndexByte(filename, '?'); q >= 0 {
		filename = filename[:q]
	}
	if dot := strings.LastIndexByte(filename, '.'); dot >= 0 {
		filename = filename[:dot]
	}
	// Strip trailing digits and underscores (e.g. "rena2" → "rena", "rena_2" → "rena")
	filename = strings.TrimRight(filename, "0123456789_")
	parts := strings.SplitN(filename, "_", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", false
	}
	return parts[0], parts[1], true
}

func capitalize(s string) string {
	if s == "" {
		return ""
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

const maxTranslationRetries = 3

type translationResult struct {
	texts  []string
	rawLLM string
}

func (s *Service) translateTexts(ctx context.Context, sourceLang, targetLang string, texts []string) ([]string, error) {
	provider := normalizeProvider(s.cfg.Provider)

	var lastResult *translationResult
	var lastErr error
	expectedCount := len(texts)

	for attempt := 1; attempt <= maxTranslationRetries; attempt++ {
		var result *translationResult
		var err error

		switch provider {
		case providerOpenAI:
			result, err = s.translateWithOpenAI(ctx, sourceLang, targetLang, texts)
		case providerDeepL:
			result, err = s.translateWithDeepL(ctx, sourceLang, targetLang, texts)
		case providerGoogle:
			result, err = s.translateWithGoogle(ctx, sourceLang, targetLang, texts)
		case providerOpenAICompatible:
			result, err = s.translateWithOpenAICompatible(ctx, sourceLang, targetLang, texts)
		case providerAnthropic:
			result, err = s.translateWithAnthropic(ctx, sourceLang, targetLang, texts)
		case providerBedrock:
			result, err = s.translateWithBedrock(ctx, sourceLang, targetLang, texts)
		default:
			return nil, fmt.Errorf("unsupported translation provider: %s", provider)
		}

		if err == nil {
			if result == nil {
				err = &TranslationError{Kind: TranslationErrorProvider, Message: "translation provider returned no result"}
			} else if len(result.texts) != expectedCount {
				err = &TranslationError{
					Kind:    TranslationErrorCountMismatch,
					Message: fmt.Sprintf("translation provider returned %d items for %d inputs", len(result.texts), expectedCount),
				}
			}
		}

		if err == nil && result != nil {
			return result.texts, nil
		}

		lastResult = result
		lastErr = err

		if attempt < maxTranslationRetries {
			if isRetryableError(err, result) {
				logging.Debugf("Translation: attempt %d/%d failed (%v), retrying...", attempt, maxTranslationRetries, err)
				select {
				case <-ctx.Done():
					return nil, ctx.Err()
				case <-time.After(time.Duration(attempt) * 200 * time.Millisecond):
				}
			} else {
				logging.Debugf("Translation: attempt %d/%d failed with non-retryable error (%v), giving up", attempt, maxTranslationRetries, err)
				break
			}
		}
	}

	if lastResult != nil && lastResult.rawLLM != "" {
		logging.Debugf("Translation: all %d attempts failed. Last LLM output (length=%d):\n%s", maxTranslationRetries, len(lastResult.rawLLM), lastResult.rawLLM)
	}

	if lastErr != nil {
		return nil, lastErr
	}
	return nil, &TranslationError{
		Kind:    TranslationErrorProvider,
		Message: fmt.Sprintf("translation failed after %d attempts", maxTranslationRetries),
	}
}

// translateActressNames uses a specialized romanization prompt for LLM providers.
// For non-LLM providers (DeepL, Google), falls back to the standard translateTexts.
func (s *Service) translateActressNames(ctx context.Context, sourceLang, targetLang string, texts []string) ([]string, error) {
	provider := normalizeProvider(s.cfg.Provider)
	switch provider {
	case providerDeepL, providerGoogle:
		return s.translateTexts(ctx, sourceLang, targetLang, texts)
	default:
		return s.translateActressNamesWithLLM(ctx, sourceLang, targetLang, texts)
	}
}

// translateActressNamesWithLLM executes actress name romanization via LLM with retry logic.
func (s *Service) translateActressNamesWithLLM(ctx context.Context, sourceLang, targetLang string, texts []string) ([]string, error) {
	provider := normalizeProvider(s.cfg.Provider)

	var lastResult *translationResult
	var lastErr error
	expectedCount := len(texts)

	for attempt := 1; attempt <= maxTranslationRetries; attempt++ {
		var result *translationResult
		var err error

		switch provider {
		case providerOpenAI:
			result, err = s.translateActressNamesWithOpenAI(ctx, sourceLang, targetLang, texts)
		case providerOpenAICompatible:
			result, err = s.translateActressNamesWithOpenAICompatible(ctx, sourceLang, targetLang, texts)
		case providerAnthropic:
			result, err = s.translateActressNamesWithAnthropic(ctx, sourceLang, targetLang, texts)
		case providerBedrock:
			result, err = s.translateActressNamesWithBedrock(ctx, sourceLang, targetLang, texts)
		default:
			return nil, fmt.Errorf("unsupported translation provider: %s", provider)
		}

		if err == nil {
			if result == nil {
				err = &TranslationError{Kind: TranslationErrorProvider, Message: "translation provider returned no result"}
			} else if len(result.texts) != expectedCount {
				err = &TranslationError{
					Kind:    TranslationErrorCountMismatch,
					Message: fmt.Sprintf("translation provider returned %d items for %d inputs", len(result.texts), expectedCount),
				}
			}
		}

		if err == nil && result != nil {
			return result.texts, nil
		}

		lastResult = result
		lastErr = err

		if attempt < maxTranslationRetries {
			if isRetryableError(err, result) {
				logging.Debugf("Translation (actress): attempt %d/%d failed (%v), retrying...", attempt, maxTranslationRetries, err)
				select {
				case <-ctx.Done():
					return nil, ctx.Err()
				case <-time.After(time.Duration(attempt) * 200 * time.Millisecond):
				}
			} else {
				logging.Debugf("Translation (actress): attempt %d/%d failed with non-retryable error (%v), giving up", attempt, maxTranslationRetries, err)
				break
			}
		}
	}

	if lastResult != nil && lastResult.rawLLM != "" {
		logging.Debugf("Translation (actress): all %d attempts failed. Last LLM output (length=%d):\n%s", maxTranslationRetries, len(lastResult.rawLLM), lastResult.rawLLM)
	}

	if lastErr != nil {
		return nil, lastErr
	}
	return nil, &TranslationError{
		Kind:    TranslationErrorProvider,
		Message: fmt.Sprintf("actress translation failed after %d attempts", maxTranslationRetries),
	}
}

func isRetryableError(err error, result *translationResult) bool {
	if err == nil {
		return result != nil && len(result.texts) == 0 && result.rawLLM != ""
	}

	var te *TranslationError
	if errors.As(err, &te) {
		switch te.Kind {
		case TranslationErrorCountMismatch, TranslationErrorParse:
			return result != nil && result.rawLLM != ""
		default:
			return false
		}
	}

	return false
}
