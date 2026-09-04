<template>
  <section v-if="review" class="review-workspace contract-review-theme">
    <header class="review-workspace__topbar">
      <button data-testid="contract-back" class="back-button" type="button" @click="router.push({ name: LEGAL_CONTRACT_REVIEW_ROUTE })"><t-icon name="chevron-left" /> {{ t('contractReview.allReviews') }}</button>
      <input v-model="title" maxlength="512" :aria-label="t('contractReview.reviewTitle')" @blur="saveTitle" @keydown.enter="($event.target as HTMLInputElement).blur()" />
      <span>{{ t('contractReview.saved') }}</span>
    </header>
    <div class="review-workspace__body">
      <div class="review-workspace__document">
        <div v-if="!review.file_name" class="upload-empty" :class="{ 'upload-empty--dragging': dragging }" @dragenter.prevent="dragging = true" @dragover.prevent @dragleave.prevent="dragging = false" @drop.prevent="onDrop">
          <div class="upload-empty__icon"><t-icon name="file-add" size="27px" /></div><h1>{{ t('contractReview.dropContract') }}</h1><p>{{ t('contractReview.uploadFormats') }}</p>
          <button data-testid="contract-choose-file" type="button" @click="fileInput?.click()">{{ t('contractReview.chooseFile') }}</button><small>{{ t('contractReview.singleDocument') }}</small>
          <input data-testid="contract-file-input" ref="fileInput" type="file" accept=".pdf,.docx,application/pdf,application/vnd.openxmlformats-officedocument.wordprocessingml.document" hidden @change="onFileInput" />
        </div>
        <ContractDocumentViewer v-else ref="viewer" :review-id="review.id" :file-name="review.file_name" :file-type="review.file_type" :issues="review.issues" :selected-issue-id="selectedIssue?.id" :source-revision="review.source_revision || review.source_hash" :locator="locator" :locator-status="locatorStatus" @marker-click="selectIssueById" @evidence-status="setEvidenceStatus" @locate-failed="handleLocateFailed" @retry-locator="retryLocator" />
        <div v-if="uploading" class="upload-overlay"><t-loading size="small" /><span>{{ t('contractReview.uploadingFile', { progress: store.uploadProgress }) }}</span><i><b :style="{ width: `${store.uploadProgress}%` }" /></i></div>
      </div>
        <ReviewPanel ref="reviewPanel" :review="review" :playbooks="store.playbooks" :models="store.models" :models-loading="store.modelsLoading" :selected-issue-id="selectedIssue?.id" :busy="busy" :reconfigure="reconfigure" :evidence-statuses="evidenceStatuses" :evidence-candidate-counts="evidenceCandidateCounts" :locator-status="locatorStatus" @config-change="saveConfig" @title-change="saveTitleValue" @start="startReview" @retry="retryReview" @reconfigure="beginReconfigure" @cancel-reconfigure="cancelReconfigure" @configure="router.push('/platform/agents')" @issue-select="locateIssue" @evidence-select="chooseEvidenceCandidate" />
    </div>
  </section>
  <div v-else class="review-loading contract-review-theme"><t-loading /> {{ t('contractReview.loadingReview') }}</div>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter } from 'vue-router'
import { Icon as TIcon, Loading as TLoading, MessagePlugin } from 'tdesign-vue-next'
import type { EvidenceStatus, RepresentedParty, ReviewIssue } from '@/api/contract-review'
import { LEGAL_CONTRACT_REVIEW_ROUTE } from '@/router/paths'
import { useContractReviewStore } from '@/stores/contractReview'
import ContractDocumentViewer from './ContractDocumentViewer.vue'
import ReviewPanel from './ReviewPanel.vue'

