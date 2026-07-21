package translation

import (
	"errors"
	"net/url"
	"strings"
)

// openAICompatibleThinkingStrategy represents a strategy for enabling/disabling
// thinking mode on OpenAI-compatible backends (vLLM, Ollama, llama.cpp).
type openAICompatibleThinkingStrategy string

const (
	openAICompatibleThinkingStrategyChatTemplateKwargs openAICompatibleThinkingStrategy = "chat_template_kwargs"
	openAICompatibleThinkingStrategyReasoningEffort    openAICompatibleThinkingStrategy = "reasoning_effort"
	openAICompatibleThinkingStrategyEnableThinking     openAICompatibleThinkingStrategy = "enable_thinking"
	openAICompatibleThinkingStrategyNone               openAICompatibleThinkingStrategy = "none"
)

// buildOpenAICompatibleThinkingStrategies returns the ordered list of thinking
// strategies to try for the given backend type and configuration.
func buildOpenAICompatibleThinkingStrategies(baseURL, model string, cfg openAICompatibleConfig) []openAICompatibleThinkingStrategy {
	strategies := preferredOpenAICompatibleThinkingStrategies(baseURL, model, cfg)
	mode := normalizeThinkingMode(cfg.ThinkingMode)
	if !cfg.EnableThinking || mode == "boolean" {
		return removeThinkingStrategy(strategies, openAICompatibleThinkingStrategyReasoningEffort)
	}
	return prioritizeThinkingStrategy(strategies, openAICompatibleThinkingStrategyReasoningEffort)
}

func preferredOpenAICompatibleThinkingStrategies(baseURL, model string, cfg openAICompatibleConfig) []openAICompatibleThinkingStrategy {
	switch cfg.BackendType {
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

func normalizeThinkingMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "low", "medium", "high":
		return strings.ToLower(strings.TrimSpace(mode))
	default:
		return "boolean"
	}
}

func removeThinkingStrategy(strategies []openAICompatibleThinkingStrategy, target openAICompatibleThinkingStrategy) []openAICompatibleThinkingStrategy {
	filtered := make([]openAICompatibleThinkingStrategy, 0, len(strategies))
	for _, strategy := range strategies {
		if strategy != target {
			filtered = append(filtered, strategy)
		}
	}
	return filtered
}

func prioritizeThinkingStrategy(strategies []openAICompatibleThinkingStrategy, target openAICompatibleThinkingStrategy) []openAICompatibleThinkingStrategy {
	ordered := []openAICompatibleThinkingStrategy{target}
	for _, strategy := range strategies {
		if strategy != target {
			ordered = append(ordered, strategy)
		}
	}
	return ordered
}

// applyOpenAICompatibleThinkingStrategy applies the given thinking strategy to
// an OpenAI chat request, returning a modified copy.
func applyOpenAICompatibleThinkingStrategy(base openAIChatRequest, strategy openAICompatibleThinkingStrategy, enabled bool, thinkingMode ...string) openAIChatRequest {
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
			req.ReasoningEffort = normalizeReasoningEffort(firstThinkingMode(thinkingMode))
		} else {
			req.ReasoningEffort = "none"
		}
	case openAICompatibleThinkingStrategyEnableThinking:
		req.EnableThinking = &enabled
	}

	return req
}

func firstThinkingMode(modes []string) string {
	if len(modes) == 0 {
		return "boolean"
	}
	return modes[0]
}

func normalizeReasoningEffort(mode string) string {
	switch normalizeThinkingMode(mode) {
	case "low", "medium", "high":
		return normalizeThinkingMode(mode)
	default:
		return "medium"
	}
}

// isRetryableThinkingStrategyError checks whether a thinking strategy error
// is worth retrying with the next strategy.
func isRetryableThinkingStrategyError(err error) bool {
	var te *translationError
	if errors.As(err, &te) && te.Kind == TranslationErrorHTTPStatus {
		// A malformed model reasoning stream is stochastic output, not an
		// unsupported thinking-control field. Retrying the same preferred
		// strategy is useful; changing control formats is not.
		if isModelOutputFormatError(te) {
			return false
		}
		return te.StatusCode == 400 || te.StatusCode == 422
	}
	return false
}

func isModelOutputFormatError(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "does not match the expected peg-") ||
		strings.Contains(message, "model produced output") && strings.Contains(message, "format")
}

// looksLikeOllamaBaseURL heuristically detects an Ollama server from its URL.
func looksLikeOllamaBaseURL(baseURL string) bool {
	parsed, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil {
		return false
	}

	host := strings.ToLower(parsed.Host)
	return strings.Contains(host, "ollama") || strings.HasSuffix(host, ":11434")
}

// looksLikeLlamaCppBackend heuristically detects a llama.cpp server from its
// URL or model name.
func looksLikeLlamaCppBackend(baseURL, model string) bool {
	parsed, err := url.Parse(strings.TrimSpace(baseURL))
	if err == nil {
		host := strings.ToLower(parsed.Host)
		path := strings.ToLower(parsed.Path)
		// LM Studio's local OpenAI-compatible server defaults to port 1234 and
		// uses llama.cpp-style thinking controls.
		if strings.Contains(host, "llama") || strings.Contains(path, "llama") || parsed.Port() == "1234" {
			return true
		}
	}

	model = strings.ToLower(strings.TrimSpace(model))
	return strings.Contains(model, ".gguf") || strings.Contains(model, "gguf")
}
