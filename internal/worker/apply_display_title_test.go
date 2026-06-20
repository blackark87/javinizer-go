package worker

import (
	"context"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/template"
	"github.com/stretchr/testify/assert"
)

// TestApplyDisplayTitle_ActressLanguageJa verifies ApplyDisplayTitle threads
// the actressLanguageJa flag into the rendered display title for <ACTORS>.
// Regression for a threading gap found by code review.
func TestApplyDisplayTitle_ActressLanguageJa(t *testing.T) {
	movie := &models.Movie{
		ID: "IPX-123",
		Actresses: []models.Actress{
			{FirstName: "Yui", LastName: "Hatano", JapaneseName: "波多野結衣"},
		},
	}

	t.Run("Latin when flag is false", func(t *testing.T) {
		m := *movie
		ApplyDisplayTitle(context.Background(), &m, &m, "<ACTORS>",
			template.NewEngine(), false, "", "", false, false, ", ")
		assert.Equal(t, "Hatano Yui", m.DisplayTitle)
	})

	t.Run("Japanese when flag is true", func(t *testing.T) {
		m := *movie
		ApplyDisplayTitle(context.Background(), &m, &m, "<ACTORS>",
			template.NewEngine(), false, "", "", false, true, ", ")
		assert.Equal(t, "波多野結衣", m.DisplayTitle)
	})
}
