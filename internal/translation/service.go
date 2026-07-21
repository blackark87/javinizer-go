package translation

import (
	"context"
	"errors"
	"fmt"
	"math"
	"math/rand"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	appconfig "github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/models"
)

const maxTranslationResponseSize = 10 * 1024 * 1024

// Service translates movie metadata fields via configured translator
// providers, retrying transient failures with exponential backoff.
type Service struct {
	cfg                 Config
	providers           map[string]TranslatorProvider
	acquireProviderCall func(context.Context) error
	releaseProviderCall func()
	randMu              sync.Mutex
	rand                *rand.Rand // per-instance random source for retry jitter; protected by randMu
}

var sharedProviderCalls struct {
	sync.Mutex
	active int
}

// New constructs a translation Service from the given config and providers,
// keyed by provider name.

// New accepts either the translation package's narrow Config or the
// application TranslationConfig.  The latter compatibility path keeps the
// actress-sync workflow independent from scrape's adapter while still using
// the same provider abstraction.
func New(rawConfig any, providers ...TranslatorProvider) *Service {
	var cfg Config
	switch value := rawConfig.(type) {
	case Config:
		cfg = value
	case appconfig.TranslationConfig:
		cfg = ConfigFromApp(value)
	default:
		cfg = Config{}
	}
	if len(providers) == 0 {
		client := http.DefaultClient
		providers = []TranslatorProvider{
			NewOpenAIProvider(cfg, client),
			NewOpenAICompatibleProvider(cfg, client),
			NewDeepLProvider(cfg, client),
			NewGoogleProvider(cfg, client),
			NewAnthropicProvider(cfg, client),
			NewBedrockProvider(cfg, client),
		}
	}
	m := make(map[string]TranslatorProvider, len(providers))
	for _, p := range providers {
		m[p.Name()] = p
	}
	service := &Service{
		cfg:       cfg,
		providers: m,
		rand:      rand.New(rand.NewSource(time.Now().UnixNano())), //nolint:gosec // non-crypto rand is fine for retry jitter
	}
	service.acquireProviderCall = func(ctx context.Context) error {
		limit := cfg.MaxConcurrency
		if limit <= 0 {
			limit = 3
		}
		return acquireSharedProviderCall(ctx, limit)
	}
	service.releaseProviderCall = releaseSharedProviderCall
	return service
}

// NewWithProviderLimiter wraps actual provider calls with a caller-supplied
// limiter in addition to the process-wide max_concurrency limiter.
func NewWithProviderLimiter(cfg any, acquire func(context.Context) error, release func()) *Service {
	service := New(cfg)
	if acquire == nil || release == nil {
		return service
	}
	sharedAcquire := service.acquireProviderCall
	sharedRelease := service.releaseProviderCall
	service.acquireProviderCall = func(ctx context.Context) error {
		if err := acquire(ctx); err != nil {
			return err
		}
		if err := sharedAcquire(ctx); err != nil {
			release()
			return err
		}
		return nil
	}
	service.releaseProviderCall = func() {
		sharedRelease()
		release()
	}
	return service
}

func acquireSharedProviderCall(ctx context.Context, limit int) error {
	for {
		sharedProviderCalls.Lock()
		if sharedProviderCalls.active < limit {
			sharedProviderCalls.active++
			sharedProviderCalls.Unlock()
			return nil
		}
		sharedProviderCalls.Unlock()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(25 * time.Millisecond):
		}
	}
}

func releaseSharedProviderCall() {
	sharedProviderCalls.Lock()
	if sharedProviderCalls.active > 0 {
		sharedProviderCalls.active--
	}
	sharedProviderCalls.Unlock()
}

// TranslationOutput holds all translation data produced by TranslateMovie.
// It carries genre/actress translation data as return values rather than
// mutating *models.Movie, making the translation seam explicit.
type TranslationOutput struct {
	Movie               *models.MovieTranslation
	Movies              []models.MovieTranslation
	GenreTranslations   []models.GenreTranslationData
	ActressTranslations []models.ActressTranslationData
}

// TranslationPlan captures the fields to translate as data (no closures), enabling
// inspection, batching, and deterministic application. It replaces the previous
// closure-based pendingField approach so that plan → execute is a two-phase seam.
type TranslationPlan struct {
	TargetLang     string
	SourceLang     string
	SourceLabel    string
	ApplyToPrimary bool // when true, translated values are also written back to scraped movie
	Fields         []TranslationField
}

