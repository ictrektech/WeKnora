package service

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
)

func reviewValidationFixture() (string, []reviewEvidenceUnit) {
	document := "本合同甲方：甲公司\n付款：30日内支付。"
	return document, []reviewEvidenceUnit{{
		ID: "primary", Role: types.ContractReviewEvidencePrimary,
		Start: 0, End: len([]rune(document)), Text: document, Prompted: true,
	}}
}

func validReviewBatchJSON(quote string) string {
	output := map[string]any{
		"issues": []map[string]any{{
			"category": "payment", "finding_type": "ambiguity", "risk_level": "medium",
			"title": "付款期限表述不清", "explanation": "付款期限需要明确。", "original_quote": quote,
			"suggestion": "明确付款起算日和到期日。", "evidence_refs": []string{"primary"},
		}},
		"facts": []any{},
	}
	encoded, _ := json.Marshal(output)
	return string(encoded)
}

func TestValidateReviewBatchRejectsMalformedMissingAndUnknownRisk(t *testing.T) {
	document, units := reviewValidationFixture()
	for name, content := range map[string]string{
		"malformed":        "{not-json}",
		"missing facts":    `{"issues":[]}`,
		"unknown risk":     strings.Replace(validReviewBatchJSON("付款：30日内支付。"), `"medium"`, `"unknown"`, 1),
		"punctuation edit": validReviewBatchJSON("付款：30日内支付！"),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := validateReviewBatchJSON(content, document, units); err == nil {
				t.Fatal("invalid model output must fail validation")
			}
		})
	}
}

func TestValidateReviewBatchRejectsUnknownAndDuplicateEvidence(t *testing.T) {
	document, units := reviewValidationFixture()
	unknown := strings.Replace(validReviewBatchJSON("付款：30日内支付。"), `"primary"`, `"not-a-source-unit"`, 1)
	duplicate := strings.Replace(validReviewBatchJSON("付款：30日内支付。"), `["primary"]`, `["primary","primary"]`, 1)
	for name, content := range map[string]string{"unknown evidence": unknown, "duplicate evidence": duplicate} {
		t.Run(name, func(t *testing.T) {
			if _, err := validateReviewBatchJSON(content, document, units); err == nil {
				t.Fatal("invalid evidence references must fail validation")
			}
		})
	}
}

func TestValidateReviewBatchIsolatesUnlocatableIssueEvidence(t *testing.T) {
	document, units := reviewValidationFixture()
	content, err := json.Marshal(map[string]any{
		"issues": []map[string]any{
			{
				"category": "payment", "finding_type": "ambiguity", "risk_level": "medium",
				"title": "付款期限表述不清", "explanation": "付款期限需要明确。", "original_quote": "付款：30日内支付。",
				"suggestion": "明确付款起算日和到期日。", "evidence_refs": []string{"primary"},
			},
			{
				"category": "term", "finding_type": "missing", "risk_level": "low",
				"title": "期限信息需要核对", "explanation": "该条引用无法在原文中定位。", "original_quote": "这段文字不在合同原文中。",
				"suggestion": "补充并核对期限条款。", "evidence_refs": []string{"primary"},
			},
		},
		"facts": []any{},
	})
	if err != nil {
		t.Fatal(err)
	}
	validated, failures, err := validateReviewBatchWithEvidenceIsolation(string(content), document, units)
	if err != nil {
		t.Fatalf("evidence-only issue failure should be isolatable: %v", err)
	}
	if len(validated.Issues) != 1 || validated.Issues[0].Title != "付款期限表述不清" {
		t.Fatalf("unexpected retained issues: %+v", validated.Issues)
	}
	if len(failures) != 1 || failures[0].Index != 2 {
		t.Fatalf("unexpected isolated failures: %+v", failures)
	}
}

func TestValidateReviewBatchIsolatesOversizedOriginalQuote(t *testing.T) {
	document, units := reviewValidationFixture()
	content, err := json.Marshal(map[string]any{
		"issues": []map[string]any{
			{
				"category": "payment", "finding_type": "ambiguity", "risk_level": "medium",
				"title": "付款期限表述不清", "explanation": "付款期限需要明确。", "original_quote": "付款：30日内支付。",
				"suggestion": "明确付款起算日和到期日。", "evidence_refs": []string{"primary"},
			},
			{
				"category": "term", "finding_type": "missing", "risk_level": "low",
				"title": "期限信息需要核对", "explanation": "模型返回的证据过长。",
				"original_quote": strings.Repeat("过长证据。", contractReviewMaxQuoteRunes),
				"suggestion":     "补充并核对期限条款。", "evidence_refs": []string{"primary"},
			},
		},
		"facts": []any{},
	})
	if err != nil {
		t.Fatal(err)
	}
	validated, failures, err := validateReviewBatchWithEvidenceIsolation(string(content), document, units)
	if err != nil {
		t.Fatalf("oversized original_quote should be isolatable: %v", err)
	}
	if len(validated.Issues) != 1 || validated.Issues[0].Title != "付款期限表述不清" {
		t.Fatalf("unexpected retained issues: %+v", validated.Issues)
	}
	if len(failures) != 1 || failures[0].Index != 2 {
		t.Fatalf("unexpected isolated failures: %+v", failures)
	}
}

