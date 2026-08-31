<template>
  <aside class="review-panel">
    <header class="review-panel__header">
      <div><span class="eyebrow">{{ t('contractReview.aiReview') }}</span><h2>{{ t('contractReview.reviewPanelTitle') }}</h2></div>
      <div class="review-panel__actions">
        <button data-testid="contract-review-again" v-if="review.status === 'completed' && !reconfigure" class="rerun-button" type="button" :disabled="busy" @click="emit('reconfigure')"><t-icon name="refresh" /> {{ t('contractReview.reviewAgain') }}</button>
        <span v-if="review.status !== 'draft'" class="status-dot" :class="`status-dot--${review.status}`">{{ statusLabel }}</span>
      </div>
    </header>

    <div v-if="setupMode" class="review-setup">
      <div class="review-setup__intro"><t-icon :name="reconfigure ? 'refresh' : review.status === 'draft' ? 'upload' : 'check-circle'" size="20px" /><div><strong>{{ t(reconfigure ? 'contractReview.reconfigureReview' : review.status === 'draft' ? 'contractReview.uploadFirst' : 'contractReview.readyToReview') }}</strong><p>{{ t(reconfigure ? 'contractReview.reconfigureDescription' : review.status === 'draft' ? 'contractReview.uploadFirstDescription' : 'contractReview.readyDescription') }}</p></div></div>
      <label>{{ t('contractReview.playbook') }}<select :value="review.playbook_id" @change="emit('configChange', { playbook_id: ($event.target as HTMLSelectElement).value })"><option v-for="playbook in playbooks" :key="playbook.id" :value="playbook.id">{{ playbook.name }}</option></select></label>
      <label>{{ t('contractReview.representedParty') }}<select :value="review.represented_party" @change="emit('configChange', { represented_party: ($event.target as HTMLSelectElement).value as RepresentedParty })"><option value="customer">{{ t('contractReview.parties.customer') }}</option><option value="vendor">{{ t('contractReview.parties.vendor') }}</option><option value="neutral">{{ t('contractReview.parties.neutral') }}</option></select></label>
      <div class="review-setup__actions"><button v-if="reconfigure" class="cancel-config" type="button" :disabled="busy" @click="emit('cancelReconfigure')">{{ t('contractReview.cancel') }}</button><button data-testid="contract-start-review" class="primary-action" type="button" :disabled="busy || review.status === 'uploading' || (!reconfigure && review.status !== 'ready')" @click="emit('start')"><t-icon name="play-circle" /> {{ review.status === 'uploading' ? t('contractReview.preparingDocument') : t('contractReview.startReview') }}</button></div>
    </div>

    <template v-else>
      <div v-if="isRunning" class="review-progress"><div class="review-progress__row"><span>{{ statusLabel }}</span><strong>{{ review.progress }}%</strong></div><div class="review-progress__track"><i :style="{ width: `${review.progress}%` }" /></div><p>{{ t('contractReview.progressiveResults') }}</p></div>
      <div v-if="review.status === 'failed'" class="review-error"><strong>{{ t('contractReview.reviewFailed') }}</strong><p>{{ review.error_message }}</p><button v-if="review.error_message?.toLowerCase().includes('model')" type="button" @click="emit('configure')">{{ t('contractReview.configureModel') }}</button><button data-testid="contract-retry-review" type="button" @click="emit('retry')">{{ t('contractReview.retry') }}</button></div>
      <nav class="review-tabs">
        <button v-for="tab in tabs" :key="tab" :data-testid="`contract-result-tab-${tab}`" type="button" :class="{ active: activeTab === tab }" @click="activeTab = tab">{{ t(`contractReview.tabs.${tab}`) }}<span v-if="tab === 'issues'">{{ issues.length }}</span></button>
      </nav>
      <div class="review-panel__content">
        <section v-if="activeTab === 'overview'" class="overview-pane">
          <div class="overall-risk"><span>{{ t('contractReview.overallRisk') }}</span><RiskBadge :risk="review.overview?.overall_risk || derivedRisk" /></div>
          <p class="summary">{{ review.overview?.executive_summary || t('contractReview.overviewPending') }}</p>
          <div class="risk-counts"><div v-for="risk in riskOrder" :key="risk"><strong>{{ counts[risk] }}</strong><span>{{ t(`contractReview.risk.${risk}`) }}</span></div></div>
          <dl v-if="review.overview?.contract_type"><dt>{{ t('contractReview.contractType') }}</dt><dd>{{ review.overview.contract_type }}</dd></dl>
          <div v-if="review.overview?.key_recommendations?.length" class="key-recommendations"><h3>{{ t('contractReview.keyRecommendations') }}</h3><ol><li v-for="item in review.overview.key_recommendations" :key="item">{{ item }}</li></ol></div>
        </section>
        <section v-else-if="activeTab === 'issues'" class="issues-pane">
          <div v-if="!issues.length" class="empty-results">{{ isRunning ? t('contractReview.findingsAppearHere') : t('contractReview.noIssues') }}</div>
          <article v-for="issue in sortedIssues" :id="`review-issue-${issue.id}`" :data-testid="`contract-issue-${issue.id}`" :key="issue.id" class="issue" :class="{ selected: selectedIssueId === issue.id }" @click="selectIssue(issue)">
            <div class="issue__top"><RiskBadge :risk="issue.risk_level" /><span>{{ clauseTitle(issue.clause_id) }}</span></div><h3>{{ issue.title }}</h3><p>{{ issue.explanation }}</p>
            <details><summary>{{ t('contractReview.originalLanguage') }}</summary><blockquote>{{ issue.original_quote }}</blockquote></details>
            <div class="issue__suggestion"><span>{{ t('contractReview.suggestedRevision') }}</span><p>{{ issue.suggestion }}</p></div>
            <button type="button" class="view-clause" @click.stop="selectIssue(issue)">{{ t('contractReview.viewClause') }} <t-icon name="locate" /></button>
          </article>
        </section>
        <section v-else-if="activeTab === 'clauses'" class="clauses-pane"><button v-for="clause in review.clauses || []" :key="clause.id" type="button" @click="selectClause(clause.id)"><div><strong>{{ clause.title }}</strong><span>{{ clause.review_status === 'completed' ? t('contractReview.reviewed') : t('contractReview.pending') }}</span></div><em>{{ t('contractReview.issueCount', { count: clause.issue_count }) }}</em></button></section>
        <section v-else class="suggestions-pane"><div v-if="!issues.length" class="empty-results">{{ t('contractReview.noSuggestions') }}</div><article v-for="issue in sortedIssues" :key="issue.id" @click="selectIssue(issue)"><RiskBadge :risk="issue.risk_level" /><div><strong>{{ issue.title }}</strong><p>{{ issue.suggestion }}</p></div></article></section>
      </div>
    </template>
  </aside>
