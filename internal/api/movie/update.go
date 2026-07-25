package movie

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/api/contracts"
	"github.com/javinizer/javinizer-go/internal/database"
)

// updateMovie godoc
// @Summary Update cached movie metadata
// @Description Persist user edits to an existing cached movie while preserving its database identity
// @Tags movies
// @Accept json
// @Produce json
// @Param id path string true "Movie ID or content ID" example:"IPX-535"
// @Param request body contracts.UpdateMovieRequest true "Updated movie data"
// @Success 200 {object} contracts.MovieResponse
// @Failure 400 {object} contracts.ErrorResponse
// @Failure 404 {object} contracts.ErrorResponse
// @Failure 500 {object} contracts.ErrorResponse
// @Router /api/v1/movies/{id} [put]
func updateMovie(deps MovieDeps) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := strings.TrimSpace(c.Param("id"))
		if id == "" {
			c.JSON(http.StatusBadRequest, contracts.ErrorResponse{Error: "movie id is required"})
			return
		}

		var req contracts.UpdateMovieRequest
		if err := c.ShouldBindJSON(&req); err != nil || req.Movie == nil {
			c.JSON(http.StatusBadRequest, contracts.ErrorResponse{Error: "movie is required"})
			return
		}

		saved, err := deps.UpdateMetadata(
			c.Request.Context(),
			id,
			contracts.MovieViewToModel(req.Movie),
		)
		if err != nil {
			if database.IsNotFound(err) {
				c.JSON(http.StatusNotFound, contracts.ErrorResponse{Error: "Movie not found"})
				return
			}
			c.JSON(http.StatusInternalServerError, contracts.ErrorResponse{Error: "Failed to update movie metadata"})
			return
		}

		c.JSON(http.StatusOK, contracts.MovieResponse{Movie: contracts.MovieViewFromModel(saved)})
	}
}
