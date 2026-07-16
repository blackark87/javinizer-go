package database

import (
	"context"
	"fmt"

	"github.com/javinizer/javinizer-go/internal/models"
)

// GenreRepository persists and queries genre records.
type GenreRepository struct {
	*BaseRepository[models.Genre, uint]
}

func newGenreRepository(db *DB) *GenreRepository {
	return &GenreRepository{
		BaseRepository: NewBaseRepository[models.Genre, uint](
			db, "genre",
			func(g models.Genre) string { return g.Name },
			WithNewEntity[models.Genre, uint](func() models.Genre { return models.Genre{} }),
		),
	}
}

// FindOrCreate returns the existing genre with the given name, or creates
// one when none exists.
func (r *GenreRepository) FindOrCreate(ctx context.Context, name string) (*models.Genre, error) {
	var genre models.Genre
	err := r.GetDB().WithContext(ctx).FirstOrCreate(&genre, models.Genre{Name: name}).Error
	if err != nil {
		return nil, wrapDBErr("find", fmt.Sprintf("genre %s", name), err)
	}
	return &genre, nil
}

// List returns all genre records.
func (r *GenreRepository) List(ctx context.Context) ([]models.Genre, error) {
	return r.ListAll(ctx)
}
