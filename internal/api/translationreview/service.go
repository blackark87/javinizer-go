package translationreview

import (
	"context"
	"fmt"
	"strings"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/translation"
)

type Result struct {
	Value          string
	TargetLanguage string
	SettingsHash   string
}

func ReviewField(
	ctx context.Context,
	tc config.TranslationConfig,
	field string,
	source string,
	actresses []models.Actress,
) (Result, error) {
	provider := strings.ToLower(strings.TrimSpace(tc.Provider))
	if !tc.Enabled {
		return Result{}, fmt.Errorf("metadata translation is disabled")
	}
	if provider != "openai" && provider != "openai-compatible" {
		return Result{}, fmt.Errorf("translation review requires an OpenAI chat provider, got %s", provider)
	}
	if field != "title" && field != "description" {
		return Result{}, fmt.Errorf("unsupported translation review field %q", field)
	}
	source = strings.TrimSpace(source)
	if source == "" {
		return Result{}, fmt.Errorf("retained Japanese source for %s is unavailable", field)
	}

	freshConfig := fieldConfig(tc, field)
	service := translation.New(freshConfig)
	freshMovie := &models.Movie{Actresses: actresses}
	if field == "title" {
		freshMovie.Title = source
	} else {
		freshMovie.Description = source
	}
	freshOutput, _, err := service.TranslateMovie(
		ctx,
		freshMovie,
		freshConfig.SettingsHash(),
	)
	if err != nil {
		return Result{}, fmt.Errorf("fresh translation failed: %w", err)
	}
	freshCandidate := translatedField(freshOutput, field)
	if strings.TrimSpace(freshCandidate) == "" {
		return Result{}, fmt.Errorf("fresh translator returned an empty result")
	}

	reviewed, err := service.ReviewJAVTranslations(ctx, []translation.QualityReviewField{{
		FieldName: "quality_review_" + field,
		Source:    source,
		Candidate: freshCandidate,
		Actresses: actresses,
	}})
	if err != nil {
		return Result{}, fmt.Errorf("translation review failed: %w", err)
	}
	if len(reviewed) != 1 || strings.TrimSpace(reviewed[0]) == "" {
		return Result{}, fmt.Errorf("translation reviewer returned an empty result")
	}
	return Result{
		Value:          strings.TrimSpace(reviewed[0]),
		TargetLanguage: freshConfig.TargetLanguage,
		SettingsHash:   freshConfig.SettingsHash(),
	}, nil
}

func fieldConfig(tc config.TranslationConfig, field string) config.TranslationConfig {
	tc.ApplyToPrimary = false
	tc.Fields = config.TranslationFieldsConfig{
		Title:       field == "title",
		Description: field == "description",
	}
	if len(tc.TargetLanguages) > 0 {
		tc.TargetLanguage = tc.TargetLanguages[0]
		tc.TargetLanguages = []string{tc.TargetLanguage}
	}
	return tc
}

func translatedField(output *translation.TranslationOutput, field string) string {
	if output == nil || output.Movie == nil {
		return ""
	}
	if field == "title" {
		return output.Movie.Title
	}
	return output.Movie.Description
}
