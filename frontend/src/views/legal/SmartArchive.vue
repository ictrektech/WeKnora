<template>
  <section class="archive-page contract-review-theme">
    <header class="archive-header">
      <div>
        <span class="eyebrow">{{ t('smartArchive.eyebrow') }}</span>
        <h1>{{ t('smartArchive.title') }}</h1>
        <p>{{ t('smartArchive.subtitle') }}</p>
      </div>
      <div class="header-actions">
        <button data-testid="archive-notifications" class="notification-button" type="button" @click="tab = 'reminders'; void loadReminders()"><t-icon name="notification" /><span v-if="unreadCount" class="notification-count">{{ unreadCount }}</span></button>
        <button v-if="canContribute" data-testid="archive-import" class="primary-button" type="button" :disabled="uploading" @click="fileInput?.click()"><t-icon name="upload" /> {{ t('smartArchive.importFiles') }}</button>
        <input data-testid="archive-file-input" ref="fileInput" type="file" hidden multiple accept=".pdf,.doc,.docx,.xls,.xlsx,.jpg,.jpeg,.png,.webp,application/pdf,application/msword,application/vnd.openxmlformats-officedocument.wordprocessingml.document,application/vnd.ms-excel,application/vnd.openxmlformats-officedocument.spreadsheetml.sheet,image/jpeg,image/jpg,image/png,image/webp" @change="onFileInput" />
      </div>
    </header>

    <div class="archive-toolbar">
      <nav class="archive-tabs">
        <button v-for="item in tabs" :key="item.id" :data-testid="`archive-tab-${item.id}`" type="button" :class="{ active: tab === item.id }" @click="selectTab(item.id)"><t-icon :name="item.icon" /> {{ item.label }}</button>
      </nav>
    </div>

    <div v-if="uploading" class="import-progress"><t-loading size="small" /> {{ t('smartArchive.importing') }} {{ store.importProgress }}%<i><b :style="{ width: `${store.importProgress}%` }" /></i></div>

    <main class="archive-content">
      <template v-if="tab === 'documents'">
        <section class="document-panel">
          <div class="document-panel__heading">
            <div><h2>{{ t('smartArchive.documents') }}</h2><span>{{ t('smartArchive.documentCount', { count: store.searchTotal }) }}</span></div>
            <div v-if="selectedCount" class="bulk-toolbar"><span>{{ t('smartArchive.selectedCount', { count: selectedCount }) }}</span><button v-if="!showArchived && canContribute" data-testid="archive-bulk-archive" class="secondary-button bulk-document-action" type="button" :disabled="bulkWorking" @click="runBulkAction('archive')"><t-icon name="archive" /><span>{{ t('smartArchive.bulkArchive') }}</span></button><template v-else-if="showArchived && canAdmin"><button data-testid="archive-bulk-restore" class="secondary-button bulk-document-action" type="button" :disabled="bulkWorking" @click="runBulkAction('restore')"><t-icon name="rollback" /><span>{{ t('smartArchive.bulkRestore') }}</span></button><button data-testid="archive-bulk-purge" class="secondary-button secondary-button--danger bulk-document-action" type="button" :disabled="bulkWorking" @click="runBulkAction('purge')"><t-icon name="delete" /><span>{{ t('smartArchive.bulkDelete') }}</span></button></template></div>
          </div>
          <div class="document-filters">
            <div class="document-search"><t-icon name="search" /><input data-testid="archive-search" v-model="query" :placeholder="t('smartArchive.searchPlaceholder')" @keydown.enter="applyDocumentSearch" /><button type="button" :title="t('smartArchive.searchAction')" @click="applyDocumentSearch"><t-icon name="arrow-right" /></button></div>
            <div class="archive-date-filter"><t-icon name="calendar" /><input v-model="dateFrom" type="date" :aria-label="t('smartArchive.dateFrom')" @change="onDocumentFiltersChanged" /><span>–</span><input v-model="dateTo" type="date" :aria-label="t('smartArchive.dateTo')" @change="onDocumentFiltersChanged" /></div>
            <select data-testid="archive-type-filter" v-model="documentTypeFilter" :aria-label="t('smartArchive.typeFilter')" @change="onDocumentFiltersChanged"><option value="">{{ t('smartArchive.allTypes') }}</option><option v-for="type in documentTypes" :key="type" :value="type">{{ documentTypeLabel(type) }}</option></select>
            <details ref="statusFilterDetails" class="archive-status-filter"><summary><span class="archive-status-dots"><i v-for="status in extractionStatuses" :key="status" :class="`archive-status-dot--${archiveDocumentStatusTone(status)}`" /></span>{{ statusFilterLabel }}<t-icon name="chevron-down" /></summary><div><label v-for="status in extractionStatuses" :key="status"><span class="legal-checkbox-hit-area"><input type="checkbox" :checked="statusFilters.includes(status)" @change="toggleExtractionStatus(status)" /></span><i :class="`archive-status-dot--${archiveDocumentStatusTone(status)}`" />{{ extractionStatusLabel(status) }}</label></div></details>
            <select data-testid="archive-show-archived" v-model="archiveView" :aria-label="t('smartArchive.archiveView')" @change="onArchiveViewChanged"><option value="active">{{ t('smartArchive.currentDocuments') }}</option><option value="archived">{{ t('smartArchive.archivedDocuments') }}</option></select>
            <button v-if="hasDocumentFilters" type="button" class="clear-document-filters" @click="clearDocumentFilters">{{ t('smartArchive.clearFilters') }}</button>
          </div>
          <div v-if="documentLoading" class="archive-empty"><t-loading /> {{ t('smartArchive.loading') }}</div>
          <div v-else-if="documentError" class="archive-empty archive-empty--error"><t-icon name="error-circle" size="28px" /><strong>{{ documentError }}</strong><button class="secondary-button" type="button" @click="refreshDocumentSearch">{{ t('smartArchive.refresh') }}</button></div>
          <div v-else-if="!store.documents.length" class="archive-empty"><t-icon name="file-paste" size="32px" /><strong>{{ t('smartArchive.emptyDocuments') }}</strong><span>{{ hasDocumentFilters ? t('smartArchive.noMatchingDocuments') : t('smartArchive.emptyDocumentsDescription') }}</span><button v-if="canContribute && !hasDocumentFilters" data-testid="archive-empty-import" class="primary-button" type="button" @click="fileInput?.click()">{{ t('smartArchive.importFiles') }}</button><button v-else-if="hasDocumentFilters" class="secondary-button" type="button" @click="clearDocumentFilters">{{ t('smartArchive.clearFilters') }}</button></div>
          <div v-else class="archive-table-wrap"><table class="archive-table"><thead><tr><th class="selection-cell"><label class="legal-checkbox-hit-area" @click.stop><input data-testid="archive-select-all" type="checkbox" :checked="allDocumentsSelected" :indeterminate="someDocumentsSelected" :aria-label="t('smartArchive.selectAll')" @click.stop="toggleAllDocuments" /></label></th><th>{{ t('smartArchive.document') }}</th><th>{{ t('smartArchive.type') }}</th><th>{{ t('smartArchive.relatedParty') }}</th><th>{{ t('smartArchive.status') }}</th><th>{{ t('smartArchive.updated') }}</th><th /></tr></thead><tbody><tr v-for="document in store.documents" :key="document.id" :data-testid="`archive-row-${document.id}`" :class="{ selected: selected?.id === document.id, 'row-checked': selectedIds.includes(document.id) }" @click="openDocument(document)"><td class="selection-cell"><label class="legal-checkbox-hit-area" @click.stop><input type="checkbox" :checked="selectedIds.includes(document.id)" :aria-label="document.title" @click.stop @change="toggleDocument(document.id, $event)" /></label></td><td><div class="doc-cell"><span class="file-icon"><t-icon name="file-paste" /></span><span><b>{{ document.title }}</b><small>{{ document.file_name }} · {{ formatSize(document.file_size) }}</small></span></div></td><td>{{ documentTypeLabel(document.document_type) }}</td><td>{{ document.customer?.name || '—' }}</td><td><span class="archive-document-status" :class="`archive-document-status--${archiveDocumentStatusTone(document.extraction_status)}`"><i />{{ extractionStatusLabel(document.extraction_status) }}</span></td><td class="muted">{{ formatDate(document.updated_at) }}</td><td><template v-if="document.archived_at && canAdmin"><button class="archive-action-button" type="button" @click.stop="restoreArchivedDocument(document)">{{ t('smartArchive.restore') }}</button><button class="archive-action-button archive-action-button--danger" type="button" @click.stop="deleteArchivedDocument(document)">{{ t('smartArchive.delete') }}</button></template><button v-else-if="!document.archived_at && canContribute" class="icon-button" type="button" :title="t('smartArchive.archive')" @click.stop="archiveDocument(document)"><t-icon name="archive" /></button></td></tr></tbody></table><div v-if="hasMoreDocuments" class="archive-load-more"><button type="button" class="secondary-button" :disabled="loadingMore" @click="loadMoreDocuments">{{ loadingMore ? t('smartArchive.loading') : t('smartArchive.loadMore') }}</button><span>{{ t('smartArchive.loadedCount', { loaded: store.documents.length, total: store.searchTotal }) }}</span></div></div>
        </section>
      </template>

      <template v-else-if="tab === 'reminders'">
        <div class="content-heading"><div><h2>{{ t('smartArchive.reminders') }}</h2><span>{{ t('smartArchive.remindersDescription') }}</span></div><div class="content-heading-actions"><div v-if="candidateSelectedCount && canContribute" class="bulk-toolbar"><span>{{ t('smartArchive.selectedCount', { count: candidateSelectedCount }) }}</span><button data-testid="archive-bulk-ignore-candidates" class="secondary-button secondary-button--danger" type="button" :disabled="candidateBulkWorking" @click="runBulkIgnoreCandidates"><t-icon name="close" /> {{ t('smartArchive.bulkIgnoreCandidates') }}</button></div><div v-if="reminderSelectedCount && canContribute" class="bulk-toolbar"><span>{{ t('smartArchive.selectedCount', { count: reminderSelectedCount }) }}</span><button data-testid="archive-bulk-delete-reminders" class="secondary-button secondary-button--danger" type="button" :disabled="reminderBulkWorking" @click="runBulkDeleteReminders"><t-icon name="delete" /> {{ t('smartArchive.bulkDeleteReminders') }}</button></div><button data-testid="archive-refresh-reminders" class="secondary-button" type="button" @click="void loadReminders()"><t-icon name="refresh" /> {{ t('smartArchive.refresh') }}</button></div></div>
        <section class="candidate-section"><div class="candidate-heading"><div class="candidate-heading-main"><label class="legal-checkbox-hit-area" @click.stop><input type="checkbox" :checked="allCandidatesSelected" :indeterminate="someCandidatesSelected" :disabled="!store.reminderCandidates.length || candidateBulkWorking" :aria-label="t('smartArchive.selectAll')" @click.stop="toggleAllCandidates" /></label><div><h3>{{ t('smartArchive.candidatesTitle') }}</h3><span>{{ t('smartArchive.candidatesDescription') }}</span></div></div><div v-if="candidateSelectedCount" class="candidate-heading-selection">{{ t('smartArchive.selectedCount', { count: candidateSelectedCount }) }}</div></div><div v-if="!store.reminderCandidates.length" class="candidate-empty">{{ t('smartArchive.noCandidates') }}</div><article v-for="candidate in store.reminderCandidates" :key="candidate.id" class="candidate-row" :class="{ 'row-checked': candidateSelectedIds.includes(candidate.id) }"><div class="selection-cell"><label class="legal-checkbox-hit-area" @click.stop><input type="checkbox" :checked="candidateSelectedIds.includes(candidate.id)" :disabled="candidateBulkWorking" :aria-label="candidate.title" @click.stop @change="toggleCandidate(candidate.id, $event)" /></label></div><div class="candidate-main"><h3>{{ candidate.title }}</h3><p>{{ candidate.document_title }} · {{ t('smartArchive.eventDate') }} {{ formatDate(candidate.event_at, true) }} · {{ t('smartArchive.suggestedReminderDate') }} {{ candidateDueDate(candidate) }}</p><small>{{ candidate.quote || candidate.description }} · {{ t('smartArchive.confidence') }} {{ Math.round(candidate.confidence * 100) }}%</small></div><button class="candidate-source" type="button" @click="openCandidateSource(candidate)">{{ t('smartArchive.openOriginal') }}</button><template v-if="candidate.needs_review"><span class="status-pill status-pill--needs_review">{{ t('smartArchive.candidateNeedsReview') }}</span><button v-if="canContribute" class="secondary-button" type="button" disabled>{{ t('smartArchive.createReminder') }}</button></template><button v-else-if="canContribute" data-testid="archive-create-reminder" class="secondary-button" type="button" @click="openCandidate(candidate)">{{ t('smartArchive.createReminder') }}</button></article></section>
        <div class="reminder-list"><div v-if="store.reminders.length" class="reminder-list-heading"><label><span class="legal-checkbox-hit-area"><input type="checkbox" :checked="allRemindersSelected" :indeterminate="someRemindersSelected" :disabled="reminderBulkWorking" :aria-label="t('smartArchive.selectAll')" @click.stop="toggleAllReminders" /></span> {{ t('smartArchive.selectAll') }}</label><span v-if="reminderSelectedCount">{{ t('smartArchive.selectedCount', { count: reminderSelectedCount }) }}</span></div><article v-for="reminder in store.reminders" :key="reminder.id" class="reminder-row" :class="{ 'row-checked': reminderSelectedIds.includes(reminder.id) }"><div class="selection-cell"><label class="legal-checkbox-hit-area" @click.stop><input type="checkbox" :checked="reminderSelectedIds.includes(reminder.id)" :disabled="reminderBulkWorking" :aria-label="reminder.title" @click.stop @change="toggleReminder(reminder.id, $event)" /></label></div><div class="reminder-date"><b>{{ reminder.due_at ? formatDate(reminder.due_at, true) : '—' }}</b><small>{{ reminder.due_at ? formatTime(reminder.due_at) : t('smartArchive.noDueDate') }}</small></div><div class="reminder-main"><h3>{{ reminder.title }}</h3><p>{{ reminder.description }}</p><small>{{ t('smartArchive.confidence') }} {{ Math.round(reminder.confidence * 100) }}%</small></div><span class="status-pill" :class="`status-pill--${reminder.status}`">{{ reminderStatusLabel(reminder.status) }}</span><div class="reminder-actions"><button v-if="canContribute && reminder.status === 'draft'" data-testid="archive-activate-reminder" class="secondary-button" type="button" @click="activateReminder(reminder)">{{ t('smartArchive.activate') }}</button><button v-if="canContribute && reminder.status === 'active'" data-testid="archive-handle-reminder" class="secondary-button" type="button" @click="handleReminder(reminder)">{{ t('smartArchive.handle') }}</button></div></article><div v-if="!store.reminders.length" class="archive-empty"><t-icon name="task-checked" size="30px" />{{ t('smartArchive.noReminders') }}</div></div>
        <div v-if="store.notifications.length" class="notification-list"><h3>{{ t('smartArchive.notifications') }}</h3><article v-for="notification in store.notifications" :key="notification.id" :data-testid="`archive-notification-${notification.id}`" :class="{ unread: !notification.read_at }"><div><b>{{ notification.title }}</b><p>{{ notification.body }}</p><small>{{ formatDate(notification.created_at) }}</small></div><div class="notification-actions"><button v-if="!notification.read_at" data-testid="archive-mark-read" class="secondary-button" type="button" @click="markRead(notification.id)">{{ t('smartArchive.markRead') }}</button><button data-testid="archive-delete-notification" class="secondary-button secondary-button--danger" type="button" @click="deleteNotification(notification)">{{ t('smartArchive.deleteNotification') }}</button></div></article></div>
      </template>

      <template v-else>
        <div class="content-heading"><div><h2>{{ t('smartArchive.reviewQueue') }}</h2><span>{{ t('smartArchive.reviewQueueDescription') }}</span></div></div><div v-if="queueDocuments.length" class="queue-list"><article v-for="document in queueDocuments" :key="document.id" @click="openDocument(document)"><t-icon name="error-circle" /><div><b>{{ document.title }}</b><p>{{ document.error_message || t('smartArchive.noVerifiedFields') }}</p></div><span class="status-pill" :class="`status-pill--${document.extraction_status}`">{{ extractionStatusLabel(document.extraction_status) }}</span><button v-if="canContribute && (document.extraction_status === 'failed' || document.extraction_status === 'needs_review')" data-testid="archive-retry-extraction" class="secondary-button" type="button" @click.stop="retryExtraction(document)">{{ t('smartArchive.reidentify') }}</button></article></div><div v-else class="queue-card"><t-icon name="task-checked" size="34px" /><h3>{{ t('smartArchive.queueEmpty') }}</h3><p>{{ t('smartArchive.queueEmptyDescription') }}</p></div>
      </template>
    </main>

    <div v-if="candidateDraft" class="modal-backdrop" @click.self="candidateDraft = null"><form class="candidate-modal" @submit.prevent="createCandidate"><header><div><span class="eyebrow">{{ t('smartArchive.candidateReview') }}</span><h2>{{ candidateDraft.title }}</h2></div><button class="icon-button" type="button" @click="candidateDraft = null"><t-icon name="close" /></button></header><div class="candidate-modal-body"><p>{{ candidateDraft.document_title }}</p><p class="candidate-dates">{{ t('smartArchive.eventDate') }}: {{ formatDate(candidateDraft.event_at, true) }} · {{ t('smartArchive.suggestedReminderDate') }}: {{ candidateDueDate(candidateDraft) }}</p><blockquote>{{ candidateDraft.quote }}</blockquote><button class="candidate-evidence-link" type="button" @click="openCandidateSource(candidateDraft)">{{ t('smartArchive.openOriginal') }}</button><label>{{ t('smartArchive.offsetDays') }}<input v-model.number="candidateOffset" type="number" min="0" max="3650" /></label><label>{{ t('smartArchive.reminderTime') }}<input v-model="candidateTime" type="time" /></label><label>{{ t('smartArchive.assignee') }}<select v-model="candidateAssignee"><option value="">{{ t('smartArchive.assigneeDefault') }}</option><option v-for="member in activeMembers" :key="member.user_id" :value="member.user_id">{{ member.username || member.email || member.user_id }}<template v-if="member.email && member.username"> · {{ member.email }}</template></option></select></label><div class="candidate-modal-actions"><button class="secondary-button" type="button" @click="candidateDraft = null">{{ t('smartArchive.cancel') }}</button><button class="primary-button" type="submit">{{ t('smartArchive.createReminder') }}</button></div></div></form></div>

    <t-drawer :visible="selected !== null" :size="drawerSize" :footer="false" :close-btn="true" class="archive-detail-drawer" @close="selected = null">
      <template #header><div v-if="selected" class="archive-drawer-header"><span class="file-icon"><t-icon name="file-paste" /></span><div><strong>{{ selected.title }}</strong><small>{{ t('smartArchive.documentDetail') }}</small></div></div></template>
      <div v-if="selected" class="detail-scroll"><div class="detail-status"><span class="archive-document-status" :class="`archive-document-status--${archiveDocumentStatusTone(selected.extraction_status)}`"><i />{{ extractionStatusLabel(selected.extraction_status) }}</span><span>{{ documentTypeLabel(selected.document_type) }}</span><time>{{ formatDate(selected.updated_at) }}</time></div><div class="detail-actions"><button data-testid="archive-preview" class="secondary-button" type="button" @click="previewDocument"><t-icon name="browse" /> {{ t('smartArchive.openOriginal') }}</button><button v-if="canContribute && (selected.extraction_status === 'failed' || selected.extraction_status === 'needs_review')" class="secondary-button" type="button" @click="retryExtraction(selected)">{{ t('smartArchive.reidentify') }}</button><template v-if="selected.archived_at && canAdmin"><button data-testid="archive-restore" class="secondary-button" type="button" @click="restoreArchivedDocument(selected)">{{ t('smartArchive.restore') }}</button><button data-testid="archive-delete" class="secondary-button secondary-button--danger" type="button" @click="deleteArchivedDocument(selected)">{{ t('smartArchive.delete') }}</button></template><button v-else-if="canContribute" data-testid="archive-archive" class="secondary-button" type="button" @click="archiveDocument(selected)">{{ t('smartArchive.archive') }}</button></div><dl class="field-list"><div><dt>{{ t('smartArchive.fileName') }}</dt><dd>{{ selected.file_name }}</dd></div><div><dt>{{ t('smartArchive.fileSize') }}</dt><dd>{{ formatSize(selected.file_size) }}</dd></div><div><dt>{{ t('smartArchive.importedAt') }}</dt><dd>{{ formatDate(selected.created_at) }}</dd></div><div><dt>{{ t('smartArchive.type') }}</dt><dd>{{ documentTypeLabel(selected.document_type) }}</dd></div><div v-if="selected.customer?.name"><dt>{{ t('smartArchive.relatedParty') }}</dt><dd>{{ selected.customer.name }}</dd></div><div><dt>{{ t('smartArchive.agreementNumber') }}</dt><dd class="mono">{{ selected.agreement_number || '—' }}</dd></div><div><dt>{{ t('smartArchive.expiry') }}</dt><dd>{{ selected.expires_at ? formatDate(selected.expires_at, true) : '—' }}</dd></div><div v-if="selected.return_due_at"><dt>{{ t('smartArchive.returnDue') }}</dt><dd>{{ formatDate(selected.return_due_at, true) }}</dd></div><div><dt>{{ t('smartArchive.amount') }}</dt><dd>{{ selected.amount ? `${selected.currency || ''} ${selected.amount}` : '—' }}</dd></div></dl><div v-if="selected.error_message" class="archive-detail-error"><strong>{{ t('smartArchive.errorDetails') }}</strong><p>{{ selected.error_message }}</p></div><h3 class="detail-section-title">{{ t('smartArchive.evidence') }}</h3><article v-for="evidence in selected.evidence" :key="evidence.id" class="evidence-row"><div><b>{{ evidence.field_name }}</b><span>{{ evidence.value }}</span><small>{{ evidence.quote }}</small></div><em>{{ Math.round(evidence.confidence * 100) }}%</em></article><p v-if="!selected.evidence?.length" class="muted">{{ t('smartArchive.noEvidence') }}</p></div>
    </t-drawer>
  </section>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { Icon as TIcon, Loading as TLoading, MessagePlugin } from 'tdesign-vue-next'
