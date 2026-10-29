package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/application/service"
	_ "github.com/Tencent/WeKnora/internal/database"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/gin-gonic/gin"
	"github.com/golang-migrate/migrate/v4"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestDefaultSettingsHTTPPersistsAfterUpgrade(t *testing.T) {
	_, source, _, ok := runtime.Caller(0)
	require.True(t, ok)
	root := filepath.Join(filepath.Dir(source), "..", "..")
	path := filepath.Join(t.TempDir(), "defaults.db")
	m, err := migrate.New("file://"+root+"/migrations/sqlite", "sqlite3://"+path)
	require.NoError(t, err)
	t.Cleanup(func() { _, _ = m.Close() })
	require.NoError(t, m.Migrate(61)) // vos-hybrag-v0.1.77
	db, err := gorm.Open(sqlite.Open(path), &gorm.Config{})
	require.NoError(t, err)
	conn, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	require.NoError(t, db.Exec(`INSERT INTO tenants (id, name, business)
		VALUES (7, 'Existing workspace', 'Existing business')`).Error)
	require.NoError(t, m.Up())
	repo := repository.NewTenantRepository(db)
	h := &TenantHandler{
		service: service.NewTenantService(repo, nil, nil),
		modelService: &defaultSettingsModelsService{models: []*types.Model{
			{ID: "chat", Type: types.ModelTypeKnowledgeQA, Status: types.ModelStatusActive},
		}},
	}
	engine := gin.New()
	engine.Use(func(c *gin.Context) {
		tenant, err := repo.GetTenantByID(c.Request.Context(), 7)
		require.NoError(t, err)
		ctx := context.WithValue(c.Request.Context(), types.TenantIDContextKey, uint64(7))
		ctx = context.WithValue(ctx, types.TenantInfoContextKey, tenant)
		c.Request = c.Request.WithContext(ctx)
		c.Next()
		require.Empty(t, c.Errors)
	})
	engine.PUT("/tenants/kv/:key", h.UpdateTenantKV)
	engine.GET("/tenants/kv/:key", h.GetTenantKV)
	for _, chatID := range []string{"chat", ""} {
		payload := `{"chat_model_id":"` + chatID + `"}`
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPut, "/tenants/kv/default-settings", strings.NewReader(payload))
		req.Header.Set("Content-Type", "application/json")
		engine.ServeHTTP(w, req)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		saved, err := repo.GetTenantByID(context.Background(), 7)
		require.NoError(t, err)
		require.NotNil(t, saved.DefaultSettings)
		require.Equal(t, chatID, saved.DefaultSettings.ChatModelID)
		require.Equal(t, "Existing business", saved.Business)
		w = httptest.NewRecorder()
		engine.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/tenants/kv/default-settings", nil))
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		var response struct {
			Data types.DefaultSettings `json:"data"`
		}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
		require.Equal(t, chatID, response.Data.ChatModelID)
	}
}
