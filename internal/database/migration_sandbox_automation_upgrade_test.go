package database

import (
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"github.com/golang-migrate/migrate/v4"
	"github.com/stretchr/testify/require"
)

// Exercise the fork's existing migration ceilings, not the upstream numbers.
func TestSandboxAutomationMigrationUpgrade(t *testing.T) {
	root := sqliteRepoRoot(t)
	chdirAndRestore(t, root)
	for _, dialect := range []string{"sqlite", "postgres"} {
		baselines := []uint{0, 47, 55}
		if dialect == "postgres" {
			baselines = []uint{0, 141, 150}
		}
		for _, baseline := range baselines {
			name := fmt.Sprintf("%s/from-%d", dialect, baseline)
			upgrade := baseline > 0
			if !upgrade {
				name = dialect + "/empty"
			}
			t.Run(name, func(t *testing.T) {
				var db *sql.DB
				var m *migrate.Migrate
				ceiling, latest := uint(55), uint(59)
				if dialect == "sqlite" {
					path := filepath.Join(t.TempDir(), "upgrade.db")
					var err error
					db, err = sql.Open("sqlite3", path)
					require.NoError(t, err)
					m, err = migrate.New("file://"+root+"/migrations/sqlite", "sqlite3://"+path)
					require.NoError(t, err)
				} else {
					dsn := os.Getenv("WEKNORA_MIGRATION_TEST_POSTGRES_DSN")
					if dsn == "" {
						t.Skip("set WEKNORA_MIGRATION_TEST_POSTGRES_DSN to a disposable PostgreSQL server")
					}
					admin, err := sql.Open("postgres", dsn)
					require.NoError(t, err)
					t.Cleanup(func() { _ = admin.Close() })
					// Each path gets a new database; no business database is reset.
					database := fmt.Sprintf("weknora_sync_from_%d", baseline)
					_, err = admin.Exec("CREATE DATABASE " + database)
					require.NoError(t, err)
					t.Cleanup(func() { _, _ = admin.Exec("DROP DATABASE " + database + " WITH (FORCE)") })
					u, err := url.Parse(dsn)
					require.NoError(t, err)
					u.Path = "/" + database
					db, err = sql.Open("postgres", u.String())
					require.NoError(t, err)
					m, err = migrate.New("file://"+root+"/migrations/versioned", u.String())
					require.NoError(t, err)
					ceiling, latest = 150, 154
				}
				t.Cleanup(func() { _ = db.Close(); _, _ = m.Close() })
				if upgrade {
					require.NoError(t, m.Migrate(baseline))
					version, dirty, err := m.Version()
					require.NoError(t, err)
					require.Equal(t, baseline, version)
					require.False(t, dirty)
					_, err = db.Exec(`INSERT INTO sessions (id, tenant_id, user_id, title, workspace_mode)
						VALUES ('saved-session', 1, 'saved-owner', 'Saved conversation', 'legal_assistant')`)
					require.NoError(t, err)
				}
				if baseline >= ceiling {
					_, err := db.Exec(`INSERT INTO automations
						(id, tenant_id, owner_user_id, name, agent_id, instruction, trigger_config, created_at, updated_at)
						VALUES ('saved-task', 1, 'saved-owner', 'Saved task', 'agent', 'Keep this instruction', '{}', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`)
					require.NoError(t, err)
					_, err = db.Exec(`INSERT INTO automation_runs
						(id, automation_id, tenant_id, scheduled_at, trigger_kind, status, answer, created_at, updated_at)
						VALUES ('saved-run', 'saved-task', 1, CURRENT_TIMESTAMP, 'schedule', 'awaiting_approval', 'Saved answer', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`)
					require.NoError(t, err)
				}
				require.NoError(t, m.Up())
				version, dirty, err := m.Version()
				require.NoError(t, err)
				require.Equal(t, latest, version)
				require.False(t, dirty)
				for _, column := range []struct {
					table, name, sqliteType, postgresType string
				}{
					{"sessions", "owner_auth_source", "VARCHAR(32)", "character varying"},
					{"automation_runs", "execution_duration_ms", "BIGINT", "bigint"},
					{"automations", "model_id", "TEXT", "character varying"},
				} {
					var kind string
					if dialect == "sqlite" {
						var notNull int
						require.NoError(t, db.QueryRow(`SELECT type, "notnull" FROM pragma_table_info(?) WHERE name = ?`, column.table, column.name).Scan(&kind, &notNull))
						require.Equal(t, column.sqliteType, kind)
						require.Equal(t, 1, notNull)
					} else {
						var nullable string
						require.NoError(t, db.QueryRow(`SELECT data_type, is_nullable FROM information_schema.columns WHERE table_schema = 'public' AND table_name = $1 AND column_name = $2`, column.table, column.name).Scan(&kind, &nullable))
						require.Equal(t, column.postgresType, kind)
						require.Equal(t, "NO", nullable)
					}
				}
				for _, index := range []string{
					"idx_session_sandbox_assignments_live_machine", "idx_session_sandbox_assignments_cleanup_due",
					"idx_sandbox_user_machines_config", "idx_sandbox_user_machines_owner", "idx_sandbox_user_machines_empty",
				} {
					if dialect == "sqlite" {
						require.True(t, sqliteIndexExists(t, db, index), index)
					} else {
						var valid bool
						require.NoError(t, db.QueryRow(`SELECT i.indisvalid FROM pg_index i JOIN pg_class c ON c.oid = i.indexrelid WHERE c.relname = $1`, index).Scan(&valid))
						require.True(t, valid, index)
					}
				}
				if upgrade {
					var owner, title, mode, source string
					require.NoError(t, db.QueryRow(`SELECT user_id, title, workspace_mode, owner_auth_source FROM sessions WHERE id = 'saved-session'`).Scan(&owner, &title, &mode, &source))
					require.Equal(t, "saved-owner", owner)
					require.Equal(t, "Saved conversation", title)
					require.Equal(t, "legal_assistant", mode)
					require.Empty(t, source)
				}
				if baseline >= ceiling {
					var instruction, model, answer, status, automationID string
					var duration int64
					require.NoError(t, db.QueryRow(`SELECT instruction, model_id FROM automations WHERE id = 'saved-task'`).Scan(&instruction, &model))
					require.Equal(t, "Keep this instruction", instruction)
					require.Empty(t, model)
					require.NoError(t, db.QueryRow(`SELECT automation_id, answer, status, execution_duration_ms FROM automation_runs WHERE id = 'saved-run'`).Scan(&automationID, &answer, &status, &duration))
					require.Equal(t, "saved-task", automationID)
					require.Equal(t, "Saved answer", answer)
					require.Equal(t, "awaiting_approval", status)
					require.Zero(t, duration)
				}
				for _, query := range []string{
					`INSERT INTO session_sandbox_assignments (session_id) VALUES ('assigned-session')`,
					`INSERT INTO sandbox_user_machines (machine_digest, owner_tenant_id, owner_id, config_tenant_id, config_id) VALUES ('digest', 1, 'owner', 1, 'config')`,
				} {
					_, err := db.Exec(query)
					require.NoError(t, err)
					_, err = db.Exec(query)
					require.Error(t, err, "primary keys must reject duplicate assignments/machines")
				}
				require.ErrorIs(t, m.Up(), migrate.ErrNoChange)
			})
		}
	}
}
