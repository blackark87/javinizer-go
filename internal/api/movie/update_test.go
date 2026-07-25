package movie

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/api/contracts"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUpdateMovie_PersistsMetadataAndPreservesIdentity(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx := context.Background()
	db, err := database.New(&database.Config{Type: "sqlite", DSN: ":memory:"})
	require.NoError(t, err)
	require.NoError(t, db.RunMigrationsOnStartup(ctx))
	t.Cleanup(func() { _ = db.Close() })

	repos := db.Repositories()
	stored, err := repos.MovieRepo.Upsert(ctx, &models.Movie{
		ContentID: "300mium00985",
		ID:        "MIUM-985",
		Title:     "Old title",
		Poster: models.PosterState{
			PosterURL: "https://example.com/old.jpg",
		},
		Actresses: []models.Actress{{
			FirstName:    "Tomo",
			LastName:     "Shiraiwa",
			JapaneseName: "白岩冬萌",
		}},
		Genres: []models.Genre{{Name: "Old genre"}},
	})
	require.NoError(t, err)
	require.Len(t, stored.Actresses, 1)

	edited := contracts.MovieViewFromModel(stored)
	edited.Code = "changed-content-id"
	edited.ID = "CHANGED-999"
	edited.DisplayTitle = "새 제목"
	edited.Title = "새 제목"
	edited.PosterURL = "https://example.com/new.jpg"
	edited.Actresses[0].FirstName = "토모"
	edited.Actresses[0].LastName = "시라이와"
	edited.Genres = []contracts.GenreView{{Name: "New genre"}}

	body, err := json.Marshal(contracts.UpdateMovieRequest{Movie: edited})
	require.NoError(t, err)

	router := gin.New()
	deps := NewMovieDeps(
		repos.MovieRepo,
		WithActressRepository(repos.ActressRepo),
	)
	router.PUT("/movies/:id", updateMovie(deps))

	req := httptest.NewRequest(http.MethodPut, "/movies/MIUM-985", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())

	var response contracts.MovieResponse
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &response))
	require.NotNil(t, response.Movie)
	assert.Equal(t, "300mium00985", response.Movie.Code)
	assert.Equal(t, "MIUM-985", response.Movie.ID)
	assert.Equal(t, "새 제목", response.Movie.DisplayTitle)
	assert.Equal(t, "https://example.com/new.jpg", response.Movie.PosterURL)
	require.Len(t, response.Movie.Genres, 1)
	assert.Equal(t, "New genre", response.Movie.Genres[0].Name)
	require.Len(t, response.Movie.Actresses, 1)
	assert.Equal(t, "토모", response.Movie.Actresses[0].FirstName)
	assert.Equal(t, "시라이와", response.Movie.Actresses[0].LastName)

	reloaded, err := repos.MovieRepo.FindByContentID(ctx, "300mium00985")
	require.NoError(t, err)
	assert.Equal(t, "MIUM-985", reloaded.ID)
	assert.Equal(t, "새 제목", reloaded.Title)
	require.Len(t, reloaded.Genres, 1)
	assert.Equal(t, "New genre", reloaded.Genres[0].Name)
	require.Len(t, reloaded.Actresses, 1)
	assert.Equal(t, "토모", reloaded.Actresses[0].FirstName)
}

func TestUpdateMovie_ValidationAndNotFound(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx := context.Background()
	db, err := database.New(&database.Config{Type: "sqlite", DSN: ":memory:"})
	require.NoError(t, err)
	require.NoError(t, db.RunMigrationsOnStartup(ctx))
	t.Cleanup(func() { _ = db.Close() })

	repos := db.Repositories()
	router := gin.New()
	router.PUT("/movies/:id", updateMovie(NewMovieDeps(repos.MovieRepo)))

	t.Run("missing movie payload", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPut, "/movies/MIUM-985", bytes.NewBufferString(`{}`))
		req.Header.Set("Content-Type", "application/json")
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, req)
		assert.Equal(t, http.StatusBadRequest, recorder.Code)
	})

	t.Run("unknown movie", func(t *testing.T) {
		req := httptest.NewRequest(
			http.MethodPut,
			"/movies/UNKNOWN-001",
			bytes.NewBufferString(`{"movie":{"id":"UNKNOWN-001","code":"unknown001"}}`),
		)
		req.Header.Set("Content-Type", "application/json")
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, req)
		assert.Equal(t, http.StatusNotFound, recorder.Code)
	})
}
