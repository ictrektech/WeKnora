package database

import (
	"path/filepath"
	"testing"

	"github.com/golang-migrate/migrate/v4/source"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/stretchr/testify/require"
)

// Preserve the fork history through PostgreSQL 000141 and SQLite 000047.
// This sync appends upstream migrations at PostgreSQL 000142–000150 and
// SQLite 000048–000055; loading either directory rejects duplicate versions.
func TestMigrationDirectoriesLoad(t *testing.T) {
	root := sqliteRepoRoot(t)
	for _, dir := range []string{"versioned", "sqlite"} {
		src, err := source.Open("file://" + filepath.Join(root, "migrations", dir))
		require.NoError(t, err, "migrations/%s must load", dir)
		require.NoError(t, src.Close())
	}
}
