package translation

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/javinizer/javinizer-go/internal/httpclient"
)

// AnthropicProvider translates text via the Anthropic Messages API.
type AnthropicProvider struct {
	cfg        Config
	httpClient httpclient.HTTPClient
}

// NewAnthropicProvider constructs an AnthropicProvider from config and an HTTP client.
func NewAnthropicProvider(cfg Config, httpClient httpclient.HTTPClient) *AnthropicProvider {
	return &AnthropicProvider{cfg: cfg, httpClient: httpClient}
}

// Name returns "anthropic".
func (p *AnthropicProvider) Name() string { return "anthropic" }

// Translate sends the given texts to the Anthropic API and returns the translated result.
func (p *AnthropicProvider) Translate(ctx context.Context, sourceLang, targetLang string, texts []string) (*translationResult, error) {
	if p == nil {
		return nil, fmt.Errorf("nil receiver: *AnthropicProvider")
	}
	baseURL := strings.TrimRight(strings.TrimSpace(p.cfg.Anthropic.BaseURL), "/")
	if baseURL == "" {
		baseURL = "https://api.anthropic.com"
	}

	apiKey := strings.TrimSpace(p.cfg.Anthropic.APIKey)
	if apiKey == "" {
		return nil, fmt.Errorf("anthropic api_key is required")
	}

	model := strings.TrimSpace(p.cfg.Anthropic.Model)
	if model == "" {
		model = "claude-sonnet-4-20250514"
	}

	if len(texts) == 0 {
		return &translationResult{}, nil
	}

	markers := translationMarkersFromContext(ctx, len(texts))
	systemPrompt, userPrompt, err := buildLLMTranslationPromptsWithMarkers(sourceLang, targetLang, texts, markers)
	if err != nil {
		return nil, err
	}

	adapter := &anthropicChatAdapter{apiKey: apiKey, markers: markers}
	return executeLLMChatTranslation(ctx, p.httpClient, adapter, "anthropic", baseURL, model, systemPrompt, userPrompt, len(texts))
}

// anthropicChatAdapter implements LLMChatAdapter for the Anthropic Messages API.
type anthropicChatAdapter struct {
	apiKey  string
	markers []string
}

func (a *anthropicChatAdapter) BuildRequest(ctx context.Context, baseURL, model string, systemPrompt, userPrompt string, textCount int) (*http.Request, error) {
	type anthropicMessage struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}
	requestBody := map[string]any{
		"model":      model,
		"max_tokens": 4096,
		"system":     systemPrompt,
		"messages":   []anthropicMessage{{Role: "user", Content: userPrompt}},
	}
	body, err := json.Marshal(requestBody)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/v1/messages", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("x-api-key", a.apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")
	req.Header.Set("Content-Type", "application/json")
	return req, nil
}

func (a *anthropicChatAdapter) DecodeResponse(providerName string, respBody []byte, textCount int) (*translationResult, error) {
	var decoded struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		StopReason string `json:"stop_reason"`
	}
	if err := json.Unmarshal(respBody, &decoded); err != nil {
		return nil, fmt.Errorf("failed to decode %s response: %w", providerName, err)
	}
	if len(decoded.Content) == 0 {
		return nil, fmt.Errorf("%s response contained no content blocks", providerName)
	}
	content := strings.TrimSpace(decoded.Content[0].Text)
	if strings.EqualFold(strings.TrimSpace(decoded.StopReason), "max_tokens") {
		return &translationResult{RawLLM: content}, &translationError{
			Kind:    TranslationErrorParse,
			Message: truncatedLLMOutputMessage(providerName, content, decoded.StopReason, []int{4096}),
		}
	}
	complete, ok := stripLLMCompletionMarker(content)
	if !ok {
		// Preserve the structurally complete legacy JSON-array response format.
		if legacy, legacyErr := parseStringArrayPayload(content); legacyErr == nil {
			return &translationResult{Texts: legacy, RawLLM: content}, nil
		}
		return &translationResult{RawLLM: content}, &translationError{
			Kind: TranslationErrorParse,
			Message: truncatedLLMOutputMessage(
				providerName,
				content,
				"missing completion marker "+llmCompletionMarker,
				[]int{4096},
			),
		}
	}
	if len(a.markers) > 0 {
		return buildLLMTranslationResult(complete, a.markers)
	}
	return buildLLMTranslationResult(complete, textCount)
}
