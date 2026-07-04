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

	movieID := movie.ID
	ctx = context.WithValue(ctx, translationMovieIDKey{}, movieID)

	targetLanguages := s.targetLanguages()
	if len(targetLanguages) == 0 {
		return nil, "", fmt.Errorf("target language is required")
	}

	sourceLang := normalizeLanguage(s.cfg.SourceLanguage)
	if sourceLang == "" {
		sourceLang = sourceLangAuto
	}

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
			isActress:  strings.HasPrefix(fieldName, "actress["),
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
	for i := range movie.Actresses {
		if models.CanonicalizeUnknownActress(&movie.Actresses[i]) {
			continue
		}
		actress := movie.Actresses[i]
		jaName := strings.TrimSpace(actress.JapaneseName)
		if jaName == "" {
			continue
		}
		if lastName, firstName, ok := extractNamesFromDMMActjpgsURL(actress.ThumbURL); ok {
			actressJaNameToRomanized[jaName] = joinName(lastName, firstName)
		} else if actress.FirstName != "" && isLikelyRomanized(actress.FirstName) {
			name := strings.TrimSpace(actress.FirstName)
			if actress.LastName != "" && isLikelyRomanized(actress.LastName) {
				name = strings.TrimSpace(actress.LastName) + " " + name
			}
			actressJaNameToRomanized[jaName] = name
		}
	}

	// Per-actress transliteration source, resolved once. Romaji pins the correct
	// kanji reading, so it is preferred over the Japanese name as LLM input:
	// ① DMM actjpgs URL, ② scraper-provided romanization, ③ cleaned Japanese name.
	// Empty entry → nothing usable, actress is skipped.
	// A romaji source is additionally resolved to Hangul with the built-in
	// transliteration table: for Korean the LLM is then bypassed entirely, so it
	// can neither echo the romaji back nor substitute a different reading.
	// The loop runs regardless of fields.Actresses because the Hangul names are
	// also used to substitute actress names appearing inside the title.
	actressSourceNames := make([]string, len(movie.Actresses))
	actressHangulNames := make([]string, len(movie.Actresses))
	for i := range movie.Actresses {
		if models.CanonicalizeUnknownActress(&movie.Actresses[i]) {
			logging.Debugf("Translation[%s]: actress[%d] skip — unknown placeholder", movieID, i)
			continue
		}
		actress := movie.Actresses[i]
		if lastName, firstName, ok := extractNamesFromDMMActjpgsURL(actress.ThumbURL); ok {
			actressSourceNames[i] = joinName(lastName, firstName)
		} else if jaName := strings.TrimSpace(actress.JapaneseName); jaName != "" {
			if romanized := actressJaNameToRomanized[jaName]; romanized != "" {
				actressSourceNames[i] = romanized
			} else {
				actressSourceNames[i] = cleanActressNameForTranslation(jaName)
			}
		}
		if actressSourceNames[i] == "" {
			logging.Debugf("Translation[%s]: actress[%d] skip — no usable source name (JapaneseName=%q ThumbURL=%q)", movieID, i, actress.JapaneseName, actress.ThumbURL)
			continue
		}
		// A Hangul name already on the record (e.g. promoted in the DB) is used
		// directly; otherwise a romaji source is transliterated via the table.
		if hangul := hangulActressName(actress); hangul != "" {
			actressHangulNames[i] = hangul
		} else if isLikelyRomanized(actressSourceNames[i]) {
			if hangul, ok := romajiToHangul(actressSourceNames[i]); ok {
				actressHangulNames[i] = hangul
			}
		}
		logging.Debugf("Translation[%s]: actress[%d] transliteration input %q (JapaneseName=%q, table=%q)", movieID, i, actressSourceNames[i], actress.JapaneseName, actressHangulNames[i])
	}

	// Bracketed VR tags ("[VR]", "【8K VR】") and shop-promotion tags ("[FANZA 限定]",
	// "[数量限定]") are dropped before translation — they label the release, not the work.
	cleanedTitle := cleanTitleForTranslation(movie.Title)
	titleForTranslation := cleanedTitle

	// Store promotions / platform notices are stripped from the description so they
	// are never translated into the output.
	descriptionForTranslation := movie.Description
	if fields.Description {
		descriptionForTranslation = cleanDescriptionForTranslation(movie.Description)
		if descriptionForTranslation != strings.TrimSpace(movie.Description) {
			logging.Debugf("Translation[%s]: description promotional text removed before translation", movieID)
		}
	}
	descriptionPromotionalOnly := fields.Description && descriptionForTranslation == "" && strings.TrimSpace(movie.Description) != ""

	// Actress names appearing inside the title or description are pinned to their
	// table-confirmed Hangul reading so those fields and the actress fields always
	// agree on the same transliteration (伊藤舞雪 → 이토 마유키, never a guessed
	// reading). Each actress with a Hangul reading gets a stable ⟦N⟧ token; the LLM
	// receives the placeholder form (an opaque token survives the round-trip far
	// better than embedded Hangul, which local models "correct" to 히비キ etc.) and
	// the token is restored afterwards. applyNameSubs returns the Hangul form, the
	// placeholder form, and the token→Hangul map for the tokens it actually used.
	type nameSub struct{ jaName, hangul, token string }
	nameSubs := make([]nameSub, 0, len(movie.Actresses))
	for i := range movie.Actresses {
		if actressHangulNames[i] == "" {
			continue
		}
		jaName := strings.TrimSpace(movie.Actresses[i].JapaneseName)
		if jaName == "" {
			continue
		}
		nameSubs = append(nameSubs, nameSub{jaName, actressHangulNames[i], fmt.Sprintf("⟦%d⟧", len(nameSubs))})
	}
	applyNameSubs := func(src string) (hangulVer, placeheldVer string, tokens map[string]string) {
		hangulVer, placeheldVer = src, src
		tokens = make(map[string]string)
		for _, sub := range nameSubs {
			if !strings.Contains(hangulVer, sub.jaName) {
				continue
			}
			hangulVer = strings.ReplaceAll(hangulVer, sub.jaName, sub.hangul)
			placeheldVer = strings.ReplaceAll(placeheldVer, sub.jaName, sub.token)
			tokens[sub.token] = sub.hangul
			logging.Debugf("Translation[%s]: substituted actress %q → Hangul %q (placeholder %q)", movieID, sub.jaName, sub.hangul, sub.token)
		}
		return
	}

	koTitleHangul, koTitlePlaceheld, titleTokens := applyNameSubs(cleanedTitle)
	koDescHangul, koDescPlaceheld, descTokens := applyNameSubs(descriptionForTranslation)
	// koTitleDirect: the title WAS only the actress name(s) — final without an LLM call.
	koTitleDirect := len(titleTokens) > 0 && !containsTranslatableText(koTitleHangul)

	// When the title is an actress name, queue it under the title_as_name label so the
	// person-name prompt rule guarantees phonetic transliteration (never a semantic
	// translation like 夏 → 여름). Romaji, when known, replaces the input because it
	// pins the correct reading of the kanji.
	titleFieldName := "title"
	if normTitle := strings.TrimSpace(titleForTranslation); normTitle != "" {
		for _, actress := range movie.Actresses {
			if strings.TrimSpace(actress.JapaneseName) != normTitle {
				continue
			}
			titleFieldName = fieldNameTitleAsName
			if romanized := actressJaNameToRomanized[normTitle]; romanized != "" {
				titleForTranslation = romanized
			}
			logging.Debugf("Translation[%s]: title %q matches actress JapaneseName → transliterating as person name (input %q)", movieID, normTitle, titleForTranslation)
			break
		}
	}

	for _, lang := range targetLanguages {
		rec := getRecord(lang)
		if fields.Title {
			switch {
			case lang == "ko" && koTitleDirect:
				rec.Title = koTitleHangul
				touchedRecords[lang] = true
				if s.cfg.ApplyToPrimary && lang == targetLanguages[0] {
					movie.Title = koTitleHangul
				}
			case lang == "ko" && len(titleTokens) > 0:
				queueField(lang, koTitlePlaceheld, func(v string) { rec.Title = v }, func(v string) { movie.Title = v }, "title")
			default:
				queueField(lang, titleForTranslation, func(v string) { rec.Title = v }, func(v string) { movie.Title = v }, titleFieldName)
			}
		}
		if fields.OriginalTitle {
			queueField(lang, movie.OriginalTitle, func(v string) { rec.OriginalTitle = v }, func(v string) { movie.OriginalTitle = v }, "original_title")
		}
		if fields.Description {
			descInput := descriptionForTranslation
			if lang == "ko" && len(descTokens) > 0 {
				descInput = koDescPlaceheld
			}
			queueField(lang, descInput, func(v string) { rec.Description = v }, func(v string) { movie.Description = v }, "description")
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
		if fields.Actresses {
			if rec.Actresses == nil {
				rec.Actresses = make([]string, len(movie.Actresses))
			}
			for i := range movie.Actresses {
				idx := i
				actress := &movie.Actresses[idx]
				if models.CanonicalizeUnknownActress(actress) {
					rec.Actresses[idx] = models.UnknownActressName
					continue
				}
				if actressSourceNames[idx] == "" {
					continue
				}
				if lang == "ko" && actressHangulNames[idx] != "" {
					// Table-resolved reading — assigned directly, no LLM slot.
					rec.Actresses[idx] = actressHangulNames[idx]
					touchedRecords[lang] = true
					if s.cfg.ApplyToPrimary && lang == targetLanguages[0] {
						replaceActressName(actress, actressHangulNames[idx])
					}
					continue
				}
				queueField(lang, actressSourceNames[idx],
					func(v string) { rec.Actresses[idx] = v },
					func(v string) { replaceActressName(actress, v) },
					fmt.Sprintf("actress[%d]", idx))
			}
		}
	}

	if len(requests) == 0 && len(touchedRecords) == 0 && !movieTranslationRecordsHaveContent(records) {
		if descriptionPromotionalOnly && s.cfg.ApplyToPrimary {
			movie.Description = ""
		}
		return nil, "", nil
	}

	// Group requests by target language and execute one batch per language.
	requestsByLang := make(map[string][]int)
	for i := range requests {
		requestsByLang[requests[i].targetLang] = append(requestsByLang[requests[i].targetLang], i)
	}

	var warnings []string
	for lang, indexes := range requestsByLang {
		texts := make([]string, 0, len(indexes))
		fieldNames := make([]string, 0, len(indexes))
		for _, idx := range indexes {
			texts = append(texts, requests[idx].text)
			fieldNames = append(fieldNames, requests[idx].fieldName)
		}

		translatedTexts, err := s.translateTexts(ctx, sourceLang, lang, texts, fieldNames)
		if err != nil {
			logging.Debugf("Translation[%s]: translateTexts failed: %v", movieID, err)
			warning := sanitizeTranslationWarning(normalizeProvider(s.cfg.Provider), err)
			return nil, warning, err
		}
		if len(translatedTexts) != len(indexes) {
			logging.Debugf("Translation[%s]: count mismatch - got %d, expected %d", movieID, len(translatedTexts), len(indexes))
			return nil, "", fmt.Errorf("translation provider returned %d items for %d inputs", len(translatedTexts), len(indexes))
		}

		// Person-name slots for a Korean target must come back in Hangul. Models —
		// local ones especially — may echo the romaji input unchanged instead of
		// transliterating it. Retry those slots individually; if the echo persists,
		// keep the source name and surface a warning rather than failing the batch.
		if lang == "ko" {
			var retryPos []int
			for i, reqIdx := range indexes {
				translated := strings.TrimSpace(translatedTexts[i])
				if translated != "" && isPersonNameField(requests[reqIdx].fieldName) && !containsHangul(translated) {
					logging.Debugf("Translation[%s]: non-Hangul result %q for %s, retrying slot individually", movieID, translated, requests[reqIdx].fieldName)
					retryPos = append(retryPos, i)
				}
			}
			if len(retryPos) > 0 {
				retryTexts := make([]string, len(retryPos))
				retryFields := make([]string, len(retryPos))
				for j, pos := range retryPos {
					retryTexts[j] = requests[indexes[pos]].text
					retryFields[j] = requests[indexes[pos]].fieldName
				}
				retried, retryErr := s.translateTextsOneByOne(ctx, sourceLang, lang, retryTexts, retryFields)
				if retryErr != nil {
					logging.Debugf("Translation[%s]: per-slot Hangul retry failed: %v", movieID, retryErr)
				}
				for j, pos := range retryPos {
					if retryErr == nil {
						if r := strings.TrimSpace(retried[j]); containsHangul(r) {
							translatedTexts[pos] = r
							continue
						}
					}
					translatedTexts[pos] = requests[indexes[pos]].text
					warnings = append(warnings, fmt.Sprintf("%s: LLM returned non-Hangul, kept source name", requests[indexes[pos]].fieldName))
				}
			}
		}

		for i, reqIdx := range indexes {
			raw := translatedTexts[i]
			translated := strings.TrimSpace(raw)
			if translated == "" {
				if requests[reqIdx].isActress {
					logging.Debugf("Translation[%s]: empty result for %s (original=%q, raw=%q), non-person name — using Unknown", movieID, requests[reqIdx].fieldName, requests[reqIdx].text, raw)
					warnings = append(warnings, fmt.Sprintf("%s: non-person name, set to Unknown", requests[reqIdx].fieldName))
					translated = models.UnknownActressName
				} else {
					logging.Debugf("Translation[%s]: empty result for %s (original=%q, raw=%q), falling back to original", movieID, requests[reqIdx].fieldName, requests[reqIdx].text, raw)
					warnings = append(warnings, fmt.Sprintf("%s: empty translation, kept original", requests[reqIdx].fieldName))
					translated = requests[reqIdx].text
				}
			} else if requests[reqIdx].isActress && models.IsUnknownActressName(translated) {
				translated = models.UnknownActressName
			}
			// Restore actress-name placeholders to their Hangul. If the model dropped
			// a placeholder, fall back to the direct Hangul field so the name is never
			// lost (surrounding text may stay untranslated).
			switch {
			case isTitleTranslationField(requests[reqIdx].fieldName):
				cleanedTitle := cleanTitleForTranslation(translated)
				if cleanedTitle != "" {
					translated = cleanedTitle
				} else {
					translated = requests[reqIdx].text
				}
				if lang == "ko" && len(titleTokens) > 0 {
					restored, ok := restoreNamePlaceholders(translated, titleTokens)
					if !ok {
						logging.Debugf("Translation[%s]: title placeholder missing in LLM output %q, using Hangul fallback", movieID, translated)
						restored = koTitleHangul
					}
					translated = restored
				}
			case requests[reqIdx].fieldName == "description":
				if lang == "ko" && len(descTokens) > 0 {
					restored, ok := restoreNamePlaceholders(translated, descTokens)
					if !ok {
						logging.Debugf("Translation[%s]: description placeholder missing in LLM output %q, using Hangul fallback", movieID, translated)
						restored = koDescHangul
					}
					translated = restored
				}
			}
			requests[reqIdx].apply(translated)
		}
	}

	var warning string
	if len(warnings) > 0 {
		warning = fmt.Sprintf("Translation (%s): %s", normalizeProvider(s.cfg.Provider), strings.Join(warnings, "; "))
		logging.Warnf("Translation[%s]: %s", movieID, warning)
	}
	if descriptionPromotionalOnly && s.cfg.ApplyToPrimary {
		movie.Description = ""
	}

	// Collect output records in target-language order
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

func movieTranslationRecordsHaveContent(records map[string]*models.MovieTranslation) bool {
	for _, record := range records {
		if record != nil && movieTranslationHasContent(*record) {
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

var descriptionPromoStoreRE = regexp.MustCompile(`(?:特集\s*)?最新作やセール商品など、お得な情報満載[の의]\s*『[^』]*KMPストア[^』]*』はこちら！?`)

var descriptionPromotionalAnchors = []string{
	"※この作品はバイノーラル録音されております",
	"※ この作品はバイノーラル録音されております",
	"※この商品は専用プレイヤーでの視聴に最適化されています",
	"※ この商品は専用プレイヤーでの視聴に最適化されています",
	"※VR専用作品は必ず下記リンクより動作環境・対応デバイス",
	"※ VR専用作品は必ず下記リンクより動作環境・対応デバイス",
	"「動作環境・対応デバイス」について",
	"※ 配信方法によって収録内容が異なる場合があります",
	"※配信方法によって収録内容が異なる場合があります",
	"特集 最新作やセール商品など、お得な情報満載",
	"最新作やセール商品など、お得な情報満載",
}

// cleanDescriptionForTranslation removes platform notices and store promotions
// that should not be translated into the output description.
func cleanDescriptionForTranslation(description string) string {
	description = strings.TrimSpace(description)
	if description == "" {
		return ""
	}

	cutAt := len(description)
	for _, anchor := range descriptionPromotionalAnchors {
		if idx := strings.Index(description, anchor); idx >= 0 && idx < cutAt {
			cutAt = idx
		}
	}
	if cutAt < len(description) {
		description = strings.TrimSpace(description[:cutAt])
	}

	description = descriptionPromoStoreRE.ReplaceAllString(description, "")
	description = asciiSpaceRunRE.ReplaceAllString(description, " ")
	return strings.TrimSpace(description)
}

// vrMarkerRE matches bracketed VR tags like [VR], [8K VR], 【VR】, ［4K VR］, （VR）.
// Only bracketed marker tokens are removed; a bare "VR" inside the title text is kept
// because there it carries meaning rather than labeling the release format.
var vrMarkerRE = regexp.MustCompile(`[\[【［(（][\s　]*(?:\d+[\s　]*[KkＫｋ][\s　]*)?[VvＶｖ][RrＲｒ](?:[\s　]*(?:専用|動画|作品))?[\s　]*[\]】］)）]`)

var asciiSpaceRunRE = regexp.MustCompile(`[ \t]{2,}`)

// cleanTitleForTranslation strips release-format and sales-channel markers from a
// title before translation: bracketed VR tags ([VR], 【8K VR】) and shop-promotion
// tags ([FANZA 限定], [数量限定]). Neither describes the work itself.
func cleanTitleForTranslation(title string) string {
	return stripPromoMarkers(stripVRMarkers(title))
}

// stripVRMarkers removes bracketed VR format tags from a title before translation.
// Scraped metadata often carries tags like "[VR]" or "【8K VR】" that may not match
// the actual file, so they are dropped from the translated title.
func stripVRMarkers(title string) string {
	if !vrMarkerRE.MatchString(title) {
		return title
	}
	cleaned := vrMarkerRE.ReplaceAllString(title, "")
	cleaned = asciiSpaceRunRE.ReplaceAllString(cleaned, " ")
	return strings.TrimSpace(cleaned)
}

// promoMarkerRE matches bracketed shop-promotion tags like [FANZA 限定], [数量限定],
// 【期間限定セール】. Only brackets whose content carries a promo keyword are removed;
// meaningful brackets (series names etc.) are kept.
var promoMarkerRE = regexp.MustCompile(`[\[【［(（][^\]】］)）]*(?:限定|特典|セール|キャンペーン|独占|割引)[^\]】］)）]*[\]】］)）]`)

// stripPromoMarkers removes bracketed shop-promotion tags from a title before
// translation. They advertise the sales channel, not the work, so they do not
// belong in the translated title.
func stripPromoMarkers(title string) string {
	if !promoMarkerRE.MatchString(title) {
		return title
	}
	cleaned := promoMarkerRE.ReplaceAllString(title, "")
	cleaned = asciiSpaceRunRE.ReplaceAllString(cleaned, " ")
	return strings.TrimSpace(cleaned)
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

// nihonshikiToHepburn converts DMM actjpgs URL romanization (Nihon-shiki) to
// standard Hepburn. Compound digraphs are listed before their single-character
// prefixes so the replacer matches the longer form first.
var nihonshikiToHepburn = strings.NewReplacer(
	"sya", "sha", "syu", "shu", "syo", "sho",
	"tya", "cha", "tyu", "chu", "tyo", "cho",
	"zya", "ja", "zyu", "ju", "zyo", "jo",
	"si", "shi",
	"ti", "chi",
	"tu", "tsu",
	"zi", "ji",
	"hu", "fu",
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

// containsTranslatableText reports whether s still carries Japanese or Latin
// content that needs LLM translation. Hangul, digits, and punctuation alone
// do not.
func containsTranslatableText(s string) bool {
	for _, r := range s {
		switch {
		case r >= 0x3040 && r <= 0x30FF: // hiragana + katakana
			return true
		case r >= 0x3400 && r <= 0x4DBF, r >= 0x4E00 && r <= 0x9FFF: // CJK ideographs
			return true
		case r >= 'A' && r <= 'Z', r >= 'a' && r <= 'z':
			return true
		}
	}
	return false
}

// restoreNamePlaceholders replaces every ⟦N⟧ placeholder token in text with its
// Hangul name. It reports ok=false when any expected token is absent from text
// (the model dropped or mangled it), so the caller can fall back.
func restoreNamePlaceholders(text string, placeholders map[string]string) (string, bool) {
	ok := true
	for token, hangul := range placeholders {
		if !strings.Contains(text, token) {
			ok = false
			continue
		}
		text = strings.ReplaceAll(text, token, hangul)
	}
	return text, ok
}

// isPersonNameField reports whether the labeled slot carries a performer's name:
// actress[N] batch entries and a title queued under the title_as_name label.
func isPersonNameField(fieldName string) bool {
	return strings.HasPrefix(fieldName, "actress[") || fieldName == fieldNameTitleAsName
}

func isTitleTranslationField(fieldName string) bool {
	return fieldName == "title" || fieldName == fieldNameTitleAsName
}

// containsHangul reports whether s contains at least one Hangul syllable.
func containsHangul(s string) bool {
	for _, r := range s {
		if r >= 0xAC00 && r <= 0xD7A3 {
			return true
		}
	}
	return false
}

// hangulActressName returns a Hangul reading already present on the actress
// record (e.g. promoted into the DB by mergeActressData), keeping Japanese name
// order (FamilyName GivenName). Returns "" when neither name part is Hangul.
func hangulActressName(a models.Actress) string {
	last := strings.TrimSpace(a.LastName)
	first := strings.TrimSpace(a.FirstName)
	lastHangul := containsHangul(last)
	firstHangul := containsHangul(first)
	switch {
	case lastHangul && firstHangul:
		return last + " " + first
	case firstHangul:
		return first
	case lastHangul:
		return last
	default:
		return ""
	}
}

func replaceActressName(actress *models.Actress, translated string) {
	translated = strings.TrimSpace(translated)
	if actress == nil || translated == "" {
		return
	}
	if models.IsUnknownActressName(translated) {
		actress.FirstName = models.UnknownActressName
		actress.LastName = ""
		actress.JapaneseName = models.UnknownActressName
		return
	}
	// Strip parenthetical noise the LLM may append (e.g. "Kuroki(Mai" → "Kuroki").
	if idx := strings.IndexAny(translated, "([（"); idx >= 0 {
		translated = strings.TrimSpace(translated[:idx])
	}
	if translated == "" {
		return
	}
	// Latin results get diacritics folded to ASCII; Hangul results are applied as-is.
	// Anything else (kana/kanji echoed back) means transliteration failed — skip it.
	if isLikelyRomanized(translated) {
		translated = normalizeRomanizationToASCII(translated)
	} else if !containsHangul(translated) {
		logging.Debugf("Translation: replaceActressName skipping non-Latin/non-Hangul result %q", translated)
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
// Single-name format: https://pics.dmm.co.jp/mono/actjpgs/firstname[N].jpg
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
	// Strip trailing digits and underscores (e.g. "rena2" → "rena", "rena_2" → "rena", "reimi21" → "reimi")
	filename = strings.TrimRight(filename, "0123456789_")
	if filename == "" {
		return "", "", false
	}
	parts := strings.SplitN(filename, "_", 2)
	if len(parts) == 1 {
		// Single name (no family/given split) — return as first name with empty last name
		return "", nihonshikiToHepburn.Replace(parts[0]), true
	}
	if parts[0] == "" || parts[1] == "" {
		return "", "", false
	}
	return nihonshikiToHepburn.Replace(parts[0]), nihonshikiToHepburn.Replace(parts[1]), true
}

func capitalize(s string) string {
	if s == "" {
		return ""
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

func joinName(lastName, firstName string) string {
	l := strings.TrimSpace(capitalize(lastName))
	f := strings.TrimSpace(capitalize(firstName))
	if l == "" {
		return f
	}
	return l + " " + f
}

const maxTranslationRetries = 3

type translationResult struct {
	texts  []string
	rawLLM string
}

func (s *Service) translateTexts(ctx context.Context, sourceLang, targetLang string, texts []string, fieldNames []string) ([]string, error) {
	provider := normalizeProvider(s.cfg.Provider)

	// Prompts are provider-independent — build them once for LLM providers.
	var systemPrompt, userPrompt string
	var markers []string
	switch provider {
	case providerOpenAI, providerOpenAICompatible, providerAnthropic, providerBedrock:
		var err error
		systemPrompt, userPrompt, markers, err = buildLLMTranslationPrompts(sourceLang, targetLang, texts, fieldNames)
		if err != nil {
			return nil, err
		}
	case providerDeepL, providerGoogle:
	default:
		return nil, fmt.Errorf("unsupported translation provider: %s", provider)
	}

	var lastResult *translationResult
	var lastErr error
	expectedCount := len(texts)

	for attempt := 1; attempt <= maxTranslationRetries; attempt++ {
		var result *translationResult
		var err error

		switch provider {
		case providerOpenAI:
			result, err = s.translateWithOpenAI(ctx, systemPrompt, userPrompt, markers)
		case providerDeepL:
			result, err = s.translateWithDeepL(ctx, sourceLang, targetLang, texts)
		case providerGoogle:
			result, err = s.translateWithGoogle(ctx, sourceLang, targetLang, texts)
		case providerOpenAICompatible:
			result, err = s.translateWithOpenAICompatible(ctx, systemPrompt, userPrompt, markers)
		case providerAnthropic:
			result, err = s.translateWithAnthropic(ctx, systemPrompt, userPrompt, markers)
		case providerBedrock:
			result, err = s.translateWithBedrock(ctx, systemPrompt, userPrompt, markers)
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
			if len(texts) > 1 && hasSlotWordCountAnomaly(texts, result.texts) {
				logging.Debugf("Translation: slot word count anomaly detected, falling back to one-by-one")
				return s.translateTextsOneByOne(ctx, sourceLang, targetLang, texts, fieldNames)
			}
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
		var te *TranslationError
		if errors.As(lastErr, &te) && (te.Kind == TranslationErrorParse || te.Kind == TranslationErrorCountMismatch) && len(texts) > 1 {
			logging.Debugf("Translation: all retries failed with parse/count error, falling back to one-by-one")
			return s.translateTextsOneByOne(ctx, sourceLang, targetLang, texts, fieldNames)
		}
		return nil, lastErr
	}
	return nil, &TranslationError{
		Kind:    TranslationErrorProvider,
		Message: fmt.Sprintf("translation failed after %d attempts", maxTranslationRetries),
	}
}

// hasSlotWordCountAnomaly detects when an LLM has merged slots: e.g. a short input (title)
// has been given a very long output (description merged into it). Uses rune-count-based
// word estimation for Japanese input (no spaces) and whitespace-split count for output.
func hasSlotWordCountAnomaly(inputs, outputs []string) bool {
	for i := range inputs {
		if i >= len(outputs) {
			return true
		}
		estimatedInputWords := max(1, len([]rune(inputs[i]))/3)
		outputWords := len(strings.Fields(outputs[i]))
		if outputWords > estimatedInputWords*10 && outputWords > 20 {
			return true
		}
	}
	return false
}

// translateTextsOneByOne translates each text individually to avoid LLM slot-merging issues.
func (s *Service) translateTextsOneByOne(ctx context.Context, sourceLang, targetLang string, texts []string, fieldNames []string) ([]string, error) {
	results := make([]string, len(texts))
	for i, text := range texts {
		var fn []string
		if i < len(fieldNames) {
			fn = []string{fieldNames[i]}
		}
		single, err := s.translateTexts(ctx, sourceLang, targetLang, []string{text}, fn)
		if err != nil {
			return nil, err
		}
		results[i] = single[0]
	}
	return results, nil
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
