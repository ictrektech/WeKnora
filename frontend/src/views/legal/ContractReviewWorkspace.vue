<template>
    <section class="review-list contract-review-theme">
    <header class="review-list__header"><div><span>{{ t('contractReview.workspaceEyebrow') }}</span><h1>{{ t('contractReview.title') }}</h1><p>{{ t('contractReview.subtitle') }}</p></div><button data-testid="contract-new-review" type="button" :disabled="creating" @click="newReview"><t-icon name="add" /> {{ t('contractReview.newReview') }}</button></header>
    <nav class="review-list__tabs"><button data-testid="contract-active-tab" type="button" :class="{ active: !archived }" @click="setArchived(false)">{{ t('contractReview.active') }}</button><button data-testid="contract-archived-tab" type="button" :class="{ active: archived }" @click="setArchived(true)">{{ t('contractReview.archived') }}</button></nav>
    <div v-if="selectedCount" class="review-list__bulk-bar">
      <strong>{{ t('contractReview.selectedCount', { count: selectedCount }) }}</strong>
      <div>
        <button data-testid="contract-bulk-archive" type="button" class="bulk-action" :disabled="bulkWorking || (!archived && !archivableSelectedCount)" @click="runBulk(archived ? 'restore' : 'archive')"><t-icon :name="archived ? 'rollback' : 'archive'" /><span>{{ archived ? t('contractReview.bulkRestore') : t('contractReview.bulkArchive') }}</span></button>
        <button data-testid="contract-bulk-delete" type="button" class="bulk-action danger" :disabled="bulkWorking" @click="runBulk('delete')"><t-icon name="delete" /><span>{{ t('contractReview.bulkDelete') }}</span></button>
      </div>
    </div>
    <div class="review-list__table">
      <div class="review-list__table-head"><span class="review-check"><label class="legal-checkbox-hit-area"><input data-testid="contract-select-all" type="checkbox" :checked="allSelected" :indeterminate.prop="someSelected" :aria-label="t('contractReview.selectAll')" @change="toggleAll" /></label></span><span>{{ t('contractReview.document') }}</span><span>{{ t('contractReview.statusLabel') }}</span><span>{{ t('contractReview.party') }}</span><span>{{ t('contractReview.updated') }}</span><span /></div>
      <div v-if="store.loading" class="review-list__empty"><t-loading size="small" /> {{ t('contractReview.loadingReviews') }}</div>
      <div v-else-if="!store.tasks.length" class="review-list__empty"><t-icon name="file-paste" size="28px" /><strong>{{ archived ? t('contractReview.noArchivedReviews') : t('contractReview.noReviews') }}</strong><p>{{ archived ? t('contractReview.noArchivedDescription') : t('contractReview.noReviewsDescription') }}</p></div>
      <article v-for="task in store.tasks" :key="task.id" :data-testid="`contract-row-${task.id}`" :class="{ selected: selectedIds.has(task.id) }" @click="open(task.id)"><span class="review-check" @click.stop><label class="legal-checkbox-hit-area"><input type="checkbox" :checked="selectedIds.has(task.id)" :aria-label="task.title" @change="toggleOne(task.id)" /></label></span><div class="review-name"><i><t-icon name="file-paste" /></i><div><strong>{{ task.title }}</strong><span>{{ task.file_name || t('contractReview.draftNoDocument') }}</span></div></div><span><b class="task-status" :class="`task-status--${task.status}`">{{ t(`contractReview.status.${task.status}`) }}</b></span><span class="muted">{{ t(`contractReview.parties.${task.represented_party}`) }}</span><span class="muted">{{ formatDate(task.updated_at) }}</span><div class="row-actions" @click.stop><button type="button" :title="t('contractReview.rename')" @click="rename(task)"><t-icon name="edit-1" /></button><button type="button" :disabled="isRunning(task.status)" :title="archived ? t('contractReview.unarchive') : t('contractReview.archive')" @click="toggleArchive(task)"><t-icon :name="archived ? 'rollback' : 'archive'" /></button><button type="button" :title="t('contractReview.delete')" @click="remove(task)"><t-icon name="delete" /></button></div></article>
    </div>
  </section>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'; import { useI18n } from 'vue-i18n'; import { useRouter } from 'vue-router'; import { Icon as TIcon, Loading as TLoading, MessagePlugin } from 'tdesign-vue-next'