// TranslationField describes a single field to translate, identified by name
// and index. The result is applied via ApplyPlan rather than closures.
type TranslationField struct {
	FieldName    string
	Index        int // -1 for scalar fields; >=0 for array elements (genres, actresses)
	Text         string
	Preset       *string           // deterministic result; skips the external provider
	AllowEmpty   bool              // an intentionally cleaned empty result must not restore Text
	Placeholders map[string]string // protected actress-name token → final Hangul
	FallbackText string            // used when a protected token is dropped
}

// TranslationResultMap maps each field key (FieldName or FieldName[idx]) to
// its translated text, enabling deterministic ApplyPlan without closures.
type TranslationResultMap map[string]string

// fieldKey returns the map key for a translation field.
func fieldKey(f TranslationField) string {
	if f.Index >= 0 {
		return fmt.Sprintf("%s[%d]", f.FieldName, f.Index)
	}
	return f.FieldName
}

// BuildTranslationPlan creates a TranslationPlan from the movie based on config.
// Fields are captured as data, not closures, making the plan inspectable and testable.
func (s *Service) BuildTranslationPlan(scraped *models.Movie, targetLang, sourceLang, sourceLabel string) TranslationPlan {
	fields := s.cfg.Fields
	var planFields []TranslationField

	queue := func(fieldName, text string, idx int) *TranslationField {
		trimmed := strings.TrimSpace(text)
		if trimmed == "" {
			return nil
		}
		planFields = append(planFields, TranslationField{
			FieldName: fieldName,
			Index:     idx,
			Text:      trimmed,
		})
		return &planFields[len(planFields)-1]
	}
	queuePreset := func(fieldName, text, value string, idx int, allowEmpty bool) {
		valueCopy := value
		planFields = append(planFields, TranslationField{
			FieldName:  fieldName,
			Index:      idx,
			Text:       strings.TrimSpace(text),
			Preset:     &valueCopy,
			AllowEmpty: allowEmpty,
		})
	}

	actressSourceNames := make([]string, len(scraped.Actresses))
	actressHangulNames := make([]string, len(scraped.Actresses))
	romanizedByJapaneseName := make(map[string]string, len(scraped.Actresses))
	for i, original := range scraped.Actresses {
		actress := original
		CleanStoredActress(&actress)
		if models.IsUnknownActressName(actress.FirstName) || models.IsUnknownActressName(actress.JapaneseName) {
			continue
		}
		if strings.TrimSpace(actress.Reading) != "" {
			// A profile reading is authoritative. Thumbnail slugs can retain an
			// older activity name and must only be used when no reading exists.
			actressSourceNames[i] = strings.TrimSpace(actress.Reading)
		} else if last, first, ok := extractNamesFromDMMActjpgsURL(actress.ThumbURL); ok {
			actressSourceNames[i] = joinRomanizedName(last, first)
		} else if romanized := romanizedActressName(actress); romanized != "" {
			actressSourceNames[i] = romanized
		} else if strings.TrimSpace(actress.JapaneseName) != "" {
			actressSourceNames[i] = cleanActressNameForTranslation(actress.JapaneseName)
		} else {
			actressSourceNames[i] = cleanActressNameForTranslation(actressDisplayTitle(actress))
		}
		if jaName := strings.TrimSpace(actress.JapaneseName); jaName != "" && isLikelyRomanized(actressSourceNames[i]) {
			romanizedByJapaneseName[jaName] = actressSourceNames[i]
		}
		if hangul := hangulActressName(actress); hangul != "" {
			actressHangulNames[i] = hangul
		} else if isLikelyRomanized(actressSourceNames[i]) {
			actressHangulNames[i], _ = romajiToHangul(actressSourceNames[i])
		}
	}

	type nameSub struct {
		japanese string
		hangul   string
		token    string
		pattern  *regexp.Regexp
		length   int
	}
	nameSubs := make([]nameSub, 0, len(scraped.Actresses))
	if targetLang == "ko" {
		for i, actress := range scraped.Actresses {
			japanese := strings.TrimSpace(actress.JapaneseName)
			if japanese == "" || actressHangulNames[i] == "" {
				continue
			}
			pattern := flexibleJapaneseNamePattern(japanese)
			if pattern == nil {
				continue
			}
			nameSubs = append(nameSubs, nameSub{
				japanese: japanese,
				hangul:   actressHangulNames[i],
				token:    fmt.Sprintf("⟦%d⟧", len(nameSubs)),
				pattern:  pattern,
				length:   len([]rune(strings.Join(strings.Fields(japanese), ""))),
			})
		}
		sort.SliceStable(nameSubs, func(i, j int) bool {
			return nameSubs[i].length > nameSubs[j].length
		})
	}
	protectNames := func(value string) (direct, protected string, placeholders map[string]string) {
		direct, protected = value, value
		placeholders = make(map[string]string)
		for _, sub := range nameSubs {
			if !sub.pattern.MatchString(protected) {
				continue
			}
			direct = sub.pattern.ReplaceAllStringFunc(direct, func(string) string { return sub.hangul })
			protected = sub.pattern.ReplaceAllStringFunc(protected, func(string) string { return sub.token })
			placeholders[sub.token] = sub.hangul
		}
		return direct, protected, placeholders
	}

	if fields.Title {
		cleaned := prepareTitleForTranslation(scraped.Title, targetLang)
		direct, protected, placeholders := protectNames(cleaned)
		titleField := "title"
		if romanized := romanizedByJapaneseName[strings.TrimSpace(cleaned)]; romanized != "" {
			titleField = "title_as_name"
			if targetLang != "ko" || len(placeholders) == 0 {
				protected = romanized
			}
		}
		if targetLang == "ko" && len(placeholders) > 0 && !containsTranslatableText(direct) {
			queuePreset(titleField, cleaned, direct, -1, false)
		} else if field := queue(titleField, protected, -1); field != nil && len(placeholders) > 0 {
			field.Placeholders = placeholders
			field.FallbackText = direct
		}
	}
	if fields.OriginalTitle {
		queue("original_title", scraped.OriginalTitle, -1)
	}
	if fields.Description {
		cleaned := cleanDescriptionForTranslation(scraped.Description)
		if cleaned == "" && strings.TrimSpace(scraped.Description) != "" {
			queuePreset("description", scraped.Description, "", -1, true)
		} else {
			direct, protected, placeholders := protectNames(cleaned)
			if field := queue("description", protected, -1); field != nil && len(placeholders) > 0 {
				field.Placeholders = placeholders
				field.FallbackText = direct
			}
		}
	}
	if fields.Director {
		queue("director", scraped.Director, -1)
	}
	if fields.Maker {
		queue("maker", scraped.Maker, -1)
	}
	if fields.Label {
		queue("label", scraped.Label, -1)
	}
	if fields.Series {
		queue("series", scraped.Series, -1)
	}
	if fields.Genres {
		for i := range scraped.Genres {
			queue("genre", scraped.Genres[i].Name, i)
		}
	}
	if fields.Actresses {
		for i := range scraped.Actresses {
			if models.IsUnknownActressName(scraped.Actresses[i].FirstName) || models.IsUnknownActressName(scraped.Actresses[i].JapaneseName) {
				queuePreset("actress", actressDisplayTitle(scraped.Actresses[i]), models.UnknownActressName, i, false)
				continue
			}
			name := actressSourceNames[i]
			if name == "" {
				continue
			}
			if targetLang == "ko" && actressHangulNames[i] != "" {
				queuePreset("actress", name, actressHangulNames[i], i, false)
			} else {
				queue("actress", name, i)
			}
		}
	}

	return TranslationPlan{
		TargetLang:     targetLang,
		SourceLang:     sourceLang,
		SourceLabel:    sourceLabel,
		ApplyToPrimary: s.cfg.ApplyToPrimary,
		Fields:         planFields,
	}
}

