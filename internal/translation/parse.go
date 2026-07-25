package translation

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/javinizer/javinizer-go/internal/logging"
)

// embeddedMarkerRE matches <<<...>>> markers that the LLM may echo verbatim
// from the prompt template. These are never valid translation content.
var embeddedMarkerRE = regexp.MustCompile(`<<<[\w\[\]]+>>>`)

func normalizeTranslationPayload(payload string) string {
	cleaned := strings.TrimSpace(payload)
	cleaned = strings.TrimPrefix(cleaned, "```json")
	cleaned = strings.TrimPrefix(cleaned, "```")
	cleaned = strings.TrimSuffix(cleaned, "```")
	return strings.TrimSpace(cleaned)
}

// parseStringArrayPayload remains available for compatibility with legacy
// provider responses and diagnostics. Semantic LLM requests do not use this
// path because their labels are required to preserve field boundaries.
func parseStringArrayPayload(payload string) ([]string, error) {
	cleaned := normalizeTranslationPayload(payload)
	if result, err := unmarshalStringArray(cleaned); err == nil {
		return result, nil
	}
	start := strings.IndexByte(cleaned, '[')
	end := strings.LastIndexByte(cleaned, ']')
	if start >= 0 && end > start {
		if result, err := unmarshalStringArray(cleaned[start : end+1]); err == nil {
			return result, nil
		}
	}
	return nil, fmt.Errorf("failed to parse translated output payload as JSON string array")
}

func unmarshalStringArray(payload string) ([]string, error) {
	var result []string
	if err := json.Unmarshal([]byte(payload), &result); err != nil {
		return nil, err
	}
	return result, nil
}

func parseLLMTranslationPayload(payload string, markerSpec any) ([]string, error) {
	cleaned := normalizeTranslationPayload(payload)
	markers := normalizeTranslationMarkers(markerSpec)
	if len(markers) == 0 || !strings.Contains(cleaned, markers[0]) {
		if legacy, err := parseStringArrayPayload(cleaned); err == nil {
			logging.Debugf("Translation: accepted legacy JSON response with %d items", len(legacy))
			return legacy, nil
		}
		// Compatibility fallback for responses produced by the upstream indexed
		// marker format while requests now use semantic field labels.
		fallback := indexedTranslationMarkers(len(markers))
		if len(fallback) == 0 || !strings.Contains(cleaned, fallback[0]) {
			// Some models omit the requested marker when reviewing only one output
			// slot. In that unambiguous review case, treat the whole response as the
			// slot; ordinary translations and multi-slot reviews still require their
			// markers so malformed provider responses are not silently accepted.
			if len(markers) == 1 && strings.HasPrefix(markers[0], "<<<quality_review_") && cleaned != "" {
				logging.Debugf("Translation: accepted unmarked single-item response")
				return []string{cleaned}, nil
			}
			return nil, fmt.Errorf("failed to parse translated output payload: first output marker not found")
		}
		markers = fallback
	}
	parsed, err := parseCompactTranslationPayload(cleaned, markers)
	if err != nil {
		return nil, err
	}
	logging.Debugf("Translation: parseLLMTranslationPayload parsed %d compact tagged items", len(parsed))
	return parsed, nil
}

func parseCompactTranslationPayload(payload string, markerSpec any) ([]string, error) {
	markers := normalizeTranslationMarkers(markerSpec)
	if len(markers) == 0 {
		return nil, fmt.Errorf("failed to parse compact translation payload: no output markers")
	}

	// Chat models sometimes echo the entire user prompt before returning the
	// actual marked translation. The prompt itself contains a complete marker
	// sequence, so selecting the first sequence silently treats the source text
	// as translated output. Try marker groups from the end and accept only the
	// last complete, internally clean group.
	starts := markerStartPositions(payload, markers[0])
	var lastErr error
	for i := len(starts) - 1; i >= 0; i-- {
		parsed, err := parseCompactTranslationPayloadAt(payload[starts[i]:], markers)
		if err == nil {
			if i != 0 {
				logging.Debugf("Translation: selected the last complete marker set from %d candidates", len(starts))
			}
			return parsed, nil
		}
		lastErr = err
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, fmt.Errorf("failed to parse compact translation payload: missing output marker 0")
}

func markerStartPositions(payload, marker string) []int {
	var positions []int
	for offset := 0; offset < len(payload); {
		index := strings.Index(payload[offset:], marker)
		if index < 0 {
			break
		}
		absolute := offset + index
		positions = append(positions, absolute)
		offset = absolute + len(marker)
	}
	return positions
}

func parseCompactTranslationPayloadAt(payload string, markers []string) ([]string, error) {
	pos := 0
	out := make([]string, 0, len(markers))

	for i, startToken := range markers {
		start := strings.Index(payload[pos:], startToken)
		if start < 0 {
			return nil, fmt.Errorf("failed to parse compact translation payload: missing output marker %d", i)
		}
		start += pos + len(startToken)

		end := len(payload)
		if i+1 < len(markers) {
			nextToken := markers[i+1]
			next := strings.Index(payload[start:], nextToken)
			if next < 0 {
				return nil, fmt.Errorf("failed to parse compact translation payload: missing output marker %d", i+1)
			}
			end = start + next
		}

		raw := payload[start:end]
		if embeddedMarkerRE.MatchString(raw) {
			return nil, fmt.Errorf("failed to parse compact translation payload: unexpected embedded output marker in slot %d", i)
		}
		out = append(out, strings.TrimSpace(raw))
		pos = end
	}

	return out, nil
}

func normalizeTranslationMarkers(markerSpec any) []string {
	switch value := markerSpec.(type) {
	case []string:
		return append([]string(nil), value...)
	case int:
		return indexedTranslationMarkers(value)
	default:
		return nil
	}
}

func indexedTranslationMarkers(count int) []string {
	if count <= 0 {
		return nil
	}
	markers := make([]string, count)
	for i := range markers {
		markers[i] = translationCompactOutputMarker(i)
	}
	return markers
}
