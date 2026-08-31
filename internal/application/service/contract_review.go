package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Tencent/WeKnora/internal/infrastructure/chunker"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	secutils "github.com/Tencent/WeKnora/internal/utils"
	"github.com/google/uuid"
	"github.com/hibiken/asynq"
	"gorm.io/gorm"
)

var (
	ErrContractReviewNotFound     = errors.New("contract review not found")
	ErrContractReviewInvalidState = errors.New("contract review is not in a valid state for this action")
	ErrContractReviewInvalidFile  = errors.New("only PDF and DOCX contracts are supported")
	ErrContractReviewModelMissing = errors.New("no contract review model is configured")
)

var contractReviewPlaybooks = []types.ContractReviewPlaybook{{
	ID: "general-contract-review", Name: "General Contract Review",
	Description: "A balanced review of commercial, legal, operational, and drafting risk.", Version: "1.0",
}}

const (
	contractReviewClauseSystemPrompt = "You are a senior commercial contracts lawyer reviewing exactly one supplied contract clause. Identify only concrete legal or commercial risks supported by that clause. Return exactly one JSON object and nothing else. Do not output markdown, code fences, headings, citations, a full-contract report, or reasoning text. If there are no concrete risks, return {\"issues\":[]}. Return at most five issues. Keep titles concise; keep explanations and suggestions concise (preferably under 180 Chinese characters each). Do not invent quotations. " +
		"Write issue titles, explanations, and suggestions in Simplified Chinese. Keep original_quote exactly in the contract's original language and wording so it can be located in the source document."
	contractReviewOverviewSystemPrompt = "Return a concise, evidence-based contract review overview as valid JSON only. Write executive_summary, contract_type, and key_recommendations in Simplified Chinese. Preserve party names and other proper nouns in their original form."
)

const (
	contractReviewClauseDefaultMaxCompletionTokens = 4096
	contractReviewClauseRetryMaxCompletionTokens   = 8192
)

func contractReviewPlaybook(id string) (types.ContractReviewPlaybook, bool) {
	for _, playbook := range contractReviewPlaybooks {
		if playbook.ID == id {
			return playbook, true
		}
	}
	return types.ContractReviewPlaybook{}, false
}

type contractReviewService struct {
	repo      interfaces.ContractReviewRepository
	files     interfaces.FileService
	resources interfaces.ResourceCatalog
	reader    interfaces.DocumentReader
	models    interfaces.ModelService
	agents    interfaces.CustomAgentService
	tasks     interfaces.TaskEnqueuer
}

func NewContractReviewService(repo interfaces.ContractReviewRepository, files interfaces.FileService,
	resources interfaces.ResourceCatalog, reader interfaces.DocumentReader, models interfaces.ModelService,
	agents interfaces.CustomAgentService, tasks interfaces.TaskEnqueuer) interfaces.ContractReviewService {
	return &contractReviewService{repo: repo, files: files, resources: resources, reader: reader, models: models, agents: agents, tasks: tasks}
}

func (s *contractReviewService) Playbooks() []types.ContractReviewPlaybook {
	return append([]types.ContractReviewPlaybook(nil), contractReviewPlaybooks...)
}

func (s *contractReviewService) Create(ctx context.Context, tenantID uint64, userID string) (*types.ContractReview, error) {
	r := &types.ContractReview{TenantID: tenantID, UserID: userID}
	if err := s.repo.Create(ctx, r); err != nil {
		return nil, err
	}
	return r, nil
}

func (s *contractReviewService) List(ctx context.Context, tenantID uint64, userID string, archived bool) ([]*types.ContractReview, error) {
	return s.repo.List(ctx, tenantID, userID, archived)
}

func (s *contractReviewService) Get(ctx context.Context, tenantID uint64, userID, id string) (*types.ContractReview, error) {
	r, err := s.repo.Get(ctx, tenantID, userID, id)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrContractReviewNotFound
	}
	return r, err
}

