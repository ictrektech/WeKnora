package postgres

import (
	"context"
	"os"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	pgdriver "gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// The PGV isolated old-volume upgrade gate supplies a disposable database.
func TestPGVBM25Integration(t *testing.T) {
	dsn := os.Getenv("HYBRAG_POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("run through PGV verify_pg_search_upgrade.sh with HYBRAG_PG_TEST_BIN")
	}
	db, err := gorm.Open(pgdriver.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()
	repo := NewPostgresRetrieveEngineRepository(db)
	ctx := context.Background()
	defer db.Exec("DELETE FROM embeddings WHERE knowledge_base_id = ?", "bm25-regression")
	rows := []*types.IndexInfo{
		{SourceID: "bm25-visible", ChunkID: "visible", KnowledgeID: "bm25-doc", KnowledgeBaseID: "bm25-regression", Content: "portable regression 文档检索", IsEnabled: true},
		{SourceID: "bm25-disabled", ChunkID: "disabled", KnowledgeID: "bm25-doc", KnowledgeBaseID: "bm25-regression", Content: "portable regression 文档检索", IsEnabled: false},
	}
	vectors := map[string][]float32{"bm25-visible": {1, 0, 0}, "bm25-disabled": {1, 0, 0}}
	if err := repo.BatchSave(ctx, rows, map[string]any{"embedding": vectors}); err != nil {
		t.Fatal(err)
	}
	for _, kind := range []types.RetrieverType{types.KeywordsRetrieverType, types.VectorRetrieverType} {
		results, err := repo.Retrieve(ctx, types.RetrieveParams{
			Query: "portable", Embedding: []float32{1, 0, 0}, KnowledgeBaseIDs: []string{"bm25-regression"},
			KnowledgeIDs: []string{"bm25-doc"}, TopK: 5, RetrieverType: kind,
		})
		if err != nil || len(results) != 1 || len(results[0].Results) != 1 || results[0].Results[0].ChunkID != "visible" {
			t.Fatalf("%s after upgrade/VACUUM: results=%v err=%v", kind, results, err)
		}
	}
	rows[0].Content = "reprocessed regression 文档检索"
	if err := repo.Save(ctx, rows[0], map[string]any{"embedding": vectors}); err != nil {
		t.Fatal(err)
	}
	results, err := repo.Retrieve(ctx, types.RetrieveParams{
		Query: "reprocessed", KnowledgeBaseIDs: []string{"bm25-regression"}, TopK: 5, RetrieverType: types.KeywordsRetrieverType,
	})
	if err != nil || len(results) != 1 || len(results[0].Results) != 1 || results[0].Results[0].ChunkID != "visible" {
		t.Fatalf("reprocessed document: results=%v err=%v", results, err)
	}
}
