package translation

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/logging"
)

type openAIChatRequest struct {
	Model              string              `json:"model"`
	Temperature        float64             `json:"temperature"`
	Messages           []openAIChatMessage `json:"messages"`
	ChatTemplateKwargs map[string]any      `json:"chat_template_kwargs,omitempty"`
	ReasoningEffort    string              `json:"reasoning_effort,omitempty"`
	EnableThinking     *bool               `json:"enable_thinking,omitempty"`
}

type openAIChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type openAIChatResponse struct {
	Choices []struct {
		Message struct {
			Content json.RawMessage `json:"content"`
		} `json:"message"`
	} `json:"choices"`
}

type openAICompatibleThinkingStrategy string

const (
	openAICompatibleThinkingStrategyChatTemplateKwargs openAICompatibleThinkingStrategy = "chat_template_kwargs"
	openAICompatibleThinkingStrategyReasoningEffort    openAICompatibleThinkingStrategy = "reasoning_effort"
	openAICompatibleThinkingStrategyEnableThinking     openAICompatibleThinkingStrategy = "enable_thinking"
	openAICompatibleThinkingStrategyNone               openAICompatibleThinkingStrategy = "none"
)

// translationMovieIDKey is the context key for propagating movie ID to provider log lines.
type translationMovieIDKey struct{}

type openAIChatCallOptions struct {
	provider  string
	baseURL   string
	endpoint  string
	model     string
	headers   map[string]string
	request   openAIChatRequest
	markers   []string
	logInput  bool
	logTiming bool
}

// makeJZMarkers generates numbered JZ_N markers for providers that use the legacy format.
func makeJZMarkers(n int) []string {
	markers := make([]string, n)
	for i := range n {
		markers[i] = translationCompactOutputMarker(i)
	}
	return markers
}

func buildLLMTranslationPrompts(sourceLang, targetLang string, texts []string, fieldNames []string) (string, string, []string, error) {
	useNamed := len(fieldNames) == len(texts) && len(texts) > 0

	markers := make([]string, len(texts))
	if useNamed {
		for i, fn := range fieldNames {
			markers[i] = "<<<" + fn + ">>>"
		}
	} else {
		for i := range texts {
			markers[i] = translationCompactOutputMarker(i)
		}
	}

	terminologyRules := "(1) Body measurements: use natural target-language equivalents (e.g. 股下 → 다리 길이, NOT 가랑이 길이). " +
		"(2) Kanji adult slang: translate by meaning using vocabulary natural to adult content in the target language — never just read the characters (e.g. 美脚 → 각선미; 爆乳 → 폭유; 神乳 → 신의 가슴). " +
		"(3) Japanese kana loanwords commonly borrowed into the target language: keep the standard phonetic form (e.g. パパ活 → 파파카츠, NOT 파파활). " +
		"(4) Translate ALL text in each item, including any Latin or English portions — do not leave any part untranslated. "
	titleNameRule := "Title-specific name rule: if a title is a short personal-name-like Japanese string, especially kana-only or a title that appears to match known actress-name context supplied by the service, transliterate it phonetically into the target language instead of translating its semantic meaning. For Korean, write なつ as 나츠 and 夏 as 나츠, NOT 여름. "

	var systemPrompt string
	var userPrompt strings.Builder

	if useNamed {
		systemPrompt = fmt.Sprintf("You are a translator specializing in Japanese adult video (JAV) content metadata. Translate each labeled section into natural, engaging promotional copy — not a literal word-for-word translation. Titles should be concise and enticing; descriptions should read as sensual, persuasive marketing blurbs in the target language. Follow these terminology rules: "+
			terminologyRules+
			titleNameRule+
			"CRITICAL labeling rule: translate the text under each <<<label>>> and return it under the SAME <<<label>>> — never merge multiple sections into one label, never omit a label, never swap labels. "+
			"Return ONLY the labeled output markers with their translations. Do not use JSON. Do not add commentary. Keep each translation on a single logical line; if needed, replace internal newlines with spaces. Source language: %s. Target language: %s.", sourceLang, targetLang)

		userPrompt.WriteString("Translate each labeled section below:\n")
		for i, text := range texts {
			userPrompt.WriteString(markers[i])
			userPrompt.WriteString("\n")
			userPrompt.WriteString(text)
			userPrompt.WriteString("\n")
		}
		userPrompt.WriteString("\nReturn output in the same labeled format:\n")
		for i := range texts {
			userPrompt.WriteString(markers[i])
			userPrompt.WriteString("\n[translation]\n")
		}
	} else {
		systemPrompt = fmt.Sprintf("You are a translator specializing in Japanese adult video (JAV) content metadata. Translate each item into natural, engaging promotional copy — not a literal word-for-word translation. Titles should be concise and enticing; descriptions should read as sensual, persuasive marketing blurbs in the target language. Follow these terminology rules: "+
			terminologyRules+
			titleNameRule+
			"CRITICAL ordering rule: item[0] MUST go under <<<JZ_0>>>, item[1] MUST go under <<<JZ_1>>>, and so on — never swap or reorder items. "+
			"Return ONLY the output markers with their translations. Do not use JSON. Do not add commentary. Do not repeat the input markers as content. Do not omit any index. Keep each translation on a single logical line; if needed, replace internal newlines with spaces. Source language: %s. Target language: %s.", sourceLang, targetLang)

		payloadBytes, err := json.Marshal(texts)
		if err != nil {
			return "", "", nil, err
		}
		userPrompt.WriteString("Translate this JSON array of strings: ")
		userPrompt.Write(payloadBytes)
		userPrompt.WriteString("\nReturn output in this exact format (replace each placeholder with the actual translation of that numbered item):\n")
		for i := range texts {
			userPrompt.WriteString(markers[i])
			userPrompt.WriteString(fmt.Sprintf("\n[translation of item %d]\n", i))
		}
	}

	return systemPrompt, strings.TrimSpace(userPrompt.String()), markers, nil
}

