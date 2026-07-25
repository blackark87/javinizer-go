package movie

import (
	"context"
	"fmt"

	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/poster"
	"github.com/javinizer/javinizer-go/internal/workflow"
)

// WorkflowFunc returns a workflow instance for handling scrape and compare operations.
// This function type allows handlers to obtain a workflow without depending on
// *core.APIDeps directly — callers inject the factory at construction time.
type WorkflowFunc func() workflow.WorkflowInterface

// MovieDeps holds the dependencies that movie handlers need.
// Replaces the removed MovieService — handlers take this directly,
// matching the ActressDeps pattern used in the actress package.
type MovieDeps struct {
	MovieRepo   database.MovieRepositoryInterface
	ActressRepo database.ActressRepositoryInterface
	WorkflowFn  WorkflowFunc
	PosterGen   poster.PosterGenerator
	AllowedDirs []string
}

// NewMovieDeps creates a MovieDeps from the given repository and options.
func NewMovieDeps(movieRepo database.MovieRepositoryInterface, opts ...MovieDepsOption) MovieDeps {
	d := MovieDeps{MovieRepo: movieRepo}
	for _, opt := range opts {
		opt(&d)
	}
	return d
}

// MovieDepsOption configures a MovieDeps instance.
type MovieDepsOption func(*MovieDeps)

// WithWorkflow sets the workflow factory function for scrape and compare operations.
func WithWorkflow(fn WorkflowFunc) MovieDepsOption {
	return func(d *MovieDeps) { d.WorkflowFn = fn }
}

// WithAllowedDirs sets the allowed directories for NFO path validation.
func WithAllowedDirs(dirs []string) MovieDepsOption {
	return func(d *MovieDeps) { d.AllowedDirs = dirs }
}

// WithPosterGen sets the poster generator for temp poster creation during scrape/rescrape.
func WithPosterGen(pg poster.PosterGenerator) MovieDepsOption {
	return func(d *MovieDeps) { d.PosterGen = pg }
}

// WithActressRepository enables persistence of explicit actress name edits.
func WithActressRepository(repo database.ActressRepositoryInterface) MovieDepsOption {
	return func(d *MovieDeps) { d.ActressRepo = repo }
}

// getWorkflow returns a workflow instance or nil if unavailable.
func (d MovieDeps) getWorkflow() workflow.WorkflowInterface {
	if d.WorkflowFn == nil {
		return nil
	}
	return d.WorkflowFn()
}

// getAllowedDirs returns the configured allowed directories.
func (d MovieDeps) getAllowedDirs() []string {
	return d.AllowedDirs
}

// FindByID returns a movie by its display ID or content ID.
func (d MovieDeps) FindByID(ctx context.Context, id string) (*models.Movie, error) {
	movie, err := d.MovieRepo.FindByID(ctx, id)
	if err == nil || !database.IsNotFound(err) {
		return movie, err
	}
	return d.MovieRepo.FindByContentID(ctx, id)
}

// List returns a paginated list of movies.
func (d MovieDeps) List(ctx context.Context, limit, offset int) ([]models.Movie, error) {
	return d.MovieRepo.List(ctx, limit, offset)
}

// UpdateMetadata persists edits for an existing cached movie while keeping its
// database identity stable. Movie IDs are referenced by organize history, so a
// metadata edit must not silently detach those records.
func (d MovieDeps) UpdateMetadata(ctx context.Context, id string, edited *models.Movie) (*models.Movie, error) {
	if edited == nil {
		return nil, fmt.Errorf("movie is required")
	}

	existing, err := d.FindByID(ctx, id)
	if err != nil {
		return nil, err
	}

	edited.ContentID = existing.ContentID
	edited.ID = existing.ID
	edited.CreatedAt = existing.CreatedAt

	if d.ActressRepo != nil {
		for i := range edited.Actresses {
			actress := &edited.Actresses[i]
			if actress.ID == 0 {
				continue
			}
			stored, findErr := d.ActressRepo.FindByID(ctx, actress.ID)
			if findErr != nil {
				if database.IsNotFound(findErr) {
					continue
				}
				return nil, fmt.Errorf("load actress %d: %w", actress.ID, findErr)
			}
			if stored.FirstName == actress.FirstName &&
				stored.LastName == actress.LastName &&
				stored.JapaneseName == actress.JapaneseName {
				continue
			}
			if renameErr := d.ActressRepo.RenameNameFields(
				ctx,
				actress.ID,
				actress.FirstName,
				actress.LastName,
				actress.JapaneseName,
			); renameErr != nil {
				return nil, fmt.Errorf("persist actress %d name edit: %w", actress.ID, renameErr)
			}
		}
	}

	saved, err := d.MovieRepo.Upsert(ctx, edited)
	if err != nil {
		return nil, err
	}
	return saved, nil
}
