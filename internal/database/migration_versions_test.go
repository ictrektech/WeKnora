package database

import (
	"path/filepath"
	"testing"

	"github.com/golang-migrate/migrate/v4/source"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/stretchr/testify/require"
)

// The fork has already published the migration range 000097-000120, so
// upstream additions are appended as 000121-000125 and local ictrek
// migrations continue at 000126. Loading each directory catches any future
// duplicate version before deployment.
func TestMigrationDirectoriesLoad(t *testing.T) {
	root := sqliteRepoRoot(t)
	for _, dir := range []string{"versioned", "sqlite"} {
		src, err := source.Open("file://" + filepath.Join(root, "migrations", dir))
		require.NoError(t, err, "migrations/%s must load", dir)
		require.NoError(t, src.Close())
	}
}
