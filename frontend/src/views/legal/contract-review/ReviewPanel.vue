<template>
  <aside class="review-panel" :aria-label="t('contractReview.reviewPanelTitle')">
    <header class="review-panel__header">
      <div><span class="eyebrow">{{ t('contractReview.aiReview') }}</span><h2>{{ t('contractReview.reviewPanelTitle') }}</h2></div>
      <div class="review-panel__actions">
        <button data-testid="contract-cancel-review" v-if="isRunning" class="cancel-review-button" type="button" :disabled="busy" @click="emit('cancel')"><t-icon name="close" aria-hidden="true" /> {{ t('contractReview.cancelReview') }}</button>
        <button data-testid="contract-review-again" v-if="review.status === 'completed' && !reconfigure" class="rerun-button" type="button" :disabled="busy" @click="emit('reconfigure')"><t-icon name="refresh" aria-hidden="true" /> {{ t('contractReview.reviewAgain') }}</button>
        <span v-if="review.status !== 'draft'" class="status-dot" :class="`status-dot--${review.status}`" role="status">{{ statusLabel }}</span>
      </div>
    </header>

    <div v-if="setupMode" class="review-setup">
      <div class="review-setup__intro"><t-icon :name="reconfigure ? 'refresh' : review.status === 'draft' ? 'upload' : 'check-circle'" size="20px" aria-hidden="true" /><div><strong>{{ t(reconfigure ? 'contractReview.reconfigureReview' : review.status === 'draft' ? 'contractReview.uploadFirst' : 'contractReview.readyToReview') }}</strong><p>{{ t(reconfigure ? 'contractReview.reconfigureDescription' : review.status === 'draft' ? 'contractReview.uploadFirstDescription' : 'contractReview.readyDescription') }}</p></div></div>
      <label class="review-setup__title">{{ t('contractReview.taskName') }}<input data-testid="contract-review-task-name" type="text" :value="review.title" maxlength="512" :placeholder="t('contractReview.taskNamePlaceholder')" :aria-label="t('contractReview.taskName')" @blur="emit('titleChange', ($event.target as HTMLInputElement).value)" @keydown.enter="($event.target as HTMLInputElement).blur()" /><small>{{ t('contractReview.taskNameHint') }}</small></label>
      <label>{{ t('contractReview.model') }}<select :value="review.model_id || ''" :disabled="modelsLoading" @change="emit('configChange', { model_id: ($event.target as HTMLSelectElement).value })"><option value="">{{ t('contractReview.useDefaultModel') }}</option><option v-if="!modelsLoading && !reviewModels.length" value="" disabled>{{ t('contractReview.noAvailableModels') }}</option><option v-for="model in reviewModels" :key="model.id" :value="model.id" :disabled="!isModelActive(model)">{{ modelDisplayName(model) }}{{ !isModelActive(model) ? ` (${t('contractReview.modelUnavailable')})` : '' }}</option></select><button v-if="!modelsLoading && !reviewModels.length" class="configure-model" type="button" @click="emit('configure')">{{ t('contractReview.configureModel') }}</button></label>
      <label>{{ t('contractReview.playbook') }}<select :value="review.playbook_id" @change="emit('configChange', { playbook_id: ($event.target as HTMLSelectElement).value })"><option v-for="playbook in playbooks" :key="playbook.id" :value="playbook.id">{{ playbook.name }}</option></select></label>
      <label>{{ t('contractReview.representedParty') }}<select :value="review.represented_party" @change="emit('configChange', { represented_party: ($event.target as HTMLSelectElement).value as RepresentedParty })"><option value="customer">{{ t('contractReview.parties.customer') }}</option><option value="vendor">{{ t('contractReview.parties.vendor') }}</option><option value="neutral">{{ t('contractReview.parties.neutral') }}</option></select></label>
      <div class="review-setup__actions"><button v-if="reconfigure" class="cancel-config" type="button" :disabled="busy" @click="emit('cancelReconfigure')">{{ t('contractReview.cancel') }}</button><button data-testid="contract-start-review" class="primary-action" type="button" :disabled="busy || review.status === 'uploading' || (!reconfigure && review.status !== 'ready')" @click="emit('start')"><t-icon name="play-circle" aria-hidden="true" /> {{ review.status === 'uploading' ? t('contractReview.preparingDocument') : t('contractReview.startReview') }}</button></div>
    </div>

    <template v-else>
      <div v-if="isRunning" class="review-progress"><div class="review-progress__row"><span>{{ statusLabel }}</span><strong>{{ review.progress }}%</strong></div><div class="review-progress__track"><i :style="{ width: `${review.progress}%` }" /></div><p>{{ t('contractReview.progressiveResults') }}</p></div>
      <div v-if="review.status === 'failed' || review.status === 'cancelled'" class="review-error" role="alert"><strong>{{ t(review.status === 'cancelled' ? 'contractReview.reviewCancelled' : 'contractReview.reviewFailed') }}</strong><p>{{ review.error_message || t('contractReview.errorDetailsUnavailable') }}</p><button v-if="review.status === 'failed' && review.error_message?.toLowerCase().includes('model')" type="button" @click="emit('configure')">{{ t('contractReview.configureModel') }}</button><button data-testid="contract-retry-review" type="button" @click="emit('retry')">{{ t('contractReview.retry') }}</button></div>
      <nav class="review-tabs" :aria-label="t('contractReview.resultTabs')" role="tablist">
        <button v-for="tab in tabs" :key="tab" :data-testid="`contract-result-tab-${tab}`" type="button" role="tab" :aria-selected="activeTab === tab" :tabindex="activeTab === tab ? 0 : -1" :class="{ active: activeTab === tab }" @click="activeTab = tab">{{ t(`contractReview.tabs.${tab}`) }}<span v-if="tab === 'issues' && issuesLoaded">{{ issues.length }}</span><span v-else-if="tab === 'issues'" aria-hidden="true">—</span></button>
      </nav>
      <div class="review-panel__content">
        <section v-if="activeTab === 'configuration'" class="configuration-pane" role="tabpanel" data-testid="contract-review-configuration">
          <div class="configuration-card">
            <h3>{{ t('contractReview.reviewConfiguration') }}</h3>
            <dl>
              <div><dt>{{ t('contractReview.taskName') }}</dt><dd :title="review.title">{{ review.title || '—' }}</dd></div>
              <div><dt>{{ t('contractReview.model') }}</dt><dd :title="selectedModelName">{{ selectedModelName }}</dd></div>
              <div><dt>{{ t('contractReview.playbook') }}</dt><dd :title="selectedPlaybookName">{{ selectedPlaybookName }}</dd></div>
              <div><dt>{{ t('contractReview.representedParty') }}</dt><dd>{{ representedPartyLabel }}</dd></div>
              <div><dt>{{ t('contractReview.document') }}</dt><dd :title="review.file_name">{{ review.file_name || t('contractReview.draftNoDocument') }}</dd></div>
              <div><dt>{{ t('contractReview.statusLabel') }}</dt><dd>{{ statusLabel }}</dd></div>
            </dl>
          </div>
        </section>
        <section v-else-if="activeTab === 'overview'" class="overview-pane" role="tabpanel">
          <div class="overall-risk"><span>{{ t('contractReview.overallRisk') }}</span><RiskBadge :risk="overallRisk" /><span v-if="issuesLoaded" class="overview-issue-total">{{ t('contractReview.issueCount', { count: issues.length }) }}</span><span class="quality-badge" :class="`quality-badge--${qualityStatus}`">{{ qualityStatusLabel }}</span></div>
          <p v-if="summary" class="summary">{{ summary }}</p>
          <p v-else class="summary summary--empty">{{ isRunning ? t('contractReview.overviewPending') : t('contractReview.summaryUnavailable') }}</p>

          <section v-if="priorityIssues.length" class="overview-priority" aria-labelledby="overview-priority-title">
            <h3 id="overview-priority-title">{{ t('contractReview.priorityFocus') }}</h3>
            <ol>
              <li v-for="issue in priorityIssues" :key="issue.id">
                <button type="button" @click="selectIssue(issue)"><RiskBadge :risk="issue.risk_level" /><span>{{ issue.title }}</span><t-icon name="chevron-right" aria-hidden="true" /></button>
              </li>
            </ol>
          </section>

          <details v-if="qualityWarnings.length" class="overview-disclosure">
            <summary>{{ t('contractReview.qualityWarnings') }} · {{ qualityWarnings.length }}</summary>
            <div class="quality-warnings" role="alert"><ul><li v-for="warning in qualityWarnings" :key="warning">{{ warning }}</li></ul></div>
          </details>

          <div class="risk-counts"><div v-for="risk in riskOrder" :key="risk"><strong>{{ countLabel(risk) }}</strong><span>{{ t(`contractReview.risk.${risk}`) }}</span></div></div>
          <dl class="overview-details">
            <template v-if="review.overview?.contract_type"><dt>{{ t('contractReview.contractType') }}</dt><dd>{{ review.overview.contract_type }}</dd></template>
            <template v-if="parties.length"><dt>{{ t('contractReview.detectedParties') }}</dt><dd><ul><li v-for="party in parties" :key="party">{{ party }}</li></ul></dd></template>
          </dl>
          <div v-if="recommendations.length" class="key-recommendations"><h3>{{ t('contractReview.keyRecommendations') }}</h3><ol><li v-for="(item, index) in recommendations.slice(0, 3)" :key="`${item}-${index}`">{{ item }}</li></ol></div>
        </section>

        <section v-else-if="activeTab === 'issues'" class="issues-pane" role="tabpanel">
          <div v-if="!issuesLoaded" class="empty-results">{{ isRunning ? t('contractReview.findingsAppearHere') : t('contractReview.issuesNotLoaded') }}</div>
          <div v-else-if="!issues.length" class="empty-results">{{ isRunning ? t('contractReview.findingsAppearHere') : t('contractReview.noIssues') }}</div>
          <article v-for="issue in sortedIssues" :id="`review-issue-${issue.id}`" :data-testid="`contract-issue-${issue.id}`" :key="issue.id" class="issue" :class="{ selected: selectedIssueId === issue.id }" tabindex="0" role="button" :aria-label="issueAriaLabel(issue)" :aria-pressed="selectedIssueId === issue.id" @click="selectIssue(issue)" @keydown="onIssueKeydown($event, issue)">
            <div class="issue__top"><RiskBadge :risk="issue.risk_level" /><span>{{ clauseTitle(issue.clause_id) }}</span><span v-if="issueCategory(issue)" class="issue__tag">{{ issueCategory(issue) }}</span><span v-if="issueFindingType(issue)" class="issue__tag">{{ issueFindingType(issue) }}</span></div>
            <h3>{{ issue.title }}</h3><p>{{ issue.explanation }}</p>
            <div class="issue__signals"><span v-if="issueCategory(issue)">{{ t('contractReview.category') }}: {{ issueCategory(issue) }}</span><span v-if="issueFindingType(issue)">{{ t('contractReview.findingType') }}: {{ issueFindingType(issue) }}</span><span v-if="issueConfidence(issue)">{{ t('contractReview.confidence') }}: {{ issueConfidence(issue) }}</span><span v-if="issue.evidence_refs?.length">{{ t('contractReview.evidenceRefs', { count: issue.evidence_refs.length }) }}</span><span class="evidence-status" :class="`evidence-status--${issueEvidenceStatus(issue)}`">{{ t('contractReview.evidenceStatus') }}: {{ evidenceStatusLabel(issueEvidenceStatus(issue)) }}</span></div>
            <details><summary>{{ t('contractReview.evidenceQuote') }}</summary><blockquote>{{ issue.original_quote || t('contractReview.noEvidenceQuote') }}</blockquote></details>
            <div v-if="issue.suggestion" class="issue__suggestion"><span>{{ t('contractReview.suggestedRevision') }}</span><p>{{ issue.suggestion }}</p></div>
            <button type="button" class="view-clause" @click.stop="selectIssue(issue)">{{ t('contractReview.viewClause') }} <t-icon name="locate" aria-hidden="true" /></button>
          </article>
        </section>

        <section v-else class="suggestions-pane" role="tabpanel">
          <div v-if="!issuesLoaded" class="empty-results">{{ t('contractReview.issuesNotLoaded') }}</div>
          <div v-else-if="!issues.length" class="empty-results">{{ t('contractReview.noSuggestions') }}</div>
          <article v-for="issue in sortedIssues" :key="issue.id" tabindex="0" role="button" :aria-label="suggestionAriaLabel(issue)" @click="selectIssue(issue)" @keydown="onIssueKeydown($event, issue)"><RiskBadge :risk="issue.risk_level" /><div><strong>{{ issue.title }}</strong><p>{{ issue.suggestion || t('contractReview.noSuggestionDetails') }}</p><span class="evidence-status" :class="`evidence-status--${issueEvidenceStatus(issue)}`">{{ evidenceStatusLabel(issueEvidenceStatus(issue)) }}</span></div></article>
        </section>
      </div>
    </template>
  </aside>