func validParty(value string) bool {
	switch types.ContractReviewParty(value) {
	case types.ContractReviewPartyCustomer, types.ContractReviewPartyVendor, types.ContractReviewPartyNeutral:
		return true
	default:
		return false
	}
}

func canRetryContractReview(status types.ContractReviewStatus) bool {
	return status == types.ContractReviewStatusFailed || status == types.ContractReviewStatusCompleted
}

func canUpdateContractReviewConfig(status types.ContractReviewStatus) bool {
	return status == types.ContractReviewStatusDraft || status == types.ContractReviewStatusReady || status == types.ContractReviewStatusCompleted
}

func (s *contractReviewService) Update(ctx context.Context, tenantID uint64, userID, id, title, playbook, party string, archived *bool) (*types.ContractReview, error) {
	r, err := s.Get(ctx, tenantID, userID, id)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(title) != "" {
		r.Title = strings.TrimSpace(title)
		r.TitleCustomized = true
	}
	if playbook != "" {
		selected, ok := contractReviewPlaybook(playbook)
		if !ok {
			return nil, fmt.Errorf("unknown playbook")
		}
		if !canUpdateContractReviewConfig(r.Status) {
			return nil, ErrContractReviewInvalidState
		}
		r.PlaybookID, r.PlaybookVersion = selected.ID, selected.Version
	}
	if party != "" {
		if !validParty(party) {
			return nil, fmt.Errorf("invalid represented party")
		}
		if !canUpdateContractReviewConfig(r.Status) {
			return nil, ErrContractReviewInvalidState
		}
		r.RepresentedParty = types.ContractReviewParty(party)
	}
	if archived != nil {
		if r.Status == types.ContractReviewStatusUploading || r.Status == types.ContractReviewStatusAnalyzing || r.Status == types.ContractReviewStatusReviewingClauses {
			return nil, ErrContractReviewInvalidState
		}
		if *archived {
			now := time.Now()
			r.ArchivedAt = &now
		} else {
			r.ArchivedAt = nil
		}
	}
	if err := s.repo.Update(ctx, r); err != nil {
		return nil, err
	}
	return s.Get(ctx, tenantID, userID, id)
}

func (s *contractReviewService) Delete(ctx context.Context, tenantID uint64, userID, id string) error {
	r, err := s.Get(ctx, tenantID, userID, id)
	if err != nil {
		return err
	}
	if r.ResourceRef != "" {
		_ = s.files.DeleteFile(ctx, r.ResourceRef)
	}
	return s.repo.Delete(ctx, tenantID, userID, id)
}

func (s *contractReviewService) BulkAction(ctx context.Context, tenantID uint64, userID string, ids []string, action types.ContractReviewBulkAction) (*types.ContractReviewBulkResult, error) {
	if len(ids) == 0 || len(ids) > 500 {
		return nil, fmt.Errorf("contract review bulk action requires between 1 and 500 ids")
	}
	if action != types.ContractReviewBulkArchive && action != types.ContractReviewBulkRestore && action != types.ContractReviewBulkDelete {
		return nil, fmt.Errorf("invalid contract review bulk action")
	}
	unique := make([]string, 0, len(ids))
	seen := make(map[string]struct{}, len(ids))
	for _, rawID := range ids {
		id := strings.TrimSpace(rawID)
		if id == "" {
			continue
		}
		if _, exists := seen[id]; exists {
			continue
		}
		seen[id] = struct{}{}
		unique = append(unique, id)
	}
	if len(unique) == 0 {
		return nil, fmt.Errorf("contract review bulk action requires at least one valid id")
	}
	result := &types.ContractReviewBulkResult{Action: action, Requested: len(unique), Items: make([]types.ContractReviewBulkItem, 0, len(unique))}
	for _, id := range unique {
		var err error
		if action == types.ContractReviewBulkDelete {
			err = s.Delete(ctx, tenantID, userID, id)
		} else {
			archived := action == types.ContractReviewBulkArchive
			_, err = s.Update(ctx, tenantID, userID, id, "", "", "", &archived)
		}
		item := types.ContractReviewBulkItem{ID: id, Success: err == nil}
		if err != nil {
			item.Error = err.Error()
			result.Failed++
		} else {
			result.Succeeded++
		}
		result.Items = append(result.Items, item)
	}
	return result, nil
}

