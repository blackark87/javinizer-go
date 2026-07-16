package organizer

import (
	"fmt"
	"strings"
)

// LinkMode selects how organized files reference their source (none, hard link, or soft link).
type LinkMode string

// LinkMode values selecting how organized files link to their source.
const (
	LinkModeNone LinkMode = ""
	LinkModeHard LinkMode = "hard"
	LinkModeSoft LinkMode = "soft"
)

// ParseLinkMode parses a link mode string (none, hard, soft) and returns the corresponding LinkMode.
func ParseLinkMode(raw string) (LinkMode, error) {
	normalized := strings.ToLower(strings.TrimSpace(raw))
	switch normalized {
	case "", "none":
		return LinkModeNone, nil
	case string(LinkModeHard):
		return LinkModeHard, nil
	case string(LinkModeSoft):
		return LinkModeSoft, nil
	default:
		return LinkModeNone, fmt.Errorf("invalid link mode %q (expected one of: none, hard, soft)", raw)
	}
}

// IsValid reports whether the LinkMode is one of the known modes.
func (m LinkMode) IsValid() bool {
	switch m {
	case LinkModeNone, LinkModeHard, LinkModeSoft:
		return true
	default:
		return false
	}
}
