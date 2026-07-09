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

func TestRestoreUnselectedSections_TranslationsAndOriginals(t *testing.T) {
	oldM := &models.Movie{
		ID: "OLD-1", ContentID: "old1",
		Title: "OldTitle", Description: "OldDesc",
		PosterURL: "old-poster", OriginalPosterURL: "old-orig-poster",
		OriginalCroppedPosterURL: "old-orig-cropped",
		Actresses:                []models.Actress{{FirstName: "OldActress"}},
		Translations: []models.MovieTranslation{
			{Language: "ko", Title: "옛제목", Description: "옛설명", Actresses: []string{"옛배우"}},
		},
	}
	newM := &models.Movie{
		ID: "NEW-1", ContentID: "new1",
		Title: "NewTitle", Description: "NewDesc",
		PosterURL: "new-poster", OriginalPosterURL: "new-orig-poster",
		OriginalCroppedPosterURL: "new-orig-cropped",
		Actresses:                []models.Actress{{FirstName: "NewActress"}},
		Translations: []models.MovieTranslation{
			{Language: "ko", Title: "새제목", Description: "새설명", Actresses: []string{"새배우"}},
		},
	}

	t.Run("only actresses selected preserves title translation + original posters", func(t *testing.T) {
		n := *newM
		n.Translations = append([]models.MovieTranslation(nil), newM.Translations...)
		restoreUnselectedSections(&n, oldM, []string{"actresses"})

		// actresses (selected) -> new
		assert.Equal(t, "NewActress", n.Actresses[0].FirstName)
		assert.Equal(t, []string{"새배우"}, n.Translations[0].Actresses)
		// title translation (unselected) -> old
		assert.Equal(t, "옛제목", n.Translations[0].Title)
		assert.Equal(t, "옛설명", n.Translations[0].Description)
		// images unselected -> original posters old
		assert.Equal(t, "old-orig-poster", n.OriginalPosterURL)
		assert.Equal(t, "old-orig-cropped", n.OriginalCroppedPosterURL)
	})

	t.Run("no text section selected restores whole translations array", func(t *testing.T) {
		n := *newM
		n.Translations = append([]models.MovieTranslation(nil), newM.Translations...)
		restoreUnselectedSections(&n, oldM, []string{"images"})
		// images selected -> new poster; translations fully old
		assert.Equal(t, "new-poster", n.PosterURL)
		assert.Equal(t, "옛제목", n.Translations[0].Title)
		assert.Equal(t, []string{"옛배우"}, n.Translations[0].Actresses)
	})
}
