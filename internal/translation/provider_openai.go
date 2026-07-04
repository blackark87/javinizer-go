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

// Field-name labels whose content is a performer's name; the prompt instructs
// phonetic transliteration (never semantic translation) for these labels.
const fieldNameTitleAsName = "title_as_name"

func buildLLMTranslationPrompts(sourceLang, targetLang string, texts []string, fieldNames []string) (string, string, []string, error) {
	if len(texts) == 0 || len(fieldNames) != len(texts) {
		return "", "", nil, fmt.Errorf("translation prompt requires one field name per text (%d names for %d texts)", len(fieldNames), len(texts))
	}

	markers := make([]string, len(texts))
	for i, fn := range fieldNames {
		markers[i] = "<<<" + fn + ">>>"
	}

	terminologyRules := "(1) Body measurements: use natural target-language equivalents (e.g. 股下 → 다리 길이, NOT 가랑이 길이). " +
		"(2) Kanji adult slang: translate by meaning using vocabulary natural to adult content in the target language — never just read the characters (e.g. 美脚 → 각선미; 爆乳 → 폭유; 神乳 → 신의 가슴; 中出し → 질내 사정; 騎乗位 → 기승위; 背面騎乗位 → 후배위 기승위; 杭打ち騎乗位 → 말뚝박기 기승위; デカ尻 → 큰 엉덩이; 尻コキ → 엉덩이 성교). " +
		"(3) Japanese kana loanwords commonly borrowed into the target language: keep the standard phonetic form (e.g. パパ活 → 파파카츠, NOT 파파활; ぶっかけ → 부카케, NOT 버카케; フェラ → 펠라). " +
		"(4) Translate ALL text in each item, including any Latin or English portions — do not leave any part untranslated. " +
		"(5) Use correct, standard target-language spelling — for Korean write 엉덩이 (NOT 엉둥이), 가슴, 사정. Do not invent or misspell words. "
	personNameRule := "Person-name rule: fields labeled <<<actress[N]>>> or <<<title_as_name>>> contain a performer's name. Transliterate it phonetically into the target language — never translate its meaning (for Korean use Hangul: なつ → 나츠 and 夏 → 나츠, NOT 여름). " +
		"Keep Japanese name order: FamilyName GivenName (e.g. 하타노 유이, not 유이 하타노). " +
		"If the input is romanized Latin text, that romaji spelling is the AUTHORITATIVE reading of the name. " +
		"NEVER substitute a different reading you believe is correct for this person, and NEVER re-derive the reading from kanji appearing elsewhere in the input — the same kanji can have multiple readings and the romaji fixes which one is right. " +
		"Transliterate the romaji syllables literally as Hepburn Japanese, not as an English word: Rena → 레나 (NOT 레이나), Reina → 레이나. " +
		"Do NOT write Japanese long vowels in Korean — drop the lengthening vowel: Yuu → 유 (NOT 유우), Tarou → 타로, Reena → 레나, Ohno → 오노. Keep distinct vowels (Yui → 유이, Aoi → 아오이). " +
		"The OUTPUT must be written in the target language's script (Hangul for a Korean target) — returning the romaji/Latin input unchanged is an ERROR: Miyashita Rena → 미야시타 레나, never Miyashita Rena. " +
		"Ignore non-name extras such as age (歳), cup size, height, or occupation. If the field contains no personal name at all, return an empty string for it. " +
		"Also apply this rule to a <<<title>>> that is a short personal-name-like Japanese string (especially kana-only). "
	properNounRule := "Proper-noun rule: fields labeled <<<maker>>>, <<<label>>>, or <<<director>>> are studio/brand/person names. Transliterate them phonetically; do NOT translate their meaning and do NOT embellish them into marketing copy. "
	titleCleanupRule := "Title cleanup rule: fields labeled <<<title>>> may include VR format labels such as [VR], 【VR】, 【8K VR】, or similar bracketed VR markers. Exclude those VR labels entirely; do NOT translate, keep, or add them. "
	descriptionCleanupRule := "Description cleanup rule: fields labeled <<<description>>> may contain platform notices or store promotions. Exclude those phrases entirely; do NOT translate or keep them. Examples to exclude include binaural/playback/device notices such as この作品はバイノーラル録音, この商品は専用プレイヤー, VR専用作品, 動作環境・対応デバイス, 配信方法によって収録内容が異なる場合があります, and store campaign text such as 最新作やセール商品など、お得な情報満載 and KMPストアはこちら. If the description contains only excluded material, return an empty string for <<<description>>>. "
	embeddedHangulRule := "Embedded-Hangul rule: any Hangul (Korean script) text already present in the input is final translated content — copy it into the output verbatim, never alter, re-transliterate, or translate it. "
	placeholderRule := "Placeholder rule: tokens of the form ⟦N⟧ (a digit inside these bracket characters) are protected placeholders standing in for a name. Reproduce each token EXACTLY as-is (same bracket characters, same digit) in a natural position in the output; never translate, transliterate, remove, renumber, or add spaces inside them. "

	systemPrompt := fmt.Sprintf("You are a translator specializing in Japanese adult video (JAV) content metadata. Translate each labeled section into natural, engaging promotional copy — not a literal word-for-word translation. Titles should be concise and enticing; descriptions should read as sensual, persuasive marketing blurbs in the target language. Follow these terminology rules: "+
		terminologyRules+
		personNameRule+
		properNounRule+
		titleCleanupRule+
		descriptionCleanupRule+
		embeddedHangulRule+
		placeholderRule+
		"CRITICAL labeling rule: translate the text under each <<<label>>> and return it under the SAME <<<label>>> — never merge multiple sections into one label, never omit a label, never swap labels. "+
		"Return ONLY the labeled output markers with their translations. Do not use JSON. Do not add commentary. Keep each translation on a single logical line; if needed, replace internal newlines with spaces. Source language: %s. Target language: %s.", sourceLang, targetLang)

	var userPrompt strings.Builder
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

	return systemPrompt, strings.TrimSpace(userPrompt.String()), markers, nil
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

func (s *Service) translateWithOpenAI(ctx context.Context, systemPrompt, userPrompt string, markers []string) (*translationResult, error) {
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

func (s *Service) translateWithOpenAICompatible(ctx context.Context, systemPrompt, userPrompt string, markers []string) (*translationResult, error) {
	baseURL := strings.TrimRight(strings.TrimSpace(s.cfg.OpenAICompatible.BaseURL), "/")
	if baseURL == "" {
		baseURL = "http://localhost:11434/v1"
	}

	apiKey := strings.TrimSpace(s.cfg.OpenAICompatible.APIKey)
	model := strings.TrimSpace(s.cfg.OpenAICompatible.Model)
	if model == "" {
		return nil, fmt.Errorf("openai-compatible model is required")
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
			// Keep the result alongside the error: a parse failure carries rawLLM,
			// which the retry logic in translateTexts needs to classify the error.
			return result, err
		}

		logging.Debugf("Translation (openai-compatible): thinking strategy %q failed (%v), trying fallback", strategy, err)
	}

	return nil, lastErr
}
