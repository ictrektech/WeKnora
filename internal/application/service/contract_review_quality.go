package service

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"math/big"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/google/uuid"
)

const (
	contractReviewMaxIssues          = 5
	contractReviewMaxFacts           = 64
	contractReviewMaxEvidenceRefs    = 8
	contractReviewMaxCategoryRunes   = 80
	contractReviewMaxTitleRunes      = 80
	contractReviewMaxTextRunes       = 540
	contractReviewMaxQuoteRunes      = 360
	contractReviewMaxEvidenceIDRunes = 128
)

var contractReviewCategoryAliases = map[string]string{
	"scope": "scope", "party": "parties", "parties": "parties", "party_obligations": "parties",
	"payment": "payment", "price": "payment", "term": "term", "期限": "term", "termination": "term",
	"acceptance": "acceptance", "delivery": "acceptance", "delivery_acceptance": "acceptance",
	"liability": "liability", "liability_remedies": "liability", "breach": "liability",
	"dispute": "dispute_resolution", "dispute_resolution": "dispute_resolution",
	"guarantee": "guarantee", "performance_security": "guarantee", "deposit": "guarantee",
	"confidentiality": "confidentiality", "intellectual_property": "intellectual_property", "ip": "intellectual_property",
	"data_security": "data_security", "privacy_security": "data_security", "compliance": "compliance",
	"other": "other", "drafting_other": "other",
	"范围": "scope", "当事方义务": "parties", "验收": "acceptance",
	"违约责任": "liability", "争议解决": "dispute_resolution", "履约保证金": "guarantee", "保证金": "guarantee",
	"保密": "confidentiality", "知识产权": "intellectual_property", "数据安全": "data_security", "合规": "compliance", "其他": "other",
}

var contractReviewFindingTypeAliases = map[string]string{
	"missing": "missing", "missing_protection": "missing", "omission": "missing", "缺失": "missing",
	"contradiction": "contradiction", "conflicting_term": "contradiction", "conflict": "contradiction", "矛盾": "contradiction",
	"ambiguity": "ambiguity", "ambiguous_language": "ambiguity", "模糊": "ambiguity",
	"placeholder": "placeholder", "空白": "placeholder", "占位符": "placeholder",
	"external_reference": "external_reference", "external_reference_risk": "external_reference", "外部引用": "external_reference",
	"inconsistency": "inconsistency", "不一致": "inconsistency",
}

var (
	contractReviewPlaceholderPattern       = regexp.MustCompile(`(?i)_{2,}|…{2,}|（\s*）|\(\s*\)|待填写|待定|未填写|空白|\b(?:TBD|TBC|N/?A)\b|<\s*(?:insert|fill|complete|to be completed)[^>]*>`)
	contractReviewUnselectedOptionPattern  = regexp.MustCompile(`[○〇□☐◯]|\[\s*\]`)
	contractReviewBlankFieldPattern        = regexp.MustCompile(`(?i)(付款|支付|价款|金额|保证金|验收|争议|仲裁|诉讼|期限|日期)[^:\n]{0,30}[:：]\s*(?:[;；。]|$)`)
	contractReviewExternalReferencePattern = regexp.MustCompile(`(详见|参见|见|依据|按照|根据)[^\n]{0,24}(附件|文件|制度|规范|清单|方案|约定)`)
	contractReviewPartyLabelPattern        = regexp.MustCompile(`(?i)(甲方|乙方|丙方|丁方|买方|卖方|委托方|受托方|采购人|供应商|承包方|出租方|承租方|Party\s+[A-Z]|Buyer|Seller|Client|Supplier)\s*(?:（[^）\n]{0,20}）|\([^\)\n]{0,20}\))?\s*[:：]`)
	contractReviewDatePattern              = regexp.MustCompile(`(\d{4})\s*[年./-]\s*(\d{1,2})\s*[月./-]\s*(\d{1,2})\s*日?`)
	contractReviewMoneyPattern             = regexp.MustCompile(`(?i)(?:人民币|RMB|CNY|¥|￥)\s*[\d,]+(?:\.\d+)?\s*(?:万元|万|元)?|[\d,]+(?:\.\d+)?\s*(?:万元|万|元|美元|USD|RMB|CNY)`)
	contractReviewPercentagePattern        = regexp.MustCompile(`(?i)\d+(?:\.\d+)?\s*%|百分之[零一二三四五六七八九十百千万亿0-9]+`)
	contractReviewDurationPattern          = regexp.MustCompile(`(?i)(?:\d+(?:\.\d+)?|[一二三四五六七八九十百千万亿]+)\s*(?:个?工作日|日历日|日|天|月|年|小时|分钟)`)
)

var errContractReviewStaleRun = errors.New("contract review analysis run is stale")

type reviewFactOutput struct {
	Type          string   `json:"type"`
	Key           string   `json:"key"`
	Value         string   `json:"value"`
	Normalized    string   `json:"normalized_value"`
	Unit          string   `json:"unit"`
	Currency      string   `json:"currency"`
	Condition     string   `json:"condition"`
	EvidenceQuote string   `json:"evidence_quote"`
	EvidenceRefs  []string `json:"evidence_refs"`
}

type reviewEvidenceUnit struct {
	ID       string
	Role     types.ContractReviewEvidenceRole
	Start    int
	End      int
	Text     string
	Prompted bool
}

type reviewResolvedEvidence struct {
	Start         int
	End           int
	Quote         string
	CanonicalRefs []string
}

type reviewValidatedBatch struct {
	Issues []reviewIssueOutput
	Facts  []types.ContractReviewFact
}

type reviewIssueValidationFailure struct {
	Index int
	Err   error
}

func decodeContractReviewJSON(content string, target any) error {
	content = cleanContractReviewJSON(content)
	if content == "" || content[0] != '{' {
		return errors.New("contract review model output must be a JSON object")
	}
	decoder := json.NewDecoder(strings.NewReader(content))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("invalid contract review JSON: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return errors.New("contract review model output contains trailing JSON")
		}
		return fmt.Errorf("invalid trailing contract review JSON: %w", err)
	}
	return nil
}

func cleanContractReviewJSON(content string) string {
	// Keep this normalization limited to outer whitespace. Markdown fences,
	// prose, and trailing JSON are not a valid model response for this stage;
	// accepting them would hide a schema failure that should move the run to
	// failed after the retry budget is exhausted.
	return strings.TrimSpace(content)
}

func contractReviewJSONObjectFields(content string) (map[string]json.RawMessage, error) {
	var fields map[string]json.RawMessage
	if err := decodeContractReviewJSON(content, &fields); err != nil {
		return nil, err
	}
	return fields, nil
}

func requireContractReviewJSONFields(fields map[string]json.RawMessage, names ...string) error {
	for _, name := range names {
		raw, ok := fields[name]
		if !ok {
			return fmt.Errorf("contract review model output is missing %q", name)
		}
		if strings.EqualFold(strings.TrimSpace(string(raw)), "null") {
			return fmt.Errorf("contract review model output field %q must not be null", name)
		}
	}
	return nil
}

func normalizeReviewEnum(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.ReplaceAll(value, "-", "_")
	value = strings.Join(strings.Fields(value), "_")
	return value
}

func canonicalReviewCategory(value string) (string, error) {
	key := normalizeReviewEnum(value)
	if canonical, ok := contractReviewCategoryAliases[key]; ok {
		return canonical, nil
	}
	return "", fmt.Errorf("unsupported issue category %q", value)
}

func canonicalReviewFindingType(value string) (string, error) {
	key := normalizeReviewEnum(value)
	if canonical, ok := contractReviewFindingTypeAliases[key]; ok {
		return canonical, nil
	}
	return "", fmt.Errorf("unsupported finding_type %q", value)
}

func hasDuplicateReviewStrings(values []string) bool {
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			return true
		}
		if _, exists := seen[value]; exists {
			return true
		}
		seen[value] = struct{}{}
	}
	return false
}

func validateReviewBatchJSON(content, document string, units []reviewEvidenceUnit) (reviewValidatedBatch, error) {
	fields, err := contractReviewJSONObjectFields(content)
	if err != nil {
		return reviewValidatedBatch{}, err
	}
	if err := requireContractReviewJSONFields(fields, "issues", "facts"); err != nil {
		return reviewValidatedBatch{}, err
	}

	var output reviewBatchOutput
	if err := decodeContractReviewJSON(content, &output); err != nil {
		return reviewValidatedBatch{}, err
	}
	if len(output.Issues) > contractReviewMaxIssues {
		return reviewValidatedBatch{}, fmt.Errorf("contract review model returned %d issues; maximum is %d", len(output.Issues), contractReviewMaxIssues)
	}
	if len(output.Facts) > contractReviewMaxFacts {
		return reviewValidatedBatch{}, fmt.Errorf("contract review model returned %d facts; maximum is %d", len(output.Facts), contractReviewMaxFacts)
	}

	validated := reviewValidatedBatch{
		Issues: make([]reviewIssueOutput, 0, len(output.Issues)),
		Facts:  make([]types.ContractReviewFact, 0, len(output.Facts)),
	}
	seenIssues := make(map[string]struct{}, len(output.Issues))
	for index, candidate := range output.Issues {
		issue, err := validateReviewIssue(candidate, document, units)
		if err != nil {
			return reviewValidatedBatch{}, fmt.Errorf("invalid issue %d: %w", index+1, err)
		}
		identity := fmt.Sprintf("%s\x00%s\x00%d\x00%d", strings.ToLower(issue.Category), strings.ToLower(issue.FindingType), issue.resolvedStart, issue.resolvedEnd)
		if _, exists := seenIssues[identity]; exists {
			return reviewValidatedBatch{}, fmt.Errorf("duplicate issue evidence in issue %d", index+1)
		}
		seenIssues[identity] = struct{}{}
		validated.Issues = append(validated.Issues, issue)
	}
	for index, candidate := range output.Facts {
		fact, err := validateReviewFact(candidate, document, units)
		if err != nil {
			return reviewValidatedBatch{}, fmt.Errorf("invalid fact %d: %w", index+1, err)
		}
		validated.Facts = append(validated.Facts, fact)
	}
	return validated, nil
}

