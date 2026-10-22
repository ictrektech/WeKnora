package types

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestLocalDevModelHubRerank(t *testing.T) {
	for _, profile := range []string{"tc232", "jetson-model-hub"} {
		t.Run(profile, func(t *testing.T) {
			for _, port := range []string{"", "32537"} {
				t.Run("port="+port, func(t *testing.T) {
					cmd := exec.Command("bash", "-c", `
source "$1" help >/dev/null
refresh_config
require_model_hub_profile
printf '%s\n%s\n' "$ICTREK_DEV_MODEL_HUB_RERANK_MODEL_NAME" "$ICTREK_DEV_MODEL_HUB_RERANK_BASE_URL"
`, "test", "../../ictrek.app/docs/local-dev/ictrek-dev.sh")
					for _, env := range os.Environ() {
						if !strings.HasPrefix(env, "ICTREK_DEV_") {
							cmd.Env = append(cmd.Env, env)
						}
					}
					cmd.Env = append(cmd.Env, "ICTREK_DEV_PROFILE="+profile, "ICTREK_DEV_MODEL_HUB_RERANK_HOST_PORT="+port)
					output, err := cmd.Output()
					require.NoError(t, err)
					values := strings.Split(strings.TrimSpace(string(output)), "\n")
					require.Len(t, values, 2)
					require.Equal(t, "qllama/bge-reranker-v2-m3:q8_0", values[0])
					if port == "" {
						port = "31537"
					}
					require.Equal(t, "http://127.0.0.1:"+port+"/v1", values[1])
					t.Setenv("ICTREK_DEV_MODEL_HUB_RERANK_MODEL_NAME", values[0])
					t.Setenv("ICTREK_DEV_MODEL_HUB_RERANK_BASE_URL", values[1])
					config := "model-hub"
					if profile == "tc232" {
						config = "tc232"
					}
					t.Setenv("BUILTIN_MODELS_CONFIG", "../../ictrek.app/docs/local-dev/config/builtin_models."+config+".yaml")
					db := setupBuiltinModelsDB(t)
					require.NoError(t, LoadBuiltinModelsConfig(context.Background(), db, t.TempDir()))
					var model Model
					require.NoError(t, db.First(&model, "id = ?", "ictrek-model-hub-ollama-rerank").Error)
					assert.Equal(t, values[0], model.Name)
					assert.Equal(t, values[1], model.Parameters.BaseURL)
					assert.Equal(t, ModelTypeRerank, model.Type)
					assert.Equal(t, ModelSourceRemote, model.Source)
					assert.Equal(t, "generic", model.Parameters.Provider)
					assert.Equal(t, "probability", model.Parameters.ExtraConfig["score_scale"])
					assert.Equal(t, profile == "jetson-model-hub", model.IsDefault)
					if profile == "tc232" {
						var original Model
						require.NoError(t, db.First(&original, "id = ?", "ictrek-weknora-vllm-rerank").Error)
						assert.True(t, original.IsDefault)
					}
				})
			}
		})
	}
}

