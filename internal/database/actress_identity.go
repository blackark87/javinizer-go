package database

import (
	"strings"
	"unicode"
)

// normalizeExactActressName produces the strict identity key used when a
// DMM-ID-less actress row is considered for reuse. Punctuation and spacing are
// ignored, but aliases and translated names are deliberately not consulted.
func normalizeExactActressName(value string) string {
	var normalized strings.Builder
	for _, char := range strings.ToLower(strings.TrimSpace(value)) {
		if unicode.IsLetter(char) || unicode.IsNumber(char) {
			normalized.WriteRune(char)
		}
	}
	return normalized.String()
}
