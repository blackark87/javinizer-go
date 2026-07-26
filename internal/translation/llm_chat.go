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

type qualityReviewItem struct {
	Source    string
	Candidate string
}

const llmCompletionMarker = "<<<JZ_DONE>>>"

const defaultLLMRequestTimeout = 120 * time.Second

var jac024TailPromptPattern = regexp.MustCompile(`極選エロギャル([0-9０-９]+|⟦[0-9]+⟧)名([0-9０-９]+|⟦[0-9]+⟧)分`)

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

func buildLLMTranslationPromptsWithMarkers(sourceLang, targetLang string, texts, markers []string) (string, string, error) {
	if len(texts) == 0 || len(markers) != len(texts) {
		return "", "", fmt.Errorf("translation prompt requires one marker per text (%d markers for %d texts)", len(markers), len(texts))
	}

	terminologyRules := "Translate every meaningful source segment. Use established current target-language JAV terminology and render ordinary words, idioms, sound words, and transparent compounds by meaning. Transliterate only person or brand names, opaque proper nouns, and genuine industry loanwords; leave no Japanese script in non-Japanese output except protected name punctuation. "
	personNameRule := "Person-name rule: <<<actress[N]>>> and <<<title_as_name>>> contain one performer. Transliterate the reading in Japanese FamilyName GivenName order; romaji is authoritative. Never invent, anglicize, or substitute a different performer name; never shorten or translate it or turn kanji into emoji. Preserve the middle dot ・ inside one name, never turn it into a comma, and never split one performer. Apply this rule to a short name-like <<<title>>> too. "
	properNounRule := "Proper-noun rule: <<<maker>>>, <<<label>>>, and <<<director>>> are names. Transliterate them phonetically and do not embellish them. "
	cleanupRules := "Title cleanup: remove bracketed VR/release labels such as [VR], 【VR】, and 【8K VR】. Description cleanup: remove a leading release-date/runtime metadata prefix and its adjacent content ID, playback/device notices, VR-only notices, platform notices, sales campaigns, and store promotions; if only excluded material remains, return an empty section. "
	placeholderRule := "Any Hangul already present is final and must be copied verbatim. Protected tokens of the form ⟦N⟧ must be reproduced exactly and never translated, removed, or renumbered. "
	koreanRules := koreanJAVPromptRules(targetLang)

	systemPrompt := fmt.Sprintf("You translate Japanese adult video (JAV) metadata and AV-studio metadata for actual studio use. Write concise contemporary titles and complete natural descriptions; avoid corny, dated, literary, moralizing, euphemistic, or invented wording. %s%s%s%s%s%sReturn marker+one-line translation for each, then exact final line %s; no JSON or commentary. Source: %s. Target: %s.", terminologyRules, koreanRules, personNameRule, properNounRule, cleanupRules, placeholderRule, llmCompletionMarker, sourceLang, targetLang)

	var userPrompt strings.Builder
	userPrompt.WriteString("Translate each labeled section below:\n")
	userPrompt.WriteString(koreanBatchPromptConstraints(targetLang, texts))
	for i, text := range texts {
		userPrompt.WriteString(markers[i])
		userPrompt.WriteByte('\n')
		userPrompt.WriteString(text)
		userPrompt.WriteByte('\n')
	}
	return systemPrompt, strings.TrimSpace(userPrompt.String()), nil
}

func buildLLMQualityReviewPromptsWithMarkers(targetLang string, items []qualityReviewItem, markers []string) (string, string, error) {
	if len(items) == 0 || len(markers) != len(items) {
		return "", "", fmt.Errorf("quality review prompt requires one marker per item (%d markers for %d items)", len(markers), len(items))
	}
	systemPrompt := "You are the mandatory second-pass quality reviewer for Japanese AV metadata translated into Korean. Compare source and candidate, then silently fix mistranslation, calques, untranslated or transliterated slang, omissions, inventions, broken text, awkward grammar, and outdated terminology. Preserve explicitness, tone, protected tokens, and performer identity. Do not restore omitted release tags, playback/device notices, or sales/store promotions. Return the complete corrected Korean text, not an assessment. " + koreanJAVPromptRules(targetLang) + "Copy every <<<quality_review_...>>> marker with its complete corrected Korean text, then exact final line " + llmCompletionMarker + ". Never echo source/candidate labels or add commentary."

	var userPrompt strings.Builder
	userPrompt.WriteString("Review and, where necessary, rewrite each candidate by comparing it with its Japanese source:\n")
	sources := make([]string, len(items))
	for i := range items {
		sources[i] = items[i].Source
	}
	userPrompt.WriteString(koreanBatchPromptConstraints(targetLang, sources))
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

func koreanJAVPromptRules(targetLang string) string {
	lang := strings.ToLower(strings.TrimSpace(targetLang))
	if lang != "ko" && !strings.HasPrefix(lang, "ko-") && !strings.HasPrefix(lang, "ko_") {
		return ""
	}

	prompt := strings.TrimSpace(koreanJAVPromptMarkdown)
	if prompt == "" {
		return ""
	}
	// Semicolons delimit compact rules. Collapse Markdown whitespace and
	// remove optional spaces after semicolons to preserve the existing prompt
	// shape while keeping the embedded source readable.
	return strings.ReplaceAll(strings.Join(strings.Fields(prompt), " "), "; ", ";") + " "
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