func translationCompactOutputMarker(i int) string {
	return fmt.Sprintf("<<<JZ_%d>>>", i)
}

// buildActressTranslationPrompts builds a specialized phonetic prompt for actress names.
// Korean targets receive Hangul transliteration instructions, while other targets
// keep the existing English/Latin ASCII romanization behavior.
func buildActressTranslationPrompts(sourceLang, targetLang string, texts []string) (string, string, error) {
	var systemPrompt string
	if strings.EqualFold(strings.TrimSpace(targetLang), "ko") {
		systemPrompt = fmt.Sprintf(
			"You are a Japanese actress name Hangul transliterator. "+
				"Transliterate each Japanese name (kanji, katakana, or hiragana) phonetically into Hangul Korean. "+
				"Rules: "+
				"(1) Use Hangul that matches the Japanese pronunciation; do NOT translate meanings. For example, なつ -> 나츠 and 夏 -> 나츠, not 여름. "+
				"(2) Return names in Japanese name order: FamilyName GivenName (e.g. '하타노 유이', not '유이 하타노'). "+
				"(3) Preserve order and return ONLY the indexed output markers in ascending order. Do not add commentary. Do not omit any index. "+
				"(4) If the input contains a personal name alongside descriptive text (age, occupation, cup size, height, etc.), transliterate ONLY the personal name — ignore age (歳), cup sizes, occupations, heights, and other non-name content. "+
				"(5) If the input contains NO personal name (e.g. it is a job title, physical description, or scene description only), return an empty string for that entry. "+
				"Source language: %s. Target language: %s.",
			sourceLang, targetLang,
		)
	} else {
		systemPrompt = fmt.Sprintf(
			"You are a Japanese actress name romanizer. "+
				"Romanize each Japanese name (kanji, katakana, or hiragana) to its English/Latin equivalent. "+
				"Rules: "+
				"(1) Use ONLY standard ASCII letters (a-z, A-Z), spaces, and hyphens. No diacritics, no accents, no special characters. Write 'u' not 'ū', 'o' not 'ō', 'a' not 'ā'. "+
				"(2) Return names in Japanese name order: FamilyName GivenName (e.g. 'Hatano Yui', not 'Yui Hatano'). "+
				"(3) Do NOT translate meaning — romanize phonetically only. "+
				"(4) Preserve order and return ONLY the indexed output markers in ascending order. Do not add commentary. Do not omit any index. "+
				"(5) If the input contains a personal name alongside descriptive text (age, occupation, cup size, height, etc.), romanize ONLY the personal name — ignore age (歳), cup sizes, occupations, heights, and other non-name content. "+
				"(6) If the input contains NO personal name (e.g. it is a job title, physical description, or scene description only), return an empty string for that entry. "+
				"Source language: %s. Target language: %s.",
			sourceLang, targetLang,
		)
	}

	payloadBytes, err := json.Marshal(texts)
	if err != nil {
		return "", "", err
	}

	var userPrompt strings.Builder
	userPrompt.WriteString("Romanize these Japanese actress names: ")
	userPrompt.Write(payloadBytes)
	userPrompt.WriteString("\nReturn output in this exact pattern:\n")
	for i := range texts {
		userPrompt.WriteString(translationCompactOutputMarker(i))
		userPrompt.WriteString("\nromanized name\n")
	}

	return systemPrompt, strings.TrimSpace(userPrompt.String()), nil
}