func TestLocalDevSchedulerHasOneModelPerRole(t *testing.T) {
	t.Setenv("BUILTIN_MODELS_CONFIG", "../../ictrek.app/docs/local-dev/config/builtin_models.scheduler.yaml")
	t.Setenv("ICTREK_DEV_SCHEDULER_BASE_URL", "http://127.0.0.1:22888/v1")
	t.Setenv("ICTREK_DEV_SCHEDULER_API_KEY", "")
	for _, role := range []string{"QA", "EMBEDDING", "RERANK"} {
		t.Setenv("ICTREK_DEV_SCHEDULER_"+role+"_MODEL", "")
	}
	db := setupBuiltinModelsDB(t)
	// Removed aliases are retired; primary IDs and user-owned models remain.
	for _, id := range []string{"ictrek-weknora-vllm-bge-m3", "ictrek-weknora-ollama-embedding", "ictrek-model-hub-ollama-qa"} {
		require.NoError(t, db.Create(&Model{ID: id, ManagedBy: BuiltinModelManagedBy, Name: "old"}).Error)
	}
	require.NoError(t, db.Create(&Model{ID: "user-cloud", Name: "cloud", Source: ModelSourceRemote}).Error)
	require.NoError(t, LoadBuiltinModelsConfig(context.Background(), db, t.TempDir()))
	var rows []Model
	require.NoError(t, db.Find(&rows).Error)
	require.Len(t, rows, 5)
	for _, id := range []string{"ictrek-weknora-vllm-bge-m3", "ictrek-model-hub-ollama-qa"} {
		live, deleted := countModels(t, db, id)
		require.Zero(t, live)
		require.EqualValues(t, 1, deleted)
	}
	defaults := map[ModelType]int{}
	for _, row := range rows {
		if row.ID == "user-cloud" {
			require.Equal(t, "cloud", row.Name)
			continue
		}
		require.Equal(t, ModelSourceRemote, row.Source)
		require.Equal(t, "http://127.0.0.1:22888/v1", row.Parameters.BaseURL)
		require.Equal(t, "openai", row.Parameters.InterfaceType)
		require.True(t, strings.HasPrefix(row.Name, "v-scheduler/ollama-"), row.Name)
		if row.Type == ModelTypeEmbedding {
			require.Equal(t, "v-scheduler/ollama-embedding/bge-m3:latest", row.Name)
			require.Equal(t, 1024, row.Parameters.EmbeddingParameters.Dimension)
		}
		if row.Type == ModelTypeKnowledgeQA || row.Type == ModelTypeVLLM {
			require.Equal(t, "reasoning_effort", row.Parameters.ExtraConfig["thinking_control"])
		} else {
			require.NotNil(t, row.Parameters.Spec)
			require.EqualValues(t, 180, row.Parameters.Spec.Compat["request_timeout_seconds"])
		}
		if row.IsDefault {
			defaults[row.Type]++
		}
	}
	require.Equal(t, map[ModelType]int{ModelTypeKnowledgeQA: 1, ModelTypeVLLM: 1, ModelTypeEmbedding: 1, ModelTypeRerank: 1}, defaults)
}

// setupBuiltinModelsDB creates an in-memory SQLite DB with `models` migrated
// via GORM AutoMigrate. AutoMigrate honours the struct tags so the
// `managed_by` and soft-delete columns are present, matching what the real
// migrations produce.
func setupBuiltinModelsDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&Model{}))
	return db
}

// writeYAML writes a builtin_models.yaml file inside a fresh temp dir and
// returns the dir (which is what LoadBuiltinModelsConfig expects as its
// configDir argument).
func writeYAML(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "builtin_models.yaml")
	require.NoError(t, os.WriteFile(path, []byte(body), 0o644))
	return dir
}

// countModels returns (live, soft-deleted) counts for the given id, used to
// distinguish "row gone via UPSERT failure" from "row soft-deleted via sweep".
func countModels(t *testing.T, db *gorm.DB, id string) (live, deleted int64) {
	t.Helper()
	require.NoError(t, db.Model(&Model{}).Where("id = ?", id).Count(&live).Error)
	require.NoError(t,
		db.Unscoped().Model(&Model{}).Where("id = ? AND deleted_at IS NOT NULL", id).Count(&deleted).Error,
	)
	return live, deleted
}

func TestLoadBuiltinModelsConfig_FileMissing(t *testing.T) {
	db := setupBuiltinModelsDB(t)
	dir := t.TempDir() // empty — no builtin_models.yaml inside

	// Pre-seed a yaml-managed row to verify it is NOT touched when the
	// config file is absent (file-missing path must skip the sweep).
	require.NoError(t, db.Create(&Model{
		ID: "pre-existing-yaml", Name: "n", Type: ModelTypeKnowledgeQA,
		Source: ModelSourceRemote, Status: ModelStatusActive,
		IsBuiltin: true, ManagedBy: BuiltinModelManagedBy,
	}).Error)

	require.NoError(t, LoadBuiltinModelsConfig(context.Background(), db, dir))

	live, deleted := countModels(t, db, "pre-existing-yaml")
	assert.Equal(t, int64(1), live, "row must remain when YAML file is absent")
	assert.Equal(t, int64(0), deleted)
}