func TestValidateReviewBatchEvidenceIsolationKeepsNonEvidenceFailuresFatal(t *testing.T) {
	document, units := reviewValidationFixture()
	content := strings.Replace(validReviewBatchJSON("付款：30日内支付。"), `"medium"`, `"unknown"`, 1)
	if _, _, err := validateReviewBatchWithEvidenceIsolation(content, document, units); err == nil {
		t.Fatal("unknown risk must remain a fatal batch validation failure")
	}
}

func TestValidateReviewFactAcceptsWhitespaceAcrossReferencedUnits(t *testing.T) {
	document := "金额为一百元。\n\n付款安排已约定，双方应按合同执行。"
	units := []reviewEvidenceUnit{
		{ID: "u1", Role: types.ContractReviewEvidencePrimary, Start: 0, End: len([]rune("金额为一百元。")), Text: "金额为一百元。"},
		{ID: "u2", Role: types.ContractReviewEvidenceSupporting, Start: len([]rune("金额为一百元。\n\n")), End: len([]rune(document)), Text: "付款安排已约定，双方应按合同执行。"},
	}
	content := `{"issues":[],"facts":[{"type":"amount","key":"total_amount","value":"一百元付款安排","evidence_quote":"金额为一百元。付款安排已约定，双方应按合同执行。","evidence_refs":["u1","u2"]}]}`
	if _, err := validateReviewBatchJSON(content, document, units); err != nil {
		t.Fatalf("quote crossing formatting whitespace should validate: %v", err)
	}
}

func TestValidateReviewIssueAcceptsExactSupportingEvidence(t *testing.T) {
	document := "当前窗口说明。\n关联条款明确约定，双方应共同遵守。"
	units := []reviewEvidenceUnit{
		{ID: "primary", Role: types.ContractReviewEvidencePrimary, Start: 0, End: len([]rune("当前窗口说明。")), Text: "当前窗口说明。"},
		{ID: "support", Role: types.ContractReviewEvidenceSupporting, Start: len([]rune("当前窗口说明。\n")), End: len([]rune(document)), Text: "关联条款明确约定，双方应共同遵守。"},
	}
	content := `{"issues":[{"category":"payment","finding_type":"contradiction","risk_level":"medium","title":"关联条款存在冲突","explanation":"关联条款的明确约定需要与当前窗口统一。","original_quote":"关联条款明确约定，双方应共同遵守。","suggestion":"核对并统一两处条款。","evidence_refs":["primary","support"]}],"facts":[]}`
	if _, err := validateReviewBatchJSON(content, document, units); err != nil {
		t.Fatalf("exact supporting evidence should validate: %v", err)
	}
}

func TestResolveReviewEvidenceUsesDeclaredUnitOrder(t *testing.T) {
	document := "付款安排已约定。\n其他条款。\n付款安排已约定。"
	units := []reviewEvidenceUnit{
		{ID: "primary", Role: types.ContractReviewEvidencePrimary, Start: 0, End: len([]rune("付款安排已约定。")), Text: "付款安排已约定。"},
		{ID: "support", Role: types.ContractReviewEvidenceSupporting, Start: len([]rune("付款安排已约定。\n其他条款。\n")), End: len([]rune(document)), Text: "付款安排已约定。"},
	}
	resolved, err := resolveReviewEvidence(document, "付款安排已约定。", units, []string{"primary", "support"}, false)
	if err != nil {
		t.Fatalf("declared primary evidence should disambiguate repeated supporting text: %v", err)
	}
	if resolved.Start != 0 {
		t.Fatalf("resolved source start=%d, want primary unit start 0", resolved.Start)
	}
}