// validateReviewBatchWithEvidenceIsolation keeps the batch/schema contract
// strict, while allowing one malformed evidence-backed issue to be quarantined
// after the normal retry budget is exhausted. A bad quote must never become a
// guessed highlight, but it also should not discard unrelated issues that have
// already passed validation. Non-evidence validation failures remain fatal.
func validateReviewBatchWithEvidenceIsolation(content, document string, units []reviewEvidenceUnit) (reviewValidatedBatch, []reviewIssueValidationFailure, error) {
	fields, err := contractReviewJSONObjectFields(content)
	if err != nil {
		return reviewValidatedBatch{}, nil, err
	}
	if err := requireContractReviewJSONFields(fields, "issues", "facts"); err != nil {
		return reviewValidatedBatch{}, nil, err
	}

	var raw struct {
		Issues json.RawMessage `json:"issues"`
		Facts  json.RawMessage `json:"facts"`
	}
	if err := decodeContractReviewJSON(content, &raw); err != nil {
		return reviewValidatedBatch{}, nil, err
	}
	var rawIssues []json.RawMessage
	if err := json.Unmarshal(raw.Issues, &rawIssues); err != nil {
		return reviewValidatedBatch{}, nil, fmt.Errorf("issues must be an array: %w", err)
	}
	var rawFacts []json.RawMessage
	if err := json.Unmarshal(raw.Facts, &rawFacts); err != nil {
		return reviewValidatedBatch{}, nil, fmt.Errorf("facts must be an array: %w", err)
	}
	if len(rawIssues) > contractReviewMaxIssues {
		return reviewValidatedBatch{}, nil, fmt.Errorf("contract review model returned %d issues; maximum is %d", len(rawIssues), contractReviewMaxIssues)
	}
	// Fact extraction is service-owned. Do not silently accept a model fact
	// batch here; callers must still treat that schema violation as fatal.
	if len(rawFacts) > 0 {
		return reviewValidatedBatch{}, nil, errors.New("model returned facts although fact extraction is service-owned")
	}

	validated := reviewValidatedBatch{Issues: make([]reviewIssueOutput, 0, len(rawIssues)), Facts: []types.ContractReviewFact{}}
	failures := make([]reviewIssueValidationFailure, 0)
	seenIssues := make(map[string]struct{}, len(rawIssues))
	for index, rawIssue := range rawIssues {
		var candidate reviewIssueOutput
		if err := decodeContractReviewJSON(string(rawIssue), &candidate); err != nil {
			return reviewValidatedBatch{}, nil, fmt.Errorf("invalid issue %d: %w", index+1, err)
		}
		issue, err := validateReviewIssue(candidate, document, units)
		if err != nil {
			if !isRecoverableReviewEvidenceError(err) {
				return reviewValidatedBatch{}, nil, fmt.Errorf("invalid issue %d: %w", index+1, err)
			}
			failures = append(failures, reviewIssueValidationFailure{Index: index + 1, Err: err})
			continue
		}
		identity := fmt.Sprintf("%s\x00%s\x00%d\x00%d", strings.ToLower(issue.Category), strings.ToLower(issue.FindingType), issue.resolvedStart, issue.resolvedEnd)
		if _, exists := seenIssues[identity]; exists {
			return reviewValidatedBatch{}, nil, fmt.Errorf("duplicate issue evidence in issue %d", index+1)
		}
		seenIssues[identity] = struct{}{}
		validated.Issues = append(validated.Issues, issue)
	}
	if len(failures) == 0 {
		return reviewValidatedBatch{}, nil, errors.New("no isolatable evidence validation failure")
	}
	return validated, failures, nil
}

func isRecoverableReviewEvidenceError(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	for _, marker := range []string{"evidence quote", "evidence_id", "evidence_refs"} {
		if strings.Contains(message, marker) {
			return true
		}
	}
	return false
}

func reviewIssueAlreadyAcceptedAtEvidence(candidate reviewIssueOutput, accepted []reviewIssueOutput) bool {
	for _, previous := range accepted {
		if !strings.EqualFold(strings.TrimSpace(candidate.Category), strings.TrimSpace(previous.Category)) ||
			!strings.EqualFold(strings.TrimSpace(candidate.FindingType), strings.TrimSpace(previous.FindingType)) {
			continue
		}
		if candidate.resolvedStart == previous.resolvedStart && candidate.resolvedEnd == previous.resolvedEnd {
			return true
		}
	}
	return false
}

func validateReviewIssue(candidate reviewIssueOutput, document string, units []reviewEvidenceUnit) (reviewIssueOutput, error) {
	var err error
	if candidate.Category, err = canonicalReviewCategory(candidate.Category); err != nil {
		return reviewIssueOutput{}, err
	}
	if candidate.FindingType, err = canonicalReviewFindingType(candidate.FindingType); err != nil {
		return reviewIssueOutput{}, err
	}
	candidate.Title = strings.TrimSpace(candidate.Title)
	candidate.Explanation = strings.TrimSpace(candidate.Explanation)
	candidate.OriginalQuote = strings.TrimSpace(candidate.OriginalQuote)
	candidate.Suggestion = strings.TrimSpace(candidate.Suggestion)
	if candidate.Title == "" || runeLen(candidate.Title) > contractReviewMaxTitleRunes {
		return reviewIssueOutput{}, fmt.Errorf("title is empty or too long")
	}
	if candidate.Explanation == "" || runeLen(candidate.Explanation) > contractReviewMaxTextRunes {
		return reviewIssueOutput{}, fmt.Errorf("explanation is empty or too long")
	}
	if candidate.Suggestion == "" || runeLen(candidate.Suggestion) > contractReviewMaxTextRunes {
		return reviewIssueOutput{}, fmt.Errorf("suggestion is empty or too long")
	}
	if candidate.OriginalQuote == "" || runeLen(candidate.OriginalQuote) > contractReviewMaxQuoteRunes {
		return reviewIssueOutput{}, fmt.Errorf("original_quote is empty or too long")
	}
	if len(candidate.EvidenceRefs) == 0 || len(candidate.EvidenceRefs) > contractReviewMaxEvidenceRefs {
		return reviewIssueOutput{}, fmt.Errorf("evidence_refs must contain between 1 and %d values", contractReviewMaxEvidenceRefs)
	}
	if hasDuplicateReviewStrings(candidate.EvidenceRefs) {
		return reviewIssueOutput{}, errors.New("issue evidence_refs contains a duplicate or empty evidence_id")
	}
	for _, ref := range candidate.EvidenceRefs {
		if runeLen(strings.TrimSpace(ref)) > contractReviewMaxEvidenceIDRunes {
			return reviewIssueOutput{}, errors.New("issue evidence_id is too long")
		}
	}
	if _, err := normalizeContractReviewRisk(candidate.RiskLevel); err != nil {
		return reviewIssueOutput{}, err
	}
	// A related provision can be the exact evidence for an issue (for example,
	// a later clause that contradicts the current window). The evidence ID is
	// still mandatory and the quote must resolve inside one of the referenced
	// source ranges; the primary window is a prompt preference, not a locator
	// shortcut or a reason to reject otherwise valid supporting evidence.
	resolved, err := resolveReviewEvidence(document, candidate.OriginalQuote, units, candidate.EvidenceRefs, false)
	if err != nil {
		return reviewIssueOutput{}, err
	}
	candidate.RiskLevel = string(validRisk(candidate.RiskLevel))
	candidate.resolvedStart, candidate.resolvedEnd = resolved.Start, resolved.End
	candidate.canonicalQuote = resolved.Quote
	candidate.OriginalQuote = resolved.Quote
	candidate.EvidenceRefs = uniqueReviewStrings(candidate.EvidenceRefs)
	if len(resolved.CanonicalRefs) > 0 {
		candidate.EvidenceRefs = resolved.CanonicalRefs
	}
	return candidate, nil
}