func TestLoadBuiltinModelsConfig_ParseError(t *testing.T) {
	db := setupBuiltinModelsDB(t)
	// Malformed YAML — should warn + return without sweeping.
	dir := writeYAML(t, "builtin_models: [oops: : bad")

	require.NoError(t, db.Create(&Model{
		ID: "pre-existing-yaml", Name: "n", Type: ModelTypeKnowledgeQA,
		Source: ModelSourceRemote, Status: ModelStatusActive,
		IsBuiltin: true, ManagedBy: BuiltinModelManagedBy,
	}).Error)

	require.NoError(t, LoadBuiltinModelsConfig(context.Background(), db, dir))

	live, deleted := countModels(t, db, "pre-existing-yaml")
	assert.Equal(t, int64(1), live, "parse error must NOT trigger sweep")
	assert.Equal(t, int64(0), deleted)
}

func TestLoadBuiltinModelsConfig_BasicUpsert(t *testing.T) {
	db := setupBuiltinModelsDB(t)
	dir := writeYAML(t, `builtin_models:
  - id: builtin-llm
    name: gpt-4o-mini
    display_name: Builtin GPT-4o Mini
    type: KnowledgeQA
    is_default: true
    parameters:
      provider: openai
      api_key: sk-123
`)
	require.NoError(t, LoadBuiltinModelsConfig(context.Background(), db, dir))

	var m Model
	require.NoError(t, db.Where("id = ?", "builtin-llm").First(&m).Error)
	assert.Equal(t, "gpt-4o-mini", m.Name)
	assert.Equal(t, "Builtin GPT-4o Mini", m.DisplayName)
	assert.Equal(t, ModelTypeKnowledgeQA, m.Type)
	assert.Equal(t, ModelSourceRemote, m.Source, "default source must be remote")
	assert.Equal(t, ModelStatusActive, m.Status, "default status must be active")
	assert.Equal(t, uint64(10000), m.TenantID, "default tenant_id must be 10000")
	assert.True(t, m.IsBuiltin, "IsBuiltin must be forced to true")
	assert.True(t, m.IsDefault)
	assert.Equal(t, BuiltinModelManagedBy, m.ManagedBy)
	assert.Equal(t, "openai", m.Parameters.Provider)
	assert.Equal(t, "sk-123", m.Parameters.APIKey)
}

func TestLoadBuiltinModelsConfig_Idempotent(t *testing.T) {
	db := setupBuiltinModelsDB(t)
	dir := writeYAML(t, `builtin_models:
  - id: builtin-llm
    name: gpt-4o-mini
    type: KnowledgeQA
    parameters:
      provider: openai
`)
	require.NoError(t, LoadBuiltinModelsConfig(context.Background(), db, dir))
	require.NoError(t, LoadBuiltinModelsConfig(context.Background(), db, dir))

	var count int64
	require.NoError(t, db.Model(&Model{}).Where("id = ?", "builtin-llm").Count(&count).Error)
	assert.Equal(t, int64(1), count, "second load must not duplicate")
}

func TestLoadBuiltinModelsConfig_PreservesRuntimeOverride(t *testing.T) {
	db := setupBuiltinModelsDB(t)
	dir := writeYAML(t, `builtin_models:
  - id: builtin-llm
    name: from-yaml
    type: KnowledgeQA
`)
	require.NoError(t, LoadBuiltinModelsConfig(context.Background(), db, dir))

	// System-admin edits clear managed_by. A later startup must not reclaim
	// that row even when the same id remains declared in YAML.
	require.NoError(t, db.Model(&Model{}).Where("id = ?", "builtin-llm").Updates(map[string]any{
		"name":       "from-admin-ui",
		"managed_by": "",
	}).Error)
	require.NoError(t, LoadBuiltinModelsConfig(context.Background(), db, dir))

	var m Model
	require.NoError(t, db.Where("id = ?", "builtin-llm").First(&m).Error)
	assert.Equal(t, "from-admin-ui", m.Name)
	assert.Empty(t, m.ManagedBy)
}