func TestResolveReviewEvidenceRepairsMismatchedRefForUniqueExactQuote(t *testing.T) {
	document := "主窗口内容。\n验收条款内容。\n违约责任条款明确约定。"
	units := []reviewEvidenceUnit{
		{ID: "primary", Role: types.ContractReviewEvidencePrimary, Start: 0, End: len([]rune("主窗口内容。")), Text: "主窗口内容。"},
		{ID: "neighbour", Role: types.ContractReviewEvidenceSupporting, Start: len([]rune("主窗口内容。\n")), End: len([]rune("主窗口内容。\n验收条款内容。")), Text: "验收条款内容。"},
		{ID: "actual", Role: types.ContractReviewEvidenceSupporting, Start: len([]rune("主窗口内容。\n验收条款内容。\n")), End: len([]rune(document)), Text: "违约责任条款明确约定。"},
	}
	resolved, err := resolveReviewEvidence(document, "违约责任条款明确约定。", units, []string{"primary", "neighbour"}, false)
	if err != nil {
		t.Fatalf("unique exact quote should be recoverable: %v", err)
	}
	if resolved.Start != len([]rune("主窗口内容。\n验收条款内容。\n")) || len(resolved.CanonicalRefs) != 1 || resolved.CanonicalRefs[0] != "actual" {
		t.Fatalf("unexpected repaired evidence: %+v", resolved)
	}
}

func TestReviewPromptEvidenceUnitsExposeOneStablePrimaryWindow(t *testing.T) {
	document := "一、付款\n付款期限为三十日。\n二、验收\n完成后验收。"
	clause := &types.ContractReviewClause{SourceStart: 0, SourceEnd: len([]rune("一、付款\n付款期限为三十日。"))}
	units := reviewPromptEvidenceUnits("source", clause, document, nil)
	if len(units) != 1 {
		t.Fatalf("primary analysis window should be one evidence unit, got %d", len(units))
	}
	if !units[0].Prompted || units[0].Role != types.ContractReviewEvidencePrimary || units[0].Text != string([]rune(document)[:clause.SourceEnd]) {
		t.Fatalf("primary window was not exposed as a prompted stable unit: %+v", units[0])
	}
	if !strings.Contains(renderReviewEvidencePrompt(units), "evidence_id=") || !strings.Contains(renderReviewEvidencePrompt(units), "付款期限为三十日") {
		t.Fatal("rendered prompt must include the stable primary evidence id and full window")
	}
}

func TestReviewEvidenceCoverageDoesNotBridgeUnrelatedText(t *testing.T) {
	document := "甲\n无关内容\n乙"
	units := []reviewEvidenceUnit{
		{Start: 0, End: 1},
		{Start: len([]rune("甲\n无关内容\n")), End: len([]rune(document))},
	}
	candidate := reviewResolvedEvidence{Start: 0, End: len([]rune(document))}
	if reviewEvidenceRangeSupported(candidate, units, document) {
		t.Fatal("evidence coverage must not bridge unrelated source text")
	}
}

func TestGenericContractWarningsCoverBlankUncheckedAndExternalReferences(t *testing.T) {
	warnings := reviewDocumentStructureWarnings("付款：\n争议解决：○仲裁 □诉讼\n详见附件清单。", "source")
	codes := make(map[string]bool, len(warnings))
	for _, warning := range warnings {
		codes[warning.Code] = true
		if warning.SourceEnd <= warning.SourceStart || warning.EvidenceID == "" {
			t.Fatalf("warning must carry an exact source range: %+v", warning)
		}
	}
	for _, code := range []string{"EMPTY_REQUIRED_FIELD", "UNSELECTED_DISPUTE_OPTION", "EXTERNAL_REFERENCE"} {
		if !codes[code] {
			t.Fatalf("missing generic warning %q: %+v", code, warnings)
		}
	}
}

func TestExtractContractReviewFactsUsesFullSourceEvidence(t *testing.T) {
	document := "甲方：甲公司\n乙方：乙公司\n签订日期：2026年01月02日\n合同期限：2026年01月02日至2026年12月31日\n合同总价：100,000元\n付款：合同生效后支付30%。\n履约保证金：合同金额的10%。\n验收：交付后五个工作日内验收。\n争议解决：提交仲裁。\n详见附件清单。"
	facts := extractContractReviewFacts(document, "source-revision")
	if len(facts) == 0 {
		t.Fatal("expected source-backed facts")
	}
	parties := partiesFromReviewFacts(facts)
	if len(parties) != 2 || parties[0] != "甲公司" || parties[1] != "乙公司" {
		t.Fatalf("unexpected parties: %+v", parties)
	}
	seenTypes := make(map[string]bool)
	seenDate := false
	for _, fact := range facts {
		seenTypes[fact.Type] = true
		if fact.Type == "date" && fact.NormalizedValue == "2026-01-02" {
			seenDate = true
		}
		if fact.Status != types.ContractReviewFactPresent || fact.EvidenceQuote == "" || len(fact.EvidenceRefs) != 1 || fact.SourceEnd <= fact.SourceStart {
			t.Fatalf("fact must have source-backed evidence: %+v", fact)
		}
		quote := string([]rune(document)[fact.SourceStart:fact.SourceEnd])
		if quote != fact.EvidenceQuote {
			t.Fatalf("fact evidence quote mismatch: got %q, source %q", fact.EvidenceQuote, quote)
		}
		if fact.EvidenceRefs[0] != contractReviewEvidenceID("source-revision", fact.SourceStart, fact.SourceEnd) {
			t.Fatalf("fact evidence id is not derived from its source range: %+v", fact)
		}
	}
	for _, factType := range []string{"party", "date", "term", "amount", "payment", "deposit", "acceptance", "dispute", "reference"} {
		if !seenTypes[factType] {
			t.Fatalf("missing generic fact type %q in %+v", factType, facts)
		}
	}
	if !seenDate {
		t.Fatalf("date facts should be normalized from the source: %+v", facts)
	}
}

