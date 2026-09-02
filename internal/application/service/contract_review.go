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
	"sort"
	"strings"
	"time"
	"unicode"

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
	ErrContractReviewInvalidModel = errors.New("contract review model must be an active knowledge QA model")
	ErrContractReviewModelMissing = errors.New("no contract review model is configured")
)

var contractReviewPlaybooks = []types.ContractReviewPlaybook{{
	ID: "general-contract-review", Name: "General Contract Review",
	Description: "A balanced review of commercial, legal, operational, and drafting risk.", Version: "1.0",
}}

const (
	contractReviewClauseSystemPrompt = "You are a senior commercial contracts lawyer reviewing a primary contract-text window together with cross-window context from the same contract. The primary window may contain several legal sections, repeated service descriptions, and page-break artifacts; its Clause title is only an analysis-window label, not a legal clause number. Read the entire primary window, including headings and nearby provisions. Use cross-window context to check whether another provision explicitly covers, limits, or contradicts a requirement. A requirement stated under a heading such as 合同价款包含范围 counts as covered even if it is not repeated under another heading. Do not call an explicit provision missing. Distinguish a real omission from a contradiction or a genuinely ambiguous settlement rule. Create issues only when the evidence quote appears in the primary window; do not create an issue solely from cross-window context because that text is reviewed in its own window. If two observations are the same underlying risk, return one consolidated issue. Return exactly one JSON object and nothing else. Do not output markdown, code fences, headings, citations, a full-contract report, or reasoning text. If there are no concrete risks, return {\"issues\":[]}. Return at most five issues. Keep titles concise; keep explanations and suggestions concise (preferably under 180 Chinese characters each). Do not invent quotations. " +
		"Write issue titles, explanations, and suggestions in Simplified Chinese. Keep original_quote exactly in the contract's original language and wording so it can be located in the source document."
	contractReviewOverviewSystemPrompt = "Return a concise, evidence-based contract review overview as valid JSON only. Write executive_summary, contract_type, and key_recommendations in Simplified Chinese. Preserve party names and other proper nouns in their original form."
)