func buildOpenAICompatibleThinkingStrategies(baseURL, model string, cfg config.OpenAICompatibleTranslationConfig) []openAICompatibleThinkingStrategy {
	switch cfg.NormalizedBackendType() {
	case "vllm":
		return []openAICompatibleThinkingStrategy{
			openAICompatibleThinkingStrategyChatTemplateKwargs,
			openAICompatibleThinkingStrategyReasoningEffort,
			openAICompatibleThinkingStrategyEnableThinking,
			openAICompatibleThinkingStrategyNone,
		}
	case "ollama":
		return []openAICompatibleThinkingStrategy{
			openAICompatibleThinkingStrategyReasoningEffort,
			openAICompatibleThinkingStrategyChatTemplateKwargs,
			openAICompatibleThinkingStrategyEnableThinking,
			openAICompatibleThinkingStrategyNone,
		}
	case "llama.cpp":
		return []openAICompatibleThinkingStrategy{
			openAICompatibleThinkingStrategyEnableThinking,
			openAICompatibleThinkingStrategyChatTemplateKwargs,
			openAICompatibleThinkingStrategyReasoningEffort,
			openAICompatibleThinkingStrategyNone,
		}
	case "other":
		return []openAICompatibleThinkingStrategy{
			openAICompatibleThinkingStrategyChatTemplateKwargs,
			openAICompatibleThinkingStrategyReasoningEffort,
			openAICompatibleThinkingStrategyEnableThinking,
			openAICompatibleThinkingStrategyNone,
		}
	}

	switch {
	case looksLikeOllamaBaseURL(baseURL):
		return []openAICompatibleThinkingStrategy{
			openAICompatibleThinkingStrategyReasoningEffort,
			openAICompatibleThinkingStrategyChatTemplateKwargs,
			openAICompatibleThinkingStrategyEnableThinking,
			openAICompatibleThinkingStrategyNone,
		}
	case looksLikeLlamaCppBackend(baseURL, model):
		return []openAICompatibleThinkingStrategy{
			openAICompatibleThinkingStrategyEnableThinking,
			openAICompatibleThinkingStrategyChatTemplateKwargs,
			openAICompatibleThinkingStrategyReasoningEffort,
			openAICompatibleThinkingStrategyNone,
		}
	default:
		return []openAICompatibleThinkingStrategy{
			openAICompatibleThinkingStrategyChatTemplateKwargs,
			openAICompatibleThinkingStrategyReasoningEffort,
			openAICompatibleThinkingStrategyEnableThinking,
			openAICompatibleThinkingStrategyNone,
		}
	}
}

