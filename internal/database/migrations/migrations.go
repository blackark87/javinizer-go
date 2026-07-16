package migrations

import (
	"context"
	"database/sql"
	"embed"
	"io/fs"

	"github.com/pressly/goose/v3"
)

//go:embed *.sql
var sqlMigrations embed.FS

// Filesystem returns embedded SQL migration files.
func Filesystem() fs.FS {
	return sqlMigrations
}

// GoMigrations returns programmatic migrations that are bundled into the binary.
// Versions 13 and 14 reconcile the two migration lineages that existed before
// upstream main was merged into the feature branch. They must inspect the live
// SQLite schema because both lineages used versions 9-12 for different changes.
func GoMigrations() []*goose.Migration {
	return []*goose.Migration{
		goose.NewGoMigration(13,
			&goose.GoFunc{RunTx: func(ctx context.Context, tx *sql.Tx) error {
				return upMigration013(ctx, tx)
			}},
			&goose.GoFunc{RunTx: func(ctx context.Context, tx *sql.Tx) error {
				return downMigration013(ctx, tx)
			}},
		),
		goose.NewGoMigration(14,
			&goose.GoFunc{RunTx: func(ctx context.Context, tx *sql.Tx) error {
				return upMigration014(ctx, tx)
			}},
			&goose.GoFunc{RunTx: func(ctx context.Context, tx *sql.Tx) error {
				return downMigration014(ctx, tx)
			}},
		),
	}
}