const { t } = useI18n(); const route = useRoute(); const router = useRouter(); const store = useContractReviewStore()
const review = computed(() => store.current)
const title = ref(''); const dragging = ref(false); const uploading = ref(false); const busy = ref(false); const reconfigure = ref(false); const fileInput = ref<HTMLInputElement | null>(null)
let titleSavePromise: Promise<unknown> = Promise.resolve()
let configSavePromise: Promise<unknown> = Promise.resolve()
type ReviewConfig = { playbook_id: string; represented_party: RepresentedParty; model_id: string }
const pendingConfig = ref<ReviewConfig | null>(null)
const originalConfig = ref<ReviewConfig | null>(null)
const viewer = ref<InstanceType<typeof ContractDocumentViewer> | null>(null); const selectedIssue = ref<ReviewIssue | null>(null)
const reviewPanel = ref<{ focusIssue: (id: string) => void } | null>(null)
const evidenceStatuses = ref<Record<string, EvidenceStatus>>({})
const evidenceCandidateCounts = ref<Record<string, number>>({})
const locator = computed(() => review.value ? store.locatorCache[review.value.id] || null : null)
const locatorStatus = computed(() => {
  if (!review.value) return 'idle'
  // Legacy rows may expose old metadata that looks like a locator, but their
  // issue offsets were not validated against a source revision. Keep them
  // readable while preventing an apparently precise highlight.
  if (review.value.quality_status === 'legacy') return 'unsupported'
  const cached = store.locatorCache[review.value.id]
  const reviewRevision = review.value.source_revision || review.value.source_hash
  const cachedRevision = cached?.source_revision || cached?.source_text_hash || cached?.source_hash
  // The review response already contains the validated locator for current
  // results. A background refresh must not hide that usable locator behind a
  // loading state; otherwise a slow endpoint makes every issue appear pending
  // even though the PDF can already be located safely.
  if (cached && (!reviewRevision || cachedRevision === reviewRevision)) return 'ready'
  return store.locatorStates[review.value.id]?.status || 'idle'
})