</template>

<script setup lang="ts">
import { computed, defineComponent, h, ref, watch, type PropType } from 'vue'
import { useI18n } from 'vue-i18n'
import { Icon as TIcon } from 'tdesign-vue-next'
import type { ContractReview, EvidenceStatus, LocatorLoadStatus, RepresentedParty, ReviewClause, ReviewIssue, ReviewPlaybook, ReviewWarningValue, RiskLevel } from '@/api/contract-review'
import type { ModelConfig } from '@/api/model'
import { reviewCollectionState } from './documentLinking'

const props = defineProps<{
  review: ContractReview
  playbooks: ReviewPlaybook[]
  models: ModelConfig[]
  modelsLoading?: boolean
  selectedIssueId?: string
  busy?: boolean
  reconfigure?: boolean
  evidenceStatuses?: Record<string, EvidenceStatus>
  locatorStatus?: LocatorLoadStatus
}>()
const emit = defineEmits<{ start: []; retry: []; cancel: []; reconfigure: []; cancelReconfigure: []; configure: []; titleChange: [value: string]; issueSelect: [issue: ReviewIssue]; configChange: [data: { playbook_id?: string; represented_party?: RepresentedParty; model_id?: string }] }>()
const { t } = useI18n()
const tabs = ['overview', 'issues', 'suggestions', 'configuration'] as const
const activeTab = ref<(typeof tabs)[number]>('overview')
const riskOrder: RiskLevel[] = ['high', 'medium', 'low']
const issues = computed(() => props.review.issues || [])
const issuesLoaded = computed(() => reviewCollectionState(props.review.issues) !== 'not_loaded')
const reconfigure = computed(() => props.reconfigure === true)
const setupMode = computed(() => ['draft', 'uploading', 'ready'].includes(props.review.status) || reconfigure.value)
const isRunning = computed(() => ['uploading', 'analyzing', 'reviewing_clauses'].includes(props.review.status))
const isModelActive = (model: ModelConfig) => !model.status || model.status === 'active'
const modelDisplayName = (model: ModelConfig) => model.display_name?.trim() || model.name
const reviewModels = computed(() => props.models.filter(model => model.type === 'KnowledgeQA' && model.id && (isModelActive(model) || model.id === props.review.model_id)))
const issueCounts = computed(() => ({ high: issues.value.filter(i => i.risk_level === 'high').length, medium: issues.value.filter(i => i.risk_level === 'medium').length, low: issues.value.filter(i => i.risk_level === 'low').length }))
const declaredCounts = computed(() => props.review.overview?.risk_counts)
const counts = computed(() => issuesLoaded.value ? issueCounts.value : {
  high: declaredCounts.value?.high ?? 0,
  medium: declaredCounts.value?.medium ?? 0,
  low: declaredCounts.value?.low ?? 0,
})
const countLabel = (risk: RiskLevel) => issuesLoaded.value || declaredCounts.value?.[risk] !== undefined ? counts.value[risk] : '—'
const derivedRisk = computed<RiskLevel | undefined>(() => issueCounts.value.high ? 'high' : issueCounts.value.medium ? 'medium' : issueCounts.value.low ? 'low' : undefined)
const overallRisk = computed<RiskLevel | undefined>(() => {
  if (!issuesLoaded.value) return props.review.overview?.overall_risk
  // Once details are loaded, the displayed risk follows the verified issue
  // collection. The overview value is only a compatibility fallback for a
  // genuinely empty result.
  return derivedRisk.value || (issues.value.length === 0 ? props.review.overview?.overall_risk : undefined)
})
const sortedIssues = computed(() => [...issues.value].sort((a,b) => riskOrder.indexOf(a.risk_level)-riskOrder.indexOf(b.risk_level) || a.sequence-b.sequence))
const priorityIssues = computed(() => sortedIssues.value.slice(0, 3))
const statusLabel = computed(() => t(`contractReview.status.${props.review.status}`))
const selectedModelName = computed(() => {
  const modelID = props.review.model_id?.trim()
  if (!modelID) return t('contractReview.useDefaultModel')
  const model = props.models.find(item => item.id === modelID)
  return model ? modelDisplayName(model) : modelID
})
const selectedPlaybookName = computed(() => props.playbooks.find(item => item.id === props.review.playbook_id)?.name || props.review.playbook_id || '—')
const representedPartyLabel = computed(() => t(`contractReview.parties.${props.review.represented_party}`))
const summary = computed(() => props.review.overview?.executive_summary?.trim() || '')
const parties = computed(() => props.review.overview?.parties || [])
const recommendations = computed(() => props.review.overview?.key_recommendations || [])
const countMismatched = computed(() => issuesLoaded.value && declaredCounts.value && riskOrder.some((risk) => declaredCounts.value?.[risk] !== undefined && declaredCounts.value[risk] !== issueCounts.value[risk]))
const qualityStatus = computed(() => {
  if (props.review.quality_status) return props.review.quality_status
  if (isRunning.value) return 'pending'
  if (countMismatched.value) return 'degraded'
  if (!issuesLoaded.value || !summary.value) return 'pending'
  return 'valid'
})
const qualityStatusLabel = computed(() => t(`contractReview.quality.${qualityStatus.value}`))
function warningText(warning: ReviewWarningValue) {
  return typeof warning === 'string' ? warning : warning.message || warning.code || ''
}
const qualityWarnings = computed(() => {
  const warnings = [...(props.review.overview?.warnings || []), ...(props.review.warnings || [])].map(warningText)
  if (countMismatched.value) warnings.push(t('contractReview.summaryDetailsMismatch'))
  if (!issuesLoaded.value && props.review.status === 'completed') warnings.push(t('contractReview.issuesNotLoaded'))
  if (!summary.value && props.review.status === 'completed') warnings.push(t('contractReview.summaryMissing'))
  if (props.locatorStatus === 'unsupported') warnings.push(t('contractReview.evidenceLocatorUnsupported'))
  if (props.locatorStatus === 'error') warnings.push(t('contractReview.evidenceLocatorFailed'))
  return Array.from(new Set(warnings.filter(Boolean)))
})
const generatedClauseTitle = /^(?:Clause|Analysis segment)\s+\d+$/i
const formatClauseTitle = (clause: ReviewClause) => {
  const title = clause.title?.trim() || ''
  return generatedClauseTitle.test(title) ? t('contractReview.analysisSegment', { number: clause.sequence + 1 }) : title || t('contractReview.generalClause')
}
const clauseTitle = (id: string) => {
  const clause = props.review.clauses?.find(c => c.id === id)
  return clause ? formatClauseTitle(clause) : t('contractReview.generalClause')
}
function translated(kind: 'categories' | 'findingTypes', value?: string) {
  if (!value) return ''
  const key = `contractReview.${kind}.${value}`
  const result = t(key)
  return result === key ? value.replace(/[_-]+/g, ' ') : result
}
const issueCategory = (issue: ReviewIssue) => translated('categories', issue.category)
const issueFindingType = (issue: ReviewIssue) => translated('findingTypes', issue.finding_type)
function issueConfidence(issue: ReviewIssue) {
  if (issue.confidence) return t(`contractReview.confidenceLevels.${issue.confidence}`)
  if (typeof issue.confidence_score === 'number' && Number.isFinite(issue.confidence_score)) {
    const value = issue.confidence_score <= 1 ? issue.confidence_score * 100 : issue.confidence_score
    return `${Math.round(Math.max(0, Math.min(100, value)))}%`
  }
  return ''
}
function issueEvidenceStatus(issue: ReviewIssue): EvidenceStatus {
  return props.evidenceStatuses?.[issue.id] || issue.evidence_status || 'pending'
}
function evidenceStatusLabel(status: EvidenceStatus) {
  return t(`contractReview.evidence.${status}`)
}
function issueAriaLabel(issue: ReviewIssue) {
  return [issue.title, t(`contractReview.risk.${issue.risk_level}`), issueCategory(issue), evidenceStatusLabel(issueEvidenceStatus(issue))].filter(Boolean).join(', ')
}
function suggestionAriaLabel(issue: ReviewIssue) {
  return [t('contractReview.suggestedRevision'), issue.title, evidenceStatusLabel(issueEvidenceStatus(issue))].filter(Boolean).join(', ')
}
function selectIssue(issue: ReviewIssue) { activeTab.value = 'issues'; emit('issueSelect', issue) }
function onIssueKeydown(event: KeyboardEvent, issue: ReviewIssue) {
  if (event.key !== 'Enter' && event.key !== ' ') return
  event.preventDefault()
  selectIssue(issue)
}
function focusIssue(id: string) {
  if (!id) return
  activeTab.value = 'issues'
  requestAnimationFrame(() => {
    const target = document.getElementById(`review-issue-${id}`)
    target?.scrollIntoView({ behavior: 'smooth', block: 'nearest' })
    target?.focus({ preventScroll: true })
  })
}
watch(() => props.selectedIssueId, (id) => { if (id) focusIssue(id) })
defineExpose({ focusIssue })