func (s *contractReviewService) Upload(ctx context.Context, tenantID uint64, userID, id, fileName, mimeType string, fileSize int64, body io.Reader) (*types.ContractReview, error) {
	r, err := s.Get(ctx, tenantID, userID, id)
	if err != nil {
		return nil, err
	}
	if r.Status != types.ContractReviewStatusDraft && r.Status != types.ContractReviewStatusReady && r.Status != types.ContractReviewStatusFailed {
		return nil, ErrContractReviewInvalidState
	}
	safe, err := secutils.SafeFileName(fileName)
	if err != nil {
		return nil, ErrContractReviewInvalidFile
	}
	ext := strings.ToLower(filepath.Ext(safe))
	if ext != ".pdf" && ext != ".docx" {
		return nil, ErrContractReviewInvalidFile
	}
	max := secutils.GetMaxFileSizeMB() * 1024 * 1024
	if fileSize <= 0 || fileSize > max {
		return nil, fmt.Errorf("file size must be between 1 byte and %dMB", secutils.GetMaxFileSizeMB())
	}
	data, err := io.ReadAll(io.LimitReader(body, max+1))
	if err != nil {
		return nil, fmt.Errorf("read contract file: %w", err)
	}
	if int64(len(data)) > max {
		return nil, fmt.Errorf("file exceeds size limit of %dMB", secutils.GetMaxFileSizeMB())
	}
	if (ext == ".pdf" && !strings.HasPrefix(string(data[:min(len(data), 5)]), "%PDF-")) ||
		(ext == ".docx" && (len(data) < 4 || string(data[:4]) != "PK\x03\x04")) {
		return nil, ErrContractReviewInvalidFile
	}
	ref, err := s.files.SaveBytes(ctx, data, tenantID, "contract_review_"+uuid.NewString()[:12]+ext, false)
	if err != nil {
		return nil, err
	}
	if s.resources != nil {
		if err := s.resources.Bind(ctx, ref, "contract_review", r.ID, "source_file"); err != nil {
			_ = s.files.DeleteFile(ctx, ref)
			return nil, err
		}
	}
	oldRef := r.ResourceRef
	r.ResourceRef, r.FileName, r.FileType, r.MimeType, r.FileSize = ref, safe, ext, strings.TrimSpace(mimeType), int64(len(data))
	r.Status, r.Progress, r.ErrorMessage, r.ExtractedContent = types.ContractReviewStatusUploading, 5, "", ""
	r.Overview = types.JSON(`{}`)
	if !r.TitleCustomized {
		r.Title = strings.TrimSuffix(safe, ext)
	}
	if err := s.repo.ClearResults(ctx, r.ID); err != nil {
		_ = s.files.DeleteFile(ctx, ref)
		return nil, err
	}
	if err := s.repo.Update(ctx, r); err != nil {
		_ = s.files.DeleteFile(ctx, ref)
		return nil, err
	}
	if oldRef != "" {
		_ = s.files.DeleteFile(ctx, oldRef)
	}
	if err := s.enqueue(ctx, types.TypeContractReviewDocumentProcess, r, 2, 10*time.Minute); err != nil {
		r.Status, r.ErrorMessage = types.ContractReviewStatusFailed, "failed to schedule document parsing"
		_ = s.repo.Update(ctx, r)
		return r, err
	}
	return r, nil
}

func (s *contractReviewService) OpenDocument(ctx context.Context, tenantID uint64, userID, id string) (*types.ContractReview, io.ReadCloser, error) {
	r, err := s.Get(ctx, tenantID, userID, id)
	if err != nil {
		return nil, nil, err
	}
	if r.ResourceRef == "" {
		return nil, nil, ErrContractReviewNotFound
	}
	f, err := s.files.GetFile(ctx, r.ResourceRef)
	return r, f, err
}