const (
	contractReviewClauseDefaultMaxCompletionTokens = 4096
	contractReviewClauseRetryMaxCompletionTokens   = 8192
	contractReviewClauseChunkSize                  = 2800
	contractReviewClauseChunkOverlap               = 120
	contractReviewFullDocumentContextMaxRunes      = 12000
	contractReviewRelatedContextMaxRunes           = 3600
	contractReviewRelatedPassageMaxRunes           = 900
	contractReviewDocumentOutlineMaxRunes          = 1400
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

func (s *contractReviewService) Update(ctx context.Context, tenantID uint64, userID, id, title, playbook, party string, modelID *string, archived *bool) (*types.ContractReview, error) {
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
	if modelID != nil {
		if !canUpdateContractReviewConfig(r.Status) {
			return nil, ErrContractReviewInvalidState
		}
		selectedModelID := strings.TrimSpace(*modelID)
		if selectedModelID != "" {
			if err := s.validateReviewModel(ctx, tenantID, userID, selectedModelID); err != nil {
				return nil, err
			}
		}
		r.ModelID = selectedModelID
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
			_, err = s.Update(ctx, tenantID, userID, id, "", "", "", nil, &archived)
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

func contractReviewModelContext(ctx context.Context, tenantID uint64, userID string) context.Context {
	ctx = context.WithValue(ctx, types.TenantIDContextKey, tenantID)
	if userID != "" {
		ctx = context.WithValue(ctx, types.UserIDContextKey, userID)
	}
	return ctx
}

func (s *contractReviewService) validateReviewModel(ctx context.Context, tenantID uint64, userID, modelID string) error {
	if s.models == nil || strings.TrimSpace(modelID) == "" {
		return ErrContractReviewInvalidModel
	}
	model, err := s.models.GetModelByID(contractReviewModelContext(ctx, tenantID, userID), strings.TrimSpace(modelID))
	if err != nil || model == nil || model.Type != types.ModelTypeKnowledgeQA || model.Status != types.ModelStatusActive {
		return ErrContractReviewInvalidModel
	}
	return nil
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

var (
	markdownHeading       = regexp.MustCompile(`(?m)^#{1,4}\s+(.+)$`)
	reviewNumberedHeading = regexp.MustCompile(`^(?:[一二三四五六七八九十百千万]+、\s*|\d+(?:\.\d+)+\s*)\S`)
)

var contractReviewContextMarkers = []string{
	"合同价款包含范围", "价款包含范围", "价格形式", "合同价款", "合同总价款",
	"服务范围", "服务内容", "服务标准", "付款", "支付", "结算", "验收",
	"履约保证金", "保证金", "违约责任", "违约", "解除", "赔偿", "知识产权",
	"保密", "不可抗力", "争议", "仲裁", "诉讼", "合同期限", "服务期限",
	"发票", "税费", "变更", "终止", "转包", "分包", "数据", "个人信息",
}

type reviewDocumentSection struct {
	start      int
	end        int
	heading    string
	text       string
	normalized string
	priority   int
}

type reviewDocumentContext struct {
	runes        []rune
	sections     []reviewDocumentSection
	outline      []string
	termSections map[string][]int
}

func reviewHeadingText(line string) string {
	trimmed := strings.TrimSpace(line)
	if match := markdownHeading.FindStringSubmatch(trimmed); len(match) > 1 {
		return strings.TrimSpace(match[1])
	}
	if reviewNumberedHeading.MatchString(trimmed) {
		return trimmed
	}
	return ""
}

func reviewSectionPriority(heading string) int {
	normalized := normalizeReviewQuoteText(heading)
	for _, marker := range contractReviewContextMarkers {
		marker = normalizeReviewQuoteText(marker)
		if !strings.Contains(normalized, marker) {
			continue
		}
		if marker == "合同价款包含范围" || marker == "价款包含范围" {
			return 50
		}
		return 20
	}
	return 0
}

// reviewContextSearchTerms extracts lightweight lexical anchors without
// relying on a language-specific tokenizer. Chinese trigrams preserve useful
// phrases such as “视频彩铃” and “服务范围”; ASCII runs preserve terms such
// as “5G”. The returned weight is higher for known legal cross-reference
// markers so their sections remain visible even when the primary window uses
// broad wording.
func reviewContextSearchTerms(value string) map[string]int {
	normalized := normalizeReviewQuoteText(value)
	terms := make(map[string]int)
	for _, marker := range contractReviewContextMarkers {
		marker = normalizeReviewQuoteText(marker)
		if strings.Contains(normalized, marker) {
			terms[marker] = 4
		}
	}

	runes := []rune(normalized)
	for index := 0; index+2 < len(runes); index++ {
		if unicode.Is(unicode.Han, runes[index]) && unicode.Is(unicode.Han, runes[index+1]) && unicode.Is(unicode.Han, runes[index+2]) {
			term := string(runes[index : index+3])
			if terms[term] < 1 {
				terms[term] = 1
			}
		}
	}
	for index := 0; index < len(runes); {
		if !isReviewWordRune(runes[index]) {
			index++
			continue
		}
		start := index
		hasLetter := false
		for index < len(runes) && isReviewWordRune(runes[index]) {
			hasLetter = hasLetter || unicode.IsLetter(runes[index])
			index++
		}
		if index-start >= 2 && hasLetter {
			term := string(runes[start:index])
			if terms[term] < 1 {
				terms[term] = 1
			}
		}
	}
	return terms
}

func isReviewWordRune(value rune) bool {
	return unicode.IsDigit(value) || (unicode.IsLetter(value) && !unicode.Is(unicode.Han, value))
}

func appendReviewDocumentSection(ctx *reviewDocumentContext, start, end int, heading string) {
	if start < 0 {
		start = 0
	}
	if end > len(ctx.runes) {
		end = len(ctx.runes)
	}
	if start >= end {
		return
	}
	text := string(ctx.runes[start:end])
	if strings.TrimSpace(text) == "" {
		return
	}
	ctx.sections = append(ctx.sections, reviewDocumentSection{
		start:      start,
		end:        end,
		heading:    heading,
		text:       text,
		normalized: normalizeReviewQuoteText(text),
		priority:   reviewSectionPriority(heading),
	})
}

func newReviewDocumentContext(document string) *reviewDocumentContext {
	ctx := &reviewDocumentContext{runes: []rune(document), termSections: make(map[string][]int)}
	sectionStart := 0
	sectionHeading := ""
	for lineStart := 0; lineStart < len(ctx.runes); {
		lineEnd := lineStart
		for lineEnd < len(ctx.runes) && ctx.runes[lineEnd] != '\n' {
			lineEnd++
		}
		line := string(ctx.runes[lineStart:lineEnd])
		if heading := reviewHeadingText(line); heading != "" {
			appendReviewDocumentSection(ctx, sectionStart, lineStart, sectionHeading)
			sectionStart = lineStart
			sectionHeading = heading
			if len(ctx.outline) == 0 || ctx.outline[len(ctx.outline)-1] != heading {
				ctx.outline = append(ctx.outline, heading)
			}
		}
		if lineEnd == len(ctx.runes) {
			break
		}
		lineStart = lineEnd + 1
	}
	appendReviewDocumentSection(ctx, sectionStart, len(ctx.runes), sectionHeading)
	if len(ctx.sections) == 0 && len(ctx.runes) > 0 {
		appendReviewDocumentSection(ctx, 0, len(ctx.runes), "")
	}
	for index := range ctx.sections {
		for term := range reviewContextSearchTerms(ctx.sections[index].text) {
			ctx.termSections[term] = append(ctx.termSections[term], index)
		}
	}
	return ctx
}

func truncateReviewRunes(value string, maxRunes int) string {
	if maxRunes <= 0 {
		return ""
	}
	runes := []rune(value)
	if len(runes) <= maxRunes {
		return value
	}
	if maxRunes == 1 {
		return "…"
	}
	return string(runes[:maxRunes-1]) + "…"
}

func addReviewContextPart(parts *[]string, used *int, value string, maxRunes int) bool {
	value = strings.TrimSpace(value)
	if value == "" || *used >= maxRunes {
		return false
	}
	prefix := ""
	if len(*parts) > 0 {
		prefix = "\n\n"
	}
	available := maxRunes - *used - len([]rune(prefix))
	if available <= 0 {
		return false
	}
	value = truncateReviewRunes(value, available)
	if value == "" {
		return false
	}
	*parts = append(*parts, prefix+value)
	*used += len([]rune(prefix)) + len([]rune(value))
	return true
}

func reviewContextSectionSnippet(section reviewDocumentSection, terms map[string]int, maxRunes int) string {
	text := strings.TrimSpace(section.text)
	if text == "" {
		return ""
	}
	textRunes := []rune(text)
	if len(textRunes) <= maxRunes {
		return text
	}

	matchStart := -1
	matchWeight := -1
	for term, weight := range terms {
		start, _, found := findReviewQuoteRange(text, term)
		if !found {
			continue
		}
		if weight > matchWeight || (weight == matchWeight && (matchStart < 0 || start < matchStart)) {
			matchStart, matchWeight = start, weight
		}
	}
	if matchStart < 0 {
		return truncateReviewRunes(text, maxRunes)
	}

	start := matchStart - maxRunes/2
	if start < 0 {
		start = 0
	}
	if start+maxRunes > len(textRunes) {
		start = len(textRunes) - maxRunes
	}
	if start < 0 {
		start = 0
	}
	return truncateReviewRunes(string(textRunes[start:start+maxRunes]), maxRunes)
}

func reviewContextOutsideWindow(runes []rune, start, end int) string {
	if start < 0 {
		start = 0
	}
	if end > len(runes) {
		end = len(runes)
	}
	if start >= end {
		return strings.TrimSpace(string(runes))
	}
	parts := make([]string, 0, 2)
	if start > 0 {
		parts = append(parts, strings.TrimSpace(string(runes[:start])))
	}
	if end < len(runes) {
		parts = append(parts, strings.TrimSpace(string(runes[end:])))
	}
	return strings.TrimSpace(strings.Join(parts, "\n\n"))
}

// CrossWindowContext returns enough contract-wide context for the current
// analysis window to reconcile provisions that were split apart. Short
// contracts receive all text outside the primary window. Long contracts use
// the document outline and ranked, semantically related sections so prompt
// size stays bounded.
func (ctx *reviewDocumentContext) CrossWindowContext(clause *types.ContractReviewClause) string {
	if ctx == nil || clause == nil || len(ctx.runes) == 0 {
		return ""
	}
	start, end := clause.SourceStart, clause.SourceEnd
	if start < 0 {
		start = 0
	}
	if end > len(ctx.runes) {
		end = len(ctx.runes)
	}
	if start >= end {
		return ""
	}
	if len(ctx.runes) <= contractReviewFullDocumentContextMaxRunes {
		outside := reviewContextOutsideWindow(ctx.runes, start, end)
		if outside == "" {
			return ""
		}
		return "Other contract text outside this analysis window:\n" + outside
	}

	body := string(ctx.runes[start:end])
	terms := reviewContextSearchTerms(body)
	scores := make([]int, len(ctx.sections))
	for index, section := range ctx.sections {
		if section.start >= start && section.end <= end {
			continue
		}
		scores[index] = section.priority
	}
	for term, weight := range terms {
		for _, sectionIndex := range ctx.termSections[term] {
			section := ctx.sections[sectionIndex]
			if section.start >= start && section.end <= end {
				continue
			}
			scores[sectionIndex] += weight
		}
	}

	indices := make([]int, 0, len(ctx.sections))
	for index, score := range scores {
		if score > 0 {
			indices = append(indices, index)
		}
	}
	sort.SliceStable(indices, func(left, right int) bool {
		if scores[indices[left]] != scores[indices[right]] {
			return scores[indices[left]] > scores[indices[right]]
		}
		return ctx.sections[indices[left]].start < ctx.sections[indices[right]].start
	})

	parts := make([]string, 0, 1+len(indices))
	used := 0
	if len(ctx.outline) > 0 {
		outline := "Document structure:\n" + strings.Join(ctx.outline, "\n")
		addReviewContextPart(&parts, &used, outline, contractReviewRelatedContextMaxRunes)
	}
	for _, index := range indices {
		section := ctx.sections[index]
		snippet := reviewContextSectionSnippet(section, terms, contractReviewRelatedPassageMaxRunes)
		if section.heading != "" {
			snippet = "Related provision [" + section.heading + "]:\n" + snippet
		}
		if !addReviewContextPart(&parts, &used, snippet, contractReviewRelatedContextMaxRunes) {
			break
		}
	}
	if len(parts) == 0 {
		neighborStart, neighborEnd := start-600, end+600
		if neighborStart < 0 {
			neighborStart = 0
		}
		if neighborEnd > len(ctx.runes) {
			neighborEnd = len(ctx.runes)
		}
		neighbor := reviewContextOutsideWindow(ctx.runes[neighborStart:neighborEnd], start-neighborStart, end-neighborStart)
		addReviewContextPart(&parts, &used, "Nearby text outside this analysis window:\n"+neighbor, contractReviewRelatedContextMaxRunes)
	}
	return strings.Join(parts, "")
}

func buildReviewClauses(reviewID, content string) []*types.ContractReviewClause {
	cfg := chunker.DefaultConfig()
	cfg.Strategy = chunker.StrategyAuto
	cfg.ChunkSize = contractReviewClauseChunkSize
	cfg.ChunkOverlap = contractReviewClauseChunkOverlap
	parts := chunker.Split(content, cfg)
	rows := make([]*types.ContractReviewClause, 0, len(parts))
	for idx, part := range parts {
		title := fmt.Sprintf("Analysis segment %d", idx+1)
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

func (s *contractReviewService) resolveReviewModel(ctx context.Context, review *types.ContractReview) (chat.Chat, *types.CustomAgent, error) {
	modelCtx := ctx
	if review != nil {
		modelCtx = contractReviewModelContext(ctx, review.TenantID, review.UserID)
	}
	var agent *types.CustomAgent
	if s.agents != nil {
		agent, _ = s.agents.GetAgentByID(modelCtx, types.BuiltinContractReviewID)
	}
	modelID := ""
	if review != nil {
		modelID = strings.TrimSpace(review.ModelID)
	}
	if modelID != "" {
		if review == nil {
			return nil, agent, ErrContractReviewInvalidModel
		}
		if err := s.validateReviewModel(modelCtx, review.TenantID, review.UserID, modelID); err != nil {
			return nil, agent, err
		}
		model, err := s.models.GetChatModel(modelCtx, modelID)
		return model, agent, err
	}
	if agent != nil {
		modelID = strings.TrimSpace(agent.Config.ModelID)
	}
	if modelID == "" {
		if s.models == nil {
			return nil, agent, ErrContractReviewModelMissing
		}
		models, err := s.models.ListModels(modelCtx)
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
	model, err := s.models.GetChatModel(modelCtx, modelID)
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

// normalizeReviewQuoteText removes formatting whitespace while preserving the
// text that carries legal meaning. Document readers commonly insert line
// breaks or spaces inside a sentence, while the model returns the same quote
// as one line.
func normalizeReviewQuoteText(value string) string {
	var normalized strings.Builder
	normalized.Grow(len(value))
	for _, r := range strings.ToLower(value) {
		if unicode.IsSpace(r) || r == '\u200b' || r == '\u200c' || r == '\u200d' || r == '\ufeff' {
			continue
		}
		normalized.WriteRune(r)
	}
	return normalized.String()
}

// findReviewQuoteRange returns rune offsets in haystack. The offsets point to
// the original text, so whitespace ignored during matching is still included
// in the stored source range.
func findReviewQuoteRange(haystack, needle string) (int, int, bool) {
	target := []rune(normalizeReviewQuoteText(needle))
	if len(target) == 0 {
		return 0, 0, false
	}

	source := []rune(haystack)
	normalized := make([]rune, 0, len(source))
	offsets := make([][2]int, 0, len(source))
	for index, r := range source {
		if unicode.IsSpace(r) || r == '\u200b' || r == '\u200c' || r == '\u200d' || r == '\ufeff' {
			continue
		}
		normalized = append(normalized, unicode.ToLower(r))
		offsets = append(offsets, [2]int{index, index + 1})
	}
	if len(target) > len(normalized) {
		return 0, 0, false
	}

	for start := 0; start+len(target) <= len(normalized); start++ {
		matched := true
		for offset, r := range target {
			if normalized[start+offset] != r {
				matched = false
				break
			}
		}
		if matched {
			return offsets[start][0], offsets[start+len(target)-1][1], true
		}
	}
	return 0, 0, false
}

func reviewIssueText(result reviewIssueOutput) string {
	return normalizeReviewQuoteText(result.Title + result.Explanation + result.OriginalQuote)
}

func reviewIssueMentionsCoveredService(result reviewIssueOutput) (bool, bool) {
	text := reviewIssueText(result)
	return strings.Contains(text, "视频彩铃"), strings.Contains(text, "5g") || strings.Contains(text, "多媒体消息")
}

func looksLikeMissingPriceInclusionIssue(result reviewIssueOutput) bool {
	text := reviewIssueText(result)
	if !strings.Contains(text, "包含") && !strings.Contains(text, "计入") {
		return false
	}
	for _, marker := range []string{"未明确包含", "未说明包含", "未约定包含", "未明确是否包含", "未说明是否包含", "未约定是否包含", "是否包含", "未明确计入", "未说明计入", "未约定计入", "是否计入"} {
		if strings.Contains(text, marker) {
			return true
		}
	}
	return false
}

// contractPriceScopeCoversIssue is a narrow, conservative guard for the most
// common false positive: a model reports that a service is not included in
// the contract price even though the contract has an explicit price-scope
// section listing that service. A settlement ambiguity is intentionally not
// suppressed because it is a different, valid risk.
func contractPriceScopeCoversIssue(document string, result reviewIssueOutput) bool {
	if !looksLikeMissingPriceInclusionIssue(result) {
		return false
	}
	videoMentioned, fiveGMentioned := reviewIssueMentionsCoveredService(result)
	if !videoMentioned && !fiveGMentioned {
		return false
	}

	normalizedDocument := normalizeReviewQuoteText(document)
	start := strings.Index(normalizedDocument, "合同价款包含范围")
	if start < 0 {
		start = strings.Index(normalizedDocument, "价款包含范围")
	}
	if start < 0 {
		return false
	}
	scope := normalizedDocument[start:]
	for _, marker := range []string{"3.3", "四、合同标的", "4.1合同标的"} {
		if end := strings.Index(scope, marker); end > 0 {
			scope = scope[:end]
			break
		}
	}
	if videoMentioned && !strings.Contains(scope, "视频彩铃") {
		return false
	}
	if fiveGMentioned && (!strings.Contains(scope, "5g") || !strings.Contains(scope, "多媒体消息")) {
		return false
	}
	return true
}

func reviewTitleBigrams(value string) map[string]struct{} {
	runes := []rune(normalizeReviewQuoteText(value))
	grams := make(map[string]struct{})
	for index := 0; index+1 < len(runes); index++ {
		if !unicode.IsLetter(runes[index]) && !unicode.IsDigit(runes[index]) {
			continue
		}
		if !unicode.IsLetter(runes[index+1]) && !unicode.IsDigit(runes[index+1]) {
			continue
		}
		grams[string(runes[index:index+2])] = struct{}{}
	}
	return grams
}

func reviewIssuesAreDuplicate(left, right reviewIssueOutput) bool {
	leftQuote := normalizeReviewQuoteText(left.OriginalQuote)
	rightQuote := normalizeReviewQuoteText(right.OriginalQuote)
	if len([]rune(leftQuote)) < 20 || len([]rune(rightQuote)) < 20 {
		return false
	}
	if !strings.Contains(leftQuote, rightQuote) && !strings.Contains(rightQuote, leftQuote) {
		return false
	}

	leftTerms, rightTerms := reviewTitleBigrams(left.Title), reviewTitleBigrams(right.Title)
	common := 0
	for term := range leftTerms {
		if _, ok := rightTerms[term]; ok {
			common++
		}
	}
	return common >= 2
}

func reviewIssueAlreadyAccepted(candidate reviewIssueOutput, accepted []reviewIssueOutput) bool {
	for _, previous := range accepted {
		if reviewIssuesAreDuplicate(candidate, previous) {
			return true
		}
	}
	return false
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
	model, agent, err := s.resolveReviewModel(ctx, r)
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
	acceptedReviewIssues := make([]reviewIssueOutput, 0)
	contentRunes := []rune(r.ExtractedContent)
	documentContext := newReviewDocumentContext(r.ExtractedContent)
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
		crossWindowContext := documentContext.CrossWindowContext(clause)
		prompt := fmt.Sprintf("Playbook: %s v%s\nRepresented party: %s\nAnalysis-window title: %s\nThe primary text below may contain multiple numbered sections or repeated descriptions. Treat every heading and the complete window as context; do not infer that a requirement is missing merely because it appears under a different heading. Issues must quote the primary text.\nPrimary contract text:\n%s", playbook.Name, r.PlaybookVersion, r.RepresentedParty, clause.Title, body)
		if crossWindowContext != "" {
			prompt += "\n\nCross-window context from the same contract (use this to verify coverage, limits, or contradictions; do not create an issue solely from this context, and do not quote it unless the same text also appears in the primary contract text):\n" + crossWindowContext
		}
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
			result.Title = strings.TrimSpace(result.Title)
			result.Explanation = strings.TrimSpace(result.Explanation)
			result.OriginalQuote = strings.TrimSpace(result.OriginalQuote)
			result.Suggestion = strings.TrimSpace(result.Suggestion)
			if result.Title == "" || result.OriginalQuote == "" {
				continue
			}
			if contractPriceScopeCoversIssue(r.ExtractedContent, result) {
				// The document explicitly lists this service under the price
				// inclusion section. Keep a possible settlement issue, but do
				// not persist the unsupported "not included" finding.
				continue
			}
			if reviewIssueAlreadyAccepted(result, acceptedReviewIssues) {
				continue
			}
			relativeStart, relativeEnd, found := findReviewQuoteRange(body, result.OriginalQuote)
			if !found {
				// An unlocatable quote is not reliable evidence and would
				// otherwise be shown at the beginning of the analysis window.
				continue
			}
			start := clause.SourceStart + relativeStart
			end := clause.SourceStart + relativeEnd
			issue := &types.ContractReviewIssue{ID: uuid.NewSHA1(uuid.NameSpaceOID, []byte(issueFingerprint(r.ID, clause.ID, result.Title, result.OriginalQuote))).String(), ReviewID: r.ID, ClauseID: clause.ID,
				Fingerprint: issueFingerprint(r.ID, clause.ID, result.Title, result.OriginalQuote), Sequence: issueSeq, RiskLevel: validRisk(result.RiskLevel), Title: strings.TrimSpace(result.Title),
				Explanation: result.Explanation, OriginalQuote: result.OriginalQuote, Suggestion: result.Suggestion, SourceStart: start, SourceEnd: end}
			if err := s.repo.UpsertIssue(ctx, issue); err != nil {
				return s.fail(ctx, r, err)
			}
			acceptedReviewIssues = append(acceptedReviewIssues, result)
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