// ApplyPlan applies translated results back to the movie and builds translation records.
// It uses the plan's field descriptors (no closures) to route each result to the
// correct movie field and translation record.
func ApplyPlan(scraped *models.Movie, plan TranslationPlan, results TranslationResultMap, translatedRecord *models.MovieTranslation, state *translationState) string {
	var warnings []string
	for _, field := range plan.Fields {
		key := fieldKey(field)
		translated, ok := results[key]
		if !ok || strings.TrimSpace(translated) == "" && !field.AllowEmpty {
			logging.Debugf("Translation: empty result for %s (original=%q), falling back to original", key, field.Text)
			warnings = append(warnings, fmt.Sprintf("%s: empty translation, kept original", key))
			translated = field.Text
		}

		// Route to the correct field on scraped movie and translation record
		applyTranslatedField(scraped, field, translated, translatedRecord, state, plan)
	}

	if len(warnings) > 0 {
		return strings.Join(warnings, "; ")
	}
	return ""
}

// applyTranslatedField routes a translated value to the correct movie field and
// translation record based on the field descriptor.
func applyTranslatedField(scraped *models.Movie, field TranslationField, translated string, translatedRecord *models.MovieTranslation, state *translationState, plan TranslationPlan) {
	if plan.SourceLabel != "" {
		translatedRecord.SourceName = plan.SourceLabel
	}

	switch field.FieldName {
	case "title", "title_as_name":
		translated = finalizeTitleTranslation(field.Text, translated, plan.TargetLang)
		translatedRecord.Title = translated
		if plan.ApplyToPrimary {
			scraped.Title = translated
		}
	case "original_title":
		translatedRecord.OriginalTitle = translated
		if plan.ApplyToPrimary {
			scraped.OriginalTitle = translated
		}
	case "description":
		translatedRecord.Description = translated
		if plan.ApplyToPrimary {
			scraped.Description = translated
		}
	case "director":
		translatedRecord.Director = translated
		if plan.ApplyToPrimary {
			scraped.Director = translated
		}
	case "maker":
		translatedRecord.Maker = translated
		if plan.ApplyToPrimary {
			scraped.Maker = translated
		}
	case "label":
		translatedRecord.Label = translated
		if plan.ApplyToPrimary {
			scraped.Label = translated
		}
	case "series":
		translatedRecord.Series = translated
		if plan.ApplyToPrimary {
			scraped.Series = translated
		}
	case "genre":
		state.genreTranslations = append(state.genreTranslations, models.GenreTranslationData{
			GenreIndex: field.Index,
			Language:   plan.TargetLang,
			Name:       translated,
			SourceName: plan.SourceLabel,
		})
		if plan.ApplyToPrimary {
			scraped.Genres[field.Index].Name = translated
		}
	case "actress":
		translated = restoreActressMiddleDots(field.Text, translated)
		first, last := models.SplitActressName(translated)
		jName := strings.TrimSpace(scraped.Actresses[field.Index].JapaneseName)
		state.actressTranslations = append(state.actressTranslations, models.ActressTranslationData{
			ActressIndex: field.Index,
			Language:     plan.TargetLang,
			FirstName:    first,
			LastName:     last,
			JapaneseName: jName,
			DisplayName:  translated,
			SourceName:   plan.SourceLabel,
		})
		if plan.ApplyToPrimary {
			replaceActressName(&scraped.Actresses[field.Index], translated)
		}
	}
}

