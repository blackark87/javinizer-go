package translation

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/javinizer/javinizer-go/internal/httpclient"
	"github.com/javinizer/javinizer-go/internal/logging"
)

// openAIChatRequest represents a chat completion request for OpenAI-compatible APIs.
type openAIChatRequest struct {
	Model              string              `json:"model"`
	Temperature        float64             `json:"temperature"`
	MaxTokens          int                 `json:"max_tokens,omitempty"`
	Messages           []openAIChatMessage `json:"messages"`
	ChatTemplateKwargs map[string]any      `json:"chat_template_kwargs,omitempty"`
	ReasoningEffort    string              `json:"reasoning_effort,omitempty"`
	EnableThinking     *bool               `json:"enable_thinking,omitempty"`
}

// openAIChatMessage represents a single message in a chat request.
type openAIChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// openAIChatResponse represents a chat completion response from OpenAI-compatible APIs.
type openAIChatResponse struct {
	Choices []struct {
		Message struct {
			Content json.RawMessage `json:"content"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
}

// openAIChatCallOptions configures an OpenAI-compatible chat translation call.
type openAIChatCallOptions struct {
	provider  string
	baseURL   string
	endpoint  string
	model     string
	headers   map[string]string
	request   openAIChatRequest
	textCount int
	markers   []string
	logInput  bool
	logTiming bool
}

type translationMarkersContextKey struct{}

type qualityReviewContextKey struct{}

type translationCorrectionContextKey struct{}

type dictionaryPromptModeContextKey struct{}

type qualityReviewItem struct {
	Source    string
	Candidate string
}

type translationCorrection struct {
	Issue     string
	Candidate string
}

const llmCompletionMarker = "<<<JZ_DONE>>>"

const defaultLLMRequestTimeout = 120 * time.Second

var jac024TailPromptPattern = regexp.MustCompile(`極選エロギャル([0-9０-９]+|⟦[0-9]+⟧)名([0-9０-９]+|⟦[0-9]+⟧)分`)

type llmPromptOptions struct {
	dictionaryEnabled bool
	dictionary        string
	correction        *translationCorrection
}

func promptOptionsFromConfig(cfg Config) llmPromptOptions {
	return llmPromptOptions{
		dictionaryEnabled: cfg.DictionaryEnabled,
		dictionary:        cfg.Dictionary,
	}
}

func promptOptionsFromContext(ctx context.Context, cfg Config) llmPromptOptions {
	options := promptOptionsFromConfig(cfg)
	if dictionaryEnabled, ok := dictionaryPromptModeFromContext(ctx); ok {
		options.dictionaryEnabled = dictionaryEnabled
	}
	if correction, ok := translationCorrectionFromContext(ctx); ok {
		options.correction = &correction
	}
	return options
}

// llmRequestContext gives each outbound LLM request its own timeout budget.
// The parent context still propagates caller cancellation, but time spent by a
// previous request, retry, or thinking-strategy fallback is never deducted from
// the next request's configured timeout.
func llmRequestContext(parent context.Context, timeoutSeconds int) (context.Context, context.CancelFunc) {
	if parent == nil {
		parent = context.Background()
	}
	timeout := time.Duration(timeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = defaultLLMRequestTimeout
	}
	return context.WithTimeout(parent, timeout)
}

func withQualityReview(ctx context.Context, items []qualityReviewItem) context.Context {
	return context.WithValue(ctx, qualityReviewContextKey{}, append([]qualityReviewItem(nil), items...))
}

func withTranslationCorrection(ctx context.Context, issue, candidate string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, translationCorrectionContextKey{}, translationCorrection{
		Issue:     strings.TrimSpace(issue),
		Candidate: strings.TrimSpace(candidate),
	})
}

func translationCorrectionFromContext(ctx context.Context) (translationCorrection, bool) {
	if ctx == nil {
		return translationCorrection{}, false
	}
	correction, ok := ctx.Value(translationCorrectionContextKey{}).(translationCorrection)
	return correction, ok && correction.Issue != ""
}

func withDictionaryPromptMode(ctx context.Context, enabled bool) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, dictionaryPromptModeContextKey{}, enabled)
}

func dictionaryPromptModeFromContext(ctx context.Context) (bool, bool) {
	if ctx == nil {
		return false, false
	}
	enabled, ok := ctx.Value(dictionaryPromptModeContextKey{}).(bool)
	return enabled, ok
}

func qualityReviewFromContext(ctx context.Context, count int) ([]qualityReviewItem, bool) {
	if ctx == nil {
		return nil, false
	}
	items, ok := ctx.Value(qualityReviewContextKey{}).([]qualityReviewItem)
	return items, ok && len(items) == count
}

func withTranslationMarkers(ctx context.Context, fieldNames []string) context.Context {
	markers := make([]string, len(fieldNames))
	for i, fieldName := range fieldNames {
		fieldName = strings.TrimSpace(fieldName)
		if fieldName == "" {
			fieldName = fmt.Sprintf("JZ_%d", i)
		}
		markers[i] = "<<<" + fieldName + ">>>"
	}
	return context.WithValue(ctx, translationMarkersContextKey{}, markers)
}

func translationMarkersFromContext(ctx context.Context, count int) []string {
	if ctx != nil {
		if markers, ok := ctx.Value(translationMarkersContextKey{}).([]string); ok && len(markers) == count {
			return append([]string(nil), markers...)
		}
	}
	return indexedTranslationMarkers(count)
}

// LLMChatAdapter abstracts the provider-specific request/response format for LLM
// chat-based translation. OpenAI and Anthropic implement this interface so that
// the shared executeLLMChatTranslation pipeline can be reused across providers.
type LLMChatAdapter interface {
	// BuildRequest constructs the provider-specific HTTP request for chat translation.
	BuildRequest(ctx context.Context, baseURL, model string, systemPrompt, userPrompt string, textCount int) (*http.Request, error)
	// DecodeResponse parses the provider-specific HTTP response body into a translationResult.
	DecodeResponse(providerName string, respBody []byte, textCount int) (*translationResult, error)
}

func buildLLMTranslationPromptsWithMarkers(sourceLang, targetLang string, texts, markers []string, options ...llmPromptOptions) (string, string, error) {
	if len(texts) == 0 || len(markers) != len(texts) {
		return "", "", fmt.Errorf("translation prompt requires one marker per text (%d markers for %d texts)", len(markers), len(texts))
	}

	terminologyRules := "Translate every meaningful source segment. Translate language/idioms/compounds/sounds by meaning and use established current target-language JAV terminology. Transliterate only person or brand names, opaque proper nouns, and genuine industry loanwords; leave no Japanese script in non-Japanese output except protected name punctuation. "
	personNameRule := "Person-name rule: <<<actress[N]>>> and <<<title_as_name>>> contain one performer. Transliterate the reading in Japanese FamilyName GivenName order; romaji is authoritative. Never invent, anglicize, or substitute a different performer name; never shorten or translate it or turn kanji into emoji. Preserve the middle dot ・ inside one name, never turn it into a comma, and never split one performer. Apply this rule to a short name-like <<<title>>> too. "
	properNounRule := "Proper-noun rule: <<<maker>>>, <<<label>>>, and <<<director>>> are names. Transliterate them phonetically and do not embellish them. "
	cleanupRules := "Title cleanup: remove bracketed VR/release labels such as [VR], 【VR】, and 【8K VR】, plus trailing source-site, streaming-platform, or store attribution accidentally scraped into the title. Description cleanup: remove a leading release-date/runtime metadata prefix and its adjacent content ID, playback/device notices, VR-only notices, platform notices, sales campaigns, and store promotions; if only excluded material remains, return an empty section. "
	placeholderRule := "Any Hangul already present is final and must be copied verbatim. Protected tokens of the form ⟦N⟧ must be reproduced exactly and never translated, removed, or renumbered. "
	promptOptions := resolveLLMPromptOptions(options)
	koreanRules := koreanJAVPromptRules(targetLang, promptOptions)

	correctionRule := ""
	if promptOptions.correction != nil {
		correctionRule = fmt.Sprintf(
			"CORRECTION RETRY: The previous answer was rejected because %s. Correct only that failure while preserving every valid translated detail. Return exactly the requested marker set once, in the requested order, followed by %s. Never emit an unrequested marker or repeat a section. ",
			promptOptions.correction.Issue,
			llmCompletionMarker,
		)
	}
	systemPrompt := fmt.Sprintf("You translate Japanese adult video (JAV) metadata and AV-studio metadata for actual studio use. Write concise contemporary titles and complete natural descriptions; avoid corny, dated, literary, moralizing, euphemistic, or invented wording. %s%s%s%s%s%s%sReturn marker+one-line translation for each, then exact final line %s; no JSON or commentary. Source: %s. Target: %s.", terminologyRules, koreanRules, personNameRule, properNounRule, cleanupRules, placeholderRule, correctionRule, llmCompletionMarker, sourceLang, targetLang)

	var userPrompt strings.Builder
	if promptOptions.correction == nil {
		userPrompt.WriteString("Translate each labeled section below:\n")
	} else {
		userPrompt.WriteString("Correct the rejected translation using the original source below.\n")
		userPrompt.WriteString("[VALIDATION FAILURE]\n")
		userPrompt.WriteString(promptOptions.correction.Issue)
		userPrompt.WriteByte('\n')
		if promptOptions.correction.Candidate != "" {
			userPrompt.WriteString("[REJECTED KOREAN CANDIDATE]\n")
			userPrompt.WriteString(promptOptions.correction.Candidate)
			userPrompt.WriteByte('\n')
		}
		userPrompt.WriteString("[ORIGINAL SOURCE]\n")
	}
	if !promptOptions.dictionaryEnabled {
		userPrompt.WriteString(koreanBatchPromptConstraints(targetLang, texts))
	}
	for i, text := range texts {
		userPrompt.WriteString(markers[i])
		userPrompt.WriteByte('\n')
		userPrompt.WriteString(text)
		userPrompt.WriteByte('\n')
	}
	return systemPrompt, strings.TrimSpace(userPrompt.String()), nil
}

func buildLLMQualityReviewPromptsWithMarkers(targetLang string, items []qualityReviewItem, markers []string, options ...llmPromptOptions) (string, string, error) {
	if len(items) == 0 || len(markers) != len(items) {
		return "", "", fmt.Errorf("quality review prompt requires one marker per item (%d markers for %d items)", len(markers), len(items))
	}
	promptOptions := resolveLLMPromptOptions(options)
	correctionRule := ""
	if promptOptions.correction != nil {
		correctionRule = fmt.Sprintf(
			"CORRECTION RETRY: The previous quality-review answer was rejected because %s. Correct only that failure. Return each requested marker exactly once, in order, followed by %s. Never repeat the source, candidate, or a completed section. ",
			promptOptions.correction.Issue,
			llmCompletionMarker,
		)
	}
	systemPrompt := "You are the mandatory second-pass quality reviewer for Japanese AV metadata translated into Korean. Judge every source/candidate pair afresh; never carry forward a prior approval, exclusion, ignore decision, or recommendation. The Korean candidate is the baseline. If it is already acceptable within normal contemporary Korean variation, return it unchanged. Otherwise make only the smallest local edits needed to fix material mistranslation, calques, untranslated or transliterated slang, omissions, inventions, broken text, awkward grammar, or outdated terminology. Never rewrite an unaffected phrase merely to prefer a synonym, different word order, or different style. A review must not introduce a typo, damage a correct Korean spelling, or make the result less fluent than the candidate. Preserve explicitness, tone, protected tokens, punctuation, bracket placement, and performer identity. Do not restore omitted release tags, playback/device notices, or sales/store promotions. Return the complete corrected Korean text, not an assessment. " + koreanJAVPromptRules(targetLang, promptOptions) + correctionRule + "Copy every <<<quality_review_...>>> marker with its complete corrected Korean text, then exact final line " + llmCompletionMarker + ". Never echo source/candidate labels or add commentary."

	var userPrompt strings.Builder
	if promptOptions.correction == nil {
		userPrompt.WriteString("Review and, where necessary, rewrite each candidate by comparing it with its Japanese source:\n")
	} else {
		userPrompt.WriteString("Correct the rejected quality-review output using the same source and candidate below.\n")
		userPrompt.WriteString("[VALIDATION FAILURE]\n")
		userPrompt.WriteString(promptOptions.correction.Issue)
		userPrompt.WriteByte('\n')
	}
	sources := make([]string, len(items))
	for i := range items {
		sources[i] = items[i].Source
	}
	if !promptOptions.dictionaryEnabled {
		userPrompt.WriteString(koreanBatchPromptConstraints(targetLang, sources))
	}
	for i, item := range items {
		userPrompt.WriteString(markers[i])
		userPrompt.WriteString("\n[JAPANESE SOURCE]\n")
		userPrompt.WriteString(item.Source)
		userPrompt.WriteString("\n[KOREAN CANDIDATE]\n")
		userPrompt.WriteString(item.Candidate)
		userPrompt.WriteByte('\n')
	}
	return systemPrompt, strings.TrimSpace(userPrompt.String()), nil
}

func koreanBatchPromptConstraints(targetLang string, sources []string) string {
	lang := strings.ToLower(strings.TrimSpace(targetLang))
	if lang != "ko" && !strings.HasPrefix(lang, "ko-") && !strings.HasPrefix(lang, "ko_") {
		return ""
	}
	var constraints strings.Builder
	hasDirectionCheck := false
	hasOppaiManko := false
	hasOppaiMankoPhrase := false
	hasReverseBunnyBusiness := false
	hasEighteenWorks := false
	hasWarikiri := false
	hasHiddenYenRecruitment := false
	hasWarikiriHiddenYenRecruitment := false
	hasBlackHairSujiPaipan := false
	hasSujiPaipan := false
	hasGyarushibeChoja := false
	hasIntroductionChain := false
	hasGokusen := false
	hasCleanupFellatio := false
	hasBeakerReinjection := false
	hasPetiteMankoPierced := false
	hasGaasyy := false
	hasNishiAzabu := false
	hasKnuckleHandjob := false
	hasKnuckleHandjobKariAttack := false
	jac024TailRule := ""
	for _, source := range sources {
		if strings.Contains(source, "逆レ") ||
			strings.Contains(source, "レイプ") ||
			strings.Contains(source, "レ×プ") ||
			strings.Contains(source, "レ〇プ") ||
			strings.Contains(source, "レ○プ") ||
			strings.Contains(source, "レ●プ") {
			hasDirectionCheck = true
		}
		if strings.Contains(source, "おっぱいマンコ") {
			hasOppaiManko = true
		}
		if strings.Contains(source, "もはや性器のおっぱいマンコが気持ち良すぎる") {
			hasOppaiMankoPhrase = true
		}
		if strings.Contains(source, "逆バニー風俗") {
			hasReverseBunnyBusiness = true
		}
		if strings.Contains(source, "18作品") {
			hasEighteenWorks = true
		}
		if strings.Contains(source, "ワリキリ") || strings.Contains(source, "割り切り") {
			hasWarikiri = true
		}
		if strings.Contains(source, "裏￥募集") {
			hasHiddenYenRecruitment = true
		}
		if strings.Contains(source, "ワリキリ裏￥募集") {
			hasWarikiriHiddenYenRecruitment = true
		}
		if strings.Contains(source, "黒髪清楚系スジパイパン") {
			hasBlackHairSujiPaipan = true
		}
		if strings.Contains(source, "スジパイパン") {
			hasSujiPaipan = true
		}
		if strings.Contains(source, "ギャルしべ長者") {
			hasGyarushibeChoja = true
		}
		if strings.Contains(source, "数珠つなぎ") && strings.Contains(source, "紹介") {
			hasIntroductionChain = true
		}
		if strings.Contains(source, "極選") {
			hasGokusen = true
		}
		if strings.Contains(source, "お掃除フェラ") {
			hasCleanupFellatio = true
		}
		if strings.Contains(source, "何度も中出しからビーカーで再注入後お掃除フェラ") {
			hasBeakerReinjection = true
		}
		if strings.Contains(source, "小柄マンコ貫かれ") {
			hasPetiteMankoPierced = true
		}
		if strings.Contains(source, "ガーシー") {
			hasGaasyy = true
		}
		if strings.Contains(source, "西麻布") {
			hasNishiAzabu = true
		}
		if strings.Contains(source, "ナックル手コキ") {
			hasKnuckleHandjob = true
		}
		if strings.Contains(source, "《ナックル手コキ》でカリ集中責め") {
			hasKnuckleHandjobKariAttack = true
		}
		if match := jac024TailPromptPattern.FindStringSubmatch(source); len(match) == 3 {
			jac024TailRule = fmt.Sprintf(
				"HIGHEST PRIORITY exact tail: %s→엄선한 야한 갸루 %s명, %s분; copy the spacing and comma exactly",
				match[0],
				match[1],
				match[2],
			)
		}
	}
	if hasDirectionCheck {
		constraints.WriteString("BATCH DIRECTION CHECK: In each Japanese source, レイプ/レ×プ/レ〇プ/レ○プ/レ●プ without an immediately preceding 逆 must be 강간, never 역강간. Correct a wrong 역강간 in the candidate. Only an explicit 逆レ/逆レイプ may be 역강간.\n")
	}
	var termChecks []string
	if hasReverseBunnyBusiness {
		termChecks = append(termChecks, "逆バニー風俗→역바니 코스튬 업소")
	}
	if hasEighteenWorks {
		termChecks = append(termChecks, "18作品→열여덟 작품")
	}
	if hasOppaiMankoPhrase {
		termChecks = append(termChecks, "もはや性器のおっぱいマンコが気持ち良すぎる！→이젠 보지나 다름없는 가슴이 너무 기분 좋다!")
	} else if hasOppaiManko {
		termChecks = append(termChecks, "おっぱいマンコ→보지나 다름없는 가슴")
	}
	if hasOppaiManko {
		termChecks = append(termChecks, "never append the Japanese original in parentheses or insert Latin fragments")
	}
	if hasWarikiriHiddenYenRecruitment {
		termChecks = append(termChecks, "ワリキリ裏￥募集→비밀 조건만남 모집, 금지: 와리키리/뒷 ￥/뒷돈 모집/조건만남 비밀 조건만남 모집")
	} else {
		if hasWarikiri {
			termChecks = append(termChecks, "ワリキリ/割り切り→조건만남, 금지: 와리키리")
		}
		if hasHiddenYenRecruitment {
			termChecks = append(termChecks, "裏￥募集→비밀 조건만남 모집, 금지: 뒷 ￥/뒷돈 모집")
		}
	}
	if hasBlackHairSujiPaipan {
		termChecks = append(termChecks, "HIGHEST PRIORITY exact phrase: 黒髪清楚系スジパイパン→흑발 청순녀의 선명한 백보지; copy this Korean phrase verbatim in both translation and review, 금지: 흑발 청순계 백보지/검은 머리 청순계 스지 백보지/머리 청순계/스지 백보지")
	} else if hasSujiPaipan {
		termChecks = append(termChecks, "スジパイパン→보지 라인이 선명한 백보지|선명한 백보지, 금지: 스지 백보지")
	}
	if hasGyarushibeChoja {
		termChecks = append(termChecks, "ギャルしべ長者→갸루 소개 릴레이 (わらしべ長者를 비튼 연쇄 소개 기획명), 금지: 갸루시베 장자/갸루 시베초자/갸루시베초자")
	}
	if hasIntroductionChain {
		termChecks = append(termChecks, "소개 문맥 数珠つなぎ→줄줄이 소개|연쇄 소개, 금지: 구슬/염주/릴레이 소개")
	}
	if hasBeakerReinjection {
		termChecks = append(termChecks, "HIGHEST PRIORITY exact phrase: 何度も中出しからビーカーで再注入後お掃除フェラ→몇 번이나 질내사정한 뒤 비커로 정액을 다시 주입하고 마무리 펠라; omit no object, 금지: 비커로 재주입/청소하는 펠라")
	} else if hasCleanupFellatio {
		termChecks = append(termChecks, "お掃除フェラ→마무리 펠라, 금지: 청소 펠라/청소하는 펠라")
	}
	if hasPetiteMankoPierced {
		termChecks = append(termChecks, "HIGHEST PRIORITY exact phrase: 小柄マンコ貫かれ→아담한 그녀의 보지가 꿰뚫리고; copy verbatim, 금지: 작은 보지/아담한 보지가")
	}
	if hasGaasyy {
		termChecks = append(termChecks, "ガーシー→가십, 금지: 가시/Garsy/GaaSyy/원문에 없는 라틴 별칭 병기")
	}
	if hasNishiAzabu {
		termChecks = append(termChecks, "西麻布→니시아자부, 금지: 니시아부/니시아부파")
	}
	if hasKnuckleHandjobKariAttack {
		termChecks = append(termChecks, "HIGHEST PRIORITY exact phrase: 《ナックル手コキ》でカリ集中責め→《손가락 대딸》로 귀두 테두리 집중 공략; copy verbatim, 금지: 너클 대딸/손가락 마디를 이용한 대딸/클리 집중 공략")
	} else if hasKnuckleHandjob {
		termChecks = append(termChecks, "ナックル手コキ→손가락 대딸, 금지: 너클 대딸/손가락 마디를 이용한 대딸")
	}
	termChecks = append(termChecks, koreanTranslationReviewTermChecks(sources)...)
	if jac024TailRule != "" {
		termChecks = append(termChecks, jac024TailRule)
	} else if hasGokusen {
		termChecks = append(termChecks, "極選→엄선, 금지: 극선/엄선한 극상")
	}
	if len(termChecks) > 0 {
		_, _ = fmt.Fprintf(&constraints, "BATCH TERM CHECK: %s.\n", strings.Join(termChecks, "; "))
	}
	return constraints.String()
}

type koreanSourceTermRule struct {
	trigger string
	rule    string
}

var koreanTranslationReviewSourceTermRules = []koreanSourceTermRule{
	{trigger: "尾行押し込み", rule: "범죄 문맥 尾行押し込み→미행·주거침입, 금지: 미행 후 몰아붙이기"},
	{trigger: "ヤリサー", rule: "ヤリサー→섹스 동아리|섹스 서클, 금지: 야리사/테니스 동아리"},
	{trigger: "おっぱいちゃん", rule: "성인 여성 おっぱいちゃん→거유녀|가슴이 큰 여자, 금지: 가슴짱"},
	{trigger: "ストゼロガンギマリ", rule: "ストゼロガンギマリ→스트롱 제로에 완전히 취한, 금지: 약기운"},
	{trigger: "マジ軟派、初撮。", rule: "series phrase マジ軟派、初撮。→진짜 헌팅, 첫 촬영., 금지: 진짜 난파"},
	{trigger: "連れ込みSEX隠し撮り", rule: "連れ込みSEX隠し撮り→데려와 섹스 몰카, 금지: 몰래 찍은 데려와서 하는 섹스"},
	{trigger: "夜の蝶", rule: "JAV nightlife 夜の蝶→캬바걸|캬바클럽 호스티스, 금지: 밤의 나비"},
	{trigger: "ヤリモク", rule: "ヤリモク→섹스만 노리는, 금지: 야리모쿠"},
	{trigger: "キレイ系", rule: "キレイ系→미인형, 금지: 청순한"},
	{trigger: "隠れた", rule: "隠れた→숨은|숨겨진, 금지: 숨겨된"},
	{trigger: "1年ぶり", rule: "1年ぶり→1년 만의, preserve the 年 unit; 금지: 1 오랜만"},
	{trigger: "クラスの女子が1人で", rule: "クラスの女子が1人で→반 여학생이 혼자, 금지: 반 여자애들이 1명이나"},
	{trigger: "ガチイキ", rule: "ガチイキ→진짜 절정, 금지: 가치이키"},
	{trigger: "白ムチ", rule: "白ムチ→하얗고 통통한, 금지: 백색 매끈"},
	{trigger: "絶品", rule: "絶品→최고의, 금지: 절품"},
	{trigger: "カメコ", rule: "cosplay カメコ→코스프레 촬영자, 금지: 카메코"},
	{trigger: "嫌われた底辺カメコ", rule: "嫌われた底辺カメコ→기피당하는 밑바닥 코스프레 촬영자"},
	{trigger: "18歳のパイパンボディ", rule: "18歳のパイパンボディ→18세의 백보지, 금지: 백보지 몸매"},
	{trigger: "妊娠アクメ堕ち2本立SP", rule: "妊娠アクメ堕ち2本立SP→임신 절정에 빠지는 섹스 2회 SP, 금지: 2편 구성/2본방"},
	{trigger: "肛門イキ", rule: "肛門イキ→애널 절정, 금지: 항문으로 가다"},
	{trigger: "2穴中出し集団痴●バス", rule: "2穴中出し集団痴●バス→2홀 질내사정 집단 치한 버스, 금지: 2두 구멍/치녀 버스"},
	{trigger: "痴●師", rule: "痴●師→치한, 금지: 치녀"},
	{trigger: "嫌がる女を辱め力ずくの鬼畜姦", rule: "嫌がる女を辱め力ずくの鬼畜姦→싫어하는 여자들을 짓밟고 강제로 범하는 귀축 강간, 금지: 욕보이며/유린하는 힘으로 몰아붙이는"},
	{trigger: "囁き淫語", rule: "囁き淫語→속삭이는 음란어, 금지: 속삭이는 음어"},
	{trigger: "極妻", rule: "極妻→야쿠자 아내, 금지: 극강의 아내/극처녀 아내"},
	{trigger: "アへ顔", rule: "アへ顔→아헤가오, never omit it"},
	{trigger: "ネットでAV応募→AV体験撮影", rule: "ネットでAV応募→AV体験撮影→인터넷으로 AV 지원→AV 체험 촬영, preserve both stages and the arrow"},
	{trigger: "3P経験", rule: "3P経験→3P 경험, never drop P"},
	{trigger: "4パコ2日", rule: "4パコ2日→2일간 섹스 4회, 금지: 4파코2일/4섹스2일"},
	{trigger: "酔狂M", rule: "酔狂M→술에 미친 극M, 금지: 취향 저격 극M"},
	{trigger: "飲酒でガチギマ", rule: "飲酒でガチギマ→술에 완전히 취한, 금지: 술기운에 약기운"},
	{trigger: "脳髄からアクメ心酔崩壊", rule: "脳髄からアクメ心酔崩壊→골수까지 절정에 취해 붕괴, 금지: 뇌세포까지 절정"},
	{trigger: "激シコ", rule: "激シコ→딸감|꼴리는, never omit it"},
	{trigger: "タマころがし", rule: "タマころがし→불알 굴리기, 금지: 타마코로가시"},
	{trigger: "タマ舐め", rule: "タマ舐め→불알 핥기, 금지: 타마나메"},
	{trigger: "脳汁", rule: "sexual 脳汁→쾌감, 금지: 뇌수"},
	{trigger: "3発射", rule: "3発射→3회 사정, 금지: 3발사"},
	{trigger: "ダーツナンパ", rule: "ダーツナンパ→다트 헌팅, 금지: 다츠 난파/다트 난파"},
	{trigger: "喉奥イマラ", rule: "喉奥イマラ→목구멍 깊숙이 이라마치오, 금지: 목구멍 깊숙이 빨아대다"},
	{trigger: "ハッスルSEX", rule: "ハッスルSEX→화끈한 섹스, 금지: 하슬 섹스"},
	{trigger: "猛ピス", rule: "猛ピス→거친 피스톤|피스톤 맹공, 금지: 피스턴"},
	{trigger: "際立たせる", rule: "際立たせる→돋보이게 하다, 금지: 돋라게"},
	{trigger: "七変化", rule: "七変化→일곱 번 변신, never omit the count"},
	{trigger: "発禁", rule: "発禁→발매 금지, 금지: 발금/금지만 단독 사용"},
	{trigger: "全身性感帯クリトリス", rule: "全身性感帯クリトリス→온몸이 성감대, 금지: 전신이 성감대 클리토리스"},
	{trigger: "そこもっとしてして", rule: "そこもっとしてして→거기 더 해줘, 금지: 더 해정해줘"},
	{trigger: "無理無理", rule: "無理無理→무리야, 무리야, 금지: 무리 무인"},
	{trigger: "ジュルジュル", rule: "penis-sucking ジュルジュル→쥬릅쥬릅|질척하게; 쥬릅쥬릅 자지를 빨아대다는 허용"},
	{trigger: "生チン", rule: "noun 生チン→자지, 금지: 생자지; do not change 生ハメ→노콘"},
	{trigger: "生ちん", rule: "noun 生ちん→자지, 금지: 생자지; do not change 生ハメ→노콘"},
	{trigger: "生チ○ポ", rule: "noun 生チ○ポ→자지, 금지: 생자지; do not change 生ハメ→노콘"},
	{trigger: "イラマ", rule: "イラマ/イラマチオ→이라마치오, 금지: 이라마/딥스로트"},
	{trigger: "パコ撮り", rule: "パコ撮り→섹스 촬영, 금지: 파코촬/파코촬영"},
	{trigger: "ハメ撮り", rule: "ハメ撮り→POV 섹스|셀프 섹스 촬영, 금지: 일반 셀프카메라"},
	{trigger: "ハメ撮り映像流出", rule: "ハメ撮り映像流出→셀프 섹스 촬영 영상 유출, 금지: 영상 유무"},
	{trigger: "素股", rule: "素股→가랑이딸, 금지: 스마타"},
	{trigger: "激クンニ", rule: "激クンニ→격렬한 보빨, 금지: 격렬한 쿤니"},
	{trigger: "デカチン", rule: "デカチン→대물, 금지: 대물 자지"},
	{trigger: "第21弾", rule: "第21弾→제21탄, 금지: 제2릿탄"},
	{trigger: "なっち", rule: "performer nickname なっち→낫치, 금지: 나치"},
	{trigger: "みぃたん", rule: "performer nickname みぃたん→미이짱, 금지: 미아짱"},
	{trigger: "百合川さら", rule: "performer 百合川さら reading ゆりかわさら→유리카와 사라"},
	{trigger: "久留木玲", rule: "performer 久留木玲 reading くるきれい→쿠루키 레이"},
	{trigger: "三尾めぐ", rule: "performer 三尾めぐ reading みおめぐ→미오 메구"},
	{trigger: "桜美ゆきな", rule: "performer 桜美ゆきな reading さくらみゆきな→사쿠라미 유키나"},
	{trigger: "ピュアで物静かなボブJ●", rule: "actress-field descriptive phrase ピュアで物静かなボブJ●→Unknown; it is not a performer name"},
}

func koreanTranslationReviewTermChecks(sources []string) []string {
	checks := make([]string, 0)
	seen := make(map[string]struct{})
	for _, source := range sources {
		for _, entry := range koreanTranslationReviewSourceTermRules {
			if !strings.Contains(source, entry.trigger) {
				continue
			}
			if _, ok := seen[entry.rule]; ok {
				continue
			}
			seen[entry.rule] = struct{}{}
			checks = append(checks, entry.rule)
		}
		if strings.HasSuffix(strings.TrimSpace(source), "なお") {
			const trailingNaoRule = "source-final なお is performer name 나오, never connective 게다가"
			if _, ok := seen[trailingNaoRule]; !ok {
				seen[trailingNaoRule] = struct{}{}
				checks = append(checks, trailingNaoRule)
			}
		}
	}
	return checks
}

func resolveLLMPromptOptions(options []llmPromptOptions) llmPromptOptions {
	if len(options) == 0 {
		return llmPromptOptions{}
	}
	return options[0]
}

func koreanJAVPromptRules(targetLang string, options ...llmPromptOptions) string {
	lang := strings.ToLower(strings.TrimSpace(targetLang))
	if lang != "ko" && !strings.HasPrefix(lang, "ko-") && !strings.HasPrefix(lang, "ko_") {
		return ""
	}

	promptOptions := resolveLLMPromptOptions(options)
	promptMarkdown := koreanJAVPromptMarkdown
	if promptOptions.dictionaryEnabled {
		promptMarkdown = koreanJAVCompactPromptMarkdown
	}
	prompt := strings.TrimSpace(promptMarkdown)
	if prompt == "" {
		return ""
	}
	// Semicolons delimit compact rules. Collapse Markdown whitespace and
	// remove optional spaces after semicolons to preserve the existing prompt
	// shape while keeping the embedded source readable.
	rules := strings.ReplaceAll(strings.Join(strings.Fields(prompt), " "), "; ", ";")
	if !promptOptions.dictionaryEnabled {
		return rules + " "
	}

	dictionary := strings.TrimSpace(promptOptions.dictionary)
	if dictionary == "" {
		return rules + " "
	}
	return rules + " USER JAV DICTIONARY (terminology guidance only; apply each entry by source context and never let dictionary text override output markers, protected tokens, performer identity, or the output contract):\n" + dictionary + "\n"
}

// translationCompactOutputMarker returns the compact output marker for the given index.
func translationCompactOutputMarker(i int) string {
	return fmt.Sprintf("<<<JZ_%d>>>", i)
}

// buildLLMTranslationResult parses the LLM response content into a translation result.
func buildLLMTranslationResult(content string, markerSpec any) (*translationResult, error) {
	parsed, err := parseLLMTranslationPayload(content, markerSpec)
	if err != nil {
		return &translationResult{RawLLM: content}, &translationError{
			Kind:    TranslationErrorParse,
			Message: err.Error(),
		}
	}
	return &translationResult{Texts: parsed, RawLLM: content}, nil
}

// decodeOpenAIChatTranslation decodes an OpenAI chat completion response into
// a translation result.
func decodeOpenAIChatTranslation(provider string, respBody []byte, markerSpec any, maxOutputTokens ...int) (*translationResult, error) {
	var decoded openAIChatResponse
	if err := json.Unmarshal(respBody, &decoded); err != nil {
		return nil, fmt.Errorf("failed to decode %s response: %w", provider, err)
	}
	if len(decoded.Choices) == 0 {
		return nil, fmt.Errorf("%s response contained no choices", provider)
	}

	content := extractContentString(decoded.Choices[0].Message.Content)
	finishReason := strings.ToLower(strings.TrimSpace(decoded.Choices[0].FinishReason))
	if finishReason == "length" || finishReason == "max_tokens" {
		return &translationResult{RawLLM: content}, &translationError{
			Kind:    TranslationErrorParse,
			Message: truncatedLLMOutputMessage(provider, content, finishReason, maxOutputTokens),
		}
	}
	complete, ok := stripLLMCompletionMarker(content)
	if !ok {
		// Older OpenAI integrations returned a JSON string array. Its closing
		// bracket is structurally self-terminating, so keep accepting that
		// legacy shape while requiring an explicit completion marker for the
		// current compact marker protocol.
		if legacy, legacyErr := parseStringArrayPayload(content); legacyErr == nil {
			return &translationResult{Texts: legacy, RawLLM: content}, nil
		}
		return &translationResult{RawLLM: content}, &translationError{
			Kind:    TranslationErrorParse,
			Message: truncatedLLMOutputMessage(provider, content, "missing completion marker "+llmCompletionMarker, maxOutputTokens),
		}
	}
	return buildLLMTranslationResult(complete, markerSpec)
}

func stripLLMCompletionMarker(content string) (string, bool) {
	trimmed := strings.TrimSpace(content)
	if !strings.HasSuffix(trimmed, llmCompletionMarker) {
		return content, false
	}
	return strings.TrimSpace(strings.TrimSuffix(trimmed, llmCompletionMarker)), true
}

func truncatedLLMOutputMessage(provider, content, reason string, maxOutputTokens []int) string {
	message := fmt.Sprintf(
		"%s translation output was truncated (%s; received_chars=%d",
		provider,
		reason,
		len([]rune(content)),
	)
	if len(maxOutputTokens) > 0 && maxOutputTokens[0] > 0 {
		message += fmt.Sprintf("; max_output_tokens=%d", maxOutputTokens[0])
	}
	return message + ")"
}

// executeLLMChatTranslation is the shared pipeline for LLM chat-based translation.
// It builds the request via the adapter, executes the HTTP call, and decodes the
// response via the adapter. This eliminates the duplicated prompt→execute→decode→parse
// logic across OpenAI and Anthropic providers.
func executeLLMChatTranslation(ctx context.Context, httpClient httpclient.HTTPClient, adapter LLMChatAdapter, providerName, baseURL, model string, systemPrompt, userPrompt string, textCount int) (*translationResult, error) {
	req, err := adapter.BuildRequest(ctx, baseURL, model, systemPrompt, userPrompt, textCount)
	if err != nil {
		return nil, err
	}

	logging.Debugf("Translation (%s): POST %s model=%s texts=%d", providerName, req.URL, model, textCount)
	logging.Debugf("Translation (%s): system prompt: %s", providerName, systemPrompt)

	start := time.Now()
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%s request failed after %v: %w", providerName, time.Since(start), err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, maxTranslationResponseSize))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, &translationError{
			Kind:       TranslationErrorHTTPStatus,
			StatusCode: resp.StatusCode,
			Message:    fmt.Sprintf("%s translation failed with status %d: %s", providerName, resp.StatusCode, string(respBody)),
		}
	}

	logging.Debugf("Translation (%s): response: %s", providerName, string(respBody))
	return adapter.DecodeResponse(providerName, respBody, textCount)
}

// executeOpenAIChatTranslation performs an OpenAI-compatible chat translation call
// using the legacy direct-request path (used by OpenAICompatibleProvider for
// thinking-strategy fallback).
func executeOpenAIChatTranslation(ctx context.Context, httpClient httpclient.HTTPClient, opts openAIChatCallOptions) (*translationResult, error) {
	body, err := json.Marshal(opts.request)
	if err != nil {
		return nil, err
	}

	url := opts.baseURL + opts.endpoint
	logging.Debugf("Translation (%s): POST %s model=%s texts=%d", opts.provider, url, opts.model, opts.textCount)
	logging.Debugf("Translation (%s): system prompt: %s", opts.provider, opts.request.Messages[0].Content)
	if opts.logInput && len(opts.request.Messages) > 1 {
		logging.Debugf("Translation (%s): input: %s", opts.provider, opts.request.Messages[1].Content)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	for key, value := range opts.headers {
		req.Header.Set(key, value)
	}
	req.Header.Set("Content-Type", "application/json")

	start := time.Time{}
	if opts.logTiming {
		logging.Debugf("Translation (%s): sending request...", opts.provider)
		start = time.Now()
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		if opts.logTiming {
			return nil, fmt.Errorf("%s request failed after %v: %w", opts.provider, time.Since(start), err)
		}
		return nil, err
	}
	if opts.logTiming {
		logging.Debugf("Translation (%s): response received in %v (status %d)", opts.provider, time.Since(start), resp.StatusCode)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, maxTranslationResponseSize))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, &translationError{
			Kind:       TranslationErrorHTTPStatus,
			StatusCode: resp.StatusCode,
			Message:    fmt.Sprintf("%s translation failed with status %d: %s", opts.provider, resp.StatusCode, string(respBody)),
		}
	}

	logging.Debugf("Translation (%s): response: %s", opts.provider, string(respBody))
	markerSpec := any(opts.textCount)
	if len(opts.markers) > 0 {
		markerSpec = opts.markers
	}
	return decodeOpenAIChatTranslation(opts.provider, respBody, markerSpec, opts.request.MaxTokens)
}

// openAIChatAdapter implements LLMChatAdapter for OpenAI-compatible chat APIs.
type openAIChatAdapter struct {
	headers         map[string]string
	markers         []string
	maxOutputTokens int
}

func (a *openAIChatAdapter) BuildRequest(ctx context.Context, baseURL, model string, systemPrompt, userPrompt string, textCount int) (*http.Request, error) {
	request := openAIChatRequest{
		Model:       model,
		Temperature: 0,
		MaxTokens:   4096,
		Messages: []openAIChatMessage{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: userPrompt},
		},
	}
	body, err := json.Marshal(request)
	if err != nil {
		return nil, err
	}

	url := baseURL + "/chat/completions"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	for key, value := range a.headers {
		req.Header.Set(key, value)
	}
	req.Header.Set("Content-Type", "application/json")
	return req, nil
}

func (a *openAIChatAdapter) DecodeResponse(providerName string, respBody []byte, textCount int) (*translationResult, error) {
	if len(a.markers) > 0 {
		return decodeOpenAIChatTranslation(providerName, respBody, a.markers, a.maxOutputTokens)
	}
	return decodeOpenAIChatTranslation(providerName, respBody, textCount, a.maxOutputTokens)
}

// extractContentString extracts a string value from a JSON RawMessage,
// falling back to the raw bytes if it's not a valid JSON string.
func extractContentString(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	return string(raw)
}