func validateReviewFact(candidate reviewFactOutput, document string, units []reviewEvidenceUnit) (types.ContractReviewFact, error) {
	candidate.Type = strings.TrimSpace(strings.ToLower(candidate.Type))
	candidate.Key = strings.TrimSpace(candidate.Key)
	candidate.Value = strings.TrimSpace(candidate.Value)
	candidate.Normalized = strings.TrimSpace(candidate.Normalized)
	candidate.Unit = strings.TrimSpace(candidate.Unit)
	candidate.Currency = strings.TrimSpace(candidate.Currency)
	candidate.Condition = strings.TrimSpace(candidate.Condition)
	candidate.EvidenceQuote = strings.TrimSpace(candidate.EvidenceQuote)
	if !validReviewFactType(candidate.Type) {
		return types.ContractReviewFact{}, fmt.Errorf("unsupported fact type %q", candidate.Type)
	}
	if candidate.Key == "" || runeLen(candidate.Key) > contractReviewMaxCategoryRunes {
		return types.ContractReviewFact{}, errors.New("fact key is empty or too long")
	}
	if candidate.Value == "" || runeLen(candidate.Value) > contractReviewMaxTextRunes {
		return types.ContractReviewFact{}, errors.New("fact value is empty or too long")
	}
	if candidate.EvidenceQuote == "" || runeLen(candidate.EvidenceQuote) > contractReviewMaxQuoteRunes {
		return types.ContractReviewFact{}, errors.New("fact evidence_quote is empty or too long")
	}
	if len(candidate.EvidenceRefs) == 0 || len(candidate.EvidenceRefs) > contractReviewMaxEvidenceRefs {
		return types.ContractReviewFact{}, fmt.Errorf("fact evidence_refs must contain between 1 and %d values", contractReviewMaxEvidenceRefs)
	}
	if hasDuplicateReviewStrings(candidate.EvidenceRefs) {
		return types.ContractReviewFact{}, errors.New("fact evidence_refs contains a duplicate or empty evidence_id")
	}
	for _, ref := range candidate.EvidenceRefs {
		if runeLen(strings.TrimSpace(ref)) > contractReviewMaxEvidenceIDRunes {
			return types.ContractReviewFact{}, errors.New("fact evidence_id is too long")
		}
	}
	if runeLen(candidate.Normalized) > 160 || runeLen(candidate.Unit) > 40 || runeLen(candidate.Currency) > 16 || runeLen(candidate.Condition) > 180 {
		return types.ContractReviewFact{}, errors.New("fact metadata is too long")
	}
	resolved, err := resolveReviewEvidence(document, candidate.EvidenceQuote, units, candidate.EvidenceRefs, false)
	if err != nil {
		return types.ContractReviewFact{}, err
	}
	if candidate.Normalized == "" {
		candidate.Normalized = normalizeReviewFactValue(candidate.Value)
	}
	evidenceRefs := uniqueReviewStrings(candidate.EvidenceRefs)
	if len(resolved.CanonicalRefs) > 0 {
		evidenceRefs = resolved.CanonicalRefs
	}
	return types.ContractReviewFact{
		Type:            candidate.Type,
		Key:             candidate.Key,
		Value:           candidate.Value,
		NormalizedValue: candidate.Normalized,
		Unit:            candidate.Unit,
		Currency:        candidate.Currency,
		Condition:       candidate.Condition,
		Status:          types.ContractReviewFactPresent,
		EvidenceQuote:   resolved.Quote,
		SourceStart:     resolved.Start,
		SourceEnd:       resolved.End,
		EvidenceRefs:    evidenceRefs,
	}, nil
}

func validReviewFactType(value string) bool {
	switch value {
	case "party", "date", "term", "amount", "payment", "deposit", "acceptance", "dispute", "placeholder", "reference":
		return true
	default:
		return false
	}
}

func normalizeReviewFactValue(value string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
}

func normalizeContractReviewRisk(value string) (types.ContractReviewRiskLevel, error) {
	normalized := strings.ToLower(normalizeReviewFactValue(value))
	switch normalized {
	case "high", "critical", "severe", "高", "高风险":
		return types.ContractReviewRiskHigh, nil
	case "medium", "moderate", "中", "中风险":
		return types.ContractReviewRiskMedium, nil
	case "low", "minor", "低", "低风险":
		return types.ContractReviewRiskLow, nil
	default:
		return "", fmt.Errorf("invalid contract review risk level %q", value)
	}
}

func runeLen(value string) int { return len([]rune(value)) }

func uniqueReviewStrings(values []string) []string {
	result := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func findReviewQuoteRanges(haystack, needle string) []reviewResolvedEvidence {
	target := []rune(normalizeReviewQuoteText(needle))
	if len(target) == 0 {
		return nil
	}
	source := []rune(haystack)
	normalized := make([]rune, 0, len(source))
	offsets := make([][2]int, 0, len(source))
	for index, value := range source {
		if unicode.IsSpace(value) || value == '\u200b' || value == '\u200c' || value == '\u200d' || value == '\ufeff' {
			continue
		}
		normalized = append(normalized, value)
		offsets = append(offsets, [2]int{index, index + 1})
	}
	if len(target) > len(normalized) {
		return nil
	}
	result := make([]reviewResolvedEvidence, 0, 1)
	for start := 0; start+len(target) <= len(normalized); start++ {
		matched := true
		for offset, value := range target {
			if normalized[start+offset] != value {
				matched = false
				break
			}
		}
		if matched {
			result = append(result, reviewResolvedEvidence{Start: offsets[start][0], End: offsets[start+len(target)-1][1], Quote: string(source[offsets[start][0]:offsets[start+len(target)-1][1]])})
		}
	}
	return result
}

func findReviewQuoteRange(haystack, needle string) (int, int, bool) {
	ranges := findReviewQuoteRanges(haystack, needle)
	if len(ranges) != 1 {
		return 0, 0, false
	}
	return ranges[0].Start, ranges[0].End, true
}

func resolveReviewEvidence(document, quote string, units []reviewEvidenceUnit, refs []string, requirePrimary bool) (reviewResolvedEvidence, error) {
	if len(refs) == 0 {
		return reviewResolvedEvidence{}, errors.New("evidence_refs is empty")
	}
	byID := make(map[string]reviewEvidenceUnit, len(units))
	for _, unit := range units {
		byID[unit.ID] = unit
	}
	selected := make([]reviewEvidenceUnit, 0, len(refs))
	hasPrimary := false
	for _, ref := range uniqueReviewStrings(refs) {
		unit, ok := byID[ref]
		if !ok {
			return reviewResolvedEvidence{}, fmt.Errorf("unknown evidence_id %q", ref)
		}
		selected = append(selected, unit)
		if unit.Role == types.ContractReviewEvidencePrimary {
			hasPrimary = true
		}
	}
	if requirePrimary && !hasPrimary {
		return reviewResolvedEvidence{}, errors.New("issue evidence_refs must include a primary evidence_id")
	}
	if requirePrimary {
		primaryUnits := make([]reviewEvidenceUnit, 0, len(selected))
		for _, unit := range selected {
			if unit.Role == types.ContractReviewEvidencePrimary {
				primaryUnits = append(primaryUnits, unit)
			}
		}
		selected = primaryUnits
	}

	candidates := findReviewQuoteRanges(document, quote)
	// Evidence refs are ordered: the first ref is the model's main evidence
	// and subsequent refs are related evidence. Resolve a quote against each
	// declared unit in that order before considering a quote that spans multiple
	// units. This avoids treating one exact passage repeated in the main and
	// related units as ambiguous, while still refusing to guess when the main
	// unit itself contains multiple matches.
	for _, unit := range selected {
		unitCandidates := make([]reviewResolvedEvidence, 0, 1)
		for _, candidate := range candidates {
			if candidate.Start >= unit.Start && candidate.End <= unit.End {
				unitCandidates = append(unitCandidates, candidate)
			}
		}
		switch len(unitCandidates) {
		case 0:
			continue
		case 1:
			return unitCandidates[0], nil
		default:
			return reviewResolvedEvidence{}, errors.New("evidence quote is ambiguous in the referenced source unit")
		}
	}
	// A quote may intentionally cross a formatting separator or adjacent
	// parser units. Only accept that form when the referenced units provide one
	// contiguous, whitespace-only-covered source range and the quote has one
	// candidate in that union.
	filtered := make([]reviewResolvedEvidence, 0, len(candidates))
	for _, candidate := range candidates {
		if reviewEvidenceRangeSupported(candidate, selected, document) {
			filtered = append(filtered, candidate)
		}
	}
	if len(filtered) == 0 {
		// A small model can copy an exact passage but attach the ID of a
		// neighbouring related unit. If the passage has exactly one occurrence
		// in the prompted source units, repair the reference deterministically
		// from that verified range. This is not fuzzy matching or first-hit
		// selection: repeated passages remain ambiguous and fail validation.
		if canonical, ok := canonicalReviewEvidenceCandidate(candidates, units, document); ok {
			return canonical, nil
		}
		return reviewResolvedEvidence{}, errors.New("evidence quote cannot be located in the referenced source units")
	}
	if len(filtered) > 1 {
		return reviewResolvedEvidence{}, errors.New("evidence quote is ambiguous in the referenced source units")
	}
	return filtered[0], nil
}

func canonicalReviewEvidenceCandidate(candidates []reviewResolvedEvidence, units []reviewEvidenceUnit, document string) (reviewResolvedEvidence, bool) {
	type candidateWithUnit struct {
		candidate reviewResolvedEvidence
		refs      []string
		unitSize  int
	}
	matches := make([]candidateWithUnit, 0, len(candidates))
	for _, candidate := range candidates {
		best := reviewEvidenceUnit{}
		found := false
		overlapping := make([]reviewEvidenceUnit, 0)
		for _, unit := range units {
			if unit.End > candidate.Start && unit.Start < candidate.End {
				overlapping = append(overlapping, unit)
			}
			if candidate.Start < unit.Start || candidate.End > unit.End {
				continue
			}
			if !found || unit.End-unit.Start < best.End-best.Start ||
				(unit.End-unit.Start == best.End-best.Start && unit.Start < best.Start) {
				best = unit
				found = true
			}
		}
		if found {
			matches = append(matches, candidateWithUnit{candidate: candidate, refs: []string{best.ID}, unitSize: best.End - best.Start})
			continue
		}
		if len(overlapping) == 0 || !reviewEvidenceRangeSupported(candidate, overlapping, document) {
			continue
		}
		sort.SliceStable(overlapping, func(left, right int) bool {
			if overlapping[left].Start != overlapping[right].Start {
				return overlapping[left].Start < overlapping[right].Start
			}
			return overlapping[left].End < overlapping[right].End
		})
		refs := make([]string, 0, len(overlapping))
		for _, unit := range overlapping {
			if strings.TrimSpace(unit.ID) != "" {
				refs = append(refs, unit.ID)
			}
		}
		if len(refs) > 0 {
			matches = append(matches, candidateWithUnit{candidate: candidate, refs: refs, unitSize: candidate.End - candidate.Start})
		}
	}
	if len(matches) != 1 || len(matches[0].refs) == 0 || matches[0].unitSize <= 0 {
		return reviewResolvedEvidence{}, false
	}
	result := matches[0].candidate
	result.CanonicalRefs = matches[0].refs
	return result, true
}

func reviewEvidenceRangeSupported(candidate reviewResolvedEvidence, units []reviewEvidenceUnit, document string) bool {
	if len(units) == 0 {
		return false
	}
	for _, unit := range units {
		if candidate.Start >= unit.Start && candidate.End <= unit.End {
			return true
		}
	}
	// A quote may cross line-based evidence units. Accept it only when the
	// referenced units form a contiguous union; taking a min/max bounding box
	// would incorrectly bridge unrelated text between two references.
	sorted := append([]reviewEvidenceUnit(nil), units...)
	sort.Slice(sorted, func(left, right int) bool { return sorted[left].Start < sorted[right].Start })
	documentRunes := []rune(document)
	coverageStart, coverageEnd := sorted[0].Start, sorted[0].End
	for _, unit := range sorted[1:] {
		if unit.Start > coverageEnd {
			// Parser units commonly stop before a paragraph/page separator. A
			// quoted passage may cross that separator, but it must never bridge
			// unrelated non-whitespace text between two evidence references.
			if coverageEnd <= len(documentRunes) && unit.Start <= len(documentRunes) && strings.TrimSpace(string(documentRunes[coverageEnd:unit.Start])) == "" {
				coverageEnd = unit.End
				continue
			}
			if candidate.Start >= coverageStart && candidate.End <= coverageEnd {
				return true
			}
			coverageStart, coverageEnd = unit.Start, unit.End
			continue
		}
		if unit.End > coverageEnd {
			coverageEnd = unit.End
		}
	}
	return candidate.Start >= coverageStart && candidate.End <= coverageEnd
}

func contractReviewSourceHash(content string) string {
	sum := sha256.Sum256([]byte(content))
	return hex.EncodeToString(sum[:])
}

func firstNonEmptyReviewString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func contractReviewEvidenceID(sourceHash string, start, end int) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s\x00%d\x00%d", sourceHash, start, end)))
	return "evidence_" + hex.EncodeToString(sum[:])
}

