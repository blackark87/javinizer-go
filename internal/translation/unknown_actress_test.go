package translation

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildTranslationPlanKeepsUnknownAsLiteralPreset(t *testing.T) {
	service := New(Config{Fields: fieldsConfig{Actresses: true}})
	movie := &models.Movie{Actresses: []models.Actress{{
		FirstName:    models.UnknownActressName,
		JapaneseName: models.UnknownActressName,
	}}}

	plan := service.BuildTranslationPlan(movie, "ko", "ja", "test")

	require.Len(t, plan.Fields, 1)
	field := plan.Fields[0]
	assert.Equal(t, "actress", field.FieldName)
	require.NotNil(t, field.Preset)
	assert.Equal(t, models.UnknownActressName, *field.Preset)
}