func (s *contractReviewService) Start(ctx context.Context, tenantID uint64, userID, id string) (*types.ContractReview, error) {
	r, err := s.Get(ctx, tenantID, userID, id)
	if err != nil {
		return nil, err
	}
	if r.Status != types.ContractReviewStatusReady {
		return nil, ErrContractReviewInvalidState
	}
	r.Status, r.Progress, r.ErrorMessage = types.ContractReviewStatusAnalyzing, 20, ""
	now := time.Now()
	r.StartedAt, r.CompletedAt = &now, nil
	if err := s.repo.Update(ctx, r); err != nil {
		return nil, err
	}
	if err := s.enqueue(ctx, types.TypeContractReviewAnalyze, r, 1, 30*time.Minute); err != nil {
		r.Status, r.ErrorMessage = types.ContractReviewStatusFailed, "failed to schedule contract review"
		_ = s.repo.Update(ctx, r)
		return r, err
	}
	return r, nil
}

func (s *contractReviewService) Retry(ctx context.Context, tenantID uint64, userID, id string) (*types.ContractReview, error) {
	r, err := s.Get(ctx, tenantID, userID, id)
	if err != nil {
		return nil, err
	}
	if !canRetryContractReview(r.Status) {
		return nil, ErrContractReviewInvalidState
	}
	if r.ExtractedContent == "" {
		r.Status, r.Progress, r.ErrorMessage = types.ContractReviewStatusUploading, 5, ""
		if err := s.repo.Update(ctx, r); err != nil {
			return nil, err
		}
		return r, s.enqueue(ctx, types.TypeContractReviewDocumentProcess, r, 2, 10*time.Minute)
	}
	r.Status, r.Progress, r.ErrorMessage = types.ContractReviewStatusAnalyzing, 20, ""
	r.CompletedAt = nil
	r.Overview = types.JSON(`{}`)
	r.Clauses = nil
	r.Issues = nil
	now := time.Now()
	r.StartedAt = &now
	if err := s.repo.ClearResults(ctx, r.ID); err != nil {
		return nil, err
	}
	if err := s.repo.Update(ctx, r); err != nil {
		return nil, err
	}
	if err := s.enqueue(ctx, types.TypeContractReviewAnalyze, r, 1, 30*time.Minute); err != nil {
		r.Status, r.ErrorMessage = types.ContractReviewStatusFailed, "failed to schedule contract review"
		_ = s.repo.Update(ctx, r)
		return r, err
	}
	return r, nil
}

func (s *contractReviewService) enqueue(ctx context.Context, taskType string, r *types.ContractReview, retries int, timeout time.Duration) error {
	payload, _ := json.Marshal(types.ContractReviewTaskPayload{TenantID: r.TenantID, UserID: r.UserID, ReviewID: r.ID})
	queue, _ := types.QueueForTaskType(taskType)
	_, err := s.tasks.Enqueue(asynq.NewTask(taskType, payload), asynq.Queue(queue), asynq.MaxRetry(retries), asynq.Timeout(timeout))
	return err
}

func reviewTaskContext(ctx context.Context, p types.ContractReviewTaskPayload) context.Context {
	ctx = context.WithValue(ctx, types.TenantIDContextKey, p.TenantID)
	ctx = context.WithValue(ctx, types.UserIDContextKey, p.UserID)
	return ctx
}