func newContractReviewAnalysisRunID() string { return uuid.NewString() }

func contractReviewConfigHash(review *types.ContractReview) string {
	if review == nil {
		return ""
	}
	snapshot := struct {
		PlaybookID       string                    `json:"playbook_id"`
		PlaybookVersion  string                    `json:"playbook_version"`
		RepresentedParty types.ContractReviewParty `json:"represented_party"`
		ModelID          string                    `json:"model_id"`
		SourceTextHash   string                    `json:"source_text_hash"`
		SchemaVersion    int                       `json:"schema_version"`
	}{
		PlaybookID: review.PlaybookID, PlaybookVersion: review.PlaybookVersion,
		RepresentedParty: review.RepresentedParty, ModelID: strings.TrimSpace(review.ModelID),
		SourceTextHash: firstNonEmptyReviewString(review.SourceTextHash, review.SourceHash), SchemaVersion: 3,
	}
	data, _ := json.Marshal(snapshot)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func buildContractReviewLocator(sourceHash, content string, metadata map[string]string) (types.JSON, error) {
	sourceRevision := "text-v2:" + sourceHash
	locator := map[string]any{
		"version":          1,
		"offset_unit":      "rune",
		"source_revision":  sourceRevision,
		"source_text_hash": sourceHash,
		"source_length":    len([]rune(content)),
		"units":            []any{},
	}
	if raw := strings.TrimSpace(metadata["source_units_json"]); raw != "" {
		var units []map[string]any
		if err := json.Unmarshal([]byte(raw), &units); err != nil {
			locator["locator_error"] = "invalid_source_units"
		} else {
			validUnits := make([]map[string]any, 0, len(units))
			seenUnitIDs := make(map[string]struct{}, len(units))
			for _, unit := range units {
				start, startOK := unit["source_start"].(float64)
				end, endOK := unit["source_end"].(float64)
				unitID, idOK := unit["unit_id"].(string)
				if !startOK || !endOK || start != math.Trunc(start) || end != math.Trunc(end) || !idOK || strings.TrimSpace(unitID) == "" || start < 0 || end <= start || end > float64(len([]rune(content))) {
					continue
				}
				if unitText, hasText := unit["text"].(string); hasText && normalizeReviewQuoteText(unitText) != normalizeReviewQuoteText(string([]rune(content)[int(start):int(end)])) {
					continue
				}
				if _, exists := seenUnitIDs[unitID]; exists {
					continue
				}
				seenUnitIDs[unitID] = struct{}{}
				validUnits = append(validUnits, unit)
			}
			sort.SliceStable(validUnits, func(left, right int) bool {
				leftStart, _ := validUnits[left]["source_start"].(float64)
				rightStart, _ := validUnits[right]["source_start"].(float64)
				if leftStart != rightStart {
					return leftStart < rightStart
				}
				leftEnd, _ := validUnits[left]["source_end"].(float64)
				rightEnd, _ := validUnits[right]["source_end"].(float64)
				if leftEnd != rightEnd {
					return leftEnd < rightEnd
				}
				return fmt.Sprint(validUnits[left]["unit_id"]) < fmt.Sprint(validUnits[right]["unit_id"])
			})
			locator["units"] = validUnits
		}
	}
	data, err := json.Marshal(locator)
	if err != nil {
		return nil, err
	}
	return types.JSON(data), nil
}

type contractReviewLocatorUnit struct {
	ID          string `json:"unit_id"`
	Kind        string `json:"kind,omitempty"`
	ParentID    string `json:"parent_id,omitempty"`
	Page        int    `json:"page,omitempty"`
	SourceStart int    `json:"source_start"`
	SourceEnd   int    `json:"source_end"`
}

// contractReviewLocatorUnits decodes both the current locator envelope and
// the parser metadata shape used by older responses. It is intentionally
// tolerant on read; write-time parser metadata is validated before it is
// saved, and an untrusted/legacy unit is never treated as a reliable match.
func contractReviewLocatorUnits(raw types.JSON) map[string]contractReviewLocatorUnit {
	result := make(map[string]contractReviewLocatorUnit)
	if len(raw) == 0 {
		return result
	}
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return result
	}
	unitsRaw := envelope["units"]
	if len(unitsRaw) == 0 {
		unitsRaw = envelope["source_units_json"]
		var encoded string
		if json.Unmarshal(unitsRaw, &encoded) == nil {
			unitsRaw = json.RawMessage(encoded)
		}
	}
	var units []contractReviewLocatorUnit
	if len(unitsRaw) == 0 || json.Unmarshal(unitsRaw, &units) != nil {
		return result
	}
	for _, unit := range units {
		if strings.TrimSpace(unit.ID) == "" || unit.SourceStart < 0 || unit.SourceEnd <= unit.SourceStart {
			continue
		}
		result[unit.ID] = unit
	}
	return result
}

func contractReviewLocatorSupportsRange(units map[string]contractReviewLocatorUnit, start, end int, document string) bool {
	if start < 0 || end <= start {
		return false
	}
	documentRunes := []rune(document)
	if end > len(documentRunes) {
		return false
	}
	type sourceRange struct{ start, end int }
	ranges := make([]sourceRange, 0, len(units))
	for _, unit := range units {
		if unit.SourceStart < end && unit.SourceEnd > start {
			ranges = append(ranges, sourceRange{start: unit.SourceStart, end: unit.SourceEnd})
		}
	}
	if len(ranges) == 0 {
		return false
	}
	sort.Slice(ranges, func(left, right int) bool {
		if ranges[left].start != ranges[right].start {
			return ranges[left].start < ranges[right].start
		}
		return ranges[left].end < ranges[right].end
	})
	covered := start
	for _, sourceRange := range ranges {
		if sourceRange.end <= covered {
			continue
		}
		if sourceRange.start > covered {
			gapEnd := sourceRange.start
			if gapEnd > end || strings.TrimSpace(string(documentRunes[covered:gapEnd])) != "" {
				return false
			}
		}
		if sourceRange.end > covered {
			covered = sourceRange.end
		}
		if covered >= end {
			return true
		}
	}
	return covered >= end
}

func reviewPromptEvidenceUnits(sourceHash string, clause *types.ContractReviewClause, document string, context *reviewDocumentContext) []reviewEvidenceUnit {
	if clause == nil {
		return nil
	}
	runes := []rune(document)
	start, end := clause.SourceStart, clause.SourceEnd
	if start < 0 {
		start = 0
	}
	if end > len(runes) {
		end = len(runes)
	}
	if start >= end {
		return nil
	}
	units := make([]reviewEvidenceUnit, 0, 12)
	windowID := contractReviewEvidenceID(sourceHash, start, end)
	// Expose the complete analysis window as one primary evidence unit. The
	// service still resolves the model's exact quote back to source_start/end,
	// while avoiding dozens of overlapping line IDs that make models select a
	// valid-looking but incorrect reference for a fact.
	units = append(units, reviewEvidenceUnit{ID: windowID, Role: types.ContractReviewEvidencePrimary, Start: start, End: end, Text: string(runes[start:end]), Prompted: true})
	seenEvidenceIDs := map[string]struct{}{windowID: {}}
	if context == nil {
		return units
	}
	for _, section := range context.relatedSectionsForClause(clause) {
		if section.start >= start && section.end <= end {
			continue
		}
		text := reviewContextSectionSnippet(section, reviewContextSearchTerms(string(runes[start:end])), contractReviewRelatedPassageMaxRunes)
		if strings.TrimSpace(text) == "" {
			continue
		}
		sectionID := contractReviewEvidenceID(sourceHash, section.start, section.end)
		if _, exists := seenEvidenceIDs[sectionID]; exists {
			continue
		}
		seenEvidenceIDs[sectionID] = struct{}{}
		units = append(units, reviewEvidenceUnit{ID: sectionID, Role: types.ContractReviewEvidenceSupporting, Start: section.start, End: section.end, Text: text, Prompted: true})
	}
	return units
}

