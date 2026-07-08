package service

import (
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
)

func TestWikiParallelDefaultsPreferKBConfigOverEnv(t *testing.T) {
	t.Setenv("WEKNORA_WIKI_INGEST_MAP_PARALLEL", "2")
	t.Setenv("WEKNORA_WIKI_INGEST_REDUCE_PARALLEL", "3")

	cfg := &types.WikiConfig{}
	if got := cfg.IngestMapParallelOrDefault(wikiParallelDefault("WEKNORA_WIKI_INGEST_MAP_PARALLEL", 10)); got != 2 {
		t.Fatalf("map env default = %d, want 2", got)
	}
	if got := cfg.IngestReduceParallelOrDefault(wikiParallelDefault("WEKNORA_WIKI_INGEST_REDUCE_PARALLEL", 10)); got != 3 {
		t.Fatalf("reduce env default = %d, want 3", got)
	}

	cfg.IngestMapParallel = 5
	cfg.IngestReduceParallel = 6
	if got := cfg.IngestMapParallelOrDefault(wikiParallelDefault("WEKNORA_WIKI_INGEST_MAP_PARALLEL", 10)); got != 5 {
		t.Fatalf("map kb override = %d, want 5", got)
	}
	if got := cfg.IngestReduceParallelOrDefault(wikiParallelDefault("WEKNORA_WIKI_INGEST_REDUCE_PARALLEL", 10)); got != 6 {
		t.Fatalf("reduce kb override = %d, want 6", got)
	}
}