// GroupByProvider groups translation fields by their provider for batch dispatch.
// Currently all fields share the same provider, but this seam enables future
// per-field provider routing (e.g., DeepL for titles, LLM for descriptions).
func GroupByProvider(plan TranslationPlan, providerName string) map[string][]TranslationField {
	groups := make(map[string][]TranslationField)
	groups[providerName] = plan.Fields
	return groups
}

// translationState holds mutable state shared between field collection and result application.
type translationState struct {
	genreTranslations   []models.GenreTranslationData
	actressTranslations []models.ActressTranslationData
}

// TranslateMovie translates selected movie metadata fields from source to target language.
// It returns a TranslationOutput carrying the translated record and genre/actress
// translation data, rather than mutating *models.Movie in-place.
func (s *Service) TranslateMovie(ctx context.Context, scraped *models.Movie, settingsHash string) (*TranslationOutput, string, error) {
	if s == nil {
		return nil, "", fmt.Errorf("translation: TranslateMovie called on nil Service")
	}
	if scraped == nil || !s.cfg.Enabled {
		return (*TranslationOutput)(nil), "", nil
	}

	sourceLang := normalizeLanguage(s.cfg.SourceLanguage)
	targetLanguages := s.TargetLanguages()
	if len(targetLanguages) == 0 {
		return (*TranslationOutput)(nil), "", fmt.Errorf("target language is required")
	}
	if sourceLang == "" {
		sourceLang = sourceLangAuto
	}

	sourceLabel := "translation:" + normalizeProvider(s.cfg.Provider)
	original := scraped.Clone()
	output := &TranslationOutput{}
	var warnings []string
	processedTarget := false

	for targetIndex, targetLang := range targetLanguages {
		if sourceLang != sourceLangAuto && sourceLang == targetLang {
			continue
		}
		processedTarget = true
		translatedRecord := models.MovieTranslation{
			Language:     targetLang,
			SourceName:   sourceLabel,
			SettingsHash: settingsHash,
		}

		plan := s.BuildTranslationPlan(original, targetLang, sourceLang, sourceLabel)
		plan.ApplyToPrimary = s.cfg.ApplyToPrimary && targetIndex == 0
		if len(plan.Fields) == 0 {
			continue
		}

		texts := make([]string, 0, len(plan.Fields))
		fieldNames := make([]string, 0, len(plan.Fields))
		providerFields := make([]TranslationField, 0, len(plan.Fields))
		results := make(TranslationResultMap, len(plan.Fields))
		for _, field := range plan.Fields {
			if field.Preset != nil {
				results[fieldKey(field)] = *field.Preset
				continue
			}
			texts = append(texts, field.Text)
			fieldNames = append(fieldNames, fieldKey(field))
			providerFields = append(providerFields, field)
		}

		if len(providerFields) > 0 {
			translatedTexts, err := s.translateTexts(ctx, sourceLang, targetLang, texts, fieldNames)
			if err != nil {
				logging.Debugf("Translation: translateTexts failed for %s: %v", targetLang, err)
				warning := sanitizeTranslationWarning(normalizeProvider(s.cfg.Provider), err)
				return nil, warning, err
			}
			if len(translatedTexts) != len(providerFields) {
				return nil, "", fmt.Errorf("translation provider returned %d items for %d inputs", len(translatedTexts), len(providerFields))
			}
			translatedTexts, qualityWarnings := s.retryLowQualitySlots(ctx, sourceLang, targetLang, providerFields, translatedTexts)
			warnings = append(warnings, qualityWarnings...)
			for i, field := range providerFields {
				translated := strings.TrimSpace(translatedTexts[i])
				if isTitleTranslationField(field.FieldName) {
					if cleaned := cleanTitleForTranslation(translated); cleaned != "" {
						translated = cleaned
					}
				}
				if len(field.Placeholders) > 0 {
					restored, ok := restoreNamePlaceholders(translated, field.Placeholders)
					if !ok {
						restored = field.FallbackText
					}
					translated = restored
				}
				results[fieldKey(field)] = translated
			}
		}
		state := &translationState{}
		if warningDetail := ApplyPlan(scraped, plan, results, &translatedRecord, state); warningDetail != "" {
			warnings = append(warnings, targetLang+": "+warningDetail)
		}
		if len(state.actressTranslations) > 0 {
			translatedRecord.Actresses = make([]string, len(original.Actresses))
			for _, actress := range state.actressTranslations {
				if actress.ActressIndex >= 0 && actress.ActressIndex < len(translatedRecord.Actresses) {
					translatedRecord.Actresses[actress.ActressIndex] = actress.DisplayName
				}
			}
		}
		output.Movies = append(output.Movies, translatedRecord)
		output.GenreTranslations = append(output.GenreTranslations, state.genreTranslations...)
		output.ActressTranslations = append(output.ActressTranslations, state.actressTranslations...)
	}

	if len(output.Movies) == 0 {
		if processedTarget {
			return output, "", nil
		}
		return nil, "", nil
	}
	output.Movie = &output.Movies[0]
	if len(warnings) == 0 {
		return output, "", nil
	}
	warning := fmt.Sprintf("Translation (%s): %s", normalizeProvider(s.cfg.Provider), strings.Join(warnings, "; "))
	logging.Warnf("Translation: %s", warning)
	return output, warning, nil
}