async function initialize() {
  try {
    await store.loadPlaybooks()
    try { await store.loadModels(true) } catch { MessagePlugin.warning(t('contractReview.modelsLoadFailed')) }
    const value = await store.load(String(route.params.reviewId)); title.value = value?.title || ''; evidenceStatuses.value = {}; evidenceCandidateCounts.value = {}; if (value) { void store.loadLocator(value.id); if (['uploading','analyzing','reviewing_clauses'].includes(value.status)) store.connect(value.id) }
  }
  catch (error: any) { MessagePlugin.error(error?.message || t('contractReview.loadFailed')); router.replace({ name: LEGAL_CONTRACT_REVIEW_ROUTE }) }
}
function saveTitleValue(value: string) {
  title.value = value
  const normalized = value.trim()
  if (!review.value || !normalized || normalized === review.value.title) return titleSavePromise
  const pending = store.update(review.value.id, { title: normalized })
  titleSavePromise = pending.catch((e: any) => { MessagePlugin.error(e?.message || t('contractReview.saveFailed')) })
  return titleSavePromise
}
function saveTitle() { void saveTitleValue(title.value) }
async function saveConfig(data: { playbook_id?: string; represented_party?: RepresentedParty; model_id?: string }) {
  if (!review.value) return
  if (reconfigure.value) {
    pendingConfig.value = {
      playbook_id: data.playbook_id ?? pendingConfig.value?.playbook_id ?? review.value.playbook_id,
      represented_party: data.represented_party ?? pendingConfig.value?.represented_party ?? review.value.represented_party,
      model_id: data.model_id ?? pendingConfig.value?.model_id ?? review.value.model_id ?? '',
    }
    if (data.playbook_id) store.current!.playbook_id = data.playbook_id
    if (data.represented_party) store.current!.represented_party = data.represented_party
    if (data.model_id !== undefined) store.current!.model_id = data.model_id
    return
  }
  const pending = store.update(review.value.id, data)
  configSavePromise = pending
  try { await pending } catch(e:any){ MessagePlugin.error(e?.message || t('contractReview.saveFailed')) }
}
function validFile(file: File) { const ext = file.name.toLowerCase().split('.').pop(); return ext === 'pdf' || ext === 'docx' }
async function upload(file?: File) { if (!review.value || !file) return; if (!validFile(file)) { MessagePlugin.warning(t('contractReview.invalidFile')); return } uploading.value = true; evidenceStatuses.value = {}; evidenceCandidateCounts.value = {}; try { await titleSavePromise; await store.upload(review.value.id, file); title.value = store.current?.title || title.value } catch(e:any){ MessagePlugin.error(e?.message || t('contractReview.uploadFailed')) } finally { uploading.value = false } }
function onFileInput(event: Event) { void upload((event.target as HTMLInputElement).files?.[0]); (event.target as HTMLInputElement).value = '' }
function onDrop(event: DragEvent) { dragging.value = false; void upload(event.dataTransfer?.files?.[0]) }
async function startReview(){
  if(!review.value)return
  busy.value=true
  try {
    await titleSavePromise
    await configSavePromise
    if (reconfigure.value) {
      if (pendingConfig.value) await store.update(review.value.id, pendingConfig.value)
      await store.retry(review.value.id)
      reconfigure.value = false; pendingConfig.value = null; originalConfig.value = null
    } else await store.start(review.value.id)
  } catch(e:any){ MessagePlugin.error(e?.message||t('contractReview.startFailed')) }
  finally{busy.value=false}
}
function beginReconfigure(){
  if (!review.value) return
  selectedIssue.value=null
  configSavePromise = Promise.resolve()
  originalConfig.value = { playbook_id: review.value.playbook_id, represented_party: review.value.represented_party, model_id: review.value.model_id || '' }
  pendingConfig.value = { ...originalConfig.value }
  reconfigure.value=true
}
function cancelReconfigure(){
  if (store.current && originalConfig.value) {
    store.current.playbook_id = originalConfig.value.playbook_id
    store.current.represented_party = originalConfig.value.represented_party
    store.current.model_id = originalConfig.value.model_id
  }
  pendingConfig.value = null; originalConfig.value = null; reconfigure.value=false
}
async function retryReview(){
  if(!review.value)return
  selectedIssue.value=null; evidenceStatuses.value = {}; evidenceCandidateCounts.value = {}; busy.value=true
  try{await store.retry(review.value.id)}catch(e:any){MessagePlugin.error(e?.message||t('contractReview.retryFailed'))}finally{busy.value=false}
}
function locateIssue(issue:ReviewIssue){ selectedIssue.value=issue; viewer.value?.locateIssue(issue) }
function selectIssueById(id:string){ const issue=review.value?.issues?.find(item=>item.id===id); if(!issue)return; selectedIssue.value=issue; reviewPanel.value?.focusIssue(id) }
function setEvidenceStatus(payload: { issueId: string; status: EvidenceStatus; candidateCount?: number }) {
  evidenceStatuses.value = { ...evidenceStatuses.value, [payload.issueId]: payload.status }
  const next = { ...evidenceCandidateCounts.value }
  if (payload.candidateCount && payload.candidateCount > 1) next[payload.issueId] = payload.candidateCount
  else delete next[payload.issueId]
  evidenceCandidateCounts.value = next
}
function chooseEvidenceCandidate(payload: { issueId: string; index: number }) {
  const issue = review.value?.issues?.find((item) => item.id === payload.issueId)
  if (!issue) return
  selectedIssue.value = issue
  viewer.value?.chooseEvidenceCandidate(payload.issueId, payload.index)
}
function handleLocateFailed(status: EvidenceStatus) {
  MessagePlugin.warning(`${t('contractReview.locateFailed')}: ${t(`contractReview.evidence.${status}`)}`)
}
async function retryLocator() { if (review.value) await store.loadLocator(review.value.id, true) }
watch(() => review.value?.title, value => { if(value && document.activeElement?.tagName !== 'INPUT') title.value=value })
watch(() => review.value?.source_revision || review.value?.source_hash, (revision, previous) => {
  if (!review.value || revision === previous) return
  evidenceStatuses.value = {}
  evidenceCandidateCounts.value = {}
  const cached = store.locatorCache[review.value.id]
  const cachedRevision = cached?.source_revision || cached?.source_text_hash || cached?.source_hash
  // `load()` seeds the cache from the review payload. Only fetch again when
  // the cache is absent or belongs to another source revision.
  if (!cached || (revision && cachedRevision !== revision)) void store.loadLocator(review.value.id, true)
})
onMounted(initialize); onBeforeUnmount(store.disconnect)
</script>