import type { ContractReview, ContractReviewBulkAction, ReviewStatus } from '@/api/contract-review'; import { LEGAL_CONTRACT_REVIEW_DETAIL_ROUTE } from '@/router/paths'; import { useContractReviewStore } from '@/stores/contractReview'
const { t, locale }=useI18n(); const router=useRouter(); const store=useContractReviewStore(); const archived=ref(false); const creating=ref(false); const bulkWorking=ref(false); const selectedIds=ref(new Set<string>())
const load=()=>store.loadList(archived.value).catch((e:any)=>MessagePlugin.error(e?.message||t('contractReview.loadFailed')))
const hasRunningTasks=computed(()=>store.tasks.some(task=>isRunning(task.status)))
const selectedTasks=computed(()=>store.tasks.filter(task=>selectedIds.value.has(task.id)))
const selectedCount=computed(()=>selectedTasks.value.length)
const archivableSelectedCount=computed(()=>selectedTasks.value.filter(task=>!isRunning(task.status)).length)
const allSelected=computed(()=>store.tasks.length>0&&store.tasks.every(task=>selectedIds.value.has(task.id)))
const someSelected=computed(()=>selectedCount.value>0&&!allSelected.value)
let statusPollTimer:ReturnType<typeof setInterval>|null=null
function stopStatusPolling(){if(statusPollTimer!==null){clearInterval(statusPollTimer);statusPollTimer=null}}
function syncStatusPolling(active:boolean){if(active&&statusPollTimer===null)statusPollTimer=setInterval(()=>{void store.loadList(archived.value,true).catch(()=>undefined)},2000);else if(!active)stopStatusPolling()}
async function newReview(){creating.value=true;try{const task=await store.create();await router.push({name:LEGAL_CONTRACT_REVIEW_DETAIL_ROUTE,params:{reviewId:task.id}})}catch(e:any){MessagePlugin.error(e?.message||t('contractReview.createFailed'))}finally{creating.value=false}}
function open(id:string){router.push({name:LEGAL_CONTRACT_REVIEW_DETAIL_ROUTE,params:{reviewId:id}})} function setArchived(value:boolean){archived.value=value;selectedIds.value=new Set();void load()}
const isRunning=(status:ReviewStatus)=>['uploading','analyzing','reviewing_clauses'].includes(status)
function toggleAll(event:Event){const checked=(event.target as HTMLInputElement).checked;selectedIds.value=checked?new Set(store.tasks.map(task=>task.id)):new Set()}
function toggleOne(id:string){const next=new Set(selectedIds.value);next.has(id)?next.delete(id):next.add(id);selectedIds.value=next}
async function runBulk(action:ContractReviewBulkAction){
  const rows=selectedTasks.value; if(!rows.length)return
  const running=rows.filter(task=>isRunning(task.status)).length
  const ids=action==='archive'?rows.filter(task=>!isRunning(task.status)).map(task=>task.id):rows.map(task=>task.id)
  if(!ids.length)return
  const confirmKey=action==='delete'&&running?'contractReview.bulkDeleteRunningConfirm':action==='delete'?'contractReview.bulkDeleteConfirm':action==='restore'?'contractReview.bulkRestoreConfirm':running?'contractReview.bulkArchiveRunningConfirm':'contractReview.bulkArchiveConfirm'
  if(!window.confirm(t(confirmKey,{count:ids.length,running})))return
  bulkWorking.value=true
  try{
    const result=await store.bulk(action,ids)
    const succeeded=new Set(result.items.filter(item=>item.success).map(item=>item.id));selectedIds.value=new Set([...selectedIds.value].filter(id=>!succeeded.has(id)))
    await store.loadList(archived.value,true)
    if(result.failed)MessagePlugin.warning(t('contractReview.bulkActionPartial',{succeeded:result.succeeded,failed:result.failed}));else MessagePlugin.success(t('contractReview.bulkActionSuccess',{count:result.succeeded}))
  }catch(e:any){MessagePlugin.error(e?.message||t('contractReview.bulkActionFailed'))}finally{bulkWorking.value=false}
}
async function rename(task:ContractReview){const value=window.prompt(t('contractReview.renamePrompt'),task.title);if(!value?.trim())return;try{await store.update(task.id,{title:value.trim()});await load()}catch(e:any){MessagePlugin.error(e?.message||t('contractReview.saveFailed'))}}
async function toggleArchive(task:ContractReview){try{await store.update(task.id,{archived:!archived.value});await load()}catch(e:any){MessagePlugin.error(e?.message||t('contractReview.archiveFailed'))}}
async function remove(task:ContractReview){if(!window.confirm(t('contractReview.deleteConfirm',{name:task.title})))return;try{await store.remove(task.id)}catch(e:any){MessagePlugin.error(e?.message||t('contractReview.deleteFailed'))}}
const formatDate=(value:string)=>new Intl.DateTimeFormat(locale.value,{month:'short',day:'numeric',hour:'2-digit',minute:'2-digit'}).format(new Date(value)); watch(hasRunningTasks,syncStatusPolling,{immediate:true}); onMounted(async()=>{await load();syncStatusPolling(hasRunningTasks.value)});onBeforeUnmount(stopStatusPolling)
</script>

<style scoped lang="less">
.review-list {
  width: 100%;
  height: 100%;
  overflow: auto;
  padding: 34px 42px 54px;
  box-sizing: border-box;
  color: var(--legal-text-primary);
  background: var(--legal-bg-page);
}