</template>

<script setup lang="ts">
import { computed, defineComponent, h, ref, watch, type PropType } from 'vue'
import { useI18n } from 'vue-i18n'
import { Icon as TIcon } from 'tdesign-vue-next'
import type { ContractReview, RepresentedParty, ReviewIssue, ReviewPlaybook, RiskLevel } from '@/api/contract-review'

const props = defineProps<{ review: ContractReview; playbooks: ReviewPlaybook[]; selectedIssueId?: string; busy?: boolean; reconfigure?: boolean }>()
const emit = defineEmits<{ start: []; retry: []; reconfigure: []; cancelReconfigure: []; configure: []; issueSelect: [issue: ReviewIssue]; configChange: [data: { playbook_id?: string; represented_party?: RepresentedParty }] }>()
const { t } = useI18n()
const tabs = ['overview', 'issues', 'clauses', 'suggestions'] as const
const activeTab = ref<(typeof tabs)[number]>('overview')
const riskOrder: RiskLevel[] = ['high', 'medium', 'low']
const issues = computed(() => props.review.issues || [])
const reconfigure = computed(() => props.reconfigure === true)
const setupMode = computed(() => ['draft', 'uploading', 'ready'].includes(props.review.status) || reconfigure.value)
const isRunning = computed(() => ['analyzing', 'reviewing_clauses'].includes(props.review.status))
const counts = computed(() => ({ high: issues.value.filter(i => i.risk_level === 'high').length, medium: issues.value.filter(i => i.risk_level === 'medium').length, low: issues.value.filter(i => i.risk_level === 'low').length }))
const derivedRisk = computed<RiskLevel>(() => counts.value.high ? 'high' : counts.value.medium ? 'medium' : 'low')
const sortedIssues = computed(() => [...issues.value].sort((a,b) => riskOrder.indexOf(a.risk_level)-riskOrder.indexOf(b.risk_level) || a.sequence-b.sequence))
const statusLabel = computed(() => t(`contractReview.status.${props.review.status}`))
const clauseTitle = (id: string) => props.review.clauses?.find(c => c.id === id)?.title || t('contractReview.generalClause')
function selectIssue(issue: ReviewIssue) { activeTab.value = 'issues'; emit('issueSelect', issue) }
function selectClause(id: string) { const issue = sortedIssues.value.find(item => item.clause_id === id); if (issue) selectIssue(issue) }
watch(() => props.selectedIssueId, (id) => { if (id) { activeTab.value = 'issues'; requestAnimationFrame(() => document.getElementById(`review-issue-${id}`)?.scrollIntoView({ behavior:'smooth', block:'nearest' })) } })