func renderReviewEvidencePrompt(units []reviewEvidenceUnit) string {
	var builder strings.Builder
	for _, unit := range units {
		if !unit.Prompted {
			continue
		}
		fmt.Fprintf(&builder, "[evidence_id=%s role=%s source_start=%d source_end=%d]\n%s\n\n", unit.ID, unit.Role, unit.Start, unit.End, unit.Text)
	}
	return strings.TrimSpace(builder.String())
}

func (ctx *reviewDocumentContext) relatedSectionsForClause(clause *types.ContractReviewClause) []reviewDocumentSection {
	if ctx == nil || clause == nil || len(ctx.runes) == 0 {
		return nil
	}
	start, end := clause.SourceStart, clause.SourceEnd
	if start < 0 {
		start = 0
	}
	if end > len(ctx.runes) {
		end = len(ctx.runes)
	}
	if start >= end {
		return nil
	}
	if len(ctx.runes) <= contractReviewFullDocumentContextMaxRunes {
		result := make([]reviewDocumentSection, 0, len(ctx.sections))
		for _, section := range ctx.sections {
			if section.start >= start && section.end <= end {
				continue
			}
			result = append(result, section)
		}
		return result
	}
	terms := reviewContextSearchTerms(string(ctx.runes[start:end]))
	scores := make([]int, len(ctx.sections))
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
	// Keep deterministic order and cap the number of supporting sections by
	// the same prompt budget used by CrossWindowContext.
	for left := 0; left < len(indices); left++ {
		for right := left + 1; right < len(indices); right++ {
			if scores[indices[right]] > scores[indices[left]] || (scores[indices[right]] == scores[indices[left]] && ctx.sections[indices[right]].start < ctx.sections[indices[left]].start) {
				indices[left], indices[right] = indices[right], indices[left]
			}
		}
	}
	result := make([]reviewDocumentSection, 0, len(indices))
	used := 0
	for _, index := range indices {
		section := ctx.sections[index]
		text := reviewContextSectionSnippet(section, terms, contractReviewRelatedPassageMaxRunes)
		if len([]rune(text))+used > contractReviewRelatedContextMaxRunes {
			break
		}
		used += len([]rune(text))
		result = append(result, section)
	}
	return result
}

func reviewFactConsistencyWarnings(facts []types.ContractReviewFact) []types.ContractReviewWarning {
	warnings := make([]types.ContractReviewWarning, 0)
	type factKey struct {
		kind      string
		key       string
		condition string
	}
	groups := make(map[factKey][]types.ContractReviewFact)
	for _, fact := range facts {
		key := factKey{kind: strings.ToLower(strings.TrimSpace(fact.Type)), key: strings.ToLower(strings.TrimSpace(fact.Key)), condition: normalizeReviewFactValue(fact.Condition)}
		groups[key] = append(groups[key], fact)
	}
	for key, group := range groups {
		if len(group) < 2 {
			continue
		}
		first := normalizeReviewFactValue(group[0].NormalizedValue)
		for _, fact := range group[1:] {
			if first != normalizeReviewFactValue(fact.NormalizedValue) {
				warnings = append(warnings, types.ContractReviewWarning{Code: "FACT_CONFLICT", Message: fmt.Sprintf("事实 %s/%s 在相同条件下存在冲突值", key.kind, key.key), EvidenceID: firstReviewEvidenceID(fact.EvidenceRefs)})
				break
			}
		}
	}

	dateFacts := make(map[string]time.Time)
	dateSources := make(map[string]string)
	for _, fact := range facts {
		if fact.Type != "date" {
			continue
		}
		date, err := time.Parse("2006-01-02", strings.TrimSpace(fact.NormalizedValue))
		if err != nil {
			continue
		}
		dateFacts[strings.ToLower(fact.Key)] = date
		dateSources[strings.ToLower(fact.Key)] = firstReviewEvidenceID(fact.EvidenceRefs)
	}
	if start, ok := dateFacts["start_date"]; ok {
		if end, exists := dateFacts["end_date"]; exists && start.After(end) {
			warnings = append(warnings, types.ContractReviewWarning{Code: "DATE_ORDER_CONFLICT", Message: "合同开始日期晚于结束日期", EvidenceID: dateSources["end_date"]})
		}
	}

	percentageTotal := new(big.Rat)
	percentageCount := 0
	for _, fact := range facts {
		key := strings.ToLower(fact.Key)
		if !strings.HasPrefix(key, "payment") || (!strings.HasSuffix(key, "percentage") && !strings.HasSuffix(key, "percent")) {
			continue
		}
		value := strings.TrimSpace(strings.TrimSuffix(strings.ReplaceAll(fact.NormalizedValue, ",", ""), "%"))
		part, ok := new(big.Rat).SetString(value)
		if !ok {
			continue
		}
		percentageTotal.Add(percentageTotal, part)
		percentageCount++
	}
	if percentageCount > 1 && percentageTotal.Cmp(big.NewRat(100, 1)) > 0 {
		warnings = append(warnings, types.ContractReviewWarning{Code: "PAYMENT_PERCENTAGE_OVER_100", Message: "付款比例合计超过 100%"})
	}
	return warnings
}

// reviewDocumentStructureWarnings identifies generic drafting signals that do
// not require a contract name, supplier name, product name, or price list.
// They are generated from the verified source text and later materialized as
// explicit issues with their own evidence range, so parser/layout artifacts do
// not silently become model findings.
func reviewDocumentStructureWarnings(document, sourceHash string) []types.ContractReviewWarning {
	warnings := make([]types.ContractReviewWarning, 0)
	if strings.TrimSpace(document) == "" {
		return warnings
	}
	runes := []rune(document)
	lineStart := 0
	appendLineWarning := func(code, message string, start, end int) {
		if end <= start {
			return
		}
		warnings = append(warnings, types.ContractReviewWarning{
			Code: code, Message: message,
			EvidenceID:  contractReviewEvidenceID(sourceHash, start, end),
			SourceStart: start, SourceEnd: end,
		})
	}
	for index := 0; index <= len(runes); index++ {
		if index != len(runes) && runes[index] != '\n' {
			continue
		}
		lineEnd := index
		line := strings.TrimSpace(string(runes[lineStart:lineEnd]))
		if line != "" {
			lower := strings.ToLower(line)
			switch {
			case contractReviewPlaceholderPattern.MatchString(line):
				appendLineWarning("PLACEHOLDER_OR_BLANK_FIELD", "合同存在空白或占位字段，需要补全后再签署", lineStart, lineEnd)
			case contractReviewBlankFieldPattern.MatchString(line):
				appendLineWarning("EMPTY_REQUIRED_FIELD", "合同字段标题后未填写具体内容，需要补全", lineStart, lineEnd)
			}
			if (strings.Contains(lower, "争议") || strings.Contains(lower, "仲裁") || strings.Contains(lower, "诉讼") || strings.Contains(lower, "管辖")) && contractReviewUnselectedOptionPattern.MatchString(line) {
				appendLineWarning("UNSELECTED_DISPUTE_OPTION", "争议解决选项存在未勾选标记，当前选择不明确", lineStart, lineEnd)
			}
			if contractReviewExternalReferencePattern.MatchString(line) {
				appendLineWarning("EXTERNAL_REFERENCE", "条款引用外部附件、文件或另行约定，签署前需核对其版本和内容", lineStart, lineEnd)
			}
		}
		lineStart = index + 1
	}
	return dedupeContractReviewWarnings(warnings)
}

func dedupeContractReviewWarnings(warnings []types.ContractReviewWarning) []types.ContractReviewWarning {
	result := make([]types.ContractReviewWarning, 0, len(warnings))
	seen := make(map[string]struct{}, len(warnings))
	for _, warning := range warnings {
		key := warning.Code + "\x00" + warning.EvidenceID + "\x00" + warning.Message
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, warning)
	}
	return result
}

func contractReviewWarningCategory(document string, warning types.ContractReviewWarning) string {
	if warning.Code == "UNSELECTED_DISPUTE_OPTION" {
		return "dispute_resolution"
	}
	runes := []rune(document)
	if warning.SourceStart >= 0 && warning.SourceEnd > warning.SourceStart && warning.SourceEnd <= len(runes) {
		line := string(runes[warning.SourceStart:warning.SourceEnd])
		switch {
		case strings.Contains(line, "付款") || strings.Contains(line, "支付") || strings.Contains(line, "价款") || strings.Contains(line, "金额"):
			return "payment"
		case strings.Contains(line, "验收") || strings.Contains(line, "交付"):
			return "acceptance"
		case strings.Contains(line, "保证金") || strings.Contains(line, "担保"):
			return "guarantee"
		case strings.Contains(line, "期限") || strings.Contains(line, "日期") || strings.Contains(line, "终止"):
			return "term"
		case strings.Contains(line, "违约") || strings.Contains(line, "赔偿") || strings.Contains(line, "责任"):
			return "liability"
		case strings.Contains(line, "仲裁") || strings.Contains(line, "诉讼") || strings.Contains(line, "管辖"):
			return "dispute_resolution"
		}
	}
	return "other"
}

