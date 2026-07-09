package batch

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
)

func oldTestMovie() *models.Movie {
	return &models.Movie{
		ID: "OLD-1", ContentID: "old1",
		Title: "OldTitle", Description: "OldDesc",
		Director: "OldDir", Maker: "OldMaker",
		PosterURL: "old-poster", Runtime: 100,
		Actresses: []models.Actress{{FirstName: "OldActress"}},
		Genres:    []models.Genre{{Name: "OldGenre"}},
	}
}

func newTestMovie() *models.Movie {
	return &models.Movie{
		ID: "NEW-1", ContentID: "new1",
		Title: "NewTitle", Description: "NewDesc",
		Director: "NewDir", Maker: "NewMaker",
		PosterURL: "new-poster", Runtime: 200,
		Actresses: []models.Actress{{FirstName: "NewActress"}},
		Genres:    []models.Genre{{Name: "NewGenre"}},
	}
}

func TestRestoreUnselectedSections(t *testing.T) {
	t.Run("only actresses selected keeps new actresses, restores rest", func(t *testing.T) {
		n, o := newTestMovie(), oldTestMovie()
		restoreUnselectedSections(n, o, []string{"actresses"})
		assert.Equal(t, "NewActress", n.Actresses[0].FirstName) // selected -> new
		assert.Equal(t, "OldTitle", n.Title)                    // unselected -> old
		assert.Equal(t, "OldDir", n.Director)
		assert.Equal(t, "old-poster", n.PosterURL)
		assert.Equal(t, "OldGenre", n.Genres[0].Name)
		assert.Equal(t, "OLD-1", n.ID) // identity always old
		assert.Equal(t, "old1", n.ContentID)
	})

	t.Run("title+images selected", func(t *testing.T) {
		n, o := newTestMovie(), oldTestMovie()
		restoreUnselectedSections(n, o, []string{"title", "images"})
		assert.Equal(t, "NewTitle", n.Title)                    // selected
		assert.Equal(t, "NewDesc", n.Description)               // part of title section
		assert.Equal(t, "new-poster", n.PosterURL)              // selected
		assert.Equal(t, "OldActress", n.Actresses[0].FirstName) // unselected -> old
		assert.Equal(t, "OldDir", n.Director)
	})

	t.Run("unknown section ignored -> everything restored to old", func(t *testing.T) {
		n, o := newTestMovie(), oldTestMovie()
		restoreUnselectedSections(n, o, []string{"nonsense"})
		assert.Equal(t, "OldTitle", n.Title)
		assert.Equal(t, "OldActress", n.Actresses[0].FirstName)
	})

	t.Run("empty selection is a no-op (full rescrape)", func(t *testing.T) {
		n, o := newTestMovie(), oldTestMovie()
		restoreUnselectedSections(n, o, nil)
		assert.Equal(t, "NewTitle", n.Title) // untouched
		assert.Equal(t, "NEW-1", n.ID)
	})
}
