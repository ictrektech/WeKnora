package postgres

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// Keep the original database error available to callers via errors.Is/As.
// Recovery belongs to the database owner, never to a request-time retriever.
func diagnoseBM25Error(err error) error {
	if err == nil {
		return nil
	}
	var state interface{ SQLState() string }
	if !errors.As(err, &state) || state.SQLState() != "XX000" {
		return err
	}
	message := strings.ToLower(err.Error())
	if !strings.Contains(message, "item_pointer_is_valid") &&
		!strings.Contains(message, "could not read block") {
		return err
	}
	return fmt.Errorf("PostgreSQL heap/index access failed (possible pg_search BM25 inconsistency); ask the PGV administrator to verify pg_search 0.22.6 and rebuild public.embeddings_search_idx with /init/pg-search-upgrade.sql (force_rebuild=true), then retry document processing/search: %w", err)
}

func pgSearchNeedsUpgrade(version string) bool {
	parts := strings.Split(version, ".")
	if len(parts) != 3 {
		return true
	}
	minimum := []int{0, 22, 6}
	for i, part := range parts {
		n, err := strconv.Atoi(part)
		if err != nil {
			return true
		}
		if n != minimum[i] {
			return n < minimum[i]
		}
	}
	return false
}
