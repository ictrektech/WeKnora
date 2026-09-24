package database

import (
	"path/filepath"
	"testing"

	"github.com/golang-migrate/migrate/v4/source"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/stretchr/testify/require"
)

// The fork's existing migration history reaches PostgreSQL 000130 and SQLite
// 000037. New upstream additions are appended at PostgreSQL 000131–000132 and
// SQLite 000038–000040. Loading each directory catches duplicate versions.
func TestMigrationDirectoriesLoad(t *testing.T) {
	root := sqliteRepoRoot(t)
	for _, dir := range []string{"versioned", "sqlite"} {
		src, err := source.Open("file://" + filepath.Join(root, "migrations", dir))
		require.NoError(t, err, "migrations/%s must load", dir)
		require.NoError(t, src.Close())
	}
}
