package translation

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/javinizer/javinizer-go/internal/httpclient"
	"github.com/javinizer/javinizer-go/internal/logging"
)

// openAIChatRequest represents a chat completion request for OpenAI-compatible APIs.
type openAIChatRequest struct {
	Model              string              `json:"model"`
	Temperature        float64             `json:"temperature"`
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

	terminologyRules := "General terminology rules: use established, current terminology from the target language's JAV/adult-video industry. Decode JAV idioms and production tropes by their actual industry meaning, not their literal dictionary image. Do not invent vocabulary. If the target language has no exact established equivalent, transliterate the source term phonetically instead of forcing a literal translation or adding a long explanation. Translate every meaningful Latin, English, and Japanese portion; leave no Japanese script in non-Japanese output except protected person-name punctuation. "
	personNameRule := "Person-name rule: sections labeled <<<actress[N]>>> or <<<title_as_name>>> contain one performer's name. Transliterate phonetically, never translate the meaning, and keep Japanese order: FamilyName GivenName. Romaji input is the authoritative reading; do not re-derive it from kanji. The Japanese middle dot ・ is punctuation inside one name: preserve it exactly, never turn it into a comma, and never split the name into multiple performers. For Korean, output Hangul and transliterate Hepburn syllables literally (Rena → 레나, Reina → 레이나; Yuu → 유, Tarou → 타로, Yui → 유이). Ignore age, cup size, height, and occupation extras; if no personal name remains, return an empty section. Also apply this rule to a short personal-name-like <<<title>>>. "
	properNounRule := "Proper-noun rule: <<<maker>>>, <<<label>>>, and <<<director>>> are names. Transliterate them phonetically and do not embellish them. "
	cleanupRules := "Title cleanup: remove bracketed VR/release labels such as [VR], 【VR】, and 【8K VR】. Description cleanup: remove playback/device notices, VR-only notices, platform notices, sales campaigns, and store promotions; if only excluded material remains, return an empty section. "
	placeholderRule := "Any Hangul already present is final and must be copied verbatim. Protected tokens of the form ⟦N⟧ must be reproduced exactly and never translated, removed, or renumbered. "
	koreanRules := koreanJAVPromptRules(targetLang)

	systemPrompt := fmt.Sprintf("You are an expert translator working strictly with Japanese adult video (JAV) metadata and AV-studio metadata. Use the concise, direct, contemporary vocabulary an actual AV production studio would publish. Never use corny, dated, literary, moralizing, broadcast-style euphemistic, or exaggerated erotic-advertising language. Titles and tags must be sharp marketing-ready metadata; descriptions must remain complete, natural modern prose rather than being reduced to keyword lists. Follow these rules: %s%s%s%s%s%sCRITICAL: return each translation under the same marker, never merge, omit, reorder, or swap sections. Return only markers and translated text; no JSON or commentary. Keep each translation on one logical line. Source language: %s. Target language: %s.", terminologyRules, koreanRules, personNameRule, properNounRule, cleanupRules, placeholderRule, sourceLang, targetLang)

	var userPrompt strings.Builder
	userPrompt.WriteString("Translate each labeled section below:\n")
	for i, text := range texts {
		userPrompt.WriteString(markers[i])
		userPrompt.WriteByte('\n')
		userPrompt.WriteString(text)
		userPrompt.WriteByte('\n')
	}
	userPrompt.WriteString("\nReturn output in the same labeled format:\n")
	for _, marker := range markers {
		userPrompt.WriteString(marker)
		userPrompt.WriteString("\n[translation]\n")
	}
	return systemPrompt, strings.TrimSpace(userPrompt.String()), nil
}

func koreanJAVPromptRules(targetLang string) string {
	lang := strings.ToLower(strings.TrimSpace(targetLang))
	if lang != "ko" && !strings.HasPrefix(lang, "ko-") && !strings.HasPrefix(lang, "ko_") {
		return ""
	}
	return "Korean JAV rules: write natural, concise, current Korean used by real AV studios. Prefer, in order: an established Korean AV term; a widely used JAV loanword or acronym; otherwise faithful Hangul transliteration. Never replace a missing equivalent with a literal calque, verbose definition, or invented word. For mappings with alternatives, choose exactly one that fits the context and never output alternatives joined by a slash, parentheses, or '또는'. Strict contextual mappings: " +
		"数珠つなぎ → 릴레이 or 연속; たすきリレー and バトンリレー → 바통 터치 or 릴레이; 芋づる式 → 연쇄 or 연속; " +
		"パパ活 and Sugar Dating → 스폰 or 조건; 一本釣り → 독점 스카우트 or 길거리 캐스팅; 箱入り and 箱入り娘 → 아가씨 or 순진녀; 逆指名 → 여배우의 선택 or 역지명; " +
		"垢抜け → 비주얼 업그레이드 or 세련된; 初々しい → 풋풋한 or 앳된; 玄人 and 玄人肌 → 프로 or 능숙한; " +
		"中出し and Creampie → 질내사정; 顔射 and Facial → 안면사정; ぶっかけ and Bukkake → 정액 세례 or 붓카케; ハメ撮り and POV → POV or 셀프카메라; 汁男優 → 사정 전문 남배우. " +
		"Interpret these JAV tropes by intent, using current Korean AV wording rather than literal imagery: ご開帳 means the full intimate reveal or unveiling; 手取り足取り means hands-on step-by-step intimate guidance; 骨抜き means being left weak from intense pleasure; 毒牙 means falling prey to predatory or corrupting seduction; 生殺し means edging or teasing without release. " +
		"Additional established Korean terminology: 股下 → 다리 길이; 美脚 → 각선미; 爆乳 → 폭유; 神乳 → 신의 가슴; 騎乗位 → 기승위; 背面騎乗位 → 후배위 기승위; 杭打ち騎乗位 → 말뚝박기 기승위; デカ尻 → 큰 엉덩이; 尻コキ → 엉덩이 성교; フェラ → 펠라. "
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
func decodeOpenAIChatTranslation(provider string, respBody []byte, markerSpec any) (*translationResult, error) {
	var decoded openAIChatResponse
	if err := json.Unmarshal(respBody, &decoded); err != nil {
		return nil, fmt.Errorf("failed to decode %s response: %w", provider, err)
	}
	if len(decoded.Choices) == 0 {
		return nil, fmt.Errorf("%s response contained no choices", provider)
	}

	return buildLLMTranslationResult(extractContentString(decoded.Choices[0].Message.Content), markerSpec)
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
	return decodeOpenAIChatTranslation(opts.provider, respBody, markerSpec)
}

// openAIChatAdapter implements LLMChatAdapter for OpenAI-compatible chat APIs.
type openAIChatAdapter struct {
	headers map[string]string
	markers []string
}

func (a *openAIChatAdapter) BuildRequest(ctx context.Context, baseURL, model string, systemPrompt, userPrompt string, textCount int) (*http.Request, error) {
	request := openAIChatRequest{
		Model:       model,
		Temperature: 0,
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
		return decodeOpenAIChatTranslation(providerName, respBody, a.markers)
	}
	return decodeOpenAIChatTranslation(providerName, respBody, textCount)
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