.review-list__header {
  max-width: 1320px;
  margin: auto;
  display: flex;
  align-items: flex-end;
  justify-content: space-between;

  span { color: var(--legal-text-secondary); font-size: 10px; letter-spacing: .12em; text-transform: uppercase; }
  h1 { margin: 7px 0 5px; font-size: 28px; letter-spacing: -.03em; }
  p { margin: 0; color: var(--legal-text-secondary); font-size: 13px; }
  button {
    height: 38px;
    padding: 0 14px;
    border: 1px solid var(--legal-brand);
    border-radius: 5px;
    color: #fff;
    background: var(--legal-brand);
    font-weight: 650;
    cursor: pointer;
    &:hover { background: var(--legal-brand-hover); }
    &:focus-visible { outline: 2px solid var(--legal-ai); outline-offset: 2px; }
  }
}

.review-list__tabs {
  max-width: 1320px;
  margin: 34px auto 0;
  border-bottom: 1px solid var(--legal-border);
  button {
    margin-right: 22px;
    padding: 0 0 10px;
    border: 0;
    background: transparent;
    color: var(--legal-text-secondary);
    font-size: 12px;
    cursor: pointer;
    &.active { color: var(--legal-brand); font-weight: 650; border-bottom: 2px solid var(--legal-brand); }
  }
}

.review-list__bulk-bar {
  max-width: 1320px;
  min-height: 42px;
  margin: 12px auto -4px;
  padding: 0 12px;
  box-sizing: border-box;
  display: flex;
  align-items: center;
  justify-content: space-between;
  border: 1px solid var(--legal-border);
  border-radius: 5px;
  background: var(--legal-bg-surface);
  strong { font-size: 11px; }
  div { display: flex; gap: 7px; }
  button {
    height: 29px;
    display: inline-flex;
    align-items: center;
    justify-content: center;
    border: 1px solid var(--legal-border);
    border-radius: 4px;
    color: var(--legal-text-primary);
    background: var(--legal-bg-surface);
    font-size: 11px;
    cursor: pointer;
    &:hover { background: var(--legal-bg-hover); }
    &.danger { color: var(--legal-risk-strong); }
    &:disabled { opacity: .45; cursor: not-allowed; }
  }
  .bulk-action {
    position: relative;
    min-width: 96px;
    padding: 0 28px;
    line-height: 1;
    text-align: center;
    > span { display: block; width: 100%; line-height: 1; text-align: center; white-space: nowrap; }
    :deep(.t-icon) { position: absolute; left: 10px; top: 50%; margin: 0; transform: translateY(-50%); }
  }
}

.review-list__table {
  max-width: 1320px;
  margin: 16px auto;
  overflow: hidden;
  border: 1px solid var(--legal-border);
  border-radius: 6px;
  background: var(--legal-bg-surface);
}

.review-list__table-head,
article { display: grid; grid-template-columns: 34px minmax(300px, 2fr) 150px 130px 160px 110px; align-items: center; }
.review-list__table-head {
  height: 37px;
  padding: 0 14px;
  border-bottom: 1px solid var(--legal-border);
  color: var(--legal-text-secondary);
  background: var(--legal-bg-hover);
  font-size: 10px;
  text-transform: uppercase;
  letter-spacing: .04em;
}
article {
  min-height: 66px;
  padding: 0 14px;
  border-bottom: 1px solid var(--legal-border);
  cursor: pointer;
  &:last-child { border: 0; }
  &:hover { background: var(--legal-bg-hover); }
  &.selected { background: var(--legal-ai-soft); }
}

.review-check {
  display: flex;
  align-items: center;
  input { width: 14px; height: 14px; margin: 0; accent-color: var(--legal-brand); cursor: pointer; }
}

.review-name {
  min-width: 0;
  display: flex;
  align-items: center;
  gap: 11px;
  i { width: 34px; height: 40px; display: flex; align-items: center; justify-content: center; border: 1px solid var(--legal-border); border-radius: 4px; color: var(--legal-ai-strong); background: var(--legal-ai-soft); }
  strong, span { display: block; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  strong { font-size: 13px; }
  span { max-width: 400px; margin-top: 4px; color: var(--legal-text-secondary); font-size: 10px; }
}
.muted { color: var(--legal-text-secondary); font-size: 11px; }
.task-status {
  display: inline-flex;
  padding: 4px 7px;
  border-radius: 3px;
  color: var(--legal-text-secondary);
  background: var(--legal-bg-hover);
  font-size: 10px;
  font-weight: 600;
  &--analyzing, &--reviewing_clauses { color: var(--legal-warning-strong); background: var(--legal-warning-soft); }
  &--completed { color: var(--legal-ai-strong); background: var(--legal-ai-soft); }
  &--failed { color: var(--legal-risk-strong); background: var(--legal-risk-soft); }
}
.row-actions {
  display: flex;
  justify-content: flex-end;
  gap: 3px;
  button {
    width: 28px;
    height: 28px;
    border: 0;
    border-radius: 4px;
    color: var(--legal-text-secondary);
    background: transparent;
    cursor: pointer;
    &:hover { color: var(--legal-text-primary); background: var(--legal-bg-hover); }
    &:disabled { opacity: .45; cursor: not-allowed; }
  }
}
.review-list__empty {
  min-height: 290px;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 8px;
  color: var(--legal-text-secondary);
  font-size: 12px;
  strong { color: var(--legal-text-primary); font-size: 13px; }
  p { margin: 0; }
}
</style>