import { useSmartArchiveStore } from '@/stores/smartArchive'
import { getArchiveDocumentPreview } from '@/api/smart-archive'
import { listMembers, type TenantMember } from '@/api/tenant/members'
import { useAuthStore } from '@/stores/auth'
import type { ArchiveDocument, ArchiveExtractionStatus, ArchiveNotification, ArchiveReminder, ArchiveReminderCandidate, ArchiveReminderStatus } from '@/api/smart-archive'
import { archiveDocumentStatusTone, buildArchiveSearchFilters, hasMoreArchiveDocuments } from './smartArchiveDocuments'

const { t, locale } = useI18n(); const store = useSmartArchiveStore(); const authStore = useAuthStore(); const tab = ref('documents'); const query = ref(''); const appliedQuery = ref(''); const dateFrom = ref(''); const dateTo = ref(''); const documentTypeFilter = ref(''); const statusFilters = ref<ArchiveExtractionStatus[]>([]); const archiveView = ref<'active' | 'archived'>('active'); const selected = ref<ArchiveDocument | null>(null); const selectedIds = ref<string[]>([]); const reminderSelectedIds = ref<string[]>([]); const candidateSelectedIds = ref<string[]>([]); const bulkWorking = ref(false); const reminderBulkWorking = ref(false); const candidateBulkWorking = ref(false); const fileInput = ref<HTMLInputElement | null>(null); const uploading = ref(false); const documentLoading = ref(false); const loadingMore = ref(false); const documentError = ref(''); const viewportWidth = ref(typeof window === 'undefined' ? 1200 : window.innerWidth); const candidateDraft = ref<ArchiveReminderCandidate | null>(null); const candidateOffset = ref(0); const candidateTime = ref('09:00'); const candidateAssignee = ref(''); const members = ref<TenantMember[]>([])
const statusFilterDetails = ref<HTMLDetailsElement | null>(null)
const canContribute = computed(() => authStore.hasRole('contributor'))
const canAdmin = computed(() => authStore.hasRole('admin'))
const documentTypes = ['contract', 'loan_agreement', 'outbound_order', 'return_order', 'renewal', 'payment', 'delivery', 'other']
const extractionStatuses: ArchiveExtractionStatus[] = ['uploading', 'parsing', 'extracting', 'linking', 'needs_review', 'completed', 'failed']
const tabs = computed(() => [{ id: 'documents', label: t('smartArchive.documents'), icon: 'file-paste' }, { id: 'reminders', label: t('smartArchive.reminders'), icon: 'task-checked' }, { id: 'queue', label: t('smartArchive.reviewQueue'), icon: 'error-circle' }])
const unreadCount = computed(() => store.notifications.filter((item) => !item.read_at).length)
const activeMembers = computed(() => members.value.filter((member) => member.status === 'active'))
const queueDocuments = computed(() => store.documents.filter((item) => item.extraction_status === 'needs_review' || item.extraction_status === 'failed'))
const showArchived = computed(() => archiveView.value === 'archived')
const documentSearchFilters = computed(() => buildArchiveSearchFilters({ dateFrom: dateFrom.value, dateTo: dateTo.value, documentType: documentTypeFilter.value, statuses: statusFilters.value, archived: showArchived.value }))
const hasDocumentFilters = computed(() => Boolean(appliedQuery.value || dateFrom.value || dateTo.value || documentTypeFilter.value || statusFilters.value.length || showArchived.value))
const hasMoreDocuments = computed(() => hasMoreArchiveDocuments(store.documents, store.searchTotal))
const statusFilterLabel = computed(() => statusFilters.value.length ? t('smartArchive.selectedStatuses', { count: statusFilters.value.length }) : t('smartArchive.allStatuses'))
const drawerSize = computed(() => viewportWidth.value <= 700 ? '100%' : '560px')
const visibleDocumentIds = computed(() => store.documents.map((document) => document.id))
const selectedCount = computed(() => selectedIds.value.length)
const allDocumentsSelected = computed(() => visibleDocumentIds.value.length > 0 && visibleDocumentIds.value.every((id) => selectedIds.value.includes(id)))
const someDocumentsSelected = computed(() => selectedCount.value > 0 && !allDocumentsSelected.value)
const visibleReminderIds = computed(() => store.reminders.map((reminder) => reminder.id))
const visibleCandidateIds = computed(() => store.reminderCandidates.map((candidate) => candidate.id))
const reminderSelectedCount = computed(() => reminderSelectedIds.value.length)
const candidateSelectedCount = computed(() => candidateSelectedIds.value.length)
const allRemindersSelected = computed(() => visibleReminderIds.value.length > 0 && visibleReminderIds.value.every((id) => reminderSelectedIds.value.includes(id)))
const someRemindersSelected = computed(() => reminderSelectedCount.value > 0 && !allRemindersSelected.value)
const allCandidatesSelected = computed(() => visibleCandidateIds.value.length > 0 && visibleCandidateIds.value.every((id) => candidateSelectedIds.value.includes(id)))
const someCandidatesSelected = computed(() => candidateSelectedCount.value > 0 && !allCandidatesSelected.value)
const selectedActiveReminderCount = computed(() => store.reminders.filter((reminder) => reminderSelectedIds.value.includes(reminder.id) && (reminder.status === 'active' || reminder.status === 'snoozed')).length)
const hasProcessingDocuments = computed(() => store.documents.some((document) => ['uploading', 'parsing', 'extracting', 'linking'].includes(document.extraction_status)))
let statusPollTimer: ReturnType<typeof setInterval> | null = null
async function refreshDocumentSearch() {
  documentLoading.value = true; documentError.value = ''
  try { await store.search(appliedQuery.value, documentSearchFilters.value, 1, false) } catch (e: any) { documentError.value = e?.message || t('smartArchive.searchFailed') } finally { documentLoading.value = false }
}
async function refreshLoadedDocumentPages() {
  try { await store.refreshSearch(appliedQuery.value, documentSearchFilters.value, store.searchPage || 1) } catch { /* The next poll retries transient failures. */ }
}
function stopStatusPolling() { if (statusPollTimer !== null) { clearInterval(statusPollTimer); statusPollTimer = null } }
function syncStatusPolling(active: boolean) {
  if (active && statusPollTimer === null) {
    statusPollTimer = setInterval(() => { if (tab.value === 'documents') void refreshLoadedDocumentPages() }, 2000)
  } else if (!active) {
    stopStatusPolling()
  }
}
watch(hasProcessingDocuments, syncStatusPolling, { immediate: true })
function toggleDocument(id: string, event: Event) { const checked = (event.target as HTMLInputElement).checked; selectedIds.value = checked ? [...new Set([...selectedIds.value, id])] : selectedIds.value.filter((item) => item !== id) }
function toggleAllDocuments(event: Event) { const checked = (event.target as HTMLInputElement).checked; selectedIds.value = checked ? [...new Set([...selectedIds.value, ...visibleDocumentIds.value])] : selectedIds.value.filter((id) => !visibleDocumentIds.value.includes(id)) }
function applyDocumentSearch() { appliedQuery.value = query.value.trim(); selectedIds.value = []; void refreshDocumentSearch() }
function onDocumentFiltersChanged() { selectedIds.value = []; void refreshDocumentSearch() }
function onArchiveViewChanged() { selectedIds.value = []; void refreshDocumentSearch() }
function toggleExtractionStatus(status: ArchiveExtractionStatus) { statusFilters.value = statusFilters.value.includes(status) ? statusFilters.value.filter(item => item !== status) : [...statusFilters.value, status]; onDocumentFiltersChanged() }
function clearDocumentFilters() { query.value = ''; appliedQuery.value = ''; dateFrom.value = ''; dateTo.value = ''; documentTypeFilter.value = ''; statusFilters.value = []; archiveView.value = 'active'; selectedIds.value = []; void refreshDocumentSearch() }
async function loadMoreDocuments() { if (loadingMore.value || !hasMoreDocuments.value) return; loadingMore.value = true; try { await store.search(appliedQuery.value, documentSearchFilters.value, store.searchPage + 1, true) } catch (e: any) { MessagePlugin.error(e?.message || t('smartArchive.searchFailed')) } finally { loadingMore.value = false } }
async function reloadDocumentContext() { if (tab.value === 'documents') await refreshDocumentSearch(); else await store.loadDocuments('', false) }
function onResize() { viewportWidth.value = window.innerWidth }
function toggleReminder(id: string, event: Event) { const checked = (event.target as HTMLInputElement).checked; reminderSelectedIds.value = checked ? [...new Set([...reminderSelectedIds.value, id])] : reminderSelectedIds.value.filter((item) => item !== id) }
function toggleAllReminders(event: Event) { const checked = (event.target as HTMLInputElement).checked; reminderSelectedIds.value = checked ? [...new Set([...reminderSelectedIds.value, ...visibleReminderIds.value])] : reminderSelectedIds.value.filter((id) => !visibleReminderIds.value.includes(id)) }
function toggleCandidate(id: string, event: Event) { const checked = (event.target as HTMLInputElement).checked; candidateSelectedIds.value = checked ? [...new Set([...candidateSelectedIds.value, id])] : candidateSelectedIds.value.filter((item) => item !== id) }
function toggleAllCandidates(event: Event) { const checked = (event.target as HTMLInputElement).checked; candidateSelectedIds.value = checked ? [...new Set([...candidateSelectedIds.value, ...visibleCandidateIds.value])] : candidateSelectedIds.value.filter((id) => !visibleCandidateIds.value.includes(id)) }
async function runBulkAction(action: 'archive' | 'restore' | 'delete' | 'purge') {
  const ids = [...selectedIds.value]
  if (!ids.length || bulkWorking.value) return
  const confirmKey = action === 'archive' ? 'smartArchive.bulkArchiveConfirm' : action === 'restore' ? 'smartArchive.bulkRestoreConfirm' : 'smartArchive.bulkDeleteConfirm'
  if (!window.confirm(t(confirmKey, { count: ids.length }))) return
  bulkWorking.value = true
  try {
    const result = await store.bulkAction(action, ids)
    selectedIds.value = []
    await reloadDocumentContext()
    if (result.failed > 0) MessagePlugin.warning(t('smartArchive.bulkActionPartial', { succeeded: result.succeeded, failed: result.failed }))
    else MessagePlugin.success(t('smartArchive.bulkActionSuccess', { count: result.succeeded }))
  } catch (e: any) {
    MessagePlugin.error(e?.message || t('smartArchive.bulkActionFailed'))
  } finally {
    bulkWorking.value = false
  }
}
async function runBulkDeleteReminders() {
  const ids = [...reminderSelectedIds.value]
  if (!ids.length || reminderBulkWorking.value) return
  const confirmKey = selectedActiveReminderCount.value > 0 ? 'smartArchive.bulkDeleteRemindersActiveConfirm' : 'smartArchive.bulkDeleteRemindersConfirm'
  if (!window.confirm(t(confirmKey, { count: ids.length, active: selectedActiveReminderCount.value }))) return
  reminderBulkWorking.value = true
  try {
    const result = await store.bulkDeleteReminders(ids)
    const succeeded = new Set(result.items.filter((item) => item.success).map((item) => item.id))
    reminderSelectedIds.value = reminderSelectedIds.value.filter((id) => !succeeded.has(id))
    await store.loadReminders()
    if (result.failed > 0) MessagePlugin.warning(t('smartArchive.bulkActionPartial', { succeeded: result.succeeded, failed: result.failed }))
    else MessagePlugin.success(t('smartArchive.bulkDeleteRemindersSuccess', { count: result.succeeded }))
  } catch (e: any) {
    MessagePlugin.error(e?.message || t('smartArchive.bulkActionFailed'))
  } finally {
    reminderBulkWorking.value = false
  }
}
async function runBulkIgnoreCandidates() {
  const ids = [...candidateSelectedIds.value]
  if (!ids.length || candidateBulkWorking.value) return
  if (!window.confirm(t('smartArchive.bulkIgnoreCandidatesConfirm', { count: ids.length }))) return
  candidateBulkWorking.value = true
  try {
    const result = await store.bulkIgnoreReminderCandidates(ids)
    const succeeded = new Set(result.items.filter((item) => item.success).map((item) => item.id))
    candidateSelectedIds.value = candidateSelectedIds.value.filter((id) => !succeeded.has(id))
    if (result.failed > 0) MessagePlugin.warning(t('smartArchive.bulkActionPartial', { succeeded: result.succeeded, failed: result.failed }))
    else MessagePlugin.success(t('smartArchive.bulkIgnoreCandidatesSuccess', { count: result.succeeded }))
  } catch (e: any) {
    MessagePlugin.error(e?.message || t('smartArchive.bulkActionFailed'))
  } finally {
    candidateBulkWorking.value = false
  }
}
const loadReminders = () => Promise.all([store.loadReminders(), store.loadReminderCandidates(), store.loadNotifications()]).catch((e: any) => MessagePlugin.error(e?.message || t('smartArchive.loadFailed')))
async function selectTab(value: string) { selectedIds.value = []; reminderSelectedIds.value = []; candidateSelectedIds.value = []; selected.value = null; tab.value = value; if (value === 'documents') await refreshDocumentSearch(); if (value === 'queue') await store.loadDocuments('', false); if (value === 'reminders') await loadReminders() }
async function onFileInput(event: Event) { const files = Array.from((event.target as HTMLInputElement).files || []); (event.target as HTMLInputElement).value = ''; if (!files.length) return; const valid = files.filter((file) => /\.(pdf|doc|docx|xls|xlsx|jpg|jpeg|png|webp)$/i.test(file.name)); if (valid.length !== files.length) MessagePlugin.warning(t('smartArchive.invalidFiles')); if (!valid.length) return; uploading.value = true; try { await store.upload(valid); MessagePlugin.success(t('smartArchive.importStarted')); await reloadDocumentContext() } catch (e: any) { MessagePlugin.error(e?.message || t('smartArchive.importFailed')) } finally { uploading.value = false } }
async function openDocument(document: ArchiveDocument) { selected.value = document; try { selected.value = (await store.loadDocument(document.id)) || document } catch { selected.value = document } }
async function archiveDocument(document: ArchiveDocument | null) { if (!document || !window.confirm(t('smartArchive.archiveConfirm', { name: document.title }))) return; try { await store.archive(document.id); selectedIds.value = selectedIds.value.filter((id) => id !== document.id); selected.value = null; await reloadDocumentContext() } catch (e: any) { MessagePlugin.error(e?.message || t('smartArchive.archiveFailed')) } }
async function restoreArchivedDocument(document: ArchiveDocument | null) { if (!document || !window.confirm(t('smartArchive.restoreConfirm', { name: document.title }))) return; try { await store.restore(document.id); selectedIds.value = selectedIds.value.filter((id) => id !== document.id); selected.value = null; await reloadDocumentContext() } catch (e: any) { MessagePlugin.error(e?.message || t('smartArchive.restoreFailed')) } }
async function deleteArchivedDocument(document: ArchiveDocument | null) { if (!document || !window.confirm(t('smartArchive.deleteConfirm', { name: document.title }))) return; try { await store.deleteDocument(document.id); selectedIds.value = selectedIds.value.filter((id) => id !== document.id); selected.value = null; await reloadDocumentContext() } catch (e: any) { MessagePlugin.error(e?.message || t('smartArchive.deleteFailed')) } }
async function retryExtraction(document: ArchiveDocument) { try { await store.retryExtraction(document.id); MessagePlugin.success(t('smartArchive.importStarted')); await reloadDocumentContext(); const refreshed = await store.loadDocument(document.id); if (refreshed) selected.value = refreshed } catch (e: any) { MessagePlugin.error(e?.message || t('smartArchive.importFailed')) } }
function documentMimeType(fileType: string) {
  switch (fileType.toLowerCase().replace(/^\./, '')) {
    case 'pdf': return 'application/pdf'
    case 'doc': return 'application/msword'
    case 'docx': return 'application/vnd.openxmlformats-officedocument.wordprocessingml.document'
    case 'xls': return 'application/vnd.ms-excel'
    case 'xlsx': return 'application/vnd.openxmlformats-officedocument.spreadsheetml.sheet'
    case 'jpg':
    case 'jpeg': return 'image/jpeg'
    case 'png': return 'image/png'
    case 'webp': return 'image/webp'
    default: return 'application/octet-stream'
  }
}
async function previewDocument() { if (!selected.value) return; try { const source = await getArchiveDocumentPreview(selected.value.id); const mime = documentMimeType(selected.value.file_type); const blob = source.type === mime ? source : new Blob([source], { type: mime }); const url = URL.createObjectURL(blob); window.open(url, '_blank', 'noopener'); window.setTimeout(() => URL.revokeObjectURL(url), 5 * 60_000) } catch (e: any) { MessagePlugin.error(e?.message || t('smartArchive.loadFailed')) } }
async function activateReminder(reminder: ArchiveReminder) { try { await store.updateReminder(reminder.id, { status: 'active' }); await store.loadReminders() } catch (e: any) { MessagePlugin.error(e?.message || t('smartArchive.reminderFailed')) } }
async function handleReminder(reminder: ArchiveReminder) { try { await store.updateReminder(reminder.id, { status: 'handled' }); await store.loadReminders() } catch (e: any) { MessagePlugin.error(e?.message || t('smartArchive.reminderFailed')) } }
function openCandidate(candidate: ArchiveReminderCandidate) { candidateDraft.value = candidate; candidateOffset.value = candidate.suggested_offset_days; candidateTime.value = '09:00'; candidateAssignee.value = candidate.assignee_id || '' }
async function openCandidateSource(candidate: ArchiveReminderCandidate) { try { const document = await store.loadDocument(candidate.document_id); if (document) selected.value = document } catch (e: any) { MessagePlugin.error(e?.message || t('smartArchive.loadFailed')) } }
async function createCandidate() { if (!candidateDraft.value) return; try { await store.createReminderFromCandidate(candidateDraft.value.id, { offset_days: candidateOffset.value, time: candidateTime.value, assignee_id: candidateAssignee.value.trim() || undefined }); candidateDraft.value = null; MessagePlugin.success(t('smartArchive.reminderCreated')); await store.loadReminders() } catch (e: any) { MessagePlugin.error(e?.message || t('smartArchive.reminderFailed')) } }
async function loadMembers() { const tenantId = Number(authStore.effectiveTenantId); if (!tenantId) return; try { const response = await listMembers(tenantId, { page: 1, page_size: 100 }); members.value = response.data?.members || [] } catch { members.value = [] } }
async function markRead(id: string) { try { await store.markNotificationRead(id) } catch (e: any) { MessagePlugin.error(e?.message || t('smartArchive.notificationUpdateFailed')) } }
async function deleteNotification(notification: ArchiveNotification) { if (!window.confirm(t('smartArchive.deleteNotificationConfirm'))) return; try { await store.deleteNotification(notification.id) } catch (e: any) { MessagePlugin.error(e?.message || t('smartArchive.notificationDeleteFailed')) } }
const formatDate = (value: string, dateOnly = false) => new Intl.DateTimeFormat(locale.value, dateOnly ? { year: 'numeric', month: 'short', day: 'numeric' } : { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' }).format(new Date(value))
const formatTime = (value: string) => new Intl.DateTimeFormat(locale.value, { hour: '2-digit', minute: '2-digit' }).format(new Date(value)); const formatSize = (value: number) => value > 1024 * 1024 ? `${(value / (1024 * 1024)).toFixed(1)} MB` : `${Math.max(1, Math.round(value / 1024))} KB`
const candidateDueDate = (candidate: ArchiveReminderCandidate) => { const date = new Date(candidate.event_at); date.setUTCDate(date.getUTCDate() - candidate.suggested_offset_days); return formatDate(date.toISOString(), true) }
const documentTypeLabel = (value: string) => t(`smartArchive.documentTypes.${value}`, value); const extractionStatusLabel = (value: ArchiveExtractionStatus) => t(`smartArchive.extractionStatuses.${value}`, value); const reminderStatusLabel = (value: ArchiveReminderStatus) => t(`smartArchive.reminderStatuses.${value}`, value)
function closeStatusFilterOnOutsideClick(event: MouseEvent) {
  const details = statusFilterDetails.value
  const target = event.target
  if (details?.open && target instanceof Node && !details.contains(target)) details.open = false
}
onMounted(async () => { window.addEventListener('resize', onResize); document.addEventListener('click', closeStatusFilterOnOutsideClick, true); await Promise.allSettled([store.loadSettings(), loadMembers()]); await refreshDocumentSearch(); await loadReminders(); syncStatusPolling(hasProcessingDocuments.value) }); onBeforeUnmount(() => { window.removeEventListener('resize', onResize); document.removeEventListener('click', closeStatusFilterOnOutsideClick, true); stopStatusPolling(); store.disconnect() })
</script>

<style scoped lang="less">
.archive-page{position:relative;width:100%;height:100%;overflow:auto;background:#f7f4ed;color:#1f1f1f}.archive-header{max-width:1320px;margin:0 auto;padding:34px 42px 24px;display:flex;align-items:flex-end;justify-content:space-between}.eyebrow{display:block;color:#8a8a83;font-size:10px;letter-spacing:.12em;text-transform:uppercase}h1{margin:7px 0 5px;font-size:28px;letter-spacing:-.04em}h2{margin:0;font-size:18px;letter-spacing:-.02em}p{margin:0;color:#777770;font-size:12px}.header-actions{display:flex;gap:8px;align-items:center}.primary-button,.secondary-button,.notification-button,.icon-button{border:1px solid #d6d6d0;border-radius:5px;background:#fff;color:#1f1f1f;cursor:pointer}.primary-button{height:36px;padding:0 13px;background:#1f1f1f;color:#fff;border-color:#1f1f1f;font-size:12px;font-weight:650}.secondary-button{height:32px;padding:0 10px;font-size:11px}.notification-button{position:relative;width:34px;height:34px}.notification-count{position:absolute;right:-3px;top:-5px;min-width:15px;height:15px;border-radius:8px;background:#a6534d;color:#fff;font-size:9px;line-height:15px}.archive-toolbar{max-width:1320px;margin:0 auto;padding:0 42px;display:flex;justify-content:space-between;border-bottom:1px solid #dddcd6}.archive-tabs{display:flex;gap:18px}.archive-tabs button{height:42px;padding:0 2px;border:0;border-bottom:2px solid transparent;background:transparent;color:#777770;font-size:12px;cursor:pointer;display:flex;align-items:center;gap:6px}.archive-tabs button.active{border-color:#1f1f1f;color:#1f1f1f;font-weight:650}.search-box{width:280px;height:31px;margin:5px 0 6px;display:flex;align-items:center;gap:7px;padding:0 9px;border:1px solid #d8d8d2;border-radius:5px;background:#fff;color:#888}.search-box input{width:100%;border:0;outline:0;background:transparent;font-size:11px}.search-box button{border:0;background:transparent;color:#999;cursor:pointer}.archive-content{max-width:1320px;margin:0 auto;padding:28px 42px 54px}.content-heading{display:flex;justify-content:space-between;align-items:flex-end;margin-bottom:17px}.content-heading h2{margin-bottom:5px}.content-heading span{color:#8a8a83;font-size:11px}.archive-switch{color:#777;font-size:11px}.archive-table-wrap{border:1px solid #deded8;border-radius:6px;background:#fff;overflow:auto}.archive-table{width:100%;border-collapse:collapse;font-size:11px}.archive-table th{height:37px;padding:0 14px;background:#f1f1ed;color:#8a8a83;text-align:left;font-size:10px;font-weight:500;letter-spacing:.04em;text-transform:uppercase}.archive-table td{height:66px;padding:0 14px;border-top:1px solid #eeeeea;cursor:pointer}.archive-table tr:hover,.archive-table tr.selected{background:#fafaf8}.doc-cell{display:flex;align-items:center;gap:10px;min-width:300px}.file-icon{width:32px;height:36px;display:flex;align-items:center;justify-content:center;border:1px solid #deded8;border-radius:4px;background:#fafaf8;color:#64645e}.doc-cell b,.doc-cell small{display:block;max-width:420px;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}.doc-cell b{font-size:12px}.doc-cell small{margin-top:4px;color:#999991;font-size:10px}.muted{color:#898982}.icon-button{width:28px;height:28px;border:0;background:transparent;color:#777}.status-pill{display:inline-flex;padding:4px 7px;border-radius:3px;background:#ededE9;color:#66635d;font-size:10px;font-weight:600}.status-pill--completed,.status-pill--active,.status-pill--handled{color:#4d4d4d;background:#f1f1ef}.status-pill--failed,.status-pill--canceled{color:#8b3e34;background:#f4e7e4}.status-pill--parsing,.status-pill--extracting,.status-pill--linking,.status-pill--draft,.status-pill--needs_review{color:#765329;background:#f6eedc}.archive-empty{min-height:310px;display:flex;flex-direction:column;align-items:center;justify-content:center;gap:9px;color:#85857e;font-size:12px}.archive-empty strong{color:#1f1f1f;font-size:14px}.archive-empty .primary-button{margin-top:8px}.import-progress{position:sticky;top:0;z-index:3;display:flex;align-items:center;gap:9px;padding:8px 42px;background:#fff1d7;color:#74591c;font-size:11px;border-bottom:1px solid #e5d8b9}.import-progress i{width:160px;height:3px;background:#e0d4b7}.import-progress i b{display:block;height:100%;background:#765329}.entity-grid{display:grid;grid-template-columns:repeat(auto-fill,minmax(260px,1fr));gap:10px}.entity-card{display:flex;gap:12px;padding:17px;border:1px solid #deded8;border-radius:6px;background:#fff}.entity-avatar{width:32px;height:32px;display:flex;align-items:center;justify-content:center;border-radius:50%;background:#efefeb;color:#555}.entity-card h3,.reminder-main h3{margin:0 0 4px;font-size:13px}.entity-card p,.entity-card small{display:block;margin-top:4px;color:#888;font-size:10px}.table-empty{text-align:center!important;color:#999}.mono{font-family:ui-monospace,SFMono-Regular,Menlo,monospace;font-size:10px}.reminder-list{border:1px solid #deded8;border-radius:6px;background:#fff}.reminder-row{min-height:80px;padding:13px 16px;display:flex;align-items:center;gap:18px;border-bottom:1px solid #eeeeea}.reminder-row:last-child{border-bottom:0}.reminder-date{width:78px;color:#1f1f1f;text-align:center}.reminder-date b,.reminder-date small{display:block}.reminder-date b{font-size:12px}.reminder-date small{margin-top:4px;color:#999;font-size:10px}.reminder-main{min-width:0;flex:1}.reminder-main p{overflow:hidden;text-overflow:ellipsis;white-space:nowrap}.reminder-main small{display:block;margin-top:5px;color:#a09f98;font-size:10px}.reminder-actions{width:80px;text-align:right}.notification-list{margin-top:26px}.notification-list>h3,.detail-section-title{margin:0 0 10px;font-size:12px}.notification-list article{padding:12px 14px;display:flex;justify-content:space-between;align-items:center;border:1px solid #deded8;background:#fff}.notification-list article+article{border-top:0}.notification-list article.unread{border-left:3px solid #1f1f1f}.notification-list p{margin:4px 0;font-size:11px}.notification-list small{color:#999;font-size:10px}.queue-card{min-height:250px;display:flex;flex-direction:column;align-items:center;justify-content:center;gap:8px;border:1px dashed #d6d6d0;background:#fff;color:#85857e}.queue-card h3{margin:4px 0 0;color:#444;font-size:14px}.archive-detail{position:fixed;z-index:5;top:0;right:0;width:410px;height:100%;background:#fff;border-left:1px solid #dddcd6;box-shadow:-10px 0 30px rgba(31,31,31,.06);display:flex;flex-direction:column}.archive-detail header{padding:23px 22px 18px;display:flex;justify-content:space-between;border-bottom:1px solid #e4e4df}.archive-detail header h2{margin-top:7px;font-size:16px;max-width:320px;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}.detail-scroll{padding:18px 22px;overflow:auto}.detail-actions{display:flex;gap:7px;margin-bottom:20px}.field-list{margin:0;border-top:1px solid #eeeeea}.field-list>div{display:flex;justify-content:space-between;padding:10px 0;border-bottom:1px solid #eeeeea}.field-list dt{color:#888;font-size:11px}.field-list dd{max-width:220px;margin:0;text-align:right;font-size:11px}.detail-section-title{margin-top:23px}.evidence-row{display:flex;justify-content:space-between;gap:8px;padding:10px 0;border-bottom:1px solid #eeeeea}.evidence-row b,.evidence-row span,.evidence-row small{display:block}.evidence-row b{font-size:10px;color:#777}.evidence-row span{margin-top:3px;font-size:11px;font-weight:600}.evidence-row small{margin-top:5px;color:#92928b;font-size:10px;line-height:1.4}.evidence-row em{color:#4d4d4d;font-size:10px;font-style:normal}.archive-detail .muted{font-size:11px}@media(max-width:900px){.archive-header,.archive-toolbar,.archive-content{padding-left:20px;padding-right:20px}.archive-header{align-items:flex-start;gap:15px;flex-direction:column}.search-box{width:220px}.archive-tabs{gap:8px}.archive-detail{width:min(410px,100%)}}
.queue-list{border:1px solid #deded8;border-radius:6px;background:#fff}.queue-list article{display:flex;align-items:center;gap:12px;padding:15px;border-bottom:1px solid #eeeeea;cursor:pointer}.queue-list article:last-child{border-bottom:0}.queue-list article>div{min-width:0;flex:1}.queue-list b,.queue-list p{display:block;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}.queue-list b{font-size:12px}.queue-list p{margin-top:4px}.candidate-section{margin-bottom:24px;border:1px solid #deded8;border-radius:6px;background:#fff}.candidate-heading{padding:15px 16px;border-bottom:1px solid #eeeeea}.candidate-heading h3{margin:0 0 4px;font-size:13px}.candidate-heading span{color:#888;font-size:11px}.candidate-row{display:flex;align-items:center;gap:12px;padding:14px 16px;border-bottom:1px solid #eeeeea}.candidate-row:last-child{border-bottom:0}.candidate-main{min-width:0;flex:1}.candidate-main h3{margin:0 0 4px;font-size:12px}.candidate-main p,.candidate-main small{display:block;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}.candidate-main small{margin-top:4px;color:#999;font-size:10px}.candidate-empty{padding:25px;text-align:center;color:#999;font-size:11px}.modal-backdrop{position:fixed;z-index:20;inset:0;display:flex;align-items:center;justify-content:center;background:rgba(25,25,22,.26)}.candidate-modal{width:min(450px,calc(100% - 32px));border:1px solid #d9d9d2;border-radius:7px;background:#fff;box-shadow:0 16px 50px rgba(31,31,31,.16)}.candidate-modal header{display:flex;justify-content:space-between;padding:20px;border-bottom:1px solid #eeeeea}.candidate-modal header h2{margin-top:6px;font-size:16px}.candidate-modal-body{padding:20px}.candidate-modal-body>p{color:#555;font-size:12px}.candidate-modal blockquote{margin:14px 0;padding:10px 12px;border-left:3px solid #c7c7be;background:#fafaf8;color:#666;font-size:11px;line-height:1.5}.candidate-modal label{display:flex;justify-content:space-between;align-items:center;margin-top:12px;color:#555;font-size:11px}.candidate-modal input{width:130px;height:30px;padding:0 8px;border:1px solid #d7d7d1;border-radius:4px}.candidate-modal-actions{display:flex;justify-content:flex-end;gap:8px;margin-top:22px}
.candidate-source{border:0;background:transparent;color:#777;font-size:10px;cursor:pointer;white-space:nowrap}.candidate-source:hover{text-decoration:underline;color:#1f1f1f}
.candidate-row .secondary-button:disabled { opacity: .55; cursor: not-allowed; }
.candidate-heading,.reminder-list-heading{display:flex;align-items:center;justify-content:space-between;gap:12px}.candidate-heading-main{display:flex;align-items:flex-start;gap:10px}.candidate-heading-main>input,.reminder-list-heading input,.candidate-row .selection-cell input{width:14px;height:14px;margin-top:2px;accent-color:#1f1f1f;cursor:pointer}.candidate-heading-selection,.reminder-list-heading{color:#777;font-size:10px}.reminder-list-heading{min-height:40px;padding:0 16px;border-bottom:1px solid #eeeeea}.reminder-list-heading label{display:flex;align-items:center;gap:7px}.reminder-row .selection-cell{width:20px;flex:0 0 20px;text-align:center}.reminder-row.row-checked,.candidate-row.row-checked{background:#fafaf8}
.archive-action-button { padding: 4px 6px; border: 0; background: transparent; color: #555; cursor: pointer; font-size: 11px; }
.archive-action-button:hover { text-decoration: underline; color: #1f1f1f; }
.archive-action-button--danger { color: #9a443b; }
.archive-action-button--danger:hover { color: #7d3029; }
.secondary-button--danger { color: #9a443b; }
.secondary-button--danger:hover:not(:disabled) { color: #7d3029; }
.notification-list article>div:first-child { min-width: 0; }
.notification-actions { display: flex; align-items: center; gap: 7px; flex-shrink: 0; margin-left: 12px; }
</style>

<style scoped lang="less">
/* Legal workspace palette overrides keep the archive module visually aligned
   while its dense layout and behavior remain unchanged. */
.candidate-modal select { width: 190px; height: 30px; padding: 0 8px; border: 1px solid var(--legal-border); border-radius: 4px; background: var(--legal-bg-surface); color: var(--legal-text-primary); }
.archive-page { color: var(--legal-text-primary); background: var(--legal-bg-page); }
.eyebrow,
.archive-page p,
.content-heading span,
.archive-switch,
.muted,
.doc-cell small,
.entity-card p,
.entity-card small,
.reminder-date small,
.reminder-main small,
.notification-list small,
.field-list dt,
.evidence-row b,
.evidence-row small,
.candidate-heading span,
.candidate-main small,
.candidate-empty,
.candidate-source,
.candidate-dates { color: var(--legal-text-secondary); }

.primary-button,
.secondary-button,
.notification-button,
.icon-button {
  border-color: var(--legal-border);
  color: var(--legal-text-primary);
  background: var(--legal-bg-surface);

  &:hover:not(:disabled) { border-color: var(--legal-ai); color: var(--legal-ai-strong); background: var(--legal-bg-hover); }
  &:focus-visible { outline: 2px solid var(--legal-ai); outline-offset: 2px; }
}
.primary-button {
  border-color: var(--legal-brand);
  color: #fff;
  background: var(--legal-brand);
  &:hover:not(:disabled) { border-color: var(--legal-brand-hover); color: #fff; background: var(--legal-brand-hover); }
}
.notification-count { color: #fff; background: var(--legal-risk); }

.archive-toolbar { border-color: var(--legal-border); }
.archive-tabs button { color: var(--legal-text-secondary); }
.archive-tabs button.active { border-color: var(--legal-brand); color: var(--legal-brand); }
.toolbar-filters { display: flex; align-items: center; gap: 8px; }
.toolbar-filters select,
.search-box,
.candidate-modal input {
  border: 1px solid var(--legal-border);
  color: var(--legal-text-primary);
  background: var(--legal-bg-surface);

  &:focus-within,
  &:focus { outline: none; border-color: var(--legal-border); box-shadow: none; }
}
.toolbar-filters select { height: 31px; border-radius: 5px; font-size: 11px; }
.search-box { color: var(--legal-text-secondary); }
.search-box button { color: var(--legal-text-secondary); }

.archive-table-wrap,
.entity-card,
.reminder-list,
.notification-list article,
.queue-card,
.queue-list,
.candidate-section,
.candidate-modal,
.archive-detail {
  border-color: var(--legal-border);
  background: var(--legal-bg-surface);
}
.archive-table th { color: var(--legal-text-secondary); background: var(--legal-bg-hover); }
.archive-table td,
.reminder-row,
.notification-list article,
.queue-list article,
.candidate-heading,
.candidate-row,
.candidate-modal header,
.archive-detail header,
.field-list,
.field-list > div,
.evidence-row { border-color: var(--legal-border); }
.archive-table tr:hover,
.archive-table tr.selected,
.archive-table tr.row-checked { background: var(--legal-bg-active); }
.selection-cell { width: 36px; padding-left: 12px !important; padding-right: 4px !important; text-align: center; }
.selection-cell input { width: 14px; height: 14px; accent-color: var(--legal-brand); cursor: pointer; }
.content-heading-actions { display: flex; align-items: center; gap: 16px; }
.archive-switch { display: inline-flex; align-items: center; gap: 6px; line-height: 14px; cursor: pointer; }
.archive-switch input { width: 14px; height: 14px; margin: 0; flex: 0 0 14px; accent-color: var(--legal-brand); cursor: pointer; }
.bulk-toolbar { display: flex; align-items: center; gap: 7px; color: var(--legal-text-secondary); font-size: 11px; }
.bulk-toolbar .secondary-button { height: 29px; display: inline-flex; align-items: center; justify-content: center; gap: 5px; text-align: center; }
.bulk-toolbar .bulk-document-action { position: relative; min-width: 96px; padding: 0 28px; line-height: 1; }
.bulk-toolbar .bulk-document-action > span { display: block; width: 100%; line-height: 1; text-align: center; white-space: nowrap; }
.bulk-toolbar .bulk-document-action :deep(.t-icon) { position: absolute; left: 10px; top: 50%; margin: 0; transform: translateY(-50%); }
.bulk-toolbar .secondary-button--danger { color: var(--legal-risk-strong); }
.file-icon,
.entity-avatar { border-color: var(--legal-border); color: var(--legal-ai-strong); background: var(--legal-ai-soft); }
.icon-button { color: var(--legal-text-secondary); background: transparent; }

.status-pill { color: var(--legal-text-secondary); background: var(--legal-bg-hover); }
.status-pill--completed,
.status-pill--active,
.status-pill--handled { color: var(--legal-ai-strong); background: var(--legal-ai-soft); }
.status-pill--failed,
.status-pill--canceled { color: var(--legal-risk-strong); background: var(--legal-risk-soft); }
.status-pill--parsing,
.status-pill--extracting,
.status-pill--linking,
.status-pill--draft,
.status-pill--needs_review { color: var(--legal-warning-strong); background: var(--legal-warning-soft); }

.archive-empty,
.queue-card { color: var(--legal-text-secondary); }
.archive-empty strong,
.queue-card h3,
.reminder-date { color: var(--legal-text-primary); }
.import-progress { border-color: #dfc9a8; color: var(--legal-warning-strong); background: var(--legal-warning-soft); }
.import-progress i { background: #dfc9a8; }
.import-progress i b { background: var(--legal-warning); }
.notification-list article.unread { border-left-color: var(--legal-ai); background: var(--legal-ai-soft); }
.evidence-row em { color: var(--legal-ai-strong); }

.archive-detail { box-shadow: -10px 0 28px rgba(31, 31, 31, .08); }
.candidate-modal blockquote { border-color: var(--legal-border); color: var(--legal-text-secondary); background: var(--legal-bg-hover); }
.modal-backdrop { background: var(--legal-overlay); }
.candidate-modal { box-shadow: 0 16px 42px rgba(31, 31, 31, .12); }
.candidate-modal-body > p,
.candidate-modal label { color: var(--legal-text-secondary); }
.candidate-modal blockquote { border-left-color: var(--legal-warning); }
.candidate-source:hover { color: var(--legal-ai-strong); }
.document-panel { overflow: visible; border: 1px solid var(--legal-border); border-radius: 6px; background: var(--legal-bg-surface); }
.document-panel__heading { min-height: 62px; padding: 0 16px; display: flex; align-items: center; justify-content: space-between; border-bottom: 1px solid var(--legal-border); }
.document-panel__heading h2 { display: inline; margin-right: 8px; font-size: 15px; }
.document-panel__heading > div > span { color: var(--legal-text-secondary); font-size: 11px; }
.document-filters { padding: 12px; display: grid; grid-template-columns: minmax(230px, 1fr) minmax(270px, auto) 145px 175px 135px auto; gap: 8px; border-bottom: 1px solid var(--legal-border); }
.document-search,
.archive-date-filter,
.document-filters > select,
.archive-status-filter summary { height: 38px; box-sizing: border-box; border: 1px solid var(--legal-border); border-radius: 5px; color: var(--legal-text-secondary); background: var(--legal-bg-page); }
.document-search { min-width: 0; padding: 0 9px; display: flex; align-items: center; gap: 7px; }
.document-search input { min-width: 0; flex: 1; border: 0; outline: 0; color: var(--legal-text-primary); background: transparent; font-size: 11px; }
.document-search button { width: 25px; height: 25px; padding: 0; border: 0; border-radius: 4px; color: var(--legal-text-secondary); background: transparent; cursor: pointer; }
.document-search button:hover { color: var(--legal-text-primary); background: var(--legal-bg-hover); }
.archive-date-filter { padding: 0 9px; display: flex; align-items: center; gap: 7px; }
.archive-date-filter input { width: 112px; padding: 0; border: 0; outline: 0; color: var(--legal-text-primary); background: transparent; font-size: 10px; }
.document-filters > select { width: 100%; padding: 0 8px; font-size: 11px; }
.archive-status-filter { position: relative; }
.archive-status-filter summary { padding: 0 10px; display: flex; align-items: center; gap: 8px; list-style: none; cursor: pointer; font-size: 11px; }
.archive-status-filter summary::-webkit-details-marker { display: none; }
.archive-status-filter summary > :last-child { margin-left: auto; }
.archive-status-filter > div { position: absolute; z-index: 20; top: 43px; right: 0; width: 195px; padding: 6px; border: 1px solid var(--legal-border); border-radius: 6px; background: var(--legal-bg-surface); box-shadow: var(--legal-shadow-soft); }
.archive-status-filter label { padding: 7px 8px; display: flex; align-items: center; gap: 8px; border-radius: 4px; cursor: pointer; color: var(--legal-text-primary); font-size: 11px; }
.archive-status-filter label:hover { background: var(--legal-bg-hover); }
.archive-status-filter input { width: 14px; height: 14px; margin: 0; accent-color: var(--legal-brand); }
.archive-status-dots { display: flex; }
.archive-status-dots i { margin-left: -2px; border: 2px solid var(--legal-bg-page); }
.archive-status-filter i,
.archive-status-dots i { width: 9px; height: 9px; flex: none; border-radius: 50%; }
.archive-status-dot--queued { background: var(--legal-status-queued); }
.archive-status-dot--running { background: var(--legal-status-running); }
.archive-status-dot--completed { background: var(--legal-status-completed); }
.archive-status-dot--failed { background: var(--legal-status-failed); }
.archive-status-dot--review { background: var(--legal-status-review); }
.clear-document-filters { align-self: center; border: 0; color: var(--legal-brand); background: transparent; cursor: pointer; font-size: 11px; white-space: nowrap; }
.document-panel .archive-table-wrap { border: 0; border-radius: 0 0 6px 6px; }
.document-panel .archive-table td { height: 66px; }
.archive-document-status { display: inline-flex; align-items: center; gap: 7px; font-size: 10px; font-weight: 700; white-space: nowrap; }
.archive-document-status i { width: 9px; height: 9px; flex: none; border-radius: 50%; }
.archive-document-status--queued { color: var(--legal-status-queued-strong); }
.archive-document-status--queued i { background: var(--legal-status-queued); box-shadow: 0 0 0 3px var(--legal-status-queued-soft); }
.archive-document-status--running { color: var(--legal-status-running-strong); }
.archive-document-status--running i { background: var(--legal-status-running); box-shadow: 0 0 0 3px var(--legal-status-running-soft); animation: archive-status-pulse 1.8s ease-out infinite; }
.archive-document-status--completed { color: var(--legal-status-completed-strong); }
.archive-document-status--completed i { background: var(--legal-status-completed); box-shadow: 0 0 0 3px var(--legal-status-completed-soft); }
.archive-document-status--failed { color: var(--legal-status-failed-strong); }
.archive-document-status--failed i { background: var(--legal-status-failed); box-shadow: 0 0 0 3px var(--legal-status-failed-soft); }
.archive-document-status--review { color: var(--legal-status-review-strong); }
.archive-document-status--review i { background: var(--legal-status-review); box-shadow: 0 0 0 3px var(--legal-status-review-soft); }
@keyframes archive-status-pulse { 0%, 100% { box-shadow: 0 0 0 3px var(--legal-status-running-soft); } 50% { box-shadow: 0 0 0 6px transparent; } }
.archive-load-more { min-height: 52px; display: flex; align-items: center; justify-content: center; gap: 10px; border-top: 1px solid var(--legal-border); }
.archive-load-more span { color: var(--legal-text-secondary); font-size: 10px; }
.archive-empty--error { color: var(--legal-status-failed-strong); }
.archive-drawer-header { min-width: 0; display: flex; align-items: center; gap: 10px; }
.archive-drawer-header > div { min-width: 0; }
.archive-drawer-header strong,
.archive-drawer-header small { display: block; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.archive-drawer-header strong { font-size: 14px; }
.archive-drawer-header small { margin-top: 3px; color: var(--legal-text-secondary); font-size: 10px; }
.detail-status { margin-bottom: 14px; padding: 13px; display: flex; align-items: center; gap: 12px; border: 1px solid var(--legal-border); border-radius: 6px; background: var(--legal-bg-hover); }
.detail-status > span:nth-child(2),
.detail-status time { color: var(--legal-text-secondary); font-size: 10px; }
.detail-status time { margin-left: auto; }
.archive-detail-drawer .detail-actions { flex-wrap: wrap; }
.archive-detail-error { margin-top: 18px; padding: 13px; border: 1px solid var(--legal-status-failed); border-radius: 6px; background: var(--legal-status-failed-soft); }
.archive-detail-error strong,
.archive-detail-error p { color: var(--legal-status-failed-strong); }
.archive-detail-error strong { font-size: 11px; }
.archive-detail-error p { margin-top: 6px; line-height: 1.5; }
.candidate-heading-main .legal-checkbox-hit-area input,
.reminder-list-heading .legal-checkbox-hit-area input {
  width: 14px;
  height: 14px;
  margin: 0;
  accent-color: var(--legal-brand);
  cursor: pointer;
}

.candidate-heading-main .legal-checkbox-hit-area {
  margin-top: -4px;
}

@media (max-width: 900px) {
  .content-heading-actions { align-items: flex-end; flex-direction: column; gap: 8px; }
  .bulk-toolbar { flex-wrap: wrap; justify-content: flex-end; }
  .document-filters { grid-template-columns: 1fr 1fr; }
}
@media (max-width: 700px) {
  .document-panel__heading { padding: 12px; align-items: flex-start; flex-direction: column; gap: 10px; }
  .document-filters { grid-template-columns: 1fr; }
  .archive-date-filter input { width: 100%; }
  .archive-table th:nth-child(3), .archive-table td:nth-child(3),
  .archive-table th:nth-child(4), .archive-table td:nth-child(4),
  .archive-table th:nth-child(6), .archive-table td:nth-child(6) { display: none; }
  .doc-cell { min-width: 210px; }
}
@media (prefers-reduced-motion: reduce) {
  .archive-document-status--running i { animation: none; }
}
</style>

<style lang="less">
/* Keep Smart Archive form controls visually quiet on click and keyboard focus.
   This is intentionally non-scoped so it can override the legal shell's
   teleported/global focus rule. Native caret, typing, and form behavior stay
   unchanged. */
:root[data-workspace-theme='legal'] .archive-page .document-filters select:focus,
:root[data-workspace-theme='legal'] .archive-page .document-filters select:focus-visible,
:root[data-workspace-theme='legal'] .archive-page .document-search:focus-within,
:root[data-workspace-theme='legal'] .archive-page .archive-date-filter:focus-within,
:root[data-workspace-theme='legal'] .archive-page .candidate-modal input:focus,
:root[data-workspace-theme='legal'] .archive-page .candidate-modal input:focus-visible,
:root[data-workspace-theme='legal'] .archive-page .candidate-modal select:focus,
:root[data-workspace-theme='legal'] .archive-page .candidate-modal select:focus-visible {
  border-color: var(--legal-border) !important;
  outline: none !important;
  box-shadow: none !important;
}

:root[data-workspace-theme='legal'] .archive-page .document-search input:focus,
:root[data-workspace-theme='legal'] .archive-page .document-search input:focus-visible,
:root[data-workspace-theme='legal'] .archive-page .archive-date-filter input:focus,
:root[data-workspace-theme='legal'] .archive-page .archive-date-filter input:focus-visible {
  border-color: transparent;
  outline: none !important;
  box-shadow: none !important;
}
</style>