func (s *contractReviewService) ProcessDocument(ctx context.Context, task *asynq.Task) error {
	var p types.ContractReviewTaskPayload
	if err := json.Unmarshal(task.Payload(), &p); err != nil {
		return err
	}
	ctx = reviewTaskContext(ctx, p)
	r, err := s.Get(ctx, p.TenantID, p.UserID, p.ReviewID)
	if err != nil {
		if errors.Is(err, ErrContractReviewNotFound) {
			return nil
		}
		return err
	}
	if r.Status != types.ContractReviewStatusUploading && !(r.Status == types.ContractReviewStatusFailed && r.ExtractedContent == "") {
		return nil
	}
	if r.Status == types.ContractReviewStatusFailed {
		r.Status, r.ErrorMessage = types.ContractReviewStatusUploading, ""
		_ = s.repo.Update(ctx, r)
	}
	f, err := s.files.GetFile(ctx, r.ResourceRef)
	if err != nil {
		return s.fail(ctx, r, err)
	}
	data, err := io.ReadAll(f)
	_ = f.Close()
	if err != nil {
		return s.fail(ctx, r, err)
	}
	if s.reader == nil {
		return s.fail(ctx, r, errors.New("document reader is not configured"))
	}
	result, err := s.reader.Read(ctx, &types.ReadRequest{FileContent: data, FileName: r.FileName, FileType: strings.TrimPrefix(r.FileType, ".")})
	if err != nil {
		return s.fail(ctx, r, err)
	}
	if result == nil || strings.TrimSpace(result.Error) != "" {
		if result == nil {
			return s.fail(ctx, r, errors.New("document reader returned no result"))
		}
		return s.fail(ctx, r, errors.New(result.Error))
	}
	if _, err := s.Get(ctx, p.TenantID, p.UserID, p.ReviewID); err != nil {
		// A user may delete a review while parsing is in flight. Never recreate
		// or update the soft-deleted aggregate after the parser returns.
		return nil
	}
	meta, _ := json.Marshal(result.Metadata)
	r.ExtractedContent, r.Metadata = strings.TrimSpace(result.MarkdownContent), types.JSON(meta)
	if r.ExtractedContent == "" {
		return s.fail(ctx, r, errors.New("the contract contains no extractable text"))
	}
	r.Status, r.Progress, r.ErrorMessage = types.ContractReviewStatusReady, 15, ""
	return s.repo.Update(ctx, r)
}

func (s *contractReviewService) fail(ctx context.Context, r *types.ContractReview, cause error) error {
	r.Status, r.ErrorMessage = types.ContractReviewStatusFailed, cause.Error()
	_ = s.repo.Update(ctx, r)
	return cause
}

type reviewIssueOutput struct {
	RiskLevel     string `json:"risk_level"`
	Title         string `json:"title"`
	Explanation   string `json:"explanation"`
	OriginalQuote string `json:"original_quote"`
	Suggestion    string `json:"suggestion"`
}
type reviewBatchOutput struct {
	Issues []reviewIssueOutput `json:"issues"`
}
type reviewOverviewOutput struct {
	OverallRisk        string   `json:"overall_risk"`
	ExecutiveSummary   string   `json:"executive_summary"`
	ContractType       string   `json:"contract_type"`
	Parties            []string `json:"parties"`
	KeyRecommendations []string `json:"key_recommendations"`
}

var markdownHeading = regexp.MustCompile(`(?m)^#{1,4}\s+(.+)$`)

func buildReviewClauses(reviewID, content string) []*types.ContractReviewClause {
	cfg := chunker.DefaultConfig()
	cfg.Strategy = chunker.StrategyAuto
	cfg.ChunkSize = 2800
	cfg.ChunkOverlap = 120
	parts := chunker.Split(content, cfg)
	rows := make([]*types.ContractReviewClause, 0, len(parts))
	for idx, part := range parts {
		title := fmt.Sprintf("Clause %d", idx+1)
		if match := markdownHeading.FindStringSubmatch(part.Content); len(match) > 1 {
			title = strings.TrimSpace(match[1])
		}
		excerpt := strings.TrimSpace(part.Content)
		if len([]rune(excerpt)) > 360 {
			excerpt = string([]rune(excerpt)[:360]) + "…"
		}
		rows = append(rows, &types.ContractReviewClause{ReviewID: reviewID, Sequence: idx, Title: title, Excerpt: excerpt, SourceStart: part.Start, SourceEnd: part.End})
	}
	return rows
}