func contractReviewWarningFindingType(code string) string {
	switch code {
	case "PLACEHOLDER_OR_BLANK_FIELD", "EMPTY_REQUIRED_FIELD":
		return "placeholder"
	case "EXTERNAL_REFERENCE":
		return "external_reference"
	case "UNSELECTED_DISPUTE_OPTION":
		return "ambiguity"
	default:
		return "inconsistency"
	}
}

func contractReviewWarningRisk(code string) types.ContractReviewRiskLevel {
	if code == "EXTERNAL_REFERENCE" {
		return types.ContractReviewRiskLow
	}
	return types.ContractReviewRiskMedium
}

func contractReviewWarningSuggestion(code string) string {
	switch code {
	case "PLACEHOLDER_OR_BLANK_FIELD", "EMPTY_REQUIRED_FIELD":
		return "补全该字段，并在签署前复核最终文本。"
	case "UNSELECTED_DISPUTE_OPTION":
		return "明确勾选唯一的争议解决方式，并删除未选项。"
	case "EXTERNAL_REFERENCE":
		return "核对外部附件、制度或另行约定的版本，并将关键内容纳入合同或附件。"
	default:
		return "核对该处合同事实，并统一相关条款。"
	}
}

func contractReviewIssueCoversWarning(issue *types.ContractReviewIssue, warning types.ContractReviewWarning, category, findingType string) bool {
	return issue != nil && issue.SourceStart == warning.SourceStart && issue.SourceEnd == warning.SourceEnd && issue.Category == category && issue.FindingType == findingType
}

func buildContractReviewStructureIssue(reviewID, sourceHash, document string, warning types.ContractReviewWarning, clauses []*types.ContractReviewClause, sequence int) (*types.ContractReviewIssue, *types.ContractReviewClause, error) {
	runes := []rune(document)
	if warning.SourceStart < 0 || warning.SourceEnd <= warning.SourceStart || warning.SourceEnd > len(runes) {
		return nil, nil, errors.New("invalid structural warning source range")
	}
	var clause *types.ContractReviewClause
	for _, candidate := range clauses {
		if candidate != nil && warning.SourceStart >= candidate.SourceStart && warning.SourceEnd <= candidate.SourceEnd {
			clause = candidate
			break
		}
	}
	if clause == nil {
		return nil, nil, errors.New("structural warning is outside all analysis windows")
	}
	category := contractReviewWarningCategory(document, warning)
	findingType := contractReviewWarningFindingType(warning.Code)
	quote := string(runes[warning.SourceStart:warning.SourceEnd])
	evidenceRefs, err := json.Marshal([]string{warning.EvidenceID})
	if err != nil {
		return nil, nil, err
	}
	title := warning.Message
	if title == "" {
		title = "合同结构需要核对"
	}
	fingerprint := issueFingerprint(reviewID, clause.ID, title, quote)
	issue := &types.ContractReviewIssue{
		ID: uuid.NewSHA1(uuid.NameSpaceOID, []byte(fingerprint)).String(), ReviewID: reviewID, ClauseID: clause.ID,
		Fingerprint: fingerprint, Sequence: sequence, RiskLevel: contractReviewWarningRisk(warning.Code),
		Category: category, FindingType: findingType, Title: title, Explanation: warning.Message,
		OriginalQuote: quote, Suggestion: contractReviewWarningSuggestion(warning.Code),
		EvidenceRefs: types.JSON(evidenceRefs), SourceStart: warning.SourceStart, SourceEnd: warning.SourceEnd,
	}
	return issue, clause, nil
}

func firstReviewEvidenceID(refs []string) string {
	if len(refs) == 0 {
		return ""
	}
	return refs[0]
}

func appendContractReviewFacts(existing []types.ContractReviewFact, facts ...types.ContractReviewFact) []types.ContractReviewFact {
	seen := make(map[string]struct{}, len(existing)+len(facts))
	for _, fact := range existing {
		key := fmt.Sprintf("%s\x00%s\x00%d\x00%d\x00%s", fact.Type, fact.Key, fact.SourceStart, fact.SourceEnd, normalizeReviewFactValue(fact.Value))
		seen[key] = struct{}{}
	}
	for _, fact := range facts {
		key := fmt.Sprintf("%s\x00%s\x00%d\x00%d\x00%s", fact.Type, fact.Key, fact.SourceStart, fact.SourceEnd, normalizeReviewFactValue(fact.Value))
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		existing = append(existing, fact)
	}
	return existing
}

type contractReviewSourceLine struct {
	start int
	end   int
	text  string
}

func contractReviewSourceLines(document string) []contractReviewSourceLine {
	runes := []rune(document)
	lines := make([]contractReviewSourceLine, 0)
	lineStart := 0
	for index := 0; index <= len(runes); index++ {
		if index != len(runes) && runes[index] != '\n' {
			continue
		}
		lineEnd := index
		trimmedStart, trimmedEnd := lineStart, lineEnd
		for trimmedStart < trimmedEnd && unicode.IsSpace(runes[trimmedStart]) {
			trimmedStart++
		}
		for trimmedEnd > trimmedStart && unicode.IsSpace(runes[trimmedEnd-1]) {
			trimmedEnd--
		}
		if trimmedStart < trimmedEnd {
			lines = append(lines, contractReviewSourceLine{
				start: trimmedStart,
				end:   trimmedEnd,
				text:  string(runes[trimmedStart:trimmedEnd]),
			})
		}
		lineStart = index + 1
	}
	return lines
}

func reviewLineMatchFocus(line contractReviewSourceLine, byteStart, byteEnd int) (int, int) {
	if byteStart < 0 || byteEnd < byteStart || byteEnd > len(line.text) {
		return line.start, line.end
	}
	return line.start + runeLen(line.text[:byteStart]), line.start + runeLen(line.text[:byteEnd])
}

func reviewFactSourceRange(line contractReviewSourceLine, focusStart, focusEnd int) (int, int) {
	if line.end <= line.start {
		return 0, 0
	}
	if focusStart < line.start || focusStart >= line.end {
		focusStart = line.start
	}
	if focusEnd <= focusStart || focusEnd > line.end {
		focusEnd = minReviewInt(line.end, focusStart+1)
	}
	if line.end-line.start <= contractReviewMaxQuoteRunes {
		return line.start, line.end
	}
	focusLength := focusEnd - focusStart
	if focusLength >= contractReviewMaxQuoteRunes {
		return focusStart, focusStart + contractReviewMaxQuoteRunes
	}
	left := focusStart - (contractReviewMaxQuoteRunes-focusLength)/2
	if left < line.start {
		left = line.start
	}
	right := left + contractReviewMaxQuoteRunes
	if right > line.end {
		right = line.end
		left = maxReviewInt(line.start, right-contractReviewMaxQuoteRunes)
	}
	return left, right
}

func minReviewInt(left, right int) int {
	if left < right {
		return left
	}
	return right
}

func maxReviewInt(left, right int) int {
	if left > right {
		return left
	}
	return right
}

func buildContractReviewFactFromSource(document, sourceHash string, line contractReviewSourceLine, focusStart, focusEnd int, factType, key, value, normalized, unit, currency, condition string) (types.ContractReviewFact, bool) {
	start, end := reviewFactSourceRange(line, focusStart, focusEnd)
	runes := []rune(document)
	if start < 0 || end <= start || end > len(runes) {
		return types.ContractReviewFact{}, false
	}
	quote := string(runes[start:end])
	if strings.TrimSpace(quote) == "" {
		return types.ContractReviewFact{}, false
	}
	value = strings.TrimSpace(value)
	if value == "" {
		return types.ContractReviewFact{}, false
	}
	if runeLen(value) > contractReviewMaxTextRunes {
		value = truncateReviewRunes(value, contractReviewMaxTextRunes)
	}
	if normalized == "" {
		normalized = normalizeReviewFactValue(value)
	}
	return types.ContractReviewFact{
		Type:            factType,
		Key:             key,
		Value:           value,
		NormalizedValue: normalized,
		Unit:            unit,
		Currency:        currency,
		Condition:       condition,
		Status:          types.ContractReviewFactPresent,
		EvidenceQuote:   quote,
		SourceStart:     start,
		SourceEnd:       end,
		EvidenceRefs:    []string{contractReviewEvidenceID(sourceHash, start, end)},
	}, true
}

func buildContractReviewFactAtRange(document, sourceHash string, start, end int, factType, key, value string) (types.ContractReviewFact, bool) {
	runes := []rune(document)
	if start < 0 {
		start = 0
	}
	if end > len(runes) {
		end = len(runes)
	}
	for start < end && unicode.IsSpace(runes[start]) {
		start++
	}
	for end > start && unicode.IsSpace(runes[end-1]) {
		end--
	}
	if end <= start {
		return types.ContractReviewFact{}, false
	}
	line := contractReviewSourceLine{start: start, end: end, text: string(runes[start:end])}
	return buildContractReviewFactFromSource(document, sourceHash, line, start, end, factType, key, value, "", "", "", "")
}

