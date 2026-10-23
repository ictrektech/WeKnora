package patentexamples

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io/fs"
	"regexp"
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func TestBundleContainsReviewedIndependentRecordings(t *testing.T) {
	catalog, err := Load()
	require.NoError(t, err)
	require.Equal(t, "2026-10-23.3", catalog.Version)
	require.Len(t, catalog.Examples, 6)
	seen := map[string]bool{}
	private := regexp.MustCompile(`(?i)attachment://|weknora\.local|10\.144\.|/home/czy|[a-f0-9]{8}-(?:[a-f0-9]{4}-){3}[a-f0-9]{12}`)
	for _, example := range catalog.Examples {
		require.False(t, seen[example.ID])
		seen[example.ID] = true
		require.True(t, example.Mode.IsPatent())
		require.Equal(t, "workbuddy/deepseek-v4.1-flash", example.Model)
		require.True(t, strings.HasPrefix(example.RecordedAt, "2026-10-22"))
		require.NotEmpty(t, example.Limitations)
		for i, message := range example.Messages {
			expectedRole := "user"
			if i%2 != 0 {
				expectedRole = "assistant"
			}
			require.Equal(t, expectedRole, message.Role)
			require.NotEmpty(t, message.Content)
			require.False(t, private.MatchString(message.Content), "recordings must not contain development identifiers")
			require.False(t, private.Match(message.Events), "tool records must not contain development identifiers")
		}
		if example.Mode == types.WorkspaceModePatentWriting && example.ID != "writing-outline" {
			require.Empty(t, example.Documents, "independent cases must not carry scheduler attachments")
			require.Empty(t, example.Attribution)
		}
	}
	outline, err := catalog.Find("writing-outline")
	require.NoError(t, err)
	require.Len(t, outline.Messages, 4)
	require.Len(t, outline.Documents, 2)
	license, err := bundle.ReadFile("data/LICENSE")
	require.NoError(t, err)
	require.Contains(t, outline.Attribution, string(license))
	require.Contains(t, outline.Attribution, "6935a14d342ed9c7a07440c999d1b7bf1852a701")
	documentDigests := map[string]string{
		"architecture":  "69a48fe7435a5a67e128780ebfebf0013c5495f7ce8d9ef308167a3ae09d9646",
		"meeting-notes": "3aa3259ecb71940275fbf7de1c4b4c5f51305c230b09d4e77bc8e4c12ca3e796",
	}
	for _, doc := range outline.Documents {
		_, content, err := outline.ReadDocument(doc.ID)
		require.NoError(t, err)
		require.Contains(t, content, "虚构示例")
		require.Equal(t, documentDigests[doc.ID], fmt.Sprintf("%x", sha256.Sum256([]byte(content))))
	}
}

func TestRecordedAnswersHaveNotBeenRewritten(t *testing.T) {
	catalog, err := Load()
	require.NoError(t, err)
	// Digests of the completed answers retained in the original live acceptance runs.
	want := map[string][]string{
		"writing-outline":     {"6b2a6831d0e1f64a8eb997cb715344fb4da39681bac025e0670ebbcc5f2467f0", "9fc30fc6a23cae32cfc894415ec8c9d072a8b6f02e5ccc13b0c3a33bb0a22a10"},
		"writing-consistency": {"96ee59e3f303bebbac5fe0fd576ca702b2d219dd24270127ace17eb0f781258f"},
		"writing-no-material": {"149c695e4065fee20d9b5a18e9c3b9b83a858c8bc3e88eaf49596c1292b9fe2c"},
	}
	for _, example := range catalog.Examples {
		if example.Mode != types.WorkspaceModePatentWriting {
			continue
		}
		var digests []string
		for _, m := range example.Messages {
			if m.Role == "assistant" {
				digests = append(digests, fmt.Sprintf("%x", sha256.Sum256([]byte(m.Content))))
			}
		}
		require.Equal(t, want[example.ID], digests, "recorded answer text must remain unchanged")
	}
}

func TestDocumentsAreBoundToTheirOwnCase(t *testing.T) {
	catalog, err := Load()
	require.NoError(t, err)
	_, err = catalog.Find("../catalog.json")
	require.ErrorIs(t, err, fs.ErrNotExist)
	for _, example := range catalog.Examples {
		for _, id := range []string{"../catalog", "LICENSE", "catalog"} {
			_, _, err := example.ReadDocument(id)
			require.ErrorIs(t, err, fs.ErrNotExist)
		}
		if example.ID != "writing-outline" {
			_, _, err := example.ReadDocument("architecture")
			require.ErrorIs(t, err, fs.ErrNotExist)
		}
	}
	first, err := catalog.Find("writing-outline")
	require.NoError(t, err)
	first.Title = "mutated"
	fresh, err := Load()
	require.NoError(t, err)
	unchanged, err := fresh.Find("writing-outline")
	require.NoError(t, err)
	require.NotEqual(t, first.Title, unchanged.Title, "request state cannot mutate the bundled examples")
}