func looksLikeOllamaBaseURL(baseURL string) bool {
	parsed, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil {
		return false
	}

	host := strings.ToLower(parsed.Host)
	return strings.Contains(host, "ollama") || strings.HasSuffix(host, ":11434")
}

func looksLikeLlamaCppBackend(baseURL, model string) bool {
	parsed, err := url.Parse(strings.TrimSpace(baseURL))
	if err == nil {
		host := strings.ToLower(parsed.Host)
		path := strings.ToLower(parsed.Path)
		if strings.Contains(host, "llama") || strings.Contains(path, "llama") {
			return true
		}
	}

	model = strings.ToLower(strings.TrimSpace(model))
	return strings.Contains(model, ".gguf") || strings.Contains(model, "gguf")
}

func applyOpenAICompatibleThinkingStrategy(base openAIChatRequest, strategy openAICompatibleThinkingStrategy, enabled bool) openAIChatRequest {
	req := base
	req.ChatTemplateKwargs = nil
	req.ReasoningEffort = ""
	req.EnableThinking = nil

	switch strategy {
	case openAICompatibleThinkingStrategyChatTemplateKwargs:
		req.ChatTemplateKwargs = map[string]any{
			"enable_thinking": enabled,
			"thinking":        enabled,
		}
	case openAICompatibleThinkingStrategyReasoningEffort:
		if enabled {
			req.ReasoningEffort = "medium"
		} else {
			req.ReasoningEffort = "none"
		}
	case openAICompatibleThinkingStrategyEnableThinking:
		req.EnableThinking = &enabled
	}

	return req
}

func isRetryableThinkingStrategyError(err error) bool {
	if err == nil {
		return false
	}

	var te *TranslationError
	if errors.As(err, &te) && te.Kind == TranslationErrorHTTPStatus {
		return te.StatusCode == 400 || te.StatusCode == 422
	}
	return false
}

func buildLLMTranslationResult(content string, markers []string) (*translationResult, error) {
	parsed, err := parseLLMTranslationPayload(content, markers)
	if err != nil {
		return &translationResult{rawLLM: content}, &TranslationError{
			Kind:    TranslationErrorParse,
			Message: err.Error(),
		}
	}
	return &translationResult{texts: parsed, rawLLM: content}, nil
}

func decodeOpenAIChatTranslation(provider string, respBody []byte, markers []string) (*translationResult, error) {
	var decoded openAIChatResponse
	if err := json.Unmarshal(respBody, &decoded); err != nil {
		return nil, fmt.Errorf("failed to decode %s response: %w", provider, err)
	}
	if len(decoded.Choices) == 0 {
		return nil, fmt.Errorf("%s response contained no choices", provider)
	}

	return buildLLMTranslationResult(extractContentString(decoded.Choices[0].Message.Content), markers)
}

func (s *Service) executeOpenAIChatTranslation(ctx context.Context, opts openAIChatCallOptions) (*translationResult, error) {
	body, err := json.Marshal(opts.request)
	if err != nil {
		return nil, err
	}

	url := opts.baseURL + opts.endpoint
	movieIDTag := ""
	if mid, ok := ctx.Value(translationMovieIDKey{}).(string); ok && mid != "" {
		movieIDTag = "[" + mid + "] "
	}
	logging.Debugf("Translation %s(%s): POST %s model=%s texts=%d", movieIDTag, opts.provider, url, opts.model, len(opts.markers))
	logging.Debugf("Translation %s(%s): system prompt: %s", movieIDTag, opts.provider, opts.request.Messages[0].Content)
	if opts.logInput && len(opts.request.Messages) > 1 {
		logging.Debugf("Translation %s(%s): input: %s", movieIDTag, opts.provider, opts.request.Messages[1].Content)
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
		logging.Debugf("Translation %s(%s): sending request...", movieIDTag, opts.provider)
		start = time.Now()
	}

	resp, err := s.httpClient.Do(req)
	if err != nil {
		if opts.logTiming {
			return nil, fmt.Errorf("%s request failed after %v: %w", opts.provider, time.Since(start), err)
		}
		return nil, err
	}
	if opts.logTiming {
		logging.Debugf("Translation %s(%s): response received in %v (status %d)", movieIDTag, opts.provider, time.Since(start), resp.StatusCode)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, maxTranslationResponseSize))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, &TranslationError{
			Kind:       TranslationErrorHTTPStatus,
			StatusCode: resp.StatusCode,
			Message:    fmt.Sprintf("%s translation failed with status %d: %s", opts.provider, resp.StatusCode, string(respBody)),
		}
	}

	logging.Debugf("Translation %s(%s): response: %s", movieIDTag, opts.provider, string(respBody))
	return decodeOpenAIChatTranslation(opts.provider, respBody, opts.markers)
}