func TestLoadBuiltinModelsConfig_EnvInterpolation(t *testing.T) {
	db := setupBuiltinModelsDB(t)
	t.Setenv("BUILTIN_TEST_KEY", "sk-from-env")
	dir := writeYAML(t, `builtin_models:
  - id: builtin-llm
    name: gpt-4o-mini
    type: KnowledgeQA
    parameters:
      provider: openai
      api_key: ${BUILTIN_TEST_KEY}
      base_url: ${BUILTIN_TEST_UNSET}
`)
	require.NoError(t, LoadBuiltinModelsConfig(context.Background(), db, dir))

	var m Model
	require.NoError(t, db.Where("id = ?", "builtin-llm").First(&m).Error)
	assert.Equal(t, "sk-from-env", m.Parameters.APIKey, "set env var must be substituted")
	assert.Equal(t, "${BUILTIN_TEST_UNSET}", m.Parameters.BaseURL,
		"unset env var must be kept as literal placeholder")
}

func TestInterpolateBuiltinModelEnv_Default(t *testing.T) {
	for _, tc := range []struct {
		name, value, input, want string
	}{
		{"empty", "", "${BUILTIN_TEST_DEFAULT:-fallback}", "fallback"},
		{"override", "custom", "${BUILTIN_TEST_DEFAULT:-fallback}", "custom"},
		{"url", "", "${BUILTIN_TEST_DEFAULT:-http://127.0.0.1:31537/v1}", "http://127.0.0.1:31537/v1"},
		{"no default", "", "${BUILTIN_TEST_DEFAULT}", "${BUILTIN_TEST_DEFAULT}"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("BUILTIN_TEST_DEFAULT", tc.value)
			assert.Equal(t, tc.want, interpolateBuiltinModelEnv(tc.input))
		})
	}
}

func TestLocalDevModelHubRerankWithoutNewEnv(t *testing.T) {
	for _, config := range []string{"tc232", "model-hub"} {
		t.Run(config, func(t *testing.T) {
			for _, key := range []string{"ICTREK_DEV_MODEL_HUB_RERANK_MODEL_NAME", "ICTREK_DEV_MODEL_HUB_RERANK_BASE_URL"} {
				t.Setenv(key, "")
				require.NoError(t, os.Unsetenv(key))
			}
			t.Setenv("BUILTIN_MODELS_CONFIG", "../../ictrek.app/docs/local-dev/config/builtin_models."+config+".yaml")
			db := setupBuiltinModelsDB(t)
			require.NoError(t, db.Create(&Model{
				ID: "ictrek-model-hub-ollama-rerank", ManagedBy: BuiltinModelManagedBy,
				Name:        "${ICTREK_DEV_MODEL_HUB_RERANK_MODEL_NAME}",
				DisplayName: "${ICTREK_DEV_MODEL_HUB_RERANK_MODEL_NAME} (Model Hub Ollama ReRank)",
				Type:        ModelTypeRerank, Source: ModelSourceRemote,
				Parameters: ModelParameters{BaseURL: "${ICTREK_DEV_MODEL_HUB_RERANK_BASE_URL}"},
			}).Error)
			require.NoError(t, LoadBuiltinModelsConfig(context.Background(), db, t.TempDir()))
			var model Model
			require.NoError(t, db.First(&model, "id = ?", "ictrek-model-hub-ollama-rerank").Error)
			assert.Equal(t, "qllama/bge-reranker-v2-m3:q8_0", model.Name)
			assert.Equal(t, "qllama/bge-reranker-v2-m3:q8_0 (Model Hub Ollama ReRank)", model.DisplayName)
			assert.Equal(t, "http://127.0.0.1:31537/v1", model.Parameters.BaseURL)
		})
	}
}