// QualityReviewField pairs a Japanese source field with its first-pass Korean translation.
type QualityReviewField struct {
	FieldName string
	Source    string
	Candidate string
}

// ReviewJAVTranslations performs a mandatory second LLM pass over first-pass JAV translations.
// It is intentionally limited to chat-based OpenAI providers because the review prompt needs
// the original and candidate text as distinct inputs.
func (s *Service) ReviewJAVTranslations(ctx context.Context, fields []QualityReviewField) ([]string, error) {
	if s == nil {
		return nil, fmt.Errorf("translation: ReviewJAVTranslations called on nil Service")
	}
	if len(fields) == 0 {
		return nil, nil
	}
	providerName := normalizeProvider(s.cfg.Provider)
	if providerName != "openai" && providerName != "openai-compatible" {
		return nil, fmt.Errorf("quality review requires an OpenAI chat provider, got %s", providerName)
	}
	provider, ok := s.providers[providerName]
	if !ok {
		return nil, fmt.Errorf("unsupported translation provider: %s", s.cfg.Provider)
	}
	markers := make([]string, len(fields))
	texts := make([]string, len(fields))
	items := make([]qualityReviewItem, len(fields))
	for i, field := range fields {
		name := strings.TrimSpace(field.FieldName)
		if name == "" {
			name = fmt.Sprintf("quality_review_%d", i)
		}
		markers[i] = name
		texts[i] = field.Candidate
		items[i] = qualityReviewItem{Source: field.Source, Candidate: field.Candidate}
	}
	targetLanguages := s.TargetLanguages()
	if len(targetLanguages) == 0 {
		return nil, fmt.Errorf("target language is required")
	}
	reviewCtx := withQualityReview(withTranslationMarkers(ctx, markers), items)
	reviewed, err := s.translateWithProvider(reviewCtx, provider, sourceLangAuto, targetLanguages[0], texts)
	if err != nil {
		if len(fields) > 1 && shouldSplitLLMRequest(err) {
			result := make([]string, 0, len(fields))
			for _, field := range fields {
				one, oneErr := s.ReviewJAVTranslations(ctx, []QualityReviewField{field})
				if oneErr != nil {
					return nil, oneErr
				}
				result = append(result, one[0])
			}
			return result, nil
		}
		return nil, err
	}
	for i := range reviewed {
		reviewed[i] = sanitizeQualityReviewText(reviewed[i])
		if isInvalidQualityReviewText(reviewed[i]) {
			logging.Warnf("Translation: quality reviewer returned contaminated output for %s; keeping first-pass candidate", markers[i])
			reviewed[i] = strings.TrimSpace(fields[i].Candidate)
		}
	}
	return reviewed, nil
}