const RiskBadge = defineComponent({ props: { risk: { type: String as PropType<RiskLevel>, required: true } }, setup(p) { return () => h('span', { class:`risk-badge risk-badge--${p.risk}` }, t(`contractReview.risk.${p.risk}`)) } })
</script>

<style scoped lang="less">
.review-panel{width:400px;min-width:400px;height:100%;display:flex;flex-direction:column;background:var(--legal-bg-surface);border-left:1px solid var(--legal-border);color:var(--legal-text-primary);}
.review-panel__header{min-height:72px;padding:15px 20px;display:flex;align-items:center;justify-content:space-between;border-bottom:1px solid var(--legal-border);box-sizing:border-box;.eyebrow{font-size:10px;letter-spacing:.11em;text-transform:uppercase;color:var(--legal-text-secondary);}h2{margin:3px 0 0;font-size:16px;font-weight:650;}}
.review-panel__actions{display:flex;align-items:center;gap:8px}.rerun-button{height:28px;display:inline-flex;align-items:center;gap:4px;padding:0 8px;border:1px solid var(--legal-border);border-radius:4px;background:var(--legal-bg-surface);color:var(--legal-text-secondary);font-size:10px;font-weight:650;cursor:pointer;&:hover{border-color:var(--legal-ai);color:var(--legal-ai-strong)}&:disabled{opacity:.45;cursor:not-allowed}}
.status-dot{padding:4px 8px;border-radius:4px;background:var(--legal-bg-hover);font-size:11px;color:var(--legal-text-secondary);&:before{content:'';display:inline-block;width:6px;height:6px;border-radius:50%;margin-right:5px;background:var(--legal-text-disabled);}&--analyzing:before,&--reviewing_clauses:before{background:var(--legal-warning);animation:pulse 1.4s infinite;}&--completed:before{background:var(--legal-ai-strong);}&--failed:before{background:var(--legal-risk);}}
.review-setup{padding:24px 20px;display:flex;flex-direction:column;gap:20px;.review-setup__intro{display:flex;gap:10px;padding-bottom:18px;border-bottom:1px solid var(--legal-border);color:var(--legal-ai-strong);strong{font-size:14px;color:var(--legal-text-primary);}p{margin:5px 0 0;font-size:12px;line-height:1.5;color:var(--legal-text-secondary);}}label{display:flex;flex-direction:column;gap:7px;font-size:11px;font-weight:650;color:var(--legal-text-secondary);text-transform:uppercase;letter-spacing:.05em;}select{height:38px;padding:0 10px;border:1px solid var(--legal-border);border-radius:5px;background:var(--legal-bg-surface);color:var(--legal-text-primary);font:inherit;text-transform:none;letter-spacing:0;&:focus{outline:2px solid var(--legal-focus-ring);border-color:var(--legal-ai);}}}.review-setup__actions{display:flex;gap:8px;align-items:center}.review-setup__actions .primary-action{flex:1;margin:0}.cancel-config{height:40px;padding:0 13px;border:1px solid var(--legal-border);border-radius:5px;background:var(--legal-bg-surface);color:var(--legal-text-secondary);font-weight:600;cursor:pointer;&:disabled{opacity:.5;cursor:not-allowed}}
.primary-action{height:40px;border:0;border-radius:5px;background:var(--legal-brand);color:#fff;font-weight:650;cursor:pointer;&:hover:not(:disabled){background:var(--legal-brand-hover);}&:disabled{background:var(--legal-text-disabled);cursor:not-allowed;}}
.review-progress{padding:13px 20px;border-bottom:1px solid var(--legal-border);background:var(--legal-ai-soft);font-size:12px;.review-progress__row{display:flex;justify-content:space-between}.review-progress__track{height:3px;margin-top:9px;background:#dededb;i{display:block;height:100%;background:var(--legal-ai);transition:width .4s;}}p{margin:8px 0 0;color:var(--legal-text-secondary);font-size:11px;}}
.review-error{margin:14px 20px;padding:13px;border:1px solid #d8aaa5;background:var(--legal-risk-soft);border-radius:5px;color:var(--legal-risk-strong);p{font-size:12px;}button{border:1px solid var(--legal-risk);background:var(--legal-bg-surface);color:var(--legal-risk-strong);border-radius:4px;padding:6px 10px;cursor:pointer;}}
.review-tabs{height:43px;display:flex;padding:0 13px;border-bottom:1px solid var(--legal-border);button{position:relative;padding:0 8px;border:0;background:transparent;color:var(--legal-text-secondary);font-size:12px;cursor:pointer;&.active{color:var(--legal-brand);font-weight:650;&:after{content:'';position:absolute;left:8px;right:8px;bottom:-1px;height:2px;background:var(--legal-brand);}}span{margin-left:4px;color:var(--legal-text-secondary);}}}
.review-panel__content{min-height:0;flex:1;overflow:auto;padding:18px 20px;}
.overall-risk{display:flex;justify-content:space-between;align-items:center;padding-bottom:15px;border-bottom:1px solid var(--legal-border);font-size:12px;color:var(--legal-text-secondary)}.summary{font-size:13px;line-height:1.65;color:var(--legal-text-primary)}.risk-counts{display:grid;grid-template-columns:repeat(3,1fr);margin:18px 0;border:1px solid var(--legal-border);border-radius:5px;div{padding:12px;border-right:1px solid var(--legal-border);&:last-child{border:0}strong,span{display:block}strong{font-size:18px}span{margin-top:2px;font-size:10px;color:var(--legal-text-secondary)}}}.overview-pane dl{dt{font-size:10px;color:var(--legal-text-secondary);text-transform:uppercase}dd{margin:5px 0;font-size:13px}}.key-recommendations h3{font-size:12px}.key-recommendations ol{padding-left:18px;font-size:12px;line-height:1.6}
.issue{padding:15px 0;border-bottom:1px solid var(--legal-border);cursor:pointer;&.selected{margin:0 -10px;padding:15px 10px;background:var(--legal-ai-soft);border-radius:5px}.issue__top{display:flex;align-items:center;gap:8px;color:var(--legal-text-secondary);font-size:10px}h3{margin:9px 0 6px;font-size:13px}p{margin:0;font-size:12px;line-height:1.55;color:var(--legal-text-secondary)}details{margin-top:10px;summary{font-size:11px;color:var(--legal-text-secondary);cursor:pointer}blockquote{margin:7px 0 0;padding-left:10px;border-left:2px solid var(--legal-warning);font-size:11px;line-height:1.5;color:var(--legal-text-secondary)}}.issue__suggestion{margin-top:11px;padding:10px;background:var(--legal-bg-hover);border-radius:4px;span{font-size:10px;font-weight:650;text-transform:uppercase;color:var(--legal-text-secondary)}p{margin-top:5px}}.view-clause{margin-top:10px;padding:0;border:0;background:transparent;color:var(--legal-ai-strong);font-size:11px;font-weight:650;cursor:pointer}}
:deep(.risk-badge){display:inline-flex;padding:3px 6px;border-radius:3px;font-size:9px;font-weight:700;text-transform:uppercase;letter-spacing:.04em;&.risk-badge--high{color:var(--legal-risk-strong);background:var(--legal-risk-soft)}&.risk-badge--medium{color:var(--legal-warning-strong);background:var(--legal-warning-soft)}&.risk-badge--low{color:var(--legal-ai-strong);background:var(--legal-ai-soft)}}
.clauses-pane button{width:100%;padding:12px 0;display:flex;align-items:center;justify-content:space-between;border:0;border-bottom:1px solid var(--legal-border);background:transparent;color:var(--legal-text-primary);text-align:left;cursor:pointer;strong,span{display:block}strong{font-size:12px}span,em{margin-top:4px;color:var(--legal-text-secondary);font-size:10px;font-style:normal}}
.suggestions-pane article{display:flex;gap:10px;padding:13px 0;border-bottom:1px solid var(--legal-border);cursor:pointer;strong{font-size:12px}p{margin:5px 0 0;font-size:12px;line-height:1.5;color:var(--legal-text-secondary)}}
.empty-results{padding:36px 10px;text-align:center;color:var(--legal-text-secondary);font-size:12px}@keyframes pulse{50%{opacity:.35}}
</style>
