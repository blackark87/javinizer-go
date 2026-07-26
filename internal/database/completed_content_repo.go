package database

import (
	"context"
	"database/sql"
	"strings"
	"time"

	"github.com/javinizer/javinizer-go/internal/models"
	"gorm.io/gorm"
)

// CompletedContent is the database read model for one organized movie.
// Paths are the distinct destinations still marked as applied.
type CompletedContent struct {
	MovieID                  string
	ContentID                string
	DisplayTitle             string
	Title                    string
	OriginalTitle            string
	PosterURL                string
	CoverURL                 string
	CroppedPosterURL         string
	OriginalPosterURL        string
	OriginalCroppedPosterURL string
	OriginalCoverURL         string
	Actresses                []models.Actress
	Paths                    []string
	LatestJobID              string
	OrganizedAt              time.Time
	MetadataCreatedAt        *time.Time
	MetadataUpdatedAt        *time.Time
}

type CompletedContentSort string

const (
	CompletedContentSortOrganized       CompletedContentSort = "organized_at"
	CompletedContentSortMetadataCreated CompletedContentSort = "metadata_created_at"
	CompletedContentSortMetadataUpdated CompletedContentSort = "metadata_updated_at"
)

type CompletedContentSortOrder string

const (
	CompletedContentSortAscending  CompletedContentSortOrder = "asc"
	CompletedContentSortDescending CompletedContentSortOrder = "desc"
)

type CompletedContentListOptions struct {
	Query     string
	ActressID uint
	Sort      CompletedContentSort
	Order     CompletedContentSortOrder
	Limit     int
	Offset    int
}

type CompletedContentActressFilter struct {
	Actress models.Actress
	Count   int64
}

type completedContentRow struct {
	MovieID                  string        `gorm:"column:movie_id"`
	ContentID                string        `gorm:"column:content_id"`
	DisplayTitle             string        `gorm:"column:display_title"`
	Title                    string        `gorm:"column:title"`
	OriginalTitle            string        `gorm:"column:original_title"`
	PosterURL                string        `gorm:"column:poster_url"`
	CoverURL                 string        `gorm:"column:cover_url"`
	CroppedPosterURL         string        `gorm:"column:cropped_poster_url"`
	OriginalPosterURL        string        `gorm:"column:original_poster_url"`
	OriginalCroppedPosterURL string        `gorm:"column:original_cropped_poster_url"`
	OriginalCoverURL         string        `gorm:"column:original_cover_url"`
	LatestJobID              string        `gorm:"column:latest_job_id"`
	OrganizedAtUnix          int64         `gorm:"column:organized_at_unix"`
	MetadataCreatedAtUnix    sql.NullInt64 `gorm:"column:metadata_created_at_unix"`
	MetadataUpdatedAtUnix    sql.NullInt64 `gorm:"column:metadata_updated_at_unix"`
}

type completedContentPathRow struct {
	MovieID string `gorm:"column:movie_id"`
	NewPath string `gorm:"column:new_path"`
}

type completedContentActressRow struct {
	MovieContentID string `gorm:"column:movie_content_id"`
	ID             uint   `gorm:"column:id"`
	DMMID          int    `gorm:"column:dmm_id"`
	FirstName      string `gorm:"column:first_name"`
	LastName       string `gorm:"column:last_name"`
	JapaneseName   string `gorm:"column:japanese_name"`
	Reading        string `gorm:"column:reading"`
	ThumbURL       string `gorm:"column:thumb_url"`
	Aliases        string `gorm:"column:aliases"`
}

type completedContentActressFilterRow struct {
	ID           uint   `gorm:"column:id"`
	DMMID        int    `gorm:"column:dmm_id"`
	FirstName    string `gorm:"column:first_name"`
	LastName     string `gorm:"column:last_name"`
	JapaneseName string `gorm:"column:japanese_name"`
	Reading      string `gorm:"column:reading"`
	ThumbURL     string `gorm:"column:thumb_url"`
	Aliases      string `gorm:"column:aliases"`
	WorkCount    int64  `gorm:"column:work_count"`
}