func sanitizeQualityReviewText(value string) string {
	value = strings.TrimSpace(value)
	for _, prefix := range []string{"[corrected Korean]", "[corrected korean]", "[교정된 한국어]", "[translation]"} {
		if strings.HasPrefix(value, prefix) {
			return strings.TrimSpace(strings.TrimPrefix(value, prefix))
		}
	}
	return value
}

func isInvalidQualityReviewText(value string) bool {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" || containsResidualJapanese(trimmed) {
		return true
	}
	lower := strings.ToLower(trimmed)
	for _, marker := range []string{"[japanese source]", "[korean candidate]", "translate each labeled section", "review and, where necessary"} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

func isTitleTranslationField(fieldName string) bool {
	return fieldName == "title" || fieldName == "title_as_name"
}

func (s *Service) retryLowQualitySlots(ctx context.Context, sourceLang, targetLang string, fields []TranslationField, translated []string) ([]string, []string) {
	if normalizeLanguage(targetLang) == "ja" {
		return translated, nil
	}
	result := append([]string(nil), translated...)
	var warnings []string
	for i, field := range fields {
		current := strings.TrimSpace(result[i])
		if current == "" {
			continue
		}
		personNeedsHangul := targetLang == "ko" && isPersonNameField(fieldKey(field)) && !containsHangul(current)
		textHasJapanese := !isPersonNameField(fieldKey(field)) && containsResidualJapanese(current)
		if !personNeedsHangul && !textHasJapanese {
			continue
		}

		retried, err := s.translateTexts(ctx, sourceLang, targetLang, []string{field.Text}, []string{fieldKey(field)})
		retryValue := ""
		if err == nil && len(retried) == 1 {
			retryValue = strings.TrimSpace(retried[0])
		}
		if personNeedsHangul {
			if containsHangul(retryValue) {
				result[i] = retryValue
				continue
			}
			result[i] = field.Text
			warnings = append(warnings, fmt.Sprintf("%s: LLM returned non-Hangul, kept source name", fieldKey(field)))
			continue
		}

		if retryValue != "" && !containsResidualJapanese(retryValue) {
			result[i] = retryValue
			continue
		}
		if retryValue != "" && countResidualJapanese(retryValue) < countResidualJapanese(current) {
			result[i] = retryValue
		}
		warnings = append(warnings, fmt.Sprintf("%s: LLM left untranslated Japanese, kept best partial", fieldKey(field)))
	}
	return result, warnings
}

// TargetLanguages returns normalized targets in configured order, removing blanks and duplicates.
func (s *Service) TargetLanguages() []string {
	if s == nil {
		return nil
	}
	raw := s.cfg.TargetLanguages
	if len(raw) == 0 {
		raw = []string{s.cfg.TargetLanguage}
	}
	seen := make(map[string]struct{}, len(raw))
	targets := make([]string, 0, len(raw))
	for _, value := range raw {
		lang := normalizeLanguage(value)
		if lang == "" {
			continue
		}
		if _, exists := seen[lang]; exists {
			continue
		}
		seen[lang] = struct{}{}
		targets = append(targets, lang)
	}
	return targets
}

