package actress

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/api/core"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
)

type actressMoviesResponse struct {
	Movies []models.Movie `json:"movies"`
	Count  int            `json:"count"`
	Total  int64          `json:"total"`
	Limit  int            `json:"limit"`
	Offset int            `json:"offset"`
}

// listActressMovies godoc
// @Summary List movies for an actress
// @Description Get a paginated list of movies linked to an actress through movie_actresses
// @Tags actress
// @Produce json
// @Param id path uint true "Actress ID"
// @Param limit query int false "Max results" default(20)
// @Param offset query int false "Skip results" default(0)
// @Success 200 {object} actressMoviesResponse
// @Failure 400 {object} ErrorResponse
// @Failure 404 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Router /api/v1/actresses/{id}/movies [get]
func listActressMovies(actressRepo *database.ActressRepository, movieRepo *database.MovieRepository) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, ok := parseActressID(c)
		if !ok {
			return
		}

		if _, err := actressRepo.FindByID(id); err != nil {
			if database.IsNotFound(err) {
				c.JSON(http.StatusNotFound, ErrorResponse{Error: "actress not found"})
				return
			}
			c.JSON(http.StatusInternalServerError, ErrorResponse{Error: err.Error()})
			return
		}

		limit, offset := core.ParsePagination(c, 20, 500)
		total, err := movieRepo.CountByActressID(id)
		if err != nil {
			c.JSON(http.StatusInternalServerError, ErrorResponse{Error: err.Error()})
			return
		}

		movies, err := movieRepo.ListByActressID(id, limit, offset)
		if err != nil {
			c.JSON(http.StatusInternalServerError, ErrorResponse{Error: err.Error()})
			return
		}

		c.JSON(http.StatusOK, actressMoviesResponse{
			Movies: movies,
			Count:  len(movies),
			Total:  total,
			Limit:  limit,
			Offset: offset,
		})
	}
}