func parseModelJSON(content string, target any) error {
	content = strings.TrimSpace(content)
	content = strings.TrimPrefix(content, "```json")
	content = strings.TrimPrefix(content, "```")
	content = strings.TrimSuffix(content, "```")
	return json.Unmarshal([]byte(strings.TrimSpace(content)), target)
}

func (s *contractReviewService) resolveReviewModel(ctx context.Context) (chat.Chat, *types.CustomAgent, error) {
	agent, _ := s.agents.GetAgentByID(ctx, types.BuiltinContractReviewID)
	modelID := ""
	if agent != nil {
		modelID = strings.TrimSpace(agent.Config.ModelID)
	}
	if modelID == "" {
		models, err := s.models.ListModels(ctx)
		if err != nil {
			return nil, agent, err
		}
		for _, m := range models {
			if m != nil && m.Type == types.ModelTypeKnowledgeQA && m.Status == types.ModelStatusActive && m.IsDefault {
				modelID = m.ID
				break
			}
		}
		if modelID == "" {
			for _, m := range models {
				if m != nil && m.Type == types.ModelTypeKnowledgeQA && m.Status == types.ModelStatusActive {
					modelID = m.ID
					break
				}
			}
		}
	}
	if modelID == "" {
		return nil, agent, ErrContractReviewModelMissing
	}
	model, err := s.models.GetChatModel(ctx, modelID)
	return model, agent, err
}

func validRisk(value string) types.ContractReviewRiskLevel {
	switch strings.ToLower(value) {
	case "high":
		return types.ContractReviewRiskHigh
	case "low":
		return types.ContractReviewRiskLow
	default:
		return types.ContractReviewRiskMedium
	}
}

func contractReviewClauseMaxCompletionTokens(agent *types.CustomAgent) int {
	if agent != nil && agent.Config.MaxCompletionTokens > 0 {
		return agent.Config.MaxCompletionTokens
	}
	return contractReviewClauseDefaultMaxCompletionTokens
}

func contractReviewClauseRetryTokens(current int) int {
	if current < contractReviewClauseRetryMaxCompletionTokens {
		return contractReviewClauseRetryMaxCompletionTokens
	}
	return current
}

func contractReviewOutputReachedLimit(resp *types.ChatResponse) bool {
	if resp == nil {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(resp.FinishReason)) {
	case "length", "max_tokens", "max_completion_tokens":
		return true
	default:
		return false
	}
}

func issueFingerprint(reviewID, clauseID, title, quote string) string {
	sum := sha256.Sum256([]byte(reviewID + "\x00" + clauseID + "\x00" + strings.ToLower(strings.TrimSpace(title)) + "\x00" + strings.Join(strings.Fields(quote), " ")))
	return hex.EncodeToString(sum[:])
}