// ListCompletedContent returns applied organize operations grouped by movie.
// Movie metadata is left-joined so an operation remains discoverable even if
// its cached movie row has since been deleted.
func (r *BatchFileOperationRepository) ListCompletedContent(
	ctx context.Context,
	options CompletedContentListOptions,
) ([]CompletedContent, int64, error) {
	base := r.completedContentBaseQuery(ctx, options.Query, options.ActressID)

	var total int64
	if err := base.Session(&gorm.Session{}).Distinct("b.movie_id").Count(&total).Error; err != nil {
		return nil, 0, wrapDBErr("count", "completed content", err)
	}
	if total == 0 {
		return []CompletedContent{}, 0, nil
	}

	var rows []completedContentRow
	err := base.Session(&gorm.Session{}).
		Select(`
			b.movie_id AS movie_id,
			COALESCE(m.content_id, '') AS content_id,
			COALESCE(m.display_title, '') AS display_title,
			COALESCE(m.title, '') AS title,
			COALESCE(m.original_title, '') AS original_title,
			COALESCE(m.poster_url, '') AS poster_url,
			COALESCE(m.cover_url, '') AS cover_url,
			COALESCE(m.cropped_poster_url, '') AS cropped_poster_url,
			COALESCE(m.original_poster_url, '') AS original_poster_url,
			COALESCE(m.original_cropped_poster_url, '') AS original_cropped_poster_url,
			COALESCE(m.original_cover_url, '') AS original_cover_url,
			(
				SELECT latest.batch_job_id
				FROM batch_file_operations AS latest
				WHERE latest.movie_id = b.movie_id
					AND latest.revert_status = ?
					AND TRIM(COALESCE(latest.new_path, '')) <> ''
				ORDER BY latest.created_at DESC, latest.id DESC
				LIMIT 1
			) AS latest_job_id,
			MAX(unixepoch(b.created_at)) AS organized_at_unix,
			unixepoch(m.created_at) AS metadata_created_at_unix,
			unixepoch(m.updated_at) AS metadata_updated_at_unix
		`, models.RevertStatusApplied).
		Group("b.movie_id").
		Order(completedContentOrderClause(options.Sort, options.Order)).
		Limit(options.Limit).
		Offset(options.Offset).
		Scan(&rows).Error
	if err != nil {
		return nil, 0, wrapDBErr("list", "completed content", err)
	}

	items := make([]CompletedContent, len(rows))
	movieIDs := make([]string, 0, len(rows))
	contentIDs := make([]string, 0, len(rows))
	itemByMovieID := make(map[string]*CompletedContent, len(rows))
	for i, row := range rows {
		items[i] = CompletedContent{
			MovieID:                  row.MovieID,
			ContentID:                row.ContentID,
			DisplayTitle:             row.DisplayTitle,
			Title:                    row.Title,
			OriginalTitle:            row.OriginalTitle,
			PosterURL:                row.PosterURL,
			CoverURL:                 row.CoverURL,
			CroppedPosterURL:         row.CroppedPosterURL,
			OriginalPosterURL:        row.OriginalPosterURL,
			OriginalCroppedPosterURL: row.OriginalCroppedPosterURL,
			OriginalCoverURL:         row.OriginalCoverURL,
			Actresses:                []models.Actress{},
			Paths:                    []string{},
			LatestJobID:              row.LatestJobID,
			OrganizedAt:              time.Unix(row.OrganizedAtUnix, 0).UTC(),
			MetadataCreatedAt:        nullableUnixTime(row.MetadataCreatedAtUnix),
			MetadataUpdatedAt:        nullableUnixTime(row.MetadataUpdatedAtUnix),
		}
		movieIDs = append(movieIDs, row.MovieID)
		if row.ContentID != "" {
			contentIDs = append(contentIDs, row.ContentID)
		}
		itemByMovieID[row.MovieID] = &items[i]
	}

	if err := r.loadCompletedContentPaths(ctx, movieIDs, itemByMovieID); err != nil {
		return nil, 0, err
	}
	if err := r.loadCompletedContentActresses(ctx, contentIDs, items); err != nil {
		return nil, 0, err
	}

	return items, total, nil
}