func reviewPartyFactKey(label string) string {
	normalized := strings.ToLower(strings.Join(strings.Fields(label), " "))
	switch {
	case strings.Contains(label, "甲方") || normalized == "party a":
		return "party_a"
	case strings.Contains(label, "乙方") || normalized == "party b":
		return "party_b"
	case strings.Contains(label, "丙方") || normalized == "party c":
		return "party_c"
	case strings.Contains(label, "丁方") || normalized == "party d":
		return "party_d"
	case strings.Contains(label, "采购人") || normalized == "buyer":
		return "party_buyer"
	case strings.Contains(label, "供应商") || normalized == "supplier":
		return "party_supplier"
	case strings.Contains(label, "委托方") || normalized == "client":
		return "party_client"
	case strings.Contains(label, "受托方"):
		return "party_agent"
	case strings.Contains(label, "承包方"):
		return "party_contractor"
	case strings.Contains(label, "出租方"):
		return "party_lessor"
	case strings.Contains(label, "承租方"):
		return "party_lessee"
	case strings.Contains(label, "买方"):
		return "party_buyer"
	case strings.Contains(label, "卖方"):
		return "party_seller"
	default:
		return "party_" + reviewFactKeySlug(label)
	}
}

func reviewFactKeySlug(value string) string {
	var builder strings.Builder
	for _, valueRune := range strings.ToLower(strings.TrimSpace(value)) {
		switch {
		case unicode.IsLetter(valueRune), unicode.IsDigit(valueRune), unicode.Is(unicode.Han, valueRune):
			builder.WriteRune(valueRune)
		default:
			builder.WriteRune('_')
		}
	}
	return strings.Trim(builder.String(), "_")
}

func cleanReviewPartyValue(value string) string {
	value = strings.TrimSpace(strings.Trim(value, "：:；;，,"))
	for _, marker := range []string{"（盖章）", "(盖章)", "（签字）", "(签字)"} {
		value = strings.TrimSpace(strings.TrimSuffix(value, marker))
	}
	if index := strings.IndexAny(value, "；;"); index >= 0 {
		value = strings.TrimSpace(value[:index])
	}
	return strings.TrimSpace(strings.Trim(value, "：:；;，,"))
}

func reviewLineContainsAny(line string, terms ...string) bool {
	for _, term := range terms {
		if strings.Contains(strings.ToLower(line), strings.ToLower(term)) {
			return true
		}
	}
	return false
}

func reviewDateFactKey(line string, index, total int) string {
	if total > 1 {
		if reviewLineContainsAny(line, "自", "起始", "开始", "start") && index == 0 {
			return "start_date"
		}
		if reviewLineContainsAny(line, "至", "截止", "结束", "终止", "end") && index == total-1 {
			return "end_date"
		}
	}
	switch {
	case reviewLineContainsAny(line, "开始", "起始", "start"):
		return "start_date"
	case reviewLineContainsAny(line, "结束", "截止", "终止", "届满", "end"):
		return "end_date"
	case reviewLineContainsAny(line, "签订", "签署", "订立", "signing"):
		return "signing_date"
	case reviewLineContainsAny(line, "生效", "effective"):
		return "effective_date"
	default:
		return fmt.Sprintf("date_%d", index+1)
	}
}

func normalizeReviewDateLiteral(value string) string {
	match := contractReviewDatePattern.FindStringSubmatch(value)
	if len(match) != 4 {
		return normalizeReviewFactValue(value)
	}
	year, yearErr := strconv.Atoi(match[1])
	month, monthErr := strconv.Atoi(match[2])
	day, dayErr := strconv.Atoi(match[3])
	if yearErr != nil || monthErr != nil || dayErr != nil || month < 1 || month > 12 || day < 1 || day > 31 {
		return normalizeReviewFactValue(value)
	}
	return fmt.Sprintf("%04d-%02d-%02d", year, month, day)
}

func normalizeReviewMoneyLiteral(value string) string {
	value = strings.TrimSpace(strings.ReplaceAll(value, ",", ""))
	value = strings.ReplaceAll(value, "￥", "¥")
	return normalizeReviewFactValue(value)
}

func normalizeReviewPercentageLiteral(value string) string {
	value = strings.TrimSpace(strings.ReplaceAll(value, " ", ""))
	value = strings.TrimSuffix(value, "%")
	return value
}

func extractContractReviewFacts(document, sourceHash string) []types.ContractReviewFact {
	if strings.TrimSpace(document) == "" {
		return []types.ContractReviewFact{}
	}
	lines := contractReviewSourceLines(document)
	facts := make([]types.ContractReviewFact, 0)
	counts := make(map[string]int)
	limits := map[string]int{
		"party": 16, "date": 24, "term": 12, "amount": 24, "payment": 24,
		"deposit": 12, "acceptance": 16, "dispute": 12, "placeholder": 24, "reference": 16,
	}
	add := func(line contractReviewSourceLine, focusStart, focusEnd int, factType, key, value, normalized string) {
		if len(facts) >= contractReviewMaxFacts || counts[factType] >= limits[factType] || strings.TrimSpace(key) == "" {
			return
		}
		fact, ok := buildContractReviewFactFromSource(document, sourceHash, line, focusStart, focusEnd, factType, key, value, normalized, "", "", "")
		if !ok {
			return
		}
		before := len(facts)
		facts = appendContractReviewFacts(facts, fact)
		if len(facts) > before {
			counts[factType]++
		}
	}

	// Party facts are collected independently of clause windows so a party
	// named on a cover page, signature block, or table is not lost at a chunk
	// boundary.
	for _, line := range lines {
		matches := contractReviewPartyLabelPattern.FindAllStringSubmatchIndex(line.text, -1)
		for index, match := range matches {
			if len(match) < 4 {
				continue
			}
			valueEnd := len(line.text)
			if index+1 < len(matches) {
				valueEnd = matches[index+1][0]
			}
			value := cleanReviewPartyValue(line.text[match[1]:valueEnd])
			if value == "" {
				continue
			}
			label := line.text[match[2]:match[3]]
			add(line, line.start, line.end, "party", reviewPartyFactKey(label), value, normalizeReviewFactValue(value))
		}
	}

	termOrdinal := 0
	amountOrdinal := 0
	paymentOrdinal := 0
	depositOrdinal := 0
	acceptanceOrdinal := 0
	disputeOrdinal := 0
	for _, line := range lines {
		dateMatches := contractReviewDatePattern.FindAllStringSubmatchIndex(line.text, -1)
		for index, match := range dateMatches {
			if len(match) < 2 {
				continue
			}
			date := line.text[match[0]:match[1]]
			focusStart, focusEnd := reviewLineMatchFocus(line, match[0], match[1])
			add(line, focusStart, focusEnd, "date", reviewDateFactKey(line.text, index, len(dateMatches)), date, normalizeReviewDateLiteral(date))
		}

		isPaymentLine := reviewLineContainsAny(line.text, "付款", "支付", "结算", "收款", "payment", "payable", "invoice")
		if isPaymentLine {
			paymentOrdinal++
			percentageMatches := contractReviewPercentagePattern.FindAllStringIndex(line.text, -1)
			if len(percentageMatches) > 0 {
				for index, match := range percentageMatches {
					focusStart, focusEnd := reviewLineMatchFocus(line, match[0], match[1])
					add(line, focusStart, focusEnd, "payment", fmt.Sprintf("payment_%d_percentage", paymentOrdinal+index), line.text[match[0]:match[1]], normalizeReviewPercentageLiteral(line.text[match[0]:match[1]]))
				}
			} else {
				add(line, line.start, line.end, "payment", fmt.Sprintf("payment_%d", paymentOrdinal), line.text, normalizeReviewFactValue(line.text))
			}
		}

		if reviewLineContainsAny(line.text, "期限", "履行期", "服务期", "交付期", "完成时间", "工期", "有效期", "duration", "term", "period") && contractReviewDurationPattern.MatchString(line.text) {
			termOrdinal++
			key := "term_" + strconv.Itoa(termOrdinal)
			if reviewLineContainsAny(line.text, "合同期限", "履行期限", "服务期限", "有效期", "contract term") {
				key = "contract_term"
			}
			add(line, line.start, line.end, "term", key, line.text, normalizeReviewFactValue(line.text))
		}

		moneyMatches := contractReviewMoneyPattern.FindAllStringIndex(line.text, -1)
		if len(moneyMatches) > 0 {
			for _, match := range moneyMatches {
				amountOrdinal++
				key := fmt.Sprintf("amount_%d", amountOrdinal)
				if len(moneyMatches) == 1 && reviewLineContainsAny(line.text, "合同金额", "合同价款", "总价", "总额", "价款") {
					key = "total_amount"
				}
				focusStart, focusEnd := reviewLineMatchFocus(line, match[0], match[1])
				literal := line.text[match[0]:match[1]]
				add(line, focusStart, focusEnd, "amount", key, literal, normalizeReviewMoneyLiteral(literal))
			}
		}

		if reviewLineContainsAny(line.text, "保证金", "押金", "履约担保", "履约保证", "performance bond", "security deposit") {
			depositOrdinal++
			key := fmt.Sprintf("deposit_%d", depositOrdinal)
			if reviewLineContainsAny(line.text, "履约保证", "履约担保", "performance bond") {
				key = "performance_security"
			}
			add(line, line.start, line.end, "deposit", key, line.text, normalizeReviewFactValue(line.text))
		}

		if reviewLineContainsAny(line.text, "验收", "交付", "考核", "确认", "acceptance", "delivery") {
			acceptanceOrdinal++
			add(line, line.start, line.end, "acceptance", fmt.Sprintf("acceptance_%d", acceptanceOrdinal), line.text, normalizeReviewFactValue(line.text))
		}

		if reviewLineContainsAny(line.text, "争议", "仲裁", "诉讼", "管辖", "适用法律", "dispute", "arbitration", "litigation", "jurisdiction") {
			disputeOrdinal++
			add(line, line.start, line.end, "dispute", fmt.Sprintf("dispute_resolution_%d", disputeOrdinal), line.text, normalizeReviewFactValue(line.text))
		}
	}

	placeholderOrdinal, referenceOrdinal := 0, 0
	for _, warning := range reviewDocumentStructureWarnings(document, sourceHash) {
		switch warning.Code {
		case "PLACEHOLDER_OR_BLANK_FIELD", "EMPTY_REQUIRED_FIELD":
			if len(facts) >= contractReviewMaxFacts || counts["placeholder"] >= limits["placeholder"] {
				continue
			}
			placeholderOrdinal++
			fact, ok := buildContractReviewFactAtRange(document, sourceHash, warning.SourceStart, warning.SourceEnd, "placeholder", fmt.Sprintf("placeholder_%d", placeholderOrdinal), warning.Message)
			if ok {
				before := len(facts)
				facts = appendContractReviewFacts(facts, fact)
				if len(facts) > before {
					counts["placeholder"]++
				}
			}
		case "EXTERNAL_REFERENCE":
			if len(facts) >= contractReviewMaxFacts || counts["reference"] >= limits["reference"] {
				continue
			}
			referenceOrdinal++
			fact, ok := buildContractReviewFactAtRange(document, sourceHash, warning.SourceStart, warning.SourceEnd, "reference", fmt.Sprintf("reference_%d", referenceOrdinal), warning.Message)
			if ok {
				before := len(facts)
				facts = appendContractReviewFacts(facts, fact)
				if len(facts) > before {
					counts["reference"]++
				}
			}
		}
	}
	return facts
}

