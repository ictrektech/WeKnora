package database

import (
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/golang-migrate/migrate/v4"
	"github.com/stretchr/testify/require"
)

func TestDefaultSettingsMigrationUpgrade(t *testing.T) {
	root := sqliteRepoRoot(t)
	for _, dialect := range []string{"sqlite", "postgres"} {
		previous, latest, directory := uint(61), uint(62), "sqlite"
		if dialect == "postgres" {
			previous, latest, directory = 156, 157, "versioned"
		}
		for _, baseline := range []uint{0, previous} {
			t.Run(fmt.Sprintf("%s/from-%d", dialect, baseline), func(t *testing.T) {
				var db *sql.DB
				var migrationURL string
				if dialect == "sqlite" {
					path := filepath.Join(t.TempDir(), "defaults.db")
					var err error
					db, err = sql.Open("sqlite3", path)
					require.NoError(t, err)
					migrationURL = "sqlite3://" + path
				} else {
					dsn := os.Getenv("WEKNORA_MIGRATION_TEST_POSTGRES_DSN")
					if dsn == "" {
						t.Skip("set WEKNORA_MIGRATION_TEST_POSTGRES_DSN to a disposable PostgreSQL server")
					}
					admin, err := sql.Open("postgres", dsn)
					require.NoError(t, err)
					t.Cleanup(func() { _ = admin.Close() })
					name := fmt.Sprintf("weknora_defaults_%d", time.Now().UnixNano())
					_, err = admin.Exec("CREATE DATABASE " + name)
					require.NoError(t, err)
					t.Cleanup(func() { _, _ = admin.Exec("DROP DATABASE " + name) })
					u, err := url.Parse(dsn)
					require.NoError(t, err)
					u.Path = "/" + name
					migrationURL = u.String()
					db, err = sql.Open("postgres", migrationURL)
					require.NoError(t, err)
				}
				m, err := migrate.New("file://"+root+"/migrations/"+directory, migrationURL)
				require.NoError(t, err)
				t.Cleanup(func() { _ = db.Close(); _, _ = m.Close() })
				if baseline > 0 {
					require.NoError(t, m.Migrate(baseline))
					version, dirty, err := m.Version()
					require.NoError(t, err)
					require.Equal(t, baseline, version)
					require.False(t, dirty)
					_, err = db.Exec(`INSERT INTO tenants (id, name, business, assistant_config)
						VALUES (1, 'Saved workspace', 'Saved business', '{"display_name":"Saved assistant"}')`)
					require.NoError(t, err)
				}
				require.NoError(t, m.Up())
				version, dirty, err := m.Version()
				require.NoError(t, err)
				require.Equal(t, latest, version)
				require.False(t, dirty)
				var kind string
				if dialect == "sqlite" {
					var notNull int
					require.NoError(t, db.QueryRow(`SELECT type, "notnull" FROM pragma_table_info('tenants') WHERE name = 'default_settings'`).Scan(&kind, &notNull))
					require.Equal(t, "TEXT", kind)
					require.Zero(t, notNull)
				} else {
					var nullable string
					require.NoError(t, db.QueryRow(`SELECT data_type, is_nullable FROM information_schema.columns WHERE table_schema = 'public' AND table_name = 'tenants' AND column_name = 'default_settings'`).Scan(&kind, &nullable))
					require.Equal(t, "jsonb", kind)
					require.Equal(t, "YES", nullable)
				}
				if baseline > 0 {
					var name, business, assistant string
					var defaults sql.NullString
					require.NoError(t, db.QueryRow(`SELECT name, business, assistant_config, default_settings FROM tenants WHERE id = 1`).Scan(&name, &business, &assistant, &defaults))
					require.Equal(t, "Saved workspace", name)
					require.Equal(t, "Saved business", business)
					require.JSONEq(t, `{"display_name":"Saved assistant"}`, assistant)
					require.False(t, defaults.Valid)
					_, err = db.Exec(`UPDATE tenants SET default_settings = '{"chat_model_id":"saved-chat","utility_model_id":"saved-utility"}' WHERE id = 1`)
					require.NoError(t, err)
					require.NoError(t, db.QueryRow(`SELECT default_settings FROM tenants WHERE id = 1`).Scan(&defaults))
					require.JSONEq(t, `{"chat_model_id":"saved-chat","utility_model_id":"saved-utility"}`, defaults.String)
				}
				require.ErrorIs(t, m.Up(), migrate.ErrNoChange)
				t.Logf("migration %d -> %d/dirty=%t, nullable %s settings and saved data verified", baseline, version, dirty, kind)
			})
		}
	}
}