func (r *BatchFileOperationRepository) completedContentBaseQuery(ctx context.Context, query string, actressID uint) *gorm.DB {
	db := r.GetDB().WithContext(ctx).
		Table("batch_file_operations AS b").
		Joins(`
			LEFT JOIN movies AS m
				ON m.content_id = (
					SELECT latest_movie.content_id
					FROM movies AS latest_movie
					WHERE latest_movie.id = b.movie_id
					ORDER BY latest_movie.updated_at DESC, latest_movie.content_id ASC
					LIMIT 1
				)
		`).
		Where(
			"b.revert_status = ? AND TRIM(COALESCE(b.new_path, '')) <> ''",
			models.RevertStatusApplied,
		)

	if actressID > 0 {
		db = db.Where(`
			EXISTS (
				SELECT 1
				FROM movie_actresses AS selected_ma
				WHERE selected_ma.movie_content_id = m.content_id
					AND selected_ma.actress_id = ?
			)
		`, actressID)
	}

	term := strings.TrimSpace(query)
	if term == "" {
		return db
	}

	like := "%" + strings.ToLower(term) + "%"
	const searchCondition = `(
		LOWER(COALESCE(b.movie_id, '')) LIKE ? OR
		LOWER(COALESCE(b.new_path, '')) LIKE ? OR
		LOWER(COALESCE(m.content_id, '')) LIKE ? OR
		LOWER(COALESCE(m.display_title, '')) LIKE ? OR
		LOWER(COALESCE(m.title, '')) LIKE ? OR
		LOWER(COALESCE(m.original_title, '')) LIKE ? OR
		EXISTS (
			SELECT 1
			FROM movie_translations AS mt
			WHERE mt.movie_id = m.content_id
				AND (
					LOWER(COALESCE(mt.title, '')) LIKE ? OR
					LOWER(COALESCE(mt.original_title, '')) LIKE ?
				)
		) OR
		EXISTS (
			SELECT 1
			FROM movie_actresses AS ma
			JOIN actresses AS a ON a.id = ma.actress_id
			WHERE ma.movie_content_id = m.content_id
				AND (
					LOWER(COALESCE(a.first_name, '')) LIKE ? OR
					LOWER(COALESCE(a.last_name, '')) LIKE ? OR
					LOWER(COALESCE(a.japanese_name, '')) LIKE ? OR
					LOWER(COALESCE(a.reading, '')) LIKE ? OR
					LOWER(COALESCE(a.aliases, '')) LIKE ? OR
					EXISTS (
						SELECT 1
						FROM actress_translations AS atr
						WHERE atr.actress_id = a.id
							AND (
								LOWER(COALESCE(atr.first_name, '')) LIKE ? OR
								LOWER(COALESCE(atr.last_name, '')) LIKE ? OR
								LOWER(COALESCE(atr.japanese_name, '')) LIKE ? OR
								LOWER(COALESCE(atr.display_name, '')) LIKE ?
							)
					)
				)
		)
	)`
	args := make([]any, 17)
	for i := range args {
		args[i] = like
	}
	return db.Where(searchCondition, args...)
}

func completedContentOrderClause(sortBy CompletedContentSort, order CompletedContentSortOrder) string {
	direction := "DESC"
	if order == CompletedContentSortAscending {
		direction = "ASC"
	}
	switch sortBy {
	case CompletedContentSortMetadataCreated:
		return "CASE WHEN metadata_created_at_unix IS NULL THEN 1 ELSE 0 END ASC, metadata_created_at_unix " + direction + ", b.movie_id ASC"
	case CompletedContentSortMetadataUpdated:
		return "CASE WHEN metadata_updated_at_unix IS NULL THEN 1 ELSE 0 END ASC, metadata_updated_at_unix " + direction + ", b.movie_id ASC"
	default:
		return "organized_at_unix " + direction + ", b.movie_id ASC"
	}
}

func nullableUnixTime(value sql.NullInt64) *time.Time {
	if !value.Valid {
		return nil
	}
	parsed := time.Unix(value.Int64, 0).UTC()
	return &parsed
}

func (r *BatchFileOperationRepository) loadCompletedContentPaths(
	ctx context.Context,
	movieIDs []string,
	items map[string]*CompletedContent,
) error {
	var rows []completedContentPathRow
	err := r.GetDB().WithContext(ctx).
		Table("batch_file_operations").
		Select("movie_id, new_path").
		Where(
			"revert_status = ? AND TRIM(COALESCE(new_path, '')) <> '' AND movie_id IN ?",
			models.RevertStatusApplied,
			movieIDs,
		).
		Order("created_at DESC, id DESC").
		Scan(&rows).Error
	if err != nil {
		return wrapDBErr("list", "completed content paths", err)
	}

	seen := make(map[string]map[string]struct{}, len(items))
	for _, row := range rows {
		item := items[row.MovieID]
		if item == nil || row.NewPath == "" {
			continue
		}
		if seen[row.MovieID] == nil {
			seen[row.MovieID] = make(map[string]struct{})
		}
		if _, ok := seen[row.MovieID][row.NewPath]; ok {
			continue
		}
		seen[row.MovieID][row.NewPath] = struct{}{}
		item.Paths = append(item.Paths, row.NewPath)
	}
	return nil
}