func (s *contractReviewService) ProcessReview(ctx context.Context, task *asynq.Task) error {
	var p types.ContractReviewTaskPayload
	if err := json.Unmarshal(task.Payload(), &p); err != nil {
		return err
	}
	ctx = reviewTaskContext(ctx, p)
	r, err := s.Get(ctx, p.TenantID, p.UserID, p.ReviewID)
	if err != nil {
		if errors.Is(err, ErrContractReviewNotFound) {
			return nil
		}
		return err
	}
	if r.Status != types.ContractReviewStatusAnalyzing && r.Status != types.ContractReviewStatusReviewingClauses && r.Status != types.ContractReviewStatusFailed {
		return nil
	}
	if r.Status == types.ContractReviewStatusFailed {
		if err := s.repo.ClearResults(ctx, r.ID); err != nil {
			return err
		}
		r.Status, r.Progress, r.ErrorMessage = types.ContractReviewStatusAnalyzing, 20, ""
		if err := s.repo.Update(ctx, r); err != nil {
			return err
		}
	}
	model, agent, err := s.resolveReviewModel(ctx)
	if err != nil {
		return s.fail(ctx, r, err)
	}
	clauses := buildReviewClauses(r.ID, r.ExtractedContent)
	if len(clauses) == 0 {
		return s.fail(ctx, r, errors.New("no reviewable clauses found"))
	}
	if err := s.repo.ReplaceClauses(ctx, r.ID, clauses); err != nil {
		return s.fail(ctx, r, err)
	}
	r.Status, r.Progress = types.ContractReviewStatusReviewingClauses, 25
	if err := s.repo.Update(ctx, r); err != nil {
		return err
	}
	// The built-in contract-review agent has a user-facing full-report prompt
	// (headings, citations, and seven report sections). Do not prepend it here:
	// this worker needs a small machine-readable clause result, and mixing the
	// two contracts is what makes the model produce verbose/truncated output.
	systemPrompt := contractReviewClauseSystemPrompt
	maxTokens, temperature := contractReviewClauseMaxCompletionTokens(agent), 0.2
	if agent != nil {
		temperature = agent.Config.Temperature
	}
	thinking := false
	format := json.RawMessage(`{"type":"object","properties":{"issues":{"type":"array","maxItems":5,"items":{"type":"object","properties":{"risk_level":{"type":"string","enum":["high","medium","low"]},"title":{"type":"string","maxLength":80},"explanation":{"type":"string","maxLength":540},"original_quote":{"type":"string","maxLength":360},"suggestion":{"type":"string","maxLength":540}},"required":["risk_level","title","explanation","original_quote","suggestion"],"additionalProperties":false}}},"required":["issues"],"additionalProperties":false}`)
	issueSeq := 0
	contentRunes := []rune(r.ExtractedContent)
	for idx, clause := range clauses {
		latest, getErr := s.Get(ctx, p.TenantID, p.UserID, p.ReviewID)
		if getErr != nil {
			return nil
		}
		if latest.DeletedAt.Valid {
			return nil
		}
		if clause.SourceStart < 0 || clause.SourceEnd > len(contentRunes) || clause.SourceStart >= clause.SourceEnd {
			return s.fail(ctx, r, fmt.Errorf("invalid source range for clause %d", idx+1))
		}
		body := string(contentRunes[clause.SourceStart:clause.SourceEnd])
		playbook, _ := contractReviewPlaybook(r.PlaybookID)
		prompt := fmt.Sprintf("Playbook: %s v%s\nRepresented party: %s\nClause title: %s\nClause text:\n%s", playbook.Name, r.PlaybookVersion, r.RepresentedParty, clause.Title, body)
		var out reviewBatchOutput
		var callErr error
		for attempt := 0; attempt < 2; attempt++ {
			attemptPrompt := prompt
			attemptMaxTokens := maxTokens
			if attempt > 0 {
				attemptMaxTokens = contractReviewClauseRetryTokens(maxTokens)
				attemptPrompt += "\nRecovery instruction: the previous response was not a complete valid JSON object. Output only the compact {\"issues\": [...]} object now; do not add any explanation, report headings, citations, or markdown."
			}
			out = reviewBatchOutput{}
			resp, e := model.Chat(ctx, []chat.Message{{Role: "system", Content: systemPrompt}, {Role: "user", Content: attemptPrompt}}, &chat.ChatOptions{Temperature: temperature, MaxCompletionTokens: attemptMaxTokens, Thinking: &thinking, Format: format})
			if e != nil {
				callErr = e
				continue
			}
			if resp == nil {
				callErr = errors.New("model returned no response")
				continue
			}
			if contractReviewOutputReachedLimit(resp) {
				callErr = fmt.Errorf("model output reached the %d-token completion limit", attemptMaxTokens)
				continue
			}
			callErr = parseModelJSON(resp.Content, &out)
			if callErr == nil {
				break
			}
		}
		if callErr != nil {
			return s.fail(ctx, r, fmt.Errorf("review clause %d: %w", idx+1, callErr))
		}
		if _, getErr := s.Get(ctx, p.TenantID, p.UserID, p.ReviewID); getErr != nil {
			return nil
		}
		for _, result := range out.Issues {
			if strings.TrimSpace(result.Title) == "" || strings.TrimSpace(result.OriginalQuote) == "" {
				continue
			}
			start := clause.SourceStart
			if rel := strings.Index(body, result.OriginalQuote); rel >= 0 {
				start = clause.SourceStart + utf8.RuneCountInString(body[:rel])
			}
			issue := &types.ContractReviewIssue{ID: uuid.NewSHA1(uuid.NameSpaceOID, []byte(issueFingerprint(r.ID, clause.ID, result.Title, result.OriginalQuote))).String(), ReviewID: r.ID, ClauseID: clause.ID,
				Fingerprint: issueFingerprint(r.ID, clause.ID, result.Title, result.OriginalQuote), Sequence: issueSeq, RiskLevel: validRisk(result.RiskLevel), Title: strings.TrimSpace(result.Title),
				Explanation: strings.TrimSpace(result.Explanation), OriginalQuote: strings.TrimSpace(result.OriginalQuote), Suggestion: strings.TrimSpace(result.Suggestion), SourceStart: start, SourceEnd: start + utf8.RuneCountInString(result.OriginalQuote)}
			if err := s.repo.UpsertIssue(ctx, issue); err != nil {
				return s.fail(ctx, r, err)
			}
			issueSeq++
			clause.IssueCount++
		}
		clause.ReviewStatus = "completed"
		_ = s.repo.UpdateClause(ctx, clause)
		r.Progress = 25 + int(float64(idx+1)/float64(len(clauses))*60)
		_ = s.repo.Update(ctx, r)
	}
	detail, err := s.Get(ctx, p.TenantID, p.UserID, p.ReviewID)
	if err != nil {
		return err
	}
	counts := map[string]int{"high": 0, "medium": 0, "low": 0}
	for _, issue := range detail.Issues {
		counts[string(issue.RiskLevel)]++
	}
	overviewPrompt := fmt.Sprintf("请用简体中文总结本次合同审查并返回 JSON。合同文件名：%s。当前代表方：%s。风险数量：高风险=%d，中风险=%d，低风险=%d。问题标题：", r.FileName, r.RepresentedParty, counts["high"], counts["medium"], counts["low"])
	for _, issue := range detail.Issues {
		overviewPrompt += issue.Title + "; "
	}
	overviewFormat := json.RawMessage(`{"type":"object","properties":{"overall_risk":{"enum":["high","medium","low"]},"executive_summary":{"type":"string"},"contract_type":{"type":"string"},"parties":{"type":"array","items":{"type":"string"}},"key_recommendations":{"type":"array","items":{"type":"string"}}},"required":["overall_risk","executive_summary","contract_type","parties","key_recommendations"]}`)
	var overview reviewOverviewOutput
	resp, err := model.Chat(ctx, []chat.Message{{Role: "system", Content: contractReviewOverviewSystemPrompt}, {Role: "user", Content: overviewPrompt}}, &chat.ChatOptions{Temperature: 0.1, MaxCompletionTokens: 1200, Thinking: &thinking, Format: overviewFormat})
	if err == nil {
		err = parseModelJSON(resp.Content, &overview)
	}
	if err != nil {
		overview = reviewOverviewOutput{OverallRisk: "medium", ExecutiveSummary: "合同审查已完成，请在“问题”中查看详细风险与修改建议。"}
	}
	overviewJSON, _ := json.Marshal(map[string]any{"overall_risk": validRisk(overview.OverallRisk), "executive_summary": overview.ExecutiveSummary, "contract_type": overview.ContractType, "parties": overview.Parties, "key_recommendations": overview.KeyRecommendations, "risk_counts": counts})
	r.Overview, r.Status, r.Progress, r.ErrorMessage = types.JSON(overviewJSON), types.ContractReviewStatusCompleted, 100, ""
	now := time.Now()
	r.CompletedAt = &now
	if err := s.repo.Update(ctx, r); err != nil {
		logger.ErrorWithFields(ctx, err, nil)
		return err
	}
	return nil
}
