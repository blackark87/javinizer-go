package translation

import (
	"strings"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildTranslationPlanProtectsJapaneseActressNameWhitespaceVariants(t *testing.T) {
	service := New(Config{Fields: fieldsConfig{Description: true}})
	for _, sourceName := range []string{"野々浦暖", "野々浦 暖", "野々浦　暖"} {
		t.Run(sourceName, func(t *testing.T) {
			movie := &models.Movie{
				Description: sourceName + "が出演する作品",
				Actresses: []models.Actress{{
					JapaneseName: "野々浦暖",
					ThumbURL:     "https://pics.dmm.co.jp/mono/actjpgs/nonoura_non.jpg",
				}},
			}

			plan := service.BuildTranslationPlan(movie, "ko", "ja", "test")
			require.Len(t, plan.Fields, 1)
			field := plan.Fields[0]
			assert.Equal(t, "description", field.FieldName)
			assert.NotContains(t, field.Text, "野々浦")
			assert.Contains(t, field.Text, "⟦0⟧")
			assert.Equal(t, "노노우라 논", field.Placeholders["⟦0⟧"])
			assert.Contains(t, field.FallbackText, "노노우라 논")

			restored, ok := restoreNamePlaceholders(field.Text, field.Placeholders)
			require.True(t, ok)
			assert.Equal(t, "노노우라 논が出演する作品", restored)
		})
	}
}

func TestBuildTranslationPlanProtectsLongestOverlappingActressNameFirst(t *testing.T) {
	service := New(Config{Fields: fieldsConfig{Description: true}})
	movie := &models.Movie{
		Description: "野々浦 暖が出演する作品",
		Actresses: []models.Actress{
			{JapaneseName: "野々浦", FirstName: "노노우라"},
			{JapaneseName: "野々浦暖", FirstName: "논", LastName: "노노우라"},
		},
	}

	plan := service.BuildTranslationPlan(movie, "ko", "ja", "test")
	require.Len(t, plan.Fields, 1)
	field := plan.Fields[0]
	assert.NotContains(t, field.Text, "暖", "the shorter overlapping name must not consume the prefix")
	require.Len(t, field.Placeholders, 1)
	for token, name := range field.Placeholders {
		assert.True(t, strings.Contains(field.Text, token))
		assert.Equal(t, "노노우라 논", name)
	}
}

func TestBuildTranslationPlanProtectsJapaneseActressNameInTitle(t *testing.T) {
	service := New(Config{Fields: fieldsConfig{Title: true}})
	movie := &models.Movie{
		Title: "野々浦 暖の作品",
		Actresses: []models.Actress{{
			JapaneseName: "野々浦暖",
			ThumbURL:     "https://pics.dmm.co.jp/mono/actjpgs/nonoura_non.jpg",
		}},
	}

	plan := service.BuildTranslationPlan(movie, "ko", "ja", "test")
	require.Len(t, plan.Fields, 1)
	field := plan.Fields[0]
	assert.Equal(t, "title", field.FieldName)
	assert.NotContains(t, field.Text, "野々浦")
	assert.Contains(t, field.Text, "⟦0⟧")
	assert.Equal(t, "노노우라 논", field.Placeholders["⟦0⟧"])
}