func TestLoadBuiltinModelsConfig_DriftSweepRemovesYAMLManaged(t *testing.T) {
	db := setupBuiltinModelsDB(t)

	// Round 1: YAML declares two entries.
	dir := writeYAML(t, `builtin_models:
  - id: builtin-llm
    name: gpt-4o-mini
    type: KnowledgeQA
  - id: builtin-rerank
    name: bge
    type: Rerank
`)
	require.NoError(t, LoadBuiltinModelsConfig(context.Background(), db, dir))

	// Round 2: rerank entry removed.
	require.NoError(t, os.WriteFile(
		filepath.Join(dir, "builtin_models.yaml"),
		[]byte(`builtin_models:
  - id: builtin-llm
    name: gpt-4o-mini
    type: KnowledgeQA
`), 0o644,
	))
	require.NoError(t, LoadBuiltinModelsConfig(context.Background(), db, dir))

	live, deleted := countModels(t, db, "builtin-rerank")
	assert.Equal(t, int64(0), live, "rerank row must be soft-deleted")
	assert.Equal(t, int64(1), deleted, "rerank row must remain recoverable in raw table")

	live, _ = countModels(t, db, "builtin-llm")
	assert.Equal(t, int64(1), live, "llm row must survive the sweep")
}

func TestLoadBuiltinModelsConfig_DriftSweepIgnoresManual(t *testing.T) {
	db := setupBuiltinModelsDB(t)

	// Seed a manual SQL-style builtin (managed_by="").
	require.NoError(t, db.Create(&Model{
		ID: "manual-builtin", Name: "manual", Type: ModelTypeKnowledgeQA,
		Source: ModelSourceRemote, Status: ModelStatusActive,
		IsBuiltin: true, ManagedBy: "",
	}).Error)

	// YAML declares a different id only. Sweep must NOT touch manual row.
	dir := writeYAML(t, `builtin_models:
  - id: builtin-llm
    name: gpt-4o-mini
    type: KnowledgeQA
`)
	require.NoError(t, LoadBuiltinModelsConfig(context.Background(), db, dir))

	live, _ := countModels(t, db, "manual-builtin")
	assert.Equal(t, int64(1), live, "manual SQL-seeded builtin must survive YAML sweep")
}

func TestLoadBuiltinModelsConfig_ResurrectsSoftDeleted(t *testing.T) {
	db := setupBuiltinModelsDB(t)
	dir := writeYAML(t, `builtin_models:
  - id: builtin-llm
    name: gpt-4o-mini
    type: KnowledgeQA
`)
	require.NoError(t, LoadBuiltinModelsConfig(context.Background(), db, dir))

	// Simulate operator soft-deleting the row via API/UI.
	require.NoError(t, db.Where("id = ?", "builtin-llm").Delete(&Model{}).Error)
	live, deleted := countModels(t, db, "builtin-llm")
	require.Equal(t, int64(0), live)
	require.Equal(t, int64(1), deleted)

	// Re-running the loader must resurrect the row (deleted_at -> NULL).
	require.NoError(t, LoadBuiltinModelsConfig(context.Background(), db, dir))
	live, deleted = countModels(t, db, "builtin-llm")
	assert.Equal(t, int64(1), live, "row must be resurrected by UPSERT")
	assert.Equal(t, int64(0), deleted, "deleted_at must be cleared")
}

func TestLoadBuiltinModelsConfig_ClearsExistingDefault(t *testing.T) {
	db := setupBuiltinModelsDB(t)

	// Seed an existing default (manual, e.g. user picked it via UI).
	require.NoError(t, db.Create(&Model{
		ID: "user-default", Name: "user", Type: ModelTypeKnowledgeQA,
		TenantID: 10000, Source: ModelSourceRemote, Status: ModelStatusActive,
		IsDefault: true, IsBuiltin: false, ManagedBy: "",
	}).Error)

	dir := writeYAML(t, `builtin_models:
  - id: builtin-llm
    name: gpt-4o-mini
    type: KnowledgeQA
    is_default: true
`)
	require.NoError(t, LoadBuiltinModelsConfig(context.Background(), db, dir))

	var prev Model
	require.NoError(t, db.Where("id = ?", "user-default").First(&prev).Error)
	assert.False(t, prev.IsDefault,
		"YAML is_default=true must clear other defaults in same (tenant,type)")

	var newDefault Model
	require.NoError(t, db.Where("id = ?", "builtin-llm").First(&newDefault).Error)
	assert.True(t, newDefault.IsDefault)
}