const RiskBadge = defineComponent({ props: { risk: { type: String as PropType<RiskLevel>, required: false } }, setup(p) { return () => h('span', { class:`risk-badge ${p.risk ? `risk-badge--${p.risk}` : 'risk-badge--unknown'}` }, p.risk ? t(`contractReview.risk.${p.risk}`) : t('contractReview.unknownRisk')) } })
</script>

<style scoped lang="less">
.review-panel{width:400px;min-width:400px;height:100%;display:flex;flex-direction:column;background:var(--legal-bg-surface);border-left:1px solid var(--legal-border);color:var(--legal-text-primary);}
.review-panel__header{min-height:72px;padding:15px 20px;display:flex;align-items:center;justify-content:space-between;border-bottom:1px solid var(--legal-border);box-sizing:border-box;.eyebrow{font-size:10px;letter-spacing:.11em;text-transform:uppercase;color:var(--legal-text-secondary);}h2{margin:3px 0 0;font-size:16px;font-weight:650;}}
.review-panel__actions{display:flex;align-items:center;gap:8px}.rerun-button,.cancel-review-button{height:28px;display:inline-flex;align-items:center;gap:4px;padding:0 8px;border:1px solid var(--legal-border);border-radius:4px;background:var(--legal-bg-surface);color:var(--legal-text-secondary);font-size:10px;font-weight:650;cursor:pointer;&:hover{border-color:var(--legal-ai);color:var(--legal-ai-strong)}&:disabled{opacity:.45;cursor:not-allowed}}.cancel-review-button{border-color:var(--legal-risk);color:var(--legal-risk-strong);&:hover{border-color:var(--legal-risk);color:var(--legal-risk-strong);background:var(--legal-risk-soft)}}
.status-dot{padding:4px 8px;border-radius:4px;background:var(--legal-bg-hover);font-size:11px;color:var(--legal-text-secondary);&:before{content:'';display:inline-block;width:6px;height:6px;border-radius:50%;margin-right:5px;background:var(--legal-text-disabled);}&--analyzing:before,&--reviewing_clauses:before{background:var(--legal-warning);animation:pulse 1.4s infinite;}&--completed:before{background:var(--legal-ai-strong);}&--failed:before,&--cancelled:before{background:var(--legal-risk);}}
.review-setup{padding:24px 20px;display:flex;flex-direction:column;gap:20px;.review-setup__intro{display:flex;gap:10px;padding-bottom:18px;border-bottom:1px solid var(--legal-border);color:var(--legal-ai-strong);strong{font-size:14px;color:var(--legal-text-primary);}p{margin:5px 0 0;font-size:12px;line-height:1.5;color:var(--legal-text-secondary);}}label{display:flex;flex-direction:column;gap:7px;font-size:11px;font-weight:650;color:var(--legal-text-secondary);text-transform:uppercase;letter-spacing:.05em;}select{height:38px;padding:0 10px;border:1px solid var(--legal-border);border-radius:5px;background:var(--legal-bg-surface);color:var(--legal-text-primary);font:inherit;text-transform:none;letter-spacing:0;&:focus{outline:2px solid var(--legal-focus-ring);border-color:var(--legal-ai);}&:disabled{opacity:.65;cursor:wait;}}.configure-model{align-self:flex-start;padding:0;border:0;background:transparent;color:var(--legal-ai-strong);font-size:11px;cursor:pointer;&:hover{text-decoration:underline;}}}
.review-setup__title input{height:38px;padding:0 10px;border:1px solid var(--legal-border);border-radius:5px;background:var(--legal-bg-surface);color:var(--legal-text-primary);font:inherit;text-transform:none;letter-spacing:0;&::placeholder{color:var(--legal-text-secondary);}&:focus{outline:2px solid var(--legal-focus-ring);border-color:var(--legal-ai);}}
.review-setup__title small{font-size:10px;font-weight:400;line-height:1.4;text-transform:none;letter-spacing:0;color:var(--legal-text-secondary);}
.review-setup__actions{display:flex;gap:8px;align-items:center}.review-setup__actions .primary-action{flex:1;margin:0}.cancel-config{height:40px;padding:0 13px;border:1px solid var(--legal-border);border-radius:5px;background:var(--legal-bg-surface);color:var(--legal-text-secondary);font-weight:600;cursor:pointer;&:disabled{opacity:.5;cursor:not-allowed}}
.primary-action{height:40px;border:0;border-radius:5px;background:var(--legal-brand);color:#fff;font-weight:650;cursor:pointer;&:hover:not(:disabled){background:var(--legal-brand-hover);}&:disabled{background:var(--legal-text-disabled);cursor:not-allowed;}}
.configuration-pane{padding:0}.configuration-card{padding:14px;border:1px solid var(--legal-border);border-radius:5px;background:var(--legal-bg-hover);h3{margin:0 0 14px;font-size:12px;font-weight:650;color:var(--legal-text-primary)}dl{margin:0;display:grid;gap:14px}dl>div{display:grid;grid-template-columns:72px minmax(0,1fr);gap:10px;align-items:baseline}dt{color:var(--legal-text-secondary);font-size:10px;text-transform:uppercase;letter-spacing:.04em}dd{min-width:0;margin:0;overflow:hidden;color:var(--legal-text-primary);font-size:12px;font-weight:600;text-overflow:ellipsis;white-space:nowrap}}
.review-progress{padding:13px 20px;border-bottom:1px solid var(--legal-border);background:var(--legal-ai-soft);font-size:12px;.review-progress__row{display:flex;justify-content:space-between}.review-progress__track{height:3px;margin-top:9px;background:var(--legal-border);i{display:block;height:100%;background:var(--legal-ai);transition:width .4s;}}p{margin:8px 0 0;color:var(--legal-text-secondary);font-size:11px;}}
.review-error{margin:14px 20px;padding:13px;border:1px solid var(--legal-risk);background:var(--legal-risk-soft);border-radius:5px;color:var(--legal-risk-strong);p{font-size:12px;word-break:break-word;}button{margin:9px 8px 0 0;border:1px solid var(--legal-risk);background:var(--legal-bg-surface);color:var(--legal-risk-strong);border-radius:4px;padding:6px 10px;cursor:pointer;&:focus-visible{outline:2px solid var(--legal-focus-ring);outline-offset:1px;}}}
.review-tabs{height:43px;display:flex;padding:0 13px;border-bottom:1px solid var(--legal-border);button{position:relative;padding:0 8px;border:0;background:transparent;color:var(--legal-text-secondary);font-size:12px;cursor:pointer;&.active{color:var(--legal-brand);font-weight:650;&:after{content:'';position:absolute;left:8px;right:8px;bottom:-1px;height:2px;background:var(--legal-brand);}}&:focus-visible{outline:2px solid var(--legal-focus-ring);outline-offset:-2px;}span{margin-left:4px;color:var(--legal-text-secondary);}}}
.review-panel__content{min-height:0;flex:1;overflow:auto;padding:18px 20px;}
.overall-risk{display:flex;align-items:center;gap:8px;padding-bottom:15px;border-bottom:1px solid var(--legal-border);font-size:12px;color:var(--legal-text-secondary)}.overall-risk>span:first-child{margin-right:auto}.summary{font-size:13px;line-height:1.65;color:var(--legal-text-primary)}.summary--empty{color:var(--legal-text-secondary);font-style:italic}
.quality-badge{padding:3px 6px;border-radius:3px;background:var(--legal-bg-hover);font-size:9px;white-space:nowrap}.quality-badge--inconsistent,.quality-badge--invalid,.quality-badge--stale{color:var(--legal-risk-strong);background:var(--legal-risk-soft)}.quality-badge--partial,.quality-badge--pending,.quality-badge--degraded,.quality-badge--legacy{color:var(--legal-warning-strong);background:var(--legal-warning-soft)}.quality-badge--complete,.quality-badge--valid{color:var(--legal-ai-strong);background:var(--legal-ai-soft)}.quality-warnings{margin:12px 0;padding:10px;border:1px solid var(--legal-warning);border-radius:4px;color:var(--legal-warning-strong);background:var(--legal-warning-soft);font-size:11px;line-height:1.5}.quality-warnings strong{font-size:11px}.quality-warnings ul{margin:5px 0 0;padding-left:17px}
.overview-issue-total{margin-left:auto;color:var(--legal-text-secondary);font-size:11px;white-space:nowrap}.overview-priority{margin:16px 0;padding:12px;border:1px solid var(--legal-border);border-radius:5px;background:var(--legal-bg-hover)}.overview-priority h3{margin:0 0 9px;font-size:11px;font-weight:650}.overview-priority ol{margin:0;padding:0;list-style:none}.overview-priority li+li{margin-top:5px}.overview-priority button{width:100%;display:flex;align-items:center;gap:7px;padding:7px 0;border:0;background:transparent;color:var(--legal-text-primary);text-align:left;font:inherit;cursor:pointer}.overview-priority button span{flex:1;overflow:hidden;text-overflow:ellipsis;white-space:nowrap;font-size:11px}.overview-priority button :deep(.t-icon){flex:none;color:var(--legal-text-secondary)}.overview-priority button:hover{color:var(--legal-brand)}.overview-priority button:focus-visible{outline:2px solid var(--legal-focus-ring);outline-offset:2px;border-radius:3px}.overview-disclosure{margin:12px 0;border-top:1px solid var(--legal-border);border-bottom:1px solid var(--legal-border)}.overview-disclosure>summary{padding:10px 0;color:var(--legal-text-secondary);font-size:11px;cursor:pointer}.overview-disclosure>summary:focus-visible{outline:2px solid var(--legal-focus-ring);outline-offset:2px}.overview-disclosure .quality-warnings{margin:0 0 10px;padding:0;border:0;background:transparent}
.risk-counts{display:grid;grid-template-columns:repeat(3,1fr);margin:18px 0;border:1px solid var(--legal-border);border-radius:5px;div{padding:12px;border-right:1px solid var(--legal-border);&:last-child{border:0}strong,span{display:block}strong{font-size:18px}span{margin-top:2px;font-size:10px;color:var(--legal-text-secondary)}}}.overview-details{dt{margin-top:12px;font-size:10px;color:var(--legal-text-secondary);text-transform:uppercase}.overview-details dd{margin:5px 0;font-size:13px}.overview-details ul{margin:0;padding-left:18px;line-height:1.5}}.source-revision{font-family:monospace;font-size:11px!important;word-break:break-all;color:var(--legal-text-secondary)}.key-recommendations h3{font-size:12px}.key-recommendations ol{padding-left:18px;font-size:12px;line-height:1.6}
.issue{padding:15px 0;border-bottom:1px solid var(--legal-border);cursor:pointer;&.selected{margin:0 -10px;padding:15px 10px;background:var(--legal-ai-soft);border-radius:5px}&:focus-visible{outline:2px solid var(--legal-focus-ring);outline-offset:2px;border-radius:4px}.issue__top{display:flex;align-items:center;flex-wrap:wrap;gap:6px;color:var(--legal-text-secondary);font-size:10px}.issue__tag{padding:2px 5px;border:1px solid var(--legal-border);border-radius:3px;color:var(--legal-text-secondary)}h3{margin:9px 0 6px;font-size:13px}p{margin:0;font-size:12px;line-height:1.55;color:var(--legal-text-secondary)}details{margin-top:10px;summary{font-size:11px;color:var(--legal-text-secondary);cursor:pointer;&:focus-visible{outline:2px solid var(--legal-focus-ring);outline-offset:2px}}blockquote{margin:7px 0 0;padding-left:10px;border-left:2px solid var(--legal-warning);font-size:11px;line-height:1.5;color:var(--legal-text-secondary);white-space:pre-wrap;word-break:break-word}}}.issue__signals{display:flex;flex-wrap:wrap;gap:8px;margin-top:8px;color:var(--legal-text-secondary);font-size:10px}.issue__suggestion{margin-top:11px;padding:10px;background:var(--legal-bg-hover);border-radius:4px;span{font-size:10px;font-weight:650;text-transform:uppercase;color:var(--legal-text-secondary)}p{margin-top:5px}}.view-clause{margin-top:10px;padding:0;border:0;background:transparent;color:var(--legal-ai-strong);font-size:11px;font-weight:650;cursor:pointer;&:focus-visible{outline:2px solid var(--legal-focus-ring);outline-offset:2px}}
:deep(.risk-badge){display:inline-flex;padding:3px 6px;border-radius:3px;font-size:9px;font-weight:700;text-transform:uppercase;letter-spacing:.04em;&.risk-badge--high{color:var(--legal-risk-strong);background:var(--legal-risk-soft)}&.risk-badge--medium{color:var(--legal-warning-strong);background:var(--legal-warning-soft)}&.risk-badge--low{color:var(--legal-ai-strong);background:var(--legal-ai-soft)}&.risk-badge--unknown{color:var(--legal-text-secondary);background:var(--legal-bg-hover)}}
.evidence-status{font-size:10px;color:var(--legal-text-secondary)}.evidence-status--located{color:var(--legal-ai-strong)}.evidence-status--legacy_exact,.evidence-status--pending{color:var(--legal-warning-strong)}.evidence-status--not_found,.evidence-status--version_mismatch,.evidence-status--unsupported,.evidence-status--error{color:var(--legal-risk-strong)}
.suggestions-pane article{display:flex;gap:10px;padding:13px 0;border-bottom:1px solid var(--legal-border);cursor:pointer;&:focus-visible{outline:2px solid var(--legal-focus-ring);outline-offset:2px;border-radius:4px}strong{font-size:12px}p{margin:5px 0;font-size:12px;line-height:1.5;color:var(--legal-text-secondary)}}
.empty-results{padding:36px 10px;text-align:center;color:var(--legal-text-secondary);font-size:12px}@keyframes pulse{50%{opacity:.35}}
</style>
