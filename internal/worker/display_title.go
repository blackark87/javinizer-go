package worker

import (
	"context"
	"path/filepath"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/template"
)

func applyDisplayTitle(ctx context.Context, job *BatchJob, cfg *config.Config, movie *models.Movie, titleSource *models.Movie) {
	applyDisplayTitleWithSource(ctx, job, cfg, movie, titleSource, "")
}

func applyDisplayTitleWithSource(ctx context.Context, job *BatchJob, cfg *config.Config, movie *models.Movie, titleSource *models.Movie, sourcePath string) {
	if cfg != nil && cfg.Metadata.NFO.DisplayTitle != "" {
		displayTmplEngine := job.TemplateEngine()
		ApplyDisplayTitleWithSource(ctx, movie, titleSource, cfg.Metadata.NFO.DisplayTitle, displayTmplEngine, cfg.Output.GroupActress, cfg.Output.GroupActressName, cfg.Output.FirstNameOrder, sourcePath)
	} else if movie.DisplayTitle == "" {
		movie.DisplayTitle = movie.Title
	}
}

func ApplyDisplayTitle(ctx context.Context, movie *models.Movie, titleSource *models.Movie, displayTitleTmpl string, templateEngine *template.Engine, groupActress bool, groupActressName string, firstNameOrder bool) {
	ApplyDisplayTitleWithSource(ctx, movie, titleSource, displayTitleTmpl, templateEngine, groupActress, groupActressName, firstNameOrder, "")
}

func ApplyDisplayTitleWithSource(ctx context.Context, movie *models.Movie, titleSource *models.Movie, displayTitleTmpl string, templateEngine *template.Engine, groupActress bool, groupActressName string, firstNameOrder bool, sourcePath string) {
	if displayTitleTmpl != "" {
		if templateEngine == nil {
			templateEngine = template.NewEngine()
		}
		displayCtx := template.NewContextFromMovie(movie)
		displayCtx.GroupActress = groupActress
		displayCtx.GroupActressName = groupActressName
		displayCtx.FirstNameOrder = firstNameOrder
		if titleSource != nil {
			displayCtx.Title = titleSource.Title
		}
		if sourcePath != "" {
			displayCtx.SetSourceFile(sourcePath, filepath.Base(sourcePath), filepath.Ext(sourcePath))
		}
		if displayName, err := templateEngine.ExecuteWithContext(ctx, displayTitleTmpl, displayCtx); err == nil {
			movie.DisplayTitle = displayName
		} else if movie.DisplayTitle == "" {
			movie.DisplayTitle = movie.Title
		}
	} else if movie.DisplayTitle == "" {
		movie.DisplayTitle = movie.Title
	}
}
