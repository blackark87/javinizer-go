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

type MovieResult struct {
	Title       Result
	Description *Result
}

func ReviewField(
	ctx context.Context,
	tc config.TranslationConfig,
	field string,
	source string,
	actresses []models.Actress,
) (Result, error) {
	if err := ValidateConfig(tc); err != nil {
		return Result{}, err
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

// ReviewMovie retranslates a retained title and optional description in one
// first-pass request and one second-pass review request.
func ReviewMovie(
	ctx context.Context,
	tc config.TranslationConfig,
	titleSource string,
	descriptionSource string,
	actresses []models.Actress,
) (MovieResult, error) {
	if err := ValidateConfig(tc); err != nil {
		return MovieResult{}, err
	}
	titleSource = strings.TrimSpace(titleSource)
	descriptionSource = strings.TrimSpace(descriptionSource)
	if titleSource == "" {
		return MovieResult{}, fmt.Errorf("retained Japanese source for title is unavailable")
	}

	freshConfig := movieConfig(tc, descriptionSource != "")
	service := translation.New(freshConfig)
	freshOutput, _, err := service.TranslateMovie(
		ctx,
		&models.Movie{
			Title:       titleSource,
			Description: descriptionSource,
			Actresses:   actresses,
		},
		freshConfig.SettingsHash(),
	)
	if err != nil {
		return MovieResult{}, fmt.Errorf("fresh translation failed: %w", err)
	}

	titleCandidate := translatedField(freshOutput, "title")
	if strings.TrimSpace(titleCandidate) == "" {
		return MovieResult{}, fmt.Errorf("fresh translator returned an empty title")
	}
	fields := []translation.QualityReviewField{{
		FieldName: "quality_review_title",
		Source:    titleSource,
		Candidate: titleCandidate,
		Actresses: actresses,
	}}
	descriptionCandidate := ""
	if descriptionSource != "" {
		descriptionCandidate = translatedField(freshOutput, "description")
		if strings.TrimSpace(descriptionCandidate) == "" {
			return MovieResult{}, fmt.Errorf("fresh translator returned an empty description")
		}
		fields = append(fields, translation.QualityReviewField{
			FieldName: "quality_review_description",
			Source:    descriptionSource,
			Candidate: descriptionCandidate,
			Actresses: actresses,
		})
	}

	reviewed, err := service.ReviewJAVTranslations(ctx, fields)
	if err != nil {
		return MovieResult{}, fmt.Errorf("translation review failed: %w", err)
	}
	if len(reviewed) != len(fields) {
		return MovieResult{}, fmt.Errorf("translation reviewer returned %d results for %d fields", len(reviewed), len(fields))
	}
	for index := range reviewed {
		reviewed[index] = strings.TrimSpace(reviewed[index])
		if reviewed[index] == "" {
			return MovieResult{}, fmt.Errorf("translation reviewer returned an empty result")
		}
	}

	result := MovieResult{
		Title: Result{
			Value:          reviewed[0],
			TargetLanguage: freshConfig.TargetLanguage,
			SettingsHash:   freshConfig.SettingsHash(),
		},
	}
	if descriptionSource != "" {
		result.Description = &Result{
			Value:          reviewed[1],
			TargetLanguage: freshConfig.TargetLanguage,
			SettingsHash:   freshConfig.SettingsHash(),
		}
	}
	return result, nil
}

func ValidateConfig(tc config.TranslationConfig) error {
	provider := strings.ToLower(strings.TrimSpace(tc.Provider))
	if !tc.Enabled {
		return fmt.Errorf("metadata translation is disabled")
	}
	if provider != "openai" && provider != "openai-compatible" {
		return fmt.Errorf("translation review requires an OpenAI chat provider, got %s", provider)
	}
	return nil
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

func movieConfig(tc config.TranslationConfig, includeDescription bool) config.TranslationConfig {
	tc.ApplyToPrimary = false
	tc.Fields = config.TranslationFieldsConfig{
		Title:       true,
		Description: includeDescription,
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
