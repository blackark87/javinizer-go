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

	terminologyRules := "(1) Body measurements: use natural target-language equivalents (e.g. 股下 → 다리 길이, not 가랑이 길이). " +
		"(2) Translate adult slang by meaning using vocabulary natural to adult content — never merely read the characters (e.g. 美脚 → 각선미; 爆乳 → 폭유; 神乳 → 신의 가슴; 中出し → 질내 사정; 騎乗位 → 기승위; 背面騎乗位 → 후배위 기승위; 杭打ち騎乗位 → 말뚝박기 기승위; デカ尻 → 큰 엉덩이; 尻コキ → 엉덩이 성교). " +
		"(3) Keep standard phonetic forms for borrowed JAV terms (e.g. パパ活 → 파파카츠; ぶっかけ → 부카케; フェラ → 펠라). " +
		"(4) Translate every part of each item, including Latin or English portions and every Japanese word; leave no Japanese hiragana, katakana, or kanji in non-Japanese output. " +
		"(5) Use correct standard target-language spelling and do not invent words. "
	personNameRule := "Person-name rule: sections labeled <<<actress[N]>>> or <<<title_as_name>>> contain a performer's name. Transliterate phonetically, never translate the meaning. Keep Japanese order: FamilyName GivenName. Romaji input is the authoritative reading; do not re-derive it from kanji. For Korean, output Hangul and transliterate Hepburn syllables literally (Rena → 레나, Reina → 레이나; Yuu → 유, Tarou → 타로, Yui → 유이). Ignore age, cup size, height, and occupation extras; if no personal name remains, return an empty section. Also apply this rule to a short personal-name-like <<<title>>>. "
	properNounRule := "Proper-noun rule: <<<maker>>>, <<<label>>>, and <<<director>>> are names. Transliterate them phonetically and do not embellish them. "
	cleanupRules := "Title cleanup: remove bracketed VR/release labels such as [VR], 【VR】, and 【8K VR】. Description cleanup: remove playback/device notices, VR-only notices, platform notices, sales campaigns, and store promotions; if only excluded material remains, return an empty section. "
	placeholderRule := "Any Hangul already present is final and must be copied verbatim. Protected tokens of the form ⟦N⟧ must be reproduced exactly and never translated, removed, or renumbered. "

	systemPrompt := fmt.Sprintf("You are a translator specializing in Japanese adult video (JAV) metadata. Translate each labeled section into natural, engaging promotional copy rather than literal prose. Titles should be concise and enticing; descriptions should read as sensual marketing copy. Follow these rules: %s%s%s%s%sCRITICAL: return each translation under the same marker, never merge, omit, reorder, or swap sections. Return only markers and translated text; no JSON or commentary. Keep each translation on one logical line. Source language: %s. Target language: %s.", terminologyRules, personNameRule, properNounRule, cleanupRules, placeholderRule, sourceLang, targetLang)

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