func partiesFromReviewFacts(facts []types.ContractReviewFact) []string {
	parties := make([]string, 0)
	seen := make(map[string]struct{})
	for _, fact := range facts {
		if fact.Type != "party" || fact.Status != types.ContractReviewFactPresent {
			continue
		}
		value := strings.TrimSpace(fact.Value)
		key := strings.ToLower(normalizeReviewFactValue(value))
		if value == "" {
			continue
		}
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		parties = append(parties, value)
	}
	return parties
}

type contractReviewRiskAggregate struct {
	Counts      map[string]int
	OverallRisk types.ContractReviewRiskLevel
}

func aggregateContractReviewRisks(issues []*types.ContractReviewIssue) contractReviewRiskAggregate {
	counts := map[string]int{"high": 0, "medium": 0, "low": 0}
	for _, issue := range issues {
		if issue == nil {
			continue
		}
		risk, err := normalizeContractReviewRisk(string(issue.RiskLevel))
		if err != nil {
			continue
		}
		counts[string(risk)]++
	}
	overall := types.ContractReviewRiskLow
	if counts["high"] > 0 {
		overall = types.ContractReviewRiskHigh
	} else if counts["medium"] > 0 {
		overall = types.ContractReviewRiskMedium
	}
	return contractReviewRiskAggregate{Counts: counts, OverallRisk: overall}
}

func validateReviewOverviewJSON(content string) (reviewOverviewOutput, error) {
	fields, err := contractReviewJSONObjectFields(content)
	if err != nil {
		return reviewOverviewOutput{}, err
	}
	if err := requireContractReviewJSONFields(fields, "executive_summary", "contract_type", "parties", "key_recommendations"); err != nil {
		return reviewOverviewOutput{}, err
	}
	var output reviewOverviewOutput
	if err := decodeContractReviewJSON(content, &output); err != nil {
		return reviewOverviewOutput{}, err
	}
	output.ExecutiveSummary = strings.TrimSpace(output.ExecutiveSummary)
	output.ContractType = strings.TrimSpace(output.ContractType)
	if output.ExecutiveSummary == "" || runeLen(output.ExecutiveSummary) > contractReviewMaxTextRunes {
		return reviewOverviewOutput{}, errors.New("overview executive_summary is empty or too long")
	}
	if output.ContractType == "" || runeLen(output.ContractType) > contractReviewMaxCategoryRunes {
		return reviewOverviewOutput{}, errors.New("overview contract_type is empty or too long")
	}
	if output.Parties == nil || output.KeyRecommendations == nil {
		return reviewOverviewOutput{}, errors.New("overview arrays must be present and non-null")
	}
	for _, party := range output.Parties {
		if strings.TrimSpace(party) == "" || runeLen(party) > contractReviewMaxCategoryRunes {
			return reviewOverviewOutput{}, errors.New("overview contains an invalid party")
		}
	}
	for _, recommendation := range output.KeyRecommendations {
		if strings.TrimSpace(recommendation) == "" || runeLen(recommendation) > contractReviewMaxTextRunes {
			return reviewOverviewOutput{}, errors.New("overview contains an invalid recommendation")
		}
	}
	if strings.TrimSpace(output.OverallRisk) != "" {
		if _, err := normalizeContractReviewRisk(output.OverallRisk); err != nil {
			return reviewOverviewOutput{}, err
		}
	}
	output.Parties = uniqueReviewStrings(output.Parties)
	output.KeyRecommendations = uniqueReviewStrings(output.KeyRecommendations)
	return output, nil
}

func buildContractReviewOverviewJSON(modelOverview reviewOverviewOutput, issues []*types.ContractReviewIssue, facts []types.ContractReviewFact, quality types.ContractReviewQualityStatus, warnings []types.ContractReviewWarning) (types.JSON, error) {
	aggregate := aggregateContractReviewRisks(issues)
	parties := partiesFromReviewFacts(facts)
	recommendations := deriveContractReviewRecommendations(issues, facts, warnings)
	data := map[string]any{
		// These keys are the legacy overview contract and must remain stable.
		"overall_risk":        aggregate.OverallRisk,
		"executive_summary":   strings.TrimSpace(modelOverview.ExecutiveSummary),
		"contract_type":       strings.TrimSpace(modelOverview.ContractType),
		"parties":             parties,
		"key_recommendations": recommendations,
		"risk_counts":         aggregate.Counts,
		// Additive quality fields for new consumers.
		"facts":          facts,
		"quality_status": quality,
		"warnings":       warnings,
	}
	encoded, err := json.Marshal(data)
	if err != nil {
		return nil, err
	}
	return types.JSON(encoded), nil
}

// deriveContractReviewRecommendations keeps the overview from becoming a
// second, independently hallucinated finding list. Every recommendation is
// either attached to a validated issue or follows a deterministic warning
// produced from the verified facts/document structure.
func deriveContractReviewRecommendations(issues []*types.ContractReviewIssue, facts []types.ContractReviewFact, warnings []types.ContractReviewWarning) []string {
	recommendations := make([]string, 0, len(issues)+len(warnings))
	for _, issue := range issues {
		if issue != nil && strings.TrimSpace(issue.Suggestion) != "" {
			recommendations = append(recommendations, issue.Suggestion)
		}
	}
	for _, warning := range warnings {
		var recommendation string
		switch warning.Code {
		case "FACT_CONFLICT", "DATE_ORDER_CONFLICT", "PAYMENT_PERCENTAGE_OVER_100":
			recommendation = "核对相互矛盾的合同事实，并以双方确认的版本统一条款。"
		case "PLACEHOLDER_OR_BLANK_FIELD", "EMPTY_REQUIRED_FIELD":
			recommendation = "补全合同中的空白或占位字段，并在签署前复核最终文本。"
		case "UNSELECTED_DISPUTE_OPTION":
			recommendation = "明确勾选唯一的争议解决方式，并删除未选项。"
		case "EXTERNAL_REFERENCE":
			recommendation = "核对外部附件、制度或另行约定的版本，并将关键内容纳入合同或附件。"
		case "PARTIES_NOT_IDENTIFIED":
			recommendation = "补充并核对合同当事方的完整名称及签署信息。"
		}
		if recommendation != "" {
			recommendations = append(recommendations, recommendation)
		}
	}
	// Keep facts in the function signature intentionally: recommendations are
	// derived from issue/fact validation, and this guard prevents a future
	// caller from accidentally passing an unvalidated fact collection.
	_ = facts
	return uniqueReviewStrings(recommendations)
}

func contractReviewWarningJSON(warnings []types.ContractReviewWarning) (types.JSON, error) {
	if warnings == nil {
		warnings = []types.ContractReviewWarning{}
	}
	data, err := json.Marshal(warnings)
	if err != nil {
		return nil, err
	}
	return types.JSON(data), nil
}

func appendContractReviewWarning(raw types.JSON, warning types.ContractReviewWarning) (types.JSON, error) {
	warnings := make([]types.ContractReviewWarning, 0, 1)
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &warnings); err != nil {
			return nil, fmt.Errorf("decode contract review warnings: %w", err)
		}
	}
	warnings = append(warnings, warning)
	return contractReviewWarningJSON(warnings)
}

func updateContractReviewOverviewQuality(raw types.JSON, quality types.ContractReviewQualityStatus, warnings types.JSON) (types.JSON, error) {
	overview := make(map[string]any)
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &overview); err != nil {
			return nil, fmt.Errorf("decode contract review overview: %w", err)
		}
	}
	if overview == nil {
		overview = make(map[string]any)
	}
	overview["quality_status"] = quality
	if len(warnings) == 0 {
		overview["warnings"] = []types.ContractReviewWarning{}
	} else {
		var decoded []types.ContractReviewWarning
		if err := json.Unmarshal(warnings, &decoded); err != nil {
			return nil, fmt.Errorf("decode contract review overview warnings: %w", err)
		}
		overview["warnings"] = decoded
	}
	encoded, err := json.Marshal(overview)
	if err != nil {
		return nil, err
	}
	return types.JSON(encoded), nil
}