func TestToolHistoryPreservesActualCallsWithoutPublishingPatentText(t *testing.T) {
	catalog, err := Load()
	require.NoError(t, err)
	wantCalls := map[string][]int{
		"search-political-qa": {3}, "search-tax-agent": {2}, "search-topic": {2},
		"writing-outline": {2, 0}, "writing-consistency": {0}, "writing-no-material": {0},
	}
	wantDurations := map[string][]int64{
		"search-political-qa": {20225}, "search-tax-agent": {15573}, "search-topic": {10507},
		"writing-outline": {92045, 56596}, "writing-consistency": {24588}, "writing-no-material": {6428},
	}
	for _, example := range catalog.Examples {
		var counts []int
		var durations []int64
		for _, message := range example.Messages {
			if message.Role != "assistant" {
				require.Empty(t, message.Events)
				continue
			}
			var events []struct {
				Type     string         `json:"type"`
				ToolName string         `json:"tool_name"`
				Output   string         `json:"output"`
				ToolData map[string]any `json:"tool_data"`
			}
			require.NoError(t, json.Unmarshal(message.Events, &events))
			calls := 0
			for _, event := range events {
				require.Contains(t, []string{"tool_call", "progress"}, event.Type, "no synthetic thinking or answer events")
				if event.Type != "tool_call" {
					continue
				}
				calls++
				if example.Mode == types.WorkspaceModePatentSearch {
					require.Contains(t, []string{"read_document", "search_knowledge"}, event.ToolName)
					require.Contains(t, event.Output, "正文和原始检索分块已省略")
					for _, field := range []string{"document", "results", "chunks", "chunk_ids", "knowledge_id", "knowledge_base_id"} {
						require.NotContains(t, event.ToolData, field)
					}
				} else {
					require.Equal(t, "read_file", event.ToolName)
					require.NotEmpty(t, event.Output)
				}
			}
			counts = append(counts, calls)
			durations = append(durations, message.AgentDurationMs)
		}
		require.Equal(t, wantCalls[example.ID], counts)
		require.Equal(t, wantDurations[example.ID], durations)
	}
}

func TestSearchExamplesContainOnlyScopedPublicationSummaries(t *testing.T) {
	catalog, err := Load()
	require.NoError(t, err)
	wantCounts := map[string]int{"search-political-qa": 1, "search-tax-agent": 1, "search-topic": 2}
	wantDigests := map[string]string{
		"search-political-qa": "3acab9bb6fe1bc53362822f583c9b62597df7bfe6acb8002b2b9ba6ca3876348",
		"search-tax-agent":    "970f63bc7e39944cc00c24bed51cdd46d5f10262d0806a78f8517643b4e84b5f",
		"search-topic":        "b94c3b0cc33ac42f7a8594b6d72f9a1e44bcd91130a1b8c78a339a39bfbc6519",
	}
	links := regexp.MustCompile(`#example-source/([^/)]+)/([^/)]+)`)
	for id, count := range wantCounts {
		example, err := catalog.Find(id)
		require.NoError(t, err)
		require.Equal(t, types.WorkspaceModePatentSearch, example.Mode)
		require.Len(t, example.Documents, count)
		require.Len(t, example.Messages, 2)
		require.Contains(t, example.Limitations, "保留完整提问和回答")
		require.Contains(t, example.Limitations, "工具返回正文未随包分发")
		require.Equal(t, wantDigests[id], fmt.Sprintf("%x", sha256.Sum256([]byte(example.Messages[1].Content))))
		for _, doc := range example.Documents {
			require.NotNil(t, doc.Publication)
			require.Equal(t, "https://patents.google.com/patent/"+doc.ID+"/zh", doc.Source)
			require.Equal(t, "2025-05-09", doc.Publication.PublicationDate)
			require.NotEmpty(t, doc.Publication.Applicant)
			_, content, err := example.ReadDocument(doc.ID)
			require.NoError(t, err)
			require.Empty(t, content, "no full patent text is bundled or fetched")
			for _, evidence := range doc.Publication.Evidence {
				require.NotEmpty(t, evidence.Location)
				require.Less(t, len([]rune(evidence.Summary)), 100)
			}
		}
		for _, link := range links.FindAllStringSubmatch(example.Messages[1].Content, -1) {
			doc, _, err := example.ReadDocument(link[1])
			require.NoError(t, err, "citations must remain in this case")
			found := false
			for _, evidence := range doc.Publication.Evidence {
				found = found || evidence.ID == link[2]
			}
			require.True(t, found, "citation must identify reviewed source evidence")
		}
	}
	files, err := fs.ReadDir(bundle, "data")
	require.NoError(t, err)
	for _, file := range files {
		require.Contains(t, []string{"catalog.json", "architecture.md", "meeting-notes.md", "LICENSE"}, file.Name(), "no patent PDFs, full text or index files may be bundled")
	}
}