func TestLoadBuiltinModelsConfig_EmptyListSweepsAllYAMLManaged(t *testing.T) {
	db := setupBuiltinModelsDB(t)

	// Seed one yaml-managed and one manual builtin.
	require.NoError(t, db.Create(&Model{
		ID: "yaml-old", Name: "y", Type: ModelTypeKnowledgeQA,
		Source: ModelSourceRemote, Status: ModelStatusActive,
		IsBuiltin: true, ManagedBy: BuiltinModelManagedBy,
	}).Error)
	require.NoError(t, db.Create(&Model{
		ID: "manual-builtin", Name: "m", Type: ModelTypeKnowledgeQA,
		Source: ModelSourceRemote, Status: ModelStatusActive,
		IsBuiltin: true, ManagedBy: "",
	}).Error)

	// Explicit empty list. Loader treats this as "no YAML-managed models
	// declared" and sweeps the entire yaml-managed slice. Manual rows are
	// untouched.
	dir := writeYAML(t, "builtin_models: []\n")
	require.NoError(t, LoadBuiltinModelsConfig(context.Background(), db, dir))

	live, _ := countModels(t, db, "yaml-old")
	assert.Equal(t, int64(0), live, "yaml-managed row must be swept")

	live, _ = countModels(t, db, "manual-builtin")
	assert.Equal(t, int64(1), live, "manual row must survive empty-list sweep")
}

func TestLoadBuiltinModelsConfig_RejectsInvalidEntries(t *testing.T) {
	// Each sub-case writes a single-entry YAML and asserts the resulting
	// `models` table stays empty (entry was rejected by validator).
	cases := []struct {
		name string
		yaml string
	}{
		{
			name: "id too long",
			yaml: "builtin_models:\n  - id: " + repeatChar("a", ModelIDMaxLen+1) +
				"\n    type: KnowledgeQA\n",
		},
		{
			name: "missing type",
			yaml: "builtin_models:\n  - id: builtin-x\n    name: x\n",
		},
		{
			name: "unknown type (case mismatch)",
			yaml: "builtin_models:\n  - id: builtin-x\n    type: knowledgeqa\n",
		},
		{
			name: "unknown type",
			yaml: "builtin_models:\n  - id: builtin-x\n    type: LLM\n",
		},
		{
			name: "unknown status",
			yaml: "builtin_models:\n  - id: builtin-x\n    type: KnowledgeQA\n    status: enabled\n",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			db := setupBuiltinModelsDB(t)
			dir := writeYAML(t, c.yaml)
			require.NoError(t, LoadBuiltinModelsConfig(context.Background(), db, dir))

			var count int64
			require.NoError(t, db.Model(&Model{}).Count(&count).Error)
			assert.Equal(t, int64(0), count,
				"invalid entry must be rejected, table must stay empty")
		})
	}
}

// repeatChar returns s repeated n times. Used to synthesize id strings
// that exceed ModelIDMaxLen without depending on strings.Repeat in tests.
func repeatChar(s string, n int) string {
	out := make([]byte, 0, len(s)*n)
	for i := 0; i < n; i++ {
		out = append(out, s...)
	}
	return string(out)
}

func TestLoadBuiltinModelsConfig_PreservesEntryIDOverBeforeCreate(t *testing.T) {
	// Regression guard: BeforeCreate now only generates a UUID when ID is
	// empty. YAML always supplies a stable id; if BeforeCreate ever forgets
	// the guard and overwrites it, the UPSERT key changes and idempotency
	// breaks. Reload the file twice and assert the same row id survives.
	db := setupBuiltinModelsDB(t)
	dir := writeYAML(t, `builtin_models:
  - id: stable-id-123
    name: n
    type: KnowledgeQA
`)
	require.NoError(t, LoadBuiltinModelsConfig(context.Background(), db, dir))
	require.NoError(t, LoadBuiltinModelsConfig(context.Background(), db, dir))

	var ids []string
	require.NoError(t, db.Model(&Model{}).Pluck("id", &ids).Error)
	assert.Equal(t, []string{"stable-id-123"}, ids,
		"YAML-declared id must survive across reloads (no UUID regeneration)")
}
