// Package patentexamples contains display-only recordings, never session or knowledge records.
package patentexamples

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"

	"github.com/Tencent/WeKnora/internal/types"
)

// Embedding keeps the offline examples in every backend image, without seed jobs.
//
//go:embed data/*
var bundle embed.FS

type Message struct {
	Role            string `json:"role"`
	Content         string `json:"content"`
	AgentDurationMs int64  `json:"agent_duration_ms,omitempty"`
	// Recorded timeline events exclude the final answer, which remains in Content.
	Events json.RawMessage `json:"events,omitempty"`
}

type Document struct {
	ID          string       `json:"id"`
	Filename    string       `json:"filename"`
	Source      string       `json:"source"`
	Publication *Publication `json:"publication,omitempty"`
}

// Publications contain only bibliographic facts and reviewed summaries, never PDF text chunks.
type Publication struct {
	Title           string     `json:"title"`
	Applicant       string     `json:"applicant"`
	ApplicationDate string     `json:"application_date"`
	PublicationDate string     `json:"publication_date"`
	Evidence        []Evidence `json:"evidence"`
}

type Evidence struct {
	ID       string `json:"id"`
	Location string `json:"location"`
	Summary  string `json:"summary"`
}

type Example struct {
	ID          string              `json:"id"`
	Mode        types.WorkspaceMode `json:"workspace_mode"`
	Title       string              `json:"title"`
	Description string              `json:"description"`
	Model       string              `json:"model"`
	RecordedAt  string              `json:"recorded_at"`
	Limitations string              `json:"limitations"`
	Attribution string              `json:"attribution,omitempty"`
	Documents   []Document          `json:"documents"`
	Messages    []Message           `json:"messages,omitempty"`
}

type Catalog struct {
	Version  string    `json:"version"`
	Examples []Example `json:"examples"`
}

func Load() (*Catalog, error) {
	raw, err := bundle.ReadFile("data/catalog.json")
	if err != nil {
		return nil, err
	}
	var catalog Catalog
	if err := json.Unmarshal(raw, &catalog); err != nil {
		return nil, fmt.Errorf("load patent examples: %w", err)
	}
	return &catalog, nil
}

func (c *Catalog) Find(id string) (*Example, error) {
	for i := range c.Examples {
		if c.Examples[i].ID == id {
			return &c.Examples[i], nil
		}
	}
	return nil, fs.ErrNotExist
}

// Only case-listed assets can be opened; publications never read files or fetch URLs.
func (e *Example) ReadDocument(id string) (Document, string, error) {
	for _, doc := range e.Documents {
		if doc.ID == id {
			if doc.Publication != nil {
				return doc, "", nil
			}
			raw, err := bundle.ReadFile("data/" + doc.ID + ".md")
			return doc, string(raw), err
		}
	}
	return Document{}, "", fs.ErrNotExist
}