// TranslateTitles translates a list of candidate titles as semantic title
// slots.  It is used by re-scrape candidate previews.
func (s *Service) TranslateTitles(ctx context.Context, titles []string) ([]string, error) {
	result := append([]string(nil), titles...)
	if s == nil || !s.cfg.Enabled || len(titles) == 0 {
		return result, nil
	}
	targets := s.TargetLanguages()
	if len(targets) == 0 {
		return result, nil
	}
	source := normalizeLanguage(s.cfg.SourceLanguage)
	if source == "" {
		source = sourceLangAuto
	}
	if source != sourceLangAuto && source == targets[0] {
		return result, nil
	}
	fields := make([]string, len(titles))
	prepared := make([]string, len(titles))
	for i := range fields {
		fields[i] = fmt.Sprintf("title[%d]", i)
		prepared[i] = prepareTitleForTranslation(titles[i], targets[0])
	}
	translated, err := s.translateTexts(ctx, source, targets[0], prepared, fields)
	if err != nil {
		return nil, err
	}
	for i := range translated {
		translated[i] = finalizeTitleTranslation(prepared[i], cleanTitleForTranslation(translated[i]), targets[0])
	}
	return translated, nil
}

// TranslateActresses runs the movie translation pipeline for actress names
// only and returns the per-language movie records used by sync persistence.
func (s *Service) TranslateActresses(ctx context.Context, actresses []models.Actress, settingsHash string) ([]models.Actress, []models.MovieTranslation, string, error) {
	cloned := append([]models.Actress(nil), actresses...)
	movie := &models.Movie{ID: "actress-sync", Actresses: cloned}
	output, warning, err := s.TranslateMovie(ctx, movie, settingsHash)
	if output == nil {
		return movie.Actresses, nil, warning, err
	}
	return movie.Actresses, output.Movies, warning, err
}

func normalizeProvider(provider string) string {
	return strings.ToLower(strings.TrimSpace(provider))
}

func sanitizeTranslationWarning(provider string, err error) string {
	var te *translationError
	if errors.As(err, &te) && te.Kind == TranslationErrorHTTPStatus {
		logging.Warnf("Translation (%s): HTTP %d error", provider, te.StatusCode)
		switch {
		case te.StatusCode == 429:
			return "Translation failed: rate limited, try again later"
		case te.StatusCode == 401:
			return "Translation failed: unauthorized, check API key"
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
	return strings.TrimSpace(strings.TrimSpace(actress.LastName) + " " + strings.TrimSpace(actress.FirstName))
}

// replaceActressName updates an actress's name fields with the translated string.
// JapaneseName remains the authoritative source identity. Translated names are
// split from Japanese display order (family name, given name) into LastName and
// FirstName so downstream formatting can honor output.first_name_order.
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
	if (containsHangul(actress.FirstName) || containsHangul(actress.LastName)) && !containsHangul(translated) {
		return
	}
	if idx := strings.IndexAny(translated, "([（"); idx >= 0 {
		translated = strings.TrimSpace(translated[:idx])
	}
	if translated == "" {
		return
	}
	if isLikelyRomanized(translated) {
		translated = normalizeRomanizationToASCII(translated)
	} else if !containsHangul(translated) {
		return
	}
	actress.FirstName, actress.LastName = models.SplitActressName(translated)
}

const maxTranslationRetries = 3

// translationResult holds translated texts and optional raw LLM output returned by a TranslatorProvider.
type translationResult struct {
	Texts  []string
	RawLLM string
}

func (s *Service) translateTexts(ctx context.Context, sourceLang, targetLang string, texts []string, fieldNameSets ...[]string) ([]string, error) {
	providerName := normalizeProvider(s.cfg.Provider)
	provider, ok := s.providers[providerName]
	if !ok {
		return nil, fmt.Errorf("unsupported translation provider: %s", s.cfg.Provider)
	}
	var fieldNames []string
	if len(fieldNameSets) > 0 {
		fieldNames = fieldNameSets[0]
	}
	if len(fieldNames) != len(texts) {
		fieldNames = make([]string, len(texts))
		for i := range fieldNames {
			fieldNames[i] = fmt.Sprintf("JZ_%d", i)
		}
	}
	translated, err := s.translateWithProvider(withTranslationMarkers(ctx, fieldNames), provider, sourceLang, targetLang, texts)
	if err != nil {
		var translationErr *translationError
		if len(texts) > 1 && ((errors.As(err, &translationErr) && translationErr.Kind == TranslationErrorParse) || shouldSplitLLMRequest(err)) {
			return s.translateTextsOneByOne(ctx, sourceLang, targetLang, texts, fieldNames)
		}
		return nil, err
	}
	if len(texts) > 1 && hasSlotWordCountAnomaly(texts, translated) {
		logging.Debugf("Translation: slot word count anomaly detected, falling back to one-by-one")
		return s.translateTextsOneByOne(ctx, sourceLang, targetLang, texts, fieldNames)
	}
	return translated, nil
}

func shouldSplitLLMRequest(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "peg-gemma4") || strings.Contains(message, "channel error") || strings.Contains(message, "context size has been exceeded")
}

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

