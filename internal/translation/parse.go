package translation

import (
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

func parseLLMTranslationPayload(payload string, markers []string) ([]string, error) {
	cleaned := normalizeTranslationPayload(payload)
	if len(markers) == 0 || !strings.Contains(cleaned, markers[0]) {
		return nil, fmt.Errorf("failed to parse translated output payload: first output marker not found")
	}
	parsed, err := parseCompactTranslationPayload(cleaned, markers)
	if err != nil {
		return nil, err
	}
	logging.Debugf("Translation: parseLLMTranslationPayload parsed %d compact tagged items", len(parsed))
	return parsed, nil
}

func parseCompactTranslationPayload(payload string, markers []string) ([]string, error) {
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
		content := embeddedMarkerRE.ReplaceAllString(raw, "")
		if content != raw {
			logging.Debugf("Translation: stripped embedded markers from slot %d (had: %q, now: %q)", i, strings.TrimSpace(raw), strings.TrimSpace(content))
		}
		out = append(out, strings.TrimSpace(content))
		pos = end
	}

	return out, nil
}
