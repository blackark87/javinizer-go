package aggregator

import (
	"strings"

	"github.com/javinizer/javinizer-go/internal/models"
)

// mergeOrAppendTranslation merges or appends an incoming translation to existing translations
func mergeOrAppendTranslation(
	existing []models.MovieTranslation,
	incoming models.MovieTranslation,
	overwrite bool,
) []models.MovieTranslation {
	targetLanguage := strings.ToLower(strings.TrimSpace(incoming.Language))
	if targetLanguage == "" {
		return existing
	}

	for i := range existing {
		if strings.ToLower(strings.TrimSpace(existing[i].Language)) != targetLanguage {
			continue
		}

		if overwrite {
			existing[i] = mergeTranslationFields(existing[i], incoming)
		}
		return existing
	}

	return append(existing, incoming)
}

// mergeTranslationFields merges incoming translation fields into current translation
func mergeTranslationFields(current, incoming models.MovieTranslation) models.MovieTranslation {
	merged := current
	merged.Language = incoming.Language

	if incoming.Title != "" {
		merged.Title = incoming.Title
	}
	if incoming.OriginalTitle != "" {
		merged.OriginalTitle = incoming.OriginalTitle
	}
	if incoming.Description != "" {
		merged.Description = incoming.Description
	}
	if incoming.Actresses != nil {
		merged.Actresses = append([]string(nil), incoming.Actresses...)
	}
	if incoming.Director != "" {
		merged.Director = incoming.Director
	}
	if incoming.Maker != "" {
		merged.Maker = incoming.Maker
	}
	if incoming.Label != "" {
		merged.Label = incoming.Label
	}
	if incoming.Series != "" {
		merged.Series = incoming.Series
	}
	if incoming.SourceName != "" {
		merged.SourceName = incoming.SourceName
	}
	if incoming.SettingsHash != "" {
		merged.SettingsHash = incoming.SettingsHash
	}

	return merged
}