func TestFactConsistencyWarningsDetectDateAndAmountConflicts(t *testing.T) {
	facts := []types.ContractReviewFact{
		{Type: "date", Key: "start_date", NormalizedValue: "2026-12-31", EvidenceRefs: []string{"start"}},
		{Type: "date", Key: "end_date", NormalizedValue: "2026-01-01", EvidenceRefs: []string{"end"}},
		{Type: "amount", Key: "total_amount", NormalizedValue: "100", EvidenceRefs: []string{"amount-1"}},
		{Type: "amount", Key: "total_amount", NormalizedValue: "200", EvidenceRefs: []string{"amount-2"}},
	}
	warnings := reviewFactConsistencyWarnings(facts)
	codes := make(map[string]bool, len(warnings))
	for _, warning := range warnings {
		codes[warning.Code] = true
	}
	if !codes["DATE_ORDER_CONFLICT"] || !codes["FACT_CONFLICT"] {
		t.Fatalf("expected date and amount conflicts, got %+v", warnings)
	}
}

func TestContractReviewRecommendationsAreDerivedFromVerifiedIssues(t *testing.T) {
	issues := []*types.ContractReviewIssue{{Suggestion: "只保留已核验的修改建议。"}}
	data, err := buildContractReviewOverviewJSON(reviewOverviewOutput{
		ExecutiveSummary: "审查摘要", ContractType: "服务合同", Parties: []string{},
		KeyRecommendations: []string{"模型自行臆测的建议"},
	}, issues, nil, types.ContractReviewQualityValid, nil)
	if err != nil {
		t.Fatal(err)
	}
	var overview map[string]any
	if err := json.Unmarshal(data, &overview); err != nil {
		t.Fatal(err)
	}
	recommendations, ok := overview["key_recommendations"].([]any)
	if !ok || len(recommendations) != 1 || recommendations[0] != "只保留已核验的修改建议。" {
		t.Fatalf("overview recommendations were not derived: %+v", overview["key_recommendations"])
	}
}

func TestContractReviewTaskRequiresCurrentAnalysisRun(t *testing.T) {
	review := &types.ContractReview{AnalysisRunID: "run-2", ConfigHash: "cfg-2"}
	if contractReviewTaskMatchesRun(types.ContractReviewTaskPayload{AnalysisRunID: "run-1", ConfigHash: "cfg-2"}, review) {
		t.Fatal("an older analysis run must be ignored")
	}
	if contractReviewTaskMatchesRun(types.ContractReviewTaskPayload{AnalysisRunID: "run-2", ConfigHash: "cfg-1"}, review) {
		t.Fatal("a task with an older config must be ignored")
	}
	if !contractReviewTaskMatchesRun(types.ContractReviewTaskPayload{AnalysisRunID: "run-2", ConfigHash: "cfg-2"}, review) {
		t.Fatal("the current analysis run should be accepted")
	}
}

func TestBuildContractReviewStructureIssueUsesGenericEvidenceRange(t *testing.T) {
	document := "付款：\n"
	clauses := []*types.ContractReviewClause{{ID: "clause-1", SourceStart: 0, SourceEnd: len([]rune(document))}}
	warning := types.ContractReviewWarning{Code: "EMPTY_REQUIRED_FIELD", Message: "字段为空", EvidenceID: "evidence-1", SourceStart: 0, SourceEnd: len([]rune(document)) - 1}
	issue, clause, err := buildContractReviewStructureIssue("review-1", "source", document, warning, clauses, 0)
	if err != nil {
		t.Fatal(err)
	}
	if clause.ID != "clause-1" || issue.Category != "payment" || issue.FindingType != "placeholder" || issue.SourceStart != warning.SourceStart || issue.SourceEnd != warning.SourceEnd {
		t.Fatalf("unexpected derived issue: %+v", issue)
	}
}