func (s *Service) translateWithOpenAI(ctx context.Context, sourceLang, targetLang string, texts []string, fieldNames []string) (*translationResult, error) {
	baseURL := strings.TrimRight(strings.TrimSpace(s.cfg.OpenAI.BaseURL), "/")
	if baseURL == "" {
		baseURL = "https://api.openai.com/v1"
	}

	apiKey := strings.TrimSpace(s.cfg.OpenAI.APIKey)
	if apiKey == "" {
		return nil, fmt.Errorf("openai api_key is required")
	}

	model := strings.TrimSpace(s.cfg.OpenAI.Model)
	if model == "" {
		model = "gpt-4o-mini"
	}

	systemPrompt, userPrompt, markers, err := buildLLMTranslationPrompts(sourceLang, targetLang, texts, fieldNames)
	if err != nil {
		return nil, err
	}

	return s.executeOpenAIChatTranslation(ctx, openAIChatCallOptions{
		provider: providerOpenAI,
		baseURL:  baseURL,
		endpoint: "/chat/completions",
		model:    model,
		headers: map[string]string{
			"Authorization": "Bearer " + apiKey,
		},
		request: openAIChatRequest{
			Model:       model,
			Temperature: 0,
			Messages: []openAIChatMessage{
				{Role: "system", Content: systemPrompt},
				{Role: "user", Content: userPrompt},
			},
		},
		markers: markers,
	})
}

func (s *Service) translateActressNamesWithOpenAI(ctx context.Context, sourceLang, targetLang string, texts []string) (*translationResult, error) {
	baseURL := strings.TrimRight(strings.TrimSpace(s.cfg.OpenAI.BaseURL), "/")
	if baseURL == "" {
		baseURL = "https://api.openai.com/v1"
	}

	apiKey := strings.TrimSpace(s.cfg.OpenAI.APIKey)
	if apiKey == "" {
		return nil, fmt.Errorf("openai api_key is required")
	}

	model := strings.TrimSpace(s.cfg.OpenAI.Model)
	if model == "" {
		model = "gpt-4o-mini"
	}

	systemPrompt, userPrompt, err := buildActressTranslationPrompts(sourceLang, targetLang, texts)
	if err != nil {
		return nil, err
	}

	return s.executeOpenAIChatTranslation(ctx, openAIChatCallOptions{
		provider: providerOpenAI,
		baseURL:  baseURL,
		endpoint: "/chat/completions",
		model:    model,
		headers: map[string]string{
			"Authorization": "Bearer " + apiKey,
		},
		request: openAIChatRequest{
			Model:       model,
			Temperature: 0,
			Messages: []openAIChatMessage{
				{Role: "system", Content: systemPrompt},
				{Role: "user", Content: userPrompt},
			},
		},
		markers: makeJZMarkers(len(texts)),
	})
}

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