func (s *Service) translateTextsOneByOne(ctx context.Context, sourceLang, targetLang string, texts, fieldNames []string) ([]string, error) {
	results := make([]string, len(texts))
	for i, input := range texts {
		name := fmt.Sprintf("JZ_%d", i)
		if i < len(fieldNames) {
			name = fieldNames[i]
		}
		translated, err := s.translateTexts(ctx, sourceLang, targetLang, []string{input}, []string{name})
		if err != nil {
			return nil, err
		}
		results[i] = translated[0]
	}
	return results, nil
}

func (s *Service) translateWithProvider(ctx context.Context, provider TranslatorProvider, sourceLang, targetLang string, texts []string) ([]string, error) {
	var lastResult *translationResult
	var lastErr error
	expectedCount := len(texts)

	for attempt := 1; attempt <= maxTranslationRetries; attempt++ {
		if s.acquireProviderCall != nil {
			if err := s.acquireProviderCall(ctx); err != nil {
				return nil, err
			}
		}
		result, err := provider.Translate(ctx, sourceLang, targetLang, texts)
		if s.releaseProviderCall != nil {
			s.releaseProviderCall()
		}

		if err == nil {
			if result == nil {
				err = &translationError{Kind: TranslationErrorProvider, Message: "translation provider returned no result"}
			} else if len(result.Texts) != expectedCount {
				err = &translationError{
					Kind:    TranslationErrorCountMismatch,
					Message: fmt.Sprintf("translation provider returned %d items for %d inputs", len(result.Texts), expectedCount),
				}
			}
		}

		if err == nil && result != nil {
			return result.Texts, nil
		}

		lastResult = result
		lastErr = err

		if attempt < maxTranslationRetries {
			if isRetryableError(err, result) {
				logging.Debugf("Translation: attempt %d/%d failed (%v), retrying...", attempt, maxTranslationRetries, err)
				expBackoff := float64(time.Millisecond) * 100 * math.Pow(2, float64(attempt-1))
				if expBackoff > float64(2*time.Second) {
					expBackoff = float64(2 * time.Second)
				}
				s.randMu.Lock()
				sleep := time.Duration(s.rand.Float64() * expBackoff) //nolint:gosec // non-crypto rand is fine for retry jitter
				s.randMu.Unlock()
				select {
				case <-ctx.Done():
					return nil, ctx.Err()
				case <-time.After(sleep):
				}
			} else {
				logging.Debugf("Translation: attempt %d/%d failed with non-retryable error (%v), giving up", attempt, maxTranslationRetries, err)
				break
			}
		}
	}

	if lastResult != nil && lastResult.RawLLM != "" {
		logging.Debugf("Translation: all %d attempts failed. Last LLM output (length=%d):\n%s", maxTranslationRetries, len(lastResult.RawLLM), lastResult.RawLLM)
	}

	if lastErr != nil {
		return nil, lastErr
	}
	return nil, &translationError{
		Kind:    TranslationErrorProvider,
		Message: fmt.Sprintf("translation failed after %d attempts", maxTranslationRetries),
	}
}

func isRetryableError(err error, result *translationResult) bool {
	if err == nil {
		return result != nil && len(result.Texts) == 0 && result.RawLLM != ""
	}

	var te *translationError
	if errors.As(err, &te) {
		switch te.Kind {
		case TranslationErrorCountMismatch, TranslationErrorParse:
			return result != nil && result.RawLLM != ""
		case TranslationErrorHTTPStatus:
			return isModelOutputFormatError(te)
		default:
			return false
		}
	}

	return false
}
