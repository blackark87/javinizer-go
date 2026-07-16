package worker

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
)

func oldRescrapeTestMovie() *models.Movie {
	return &models.Movie{
		ID: "OLD-1", ContentID: "old1",
		Title: "OldTitle", Description: "OldDesc",
		Director: "OldDir", Maker: "OldMaker",
		Poster: models.PosterState{PosterURL: "old-poster"}, Runtime: 100,
		Actresses: []models.Actress{{FirstName: "OldActress"}},
		Genres:    []models.Genre{{Name: "OldGenre"}},
	}
}

func newRescrapeTestMovie() *models.Movie {
	return &models.Movie{
		ID: "NEW-1", ContentID: "new1",
		Title: "NewTitle", Description: "NewDesc",
		Director: "NewDir", Maker: "NewMaker",
		Poster: models.PosterState{PosterURL: "new-poster"}, Runtime: 200,
		Actresses: []models.Actress{{FirstName: "NewActress"}},
		Genres:    []models.Genre{{Name: "NewGenre"}},
	}
}

func TestRestoreUnselectedRescrapeSections(t *testing.T) {
	t.Run("only actresses selected", func(t *testing.T) {
		fresh, old := newRescrapeTestMovie(), oldRescrapeTestMovie()
		restoreUnselectedRescrapeSections(fresh, old, []string{"actresses"})
		assert.Equal(t, "NewActress", fresh.Actresses[0].FirstName)
		assert.Equal(t, "OldTitle", fresh.Title)
		assert.Equal(t, "OldDir", fresh.Director)
		assert.Equal(t, "old-poster", fresh.Poster.PosterURL)
		assert.Equal(t, "OldGenre", fresh.Genres[0].Name)
		assert.Equal(t, "OLD-1", fresh.ID)
		assert.Equal(t, "old1", fresh.ContentID)
	})

	t.Run("title and images selected", func(t *testing.T) {
		fresh, old := newRescrapeTestMovie(), oldRescrapeTestMovie()
		restoreUnselectedRescrapeSections(fresh, old, []string{"title", "images"})
		assert.Equal(t, "NewTitle", fresh.Title)
		assert.Equal(t, "NewDesc", fresh.Description)
		assert.Equal(t, "new-poster", fresh.Poster.PosterURL)
		assert.Equal(t, "OldActress", fresh.Actresses[0].FirstName)
		assert.Equal(t, "OldDir", fresh.Director)
	})

	t.Run("unknown section restores everything", func(t *testing.T) {
		fresh, old := newRescrapeTestMovie(), oldRescrapeTestMovie()
		restoreUnselectedRescrapeSections(fresh, old, []string{"nonsense"})
		assert.Equal(t, "OldTitle", fresh.Title)
		assert.Equal(t, "OldActress", fresh.Actresses[0].FirstName)
	})

	t.Run("empty selection is full rescrape", func(t *testing.T) {
		fresh, old := newRescrapeTestMovie(), oldRescrapeTestMovie()
		restoreUnselectedRescrapeSections(fresh, old, nil)
		assert.Equal(t, "NewTitle", fresh.Title)
		assert.Equal(t, "NEW-1", fresh.ID)
	})
}

func TestRestoreUnselectedRescrapeSections_TranslationsAndOriginals(t *testing.T) {
	oldMovie := &models.Movie{
		ID: "OLD-1", ContentID: "old1",
		Title: "OldTitle", Description: "OldDesc",
		Poster:       models.PosterState{PosterURL: "old-poster", OriginalPosterURL: "old-orig-poster", OriginalCroppedPosterURL: "old-orig-cropped"},
		Actresses:    []models.Actress{{FirstName: "OldActress"}},
		Translations: []models.MovieTranslation{{Language: "ko", Title: "옛제목", Description: "옛설명", Actresses: []string{"옛배우"}}},
	}
	newMovie := &models.Movie{
		ID: "NEW-1", ContentID: "new1",
		Title: "NewTitle", Description: "NewDesc",
		Poster:       models.PosterState{PosterURL: "new-poster", OriginalPosterURL: "new-orig-poster", OriginalCroppedPosterURL: "new-orig-cropped"},
		Actresses:    []models.Actress{{FirstName: "NewActress"}},
		Translations: []models.MovieTranslation{{Language: "ko", Title: "새제목", Description: "새설명", Actresses: []string{"새배우"}}},
	}

	t.Run("actresses preserves title translation and original posters", func(t *testing.T) {
		fresh := *newMovie
		fresh.Translations = append([]models.MovieTranslation(nil), newMovie.Translations...)
		restoreUnselectedRescrapeSections(&fresh, oldMovie, []string{"actresses"})

		assert.Equal(t, "NewActress", fresh.Actresses[0].FirstName)
		assert.Equal(t, []string{"새배우"}, fresh.Translations[0].Actresses)
		assert.Equal(t, "옛제목", fresh.Translations[0].Title)
		assert.Equal(t, "옛설명", fresh.Translations[0].Description)
		assert.Equal(t, "old-orig-poster", fresh.Poster.OriginalPosterURL)
		assert.Equal(t, "old-orig-cropped", fresh.Poster.OriginalCroppedPosterURL)
	})

	t.Run("images restores all translations", func(t *testing.T) {
		fresh := *newMovie
		fresh.Translations = append([]models.MovieTranslation(nil), newMovie.Translations...)
		restoreUnselectedRescrapeSections(&fresh, oldMovie, []string{"images"})
		assert.Equal(t, "new-poster", fresh.Poster.PosterURL)
		assert.Equal(t, "옛제목", fresh.Translations[0].Title)
		assert.Equal(t, []string{"옛배우"}, fresh.Translations[0].Actresses)
	})
}

func TestRescrapeSectionSelected(t *testing.T) {
	assert.True(t, rescrapeSectionSelected(nil, "images"))
	assert.True(t, rescrapeSectionSelected([]string{" Images "}, "images"))
	assert.False(t, rescrapeSectionSelected([]string{"title"}, "images"))
}