func (r *BatchFileOperationRepository) loadCompletedContentActresses(
	ctx context.Context,
	contentIDs []string,
	items []CompletedContent,
) error {
	if len(contentIDs) == 0 {
		return nil
	}

	var rows []completedContentActressRow
	err := r.GetDB().WithContext(ctx).
		Table("movie_actresses AS ma").
		Select(`
			ma.movie_content_id,
			a.id,
			a.dmm_id,
			a.first_name,
			a.last_name,
			a.japanese_name,
			a.reading,
			a.thumb_url,
			a.aliases
		`).
		Joins("JOIN actresses AS a ON a.id = ma.actress_id").
		Where("ma.movie_content_id IN ?", contentIDs).
		Order("ma.movie_content_id ASC, a.id ASC").
		Scan(&rows).Error
	if err != nil {
		return wrapDBErr("list", "completed content actresses", err)
	}

	actressIDs := make([]uint, 0, len(rows))
	for _, row := range rows {
		actressIDs = append(actressIDs, row.ID)
	}
	translationsByActress := make(map[uint][]models.ActressTranslation)
	if len(actressIDs) > 0 {
		var translations []models.ActressTranslation
		if err := r.GetDB().WithContext(ctx).
			Where("actress_id IN ?", actressIDs).
			Order("actress_id ASC, language ASC").
			Find(&translations).Error; err != nil {
			return wrapDBErr("list", "completed content actress translations", err)
		}
		for _, translation := range translations {
			translationsByActress[translation.ActressID] = append(
				translationsByActress[translation.ActressID],
				translation,
			)
		}
	}

	itemsByContentID := make(map[string]*CompletedContent, len(items))
	for i := range items {
		if items[i].ContentID != "" {
			itemsByContentID[items[i].ContentID] = &items[i]
		}
	}
	for _, row := range rows {
		item := itemsByContentID[row.MovieContentID]
		if item == nil {
			continue
		}
		item.Actresses = append(item.Actresses, models.Actress{
			ID:           row.ID,
			DMMID:        row.DMMID,
			FirstName:    row.FirstName,
			LastName:     row.LastName,
			JapaneseName: row.JapaneseName,
			Reading:      row.Reading,
			ThumbURL:     row.ThumbURL,
			Aliases:      row.Aliases,
			Translations: translationsByActress[row.ID],
		})
	}
	return nil
}

func (r *BatchFileOperationRepository) ListCompletedContentActressFilters(
	ctx context.Context,
) ([]CompletedContentActressFilter, error) {
	var rows []completedContentActressFilterRow
	err := r.completedContentBaseQuery(ctx, "", 0).
		Joins("JOIN movie_actresses AS filter_ma ON filter_ma.movie_content_id = m.content_id").
		Joins("JOIN actresses AS a ON a.id = filter_ma.actress_id").
		Select(`
			'' AS movie_content_id,
			a.id,
			a.dmm_id,
			a.first_name,
			a.last_name,
			a.japanese_name,
			a.reading,
			a.thumb_url,
			a.aliases,
			COUNT(DISTINCT b.movie_id) AS work_count
		`).
		Group("a.id").
		Order("a.japanese_name ASC, a.id ASC").
		Scan(&rows).Error
	if err != nil {
		return nil, wrapDBErr("list", "completed content actress filters", err)
	}
	if len(rows) == 0 {
		return []CompletedContentActressFilter{}, nil
	}

	actressIDs := make([]uint, 0, len(rows))
	for _, row := range rows {
		actressIDs = append(actressIDs, row.ID)
	}
	translationsByActress := make(map[uint][]models.ActressTranslation, len(rows))
	var translations []models.ActressTranslation
	if err := r.GetDB().WithContext(ctx).
		Where("actress_id IN ?", actressIDs).
		Order("actress_id ASC, language ASC").
		Find(&translations).Error; err != nil {
		return nil, wrapDBErr("list", "completed content actress filter translations", err)
	}
	for _, translation := range translations {
		translationsByActress[translation.ActressID] = append(
			translationsByActress[translation.ActressID],
			translation,
		)
	}

	filters := make([]CompletedContentActressFilter, len(rows))
	for i, row := range rows {
		filters[i] = CompletedContentActressFilter{
			Actress: models.Actress{
				ID:           row.ID,
				DMMID:        row.DMMID,
				FirstName:    row.FirstName,
				LastName:     row.LastName,
				JapaneseName: row.JapaneseName,
				Reading:      row.Reading,
				ThumbURL:     row.ThumbURL,
				Aliases:      row.Aliases,
				Translations: translationsByActress[row.ID],
			},
			Count: row.WorkCount,
		}
	}
	return filters, nil
}

var _ CompletedContentRepositoryInterface = (*BatchFileOperationRepository)(nil)
