package migrations

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestFilesystem(t *testing.T) {
	fs := Filesystem()
	assert.NotNil(t, fs)
}

func TestGoMigrations(t *testing.T) {
	migrations := GoMigrations()
	if assert.Len(t, migrations, 2) {
		assert.Equal(t, int64(13), migrations[0].Version)
		assert.Equal(t, int64(14), migrations[1].Version)
	}
}