<style scoped lang="less">
.review-workspace {
  width: 100%;
  height: 100%;
  min-height: 0;
  display: flex;
  flex-direction: column;
  color: var(--legal-text-primary);
  background: var(--legal-bg-surface);
}
.review-workspace__topbar {
  height: 48px;
  min-height: 48px;
  padding: 0 16px;
  display: flex;
  align-items: center;
  gap: 14px;
  box-sizing: border-box;
  border-bottom: 1px solid var(--legal-border);
  background: var(--legal-bg-surface);
  .back-button { display: flex; align-items: center; gap: 4px; padding: 0; border: 0; color: var(--legal-text-secondary); background: transparent; font-size: 12px; cursor: pointer; }
  .back-button:hover { color: var(--legal-brand); }
  .back-button:after { content: ''; width: 1px; height: 18px; margin-left: 8px; background: var(--legal-border); }
  input {
    min-width: 180px;
    max-width: 520px;
    flex: 1;
    padding: 5px 7px;
    border: 1px solid transparent;
    border-radius: 4px;
    color: var(--legal-text-primary);
    background: transparent;
    font: 600 13px/1.2 inherit;
    &:hover { border-color: var(--legal-border-strong); background: var(--legal-bg-paper); }
    &:focus { outline: 2px solid var(--legal-focus-ring); border-color: var(--legal-ai); background: var(--legal-bg-paper); }
  }
  span { margin-left: auto; color: var(--legal-text-secondary); font-size: 10px; }
}
.review-workspace__body { min-height: 0; flex: 1; display: flex; }
.review-workspace__document { position: relative; min-width: 0; flex: 1; }
.upload-empty {
  width: 100%;
  height: 100%;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  box-sizing: border-box;
  border: 2px solid transparent;
  background: var(--legal-bg-page);
  transition: .15s;
  &--dragging { border-color: var(--legal-ai); background: var(--legal-ai-soft); }
  .upload-empty__icon { width: 58px; height: 58px; display: flex; align-items: center; justify-content: center; border: 1px solid var(--legal-border); border-radius: 8px; color: var(--legal-ai-strong); background: var(--legal-bg-surface); }
  h1 { margin: 20px 0 5px; font-size: 20px; font-weight: 600; }
  p { margin: 0; color: var(--legal-text-secondary); font-size: 13px; }
  button:not(.back-button) { margin-top: 22px; padding: 9px 16px; border: 1px solid var(--legal-brand); border-radius: 5px; color: #fff; background: var(--legal-brand); font-weight: 600; cursor: pointer; &:hover { background: var(--legal-brand-hover); } }
  small { margin-top: 13px; color: var(--legal-text-secondary); font-size: 10px; }
}
.upload-overlay {
  position: absolute;
  inset: 0;
  z-index: 4;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 10px;
  color: var(--legal-text-secondary);
  background: rgba(247, 244, 237, .9);
  font-size: 12px;
  i { width: 220px; height: 3px; background: var(--legal-border); b { display: block; height: 100%; background: var(--legal-ai); } }
}
.review-loading { width: 100%; height: 100%; display: flex; align-items: center; justify-content: center; gap: 10px; color: var(--legal-text-secondary); background: var(--legal-bg-page); font-size: 13px; }
@media(max-width:1050px){.review-workspace__body{position:relative}:deep(.review-panel){width:360px;min-width:360px}}
</style>