func (s *Service) translateWithOpenAICompatible(ctx context.Context, sourceLang, targetLang string, texts []string, fieldNames []string) (*translationResult, error) {
	baseURL := strings.TrimRight(strings.TrimSpace(s.cfg.OpenAICompatible.BaseURL), "/")
	if baseURL == "" {
		baseURL = "http://localhost:11434/v1"
	}

	apiKey := strings.TrimSpace(s.cfg.OpenAICompatible.APIKey)
	model := strings.TrimSpace(s.cfg.OpenAICompatible.Model)
	if model == "" {
		return nil, fmt.Errorf("openai-compatible model is required")
	}

	systemPrompt, userPrompt, markers, err := buildLLMTranslationPrompts(sourceLang, targetLang, texts, fieldNames)
	if err != nil {
		return nil, err
	}

	headers := map[string]string{}
	if apiKey != "" {
		headers["Authorization"] = "Bearer " + apiKey
	}

	baseRequest := openAIChatRequest{
		Model:       model,
		Temperature: 0,
		Messages: []openAIChatMessage{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: userPrompt},
		},
	}

	thinkingEnabled := s.cfg.OpenAICompatible.EffectiveEnableThinking()
	strategies := buildOpenAICompatibleThinkingStrategies(baseURL, model, s.cfg.OpenAICompatible)

	var lastErr error
	for _, strategy := range strategies {
		request := applyOpenAICompatibleThinkingStrategy(baseRequest, strategy, thinkingEnabled)
		result, err := s.executeOpenAIChatTranslation(ctx, openAIChatCallOptions{
			provider:  providerOpenAICompatible,
			baseURL:   baseURL,
			endpoint:  "/chat/completions",
			model:     model,
			headers:   headers,
			request:   request,
			markers:   markers,
			logInput:  true,
			logTiming: true,
		})
		if err == nil {
			return result, nil
		}

		lastErr = err
		if strategy == openAICompatibleThinkingStrategyNone || !isRetryableThinkingStrategyError(err) {
			return nil, err
		}

		logging.Debugf("Translation (openai-compatible): thinking strategy %q failed (%v), trying fallback", strategy, err)
	}

	return nil, lastErr
}

func (s *Service) translateActressNamesWithOpenAICompatible(ctx context.Context, sourceLang, targetLang string, texts []string) (*translationResult, error) {
	baseURL := strings.TrimRight(strings.TrimSpace(s.cfg.OpenAICompatible.BaseURL), "/")
	if baseURL == "" {
		baseURL = "http://localhost:11434/v1"
	}

	apiKey := strings.TrimSpace(s.cfg.OpenAICompatible.APIKey)
	model := strings.TrimSpace(s.cfg.OpenAICompatible.Model)
	if model == "" {
		return nil, fmt.Errorf("openai-compatible model is required")
	}

	systemPrompt, userPrompt, err := buildActressTranslationPrompts(sourceLang, targetLang, texts)
	if err != nil {
		return nil, err
	}

	headers := map[string]string{}
	if apiKey != "" {
		headers["Authorization"] = "Bearer " + apiKey
	}

	baseRequest := openAIChatRequest{
		Model:       model,
		Temperature: 0,
		Messages: []openAIChatMessage{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: userPrompt},
		},
	}

	thinkingEnabled := s.cfg.OpenAICompatible.EffectiveEnableThinking()
	strategies := buildOpenAICompatibleThinkingStrategies(baseURL, model, s.cfg.OpenAICompatible)

	var lastErr error
	for _, strategy := range strategies {
		request := applyOpenAICompatibleThinkingStrategy(baseRequest, strategy, thinkingEnabled)
		result, err := s.executeOpenAIChatTranslation(ctx, openAIChatCallOptions{
			provider:  providerOpenAICompatible,
			baseURL:   baseURL,
			endpoint:  "/chat/completions",
			model:     model,
			headers:   headers,
			request:   request,
			markers:   makeJZMarkers(len(texts)),
			logInput:  true,
			logTiming: true,
		})
		if err == nil {
			return result, nil
		}

		lastErr = err
		if strategy == openAICompatibleThinkingStrategyNone || !isRetryableThinkingStrategyError(err) {
			return nil, err
		}

		logging.Debugf("Translation (openai-compatible actress): thinking strategy %q failed (%v), trying fallback", strategy, err)
	}

	return nil, lastErr
}
