package postgres

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

type databaseTestError struct{ state, message string }

func (e *databaseTestError) Error() string    { return e.message }
func (e *databaseTestError) SQLState() string { return e.state }

func TestDiagnoseBM25Error(t *testing.T) {
	for _, message := range []string{"assertion failed: item_pointer_is_valid(ctid)", "could not read blocks 435..435"} {
		original := &databaseTestError{"XX000", message}
		got := diagnoseBM25Error(fmt.Errorf("batch index failed: %w", original))
		if !errors.Is(got, original) || !strings.Contains(got.Error(), "force_rebuild=true") {
			t.Fatalf("missing recovery guidance or lost error: %v", got)
		}
	}
	for _, original := range []error{nil, errors.New("connection refused"),
		&databaseTestError{"23505", "duplicate key"}, &databaseTestError{"XX000", "unrelated internal error"}} {
		if got := diagnoseBM25Error(original); got != original {
			t.Fatalf("changed unrelated error: %v", got)
		}
	}
}

func TestPgSearchNeedsUpgrade(t *testing.T) {
	for version, want := range map[string]bool{
		"0.22.2": true, "0.22.5": true, "0.22.6": false, "0.22.7": false,
		"0.25.9": false, "1.0.0": false, "": true, "unknown": true,
	} {
		if got := pgSearchNeedsUpgrade(version); got != want {
			t.Errorf("%s: got %v, want %v", version, got, want)
		}
	}
}
