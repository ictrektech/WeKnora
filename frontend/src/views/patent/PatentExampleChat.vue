<template>
  <section class="example-chat chat" :class="{ 'has-source-panel': libraryOpen }" data-assistant-ignore="page" data-testid="patent-examples">
    <div class="example-main">
      <div class="chat-topbar">
        <ChatHeader :session="headerSession" read-only />
        <span class="example-badge">{{ t('patentExamples.badge') }}</span>
        <t-button v-if="publications.length" class="example-library-button" data-testid="patent-example-knowledge" :aria-label="t('patentExamples.knowledgeBase')" variant="text" size="small" @click="openLibrary">
          <t-icon name="folder" /><span class="example-library-label">{{ t('patentExamples.knowledgeBase') }} · {{ publications.length }}</span>
        </t-button>
        <t-popup v-if="example" trigger="click" placement="bottom-right" destroy-on-close>
          <t-button variant="text" size="small" :aria-label="t('patentExamples.details')"><t-icon name="info-circle" /></t-button>
          <template #content>
            <div class="example-information" data-assistant-ignore>
              <p>{{ t('patentExamples.notice') }}</p>
              <p>{{ t('patentExamples.model') }}: {{ example.model }}</p>
              <p>{{ t('patentExamples.recordedAt') }}: {{ example.recorded_at.slice(0, 10) }}</p>
              <p>{{ example.limitations }}</p>
              <details v-if="example.attribution">
                <summary>{{ t('patentExamples.attribution') }}</summary>
                <pre>{{ example.attribution }}</pre>
              </details>
            </div>
          </template>
        </t-popup>
      </div>

      <div ref="scrollBox" class="chat_scroll_box" @scroll.passive="hideCitation">
        <div class="msg_list" @click.capture="openCitation" @mouseover.capture="hoverCitation" @mouseout.capture="leaveCitation" @focusin.capture="focusCitation" @focusout.capture="leaveCitation">
          <p v-if="!visible" role="status">{{ t('patentExamples.disabled') }}</p>
          <p v-else-if="loading" role="status">{{ t('patentExamples.loading') }}</p>
          <div v-else-if="failed" role="alert">
            <p>{{ t('patentExamples.loadFailed') }}</p>
            <t-button @click="load">{{ t('patentWorkspace.retry') }}</t-button>
          </div>
          <template v-else-if="example">
            <div v-for="(message, index) in messages" :key="`${example.id}-${index}`" class="message-row" :data-message-role="message.role" data-testid="patent-example-message">
              <UserMessage v-if="message.role === 'user'" :content="message.content"
                :session-id="example.id" :attachments="index === 0 ? attachments : []" />
              <BotMessage v-else :content="message.content" :session="message" :show-toolbar="false" />
            </div>
          </template>
        </div>
      </div>

      <footer class="example-footer" role="status">
        <span><t-icon name="lock-on" /> {{ t('patentExamples.footer') }}</span>
        <t-button v-if="canEdit" size="small" variant="text" @click="ui.openSettings('integration-patent')">{{ t('patentExamples.settings') }}</t-button>
      </footer>
    </div>

    <aside v-if="libraryOpen" class="example-source-panel" role="complementary" :aria-label="t('chat.referencesDrawerTitle')" data-assistant-ignore data-testid="patent-example-source-panel">
      <header class="source-panel-header">
        <t-button v-if="selectedDocumentId" variant="text" size="small" :aria-label="t('patentExamples.backToDocuments')" @click="openLibrary"><t-icon name="chevron-left" />{{ t('patentExamples.backToDocuments') }} · {{ publications.length }}</t-button>
        <strong v-else>{{ t('chat.referencesDrawerTitle') }} · {{ publications.length }}</strong>
        <t-button variant="text" shape="square" size="small" :aria-label="t('common.close')" @click="closeSourcePanel"><t-icon name="close" /></t-button>
      </header>
      <div v-if="previewDocument?.document?.publication" class="publication-file">
        <DocumentFileIcon :file-name="previewDocument.filename" /><strong>{{ previewDocument.filename }}</strong>
      </div>
      <div ref="previewContainer" class="source-panel-body" data-testid="patent-example-preview">
        <p class="preview-hint">{{ t('patentExamples.externalNotice') }}</p>
        <div v-if="!selectedDocumentId" class="example-document-list" data-testid="patent-example-library">
          <div class="library-heading"><t-icon name="folder" /><strong>{{ t('patentExamples.documents') }}</strong><t-tag variant="light">{{ t('patentExamples.badge') }}</t-tag></div>
          <button v-for="doc in publications" :key="doc.id" class="example-document-row" @click="openPublication(doc.id)">
            <DocumentFileIcon :file-name="doc.filename" source-type="url" />
            <span class="document-name"><strong>{{ doc.filename }}</strong><span>{{ doc.publication?.title }}</span></span>
            <span class="document-status">{{ t('patentExamples.externalOnly') }} <t-icon name="chevron-right" /></span>
          </button>
        </div>
        <p v-if="previewLoading" role="status">{{ t('patentExamples.loading') }}</p>
        <div v-else-if="previewFailed" role="alert">
          <p>{{ t('patentExamples.loadFailed') }}</p>
          <t-button @click="preview(selectedDocumentId)">{{ t('patentWorkspace.retry') }}</t-button>
        </div>
        <template v-else-if="previewDocument?.document?.publication">
          <div class="publication-details" data-testid="patent-example-publication">
            <h3>{{ previewDocument.document.publication.title }}</h3>
            <dl>
              <dt>{{ t('patentExamples.publicationNumber') }}</dt><dd>{{ previewDocument.document.id }}</dd>
              <dt>{{ t('patentExamples.applicant') }}</dt><dd>{{ previewDocument.document.publication.applicant }}</dd>
              <dt>{{ t('patentExamples.applicationDate') }}</dt><dd>{{ previewDocument.document.publication.application_date }}</dd>
              <dt>{{ t('patentExamples.publicationDate') }}</dt><dd>{{ previewDocument.document.publication.publication_date }}</dd>
            </dl>
            <a v-if="sourceUrl" class="external-source" :href="sourceUrl" target="_blank" rel="noopener noreferrer">{{ t('patentExamples.openOriginal') }} <t-icon name="jump" /></a>
            <h4>{{ t('patentExamples.evidenceSummary') }}</h4>
            <div v-for="evidence in previewDocument.document.publication.evidence" :key="evidence.id" :data-evidence-id="evidence.id" :class="['evidence-summary', { 'is-active': selectedEvidence === evidence.id }]">
              <strong>{{ evidence.location }}</strong><p>{{ evidence.summary }}</p>
            </div>
          </div>
        </template>
      </div>
    </aside>

    <t-drawer v-if="mode === 'patent_writing'" :visible="attachmentDrawer.visible.value" :header="attachmentDrawer.target.value?.fileName || t('patentExamples.materials')" :footer="false" size="min(654px, 94vw)" destroy-on-close @close="closePreview">
      <div data-assistant-ignore data-testid="patent-example-preview">
        <p class="preview-hint">{{ t('patentExamples.settingsHint') }}</p>
        <p v-if="previewLoading" role="status">{{ t('patentExamples.loading') }}</p>
        <div v-else-if="previewFailed" role="alert">
          <p>{{ t('patentExamples.loadFailed') }}</p>
          <t-button @click="preview(selectedDocumentId)">{{ t('patentWorkspace.retry') }}</t-button>
        </div>
        <DocMarkdown v-else-if="previewDocument" :source="previewDocument.content" />
      </div>
    </t-drawer>
    <ChatCitationFloat :float="citationFloat" exclude-from-assistant :on-enter="cancelCitationClose" :on-leave="scheduleCitationClose" />
  </section>
</template>

<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { useAuthStore } from '@/stores/auth'
import { useUIStore } from '@/stores/ui'
import { usePatentWorkspaceStore } from '@/stores/patentWorkspace'
import { getPatentExample, getPatentExampleDocument, type PatentExample, type PatentExampleDocument } from '@/api/patent-examples'
import { provideChatAttachmentPreviewDrawer } from '@/composables/useChatAttachmentPreviewDrawer'
import { workspaceHome, type PatentMode } from '@/utils/workspaceMode'
import ChatHeader from '@/components/ChatHeader.vue'
import UserMessage from '@/views/chat/components/usermsg.vue'
import BotMessage from '@/views/chat/components/botmsg.vue'
import DocMarkdown from '@/views/chat/components/tool-results/DocMarkdown.vue'
import DocumentFileIcon from '@/views/knowledge/components/DocumentFileIcon.vue'
import ChatCitationFloat from '@/components/ChatCitationFloat.vue'
import type { CitationFloatState } from '@/composables/useChatCitationPopover'
import { safeDocUrl } from '@/utils/catalogToolDisplay'
import { formatExampleCitations, resolveExampleCitation } from './patentExampleCitations'

const props = defineProps<{ mode: PatentMode }>()
const { t } = useI18n()
const route = useRoute()
const router = useRouter()
const auth = useAuthStore()
const ui = useUIStore()
const workspace = usePatentWorkspaceStore()
const attachmentDrawer = provideChatAttachmentPreviewDrawer()
const canEdit = computed(() => auth.canAccessAllTenants || auth.hasRole('admin'))
const visible = computed(() => workspace.loaded && workspace.enabled && workspace.examplesEnabled && !workspace.loadError)
const example = ref<PatentExample | null>(null)
const headerSession = computed(() => ({ id: example.value?.id || '', title: example.value?.title || t('patentExamples.title') }))
const messages = computed(() => (example.value?.messages || []).map(message => {
  const content = message.role === 'assistant' ? formatExampleCitations(message.content, publications.value) : message.content
  return { ...message, content, is_completed: true, isAgentMode: message.role === 'assistant',
    agentEventStream: [
      ...(message.events || []).map(event => ({ ...event,
        ...(event.content ? { content: formatExampleCitations(event.content, publications.value) } : {}),
      })),
      { type: 'agent_complete', total_duration_ms: message.agent_duration_ms || 0 },
      { type: 'answer', content, done: true },
    ],
  }
}))
const publications = computed(() => (example.value?.documents || []).filter(doc => doc.publication))
const attachments = computed(() => (example.value?.documents || []).filter(doc => !doc.publication).map(doc => ({ id: doc.id, file_name: doc.filename, file_type: 'md' })))
const loading = ref(false)
const failed = ref(false)
const scrollBox = ref<HTMLElement | null>(null)
const previewDocument = ref<{ filename: string; content: string; document: PatentExampleDocument } | null>(null)
const libraryOpen = ref(false)
const selectedDocumentId = ref('')
const selectedEvidence = ref('')
const previewContainer = ref<HTMLElement | null>(null)
const sourceUrl = computed(() => safeDocUrl(previewDocument.value?.document?.source || ''))
const previewLoading = ref(false)
const previewFailed = ref(false)
const citationFloat = ref<CitationFloatState>({ visible: false, type: 'kb', top: 0, left: 0, title: '', content: '', url: '', loading: false, error: '' })
let citationCloseTimer: ReturnType<typeof setTimeout> | undefined
let citationTrigger: HTMLElement | null = null
let restoringCitationFocus = false
let generation = 0
let previewGeneration = 0

function closePreview() {
  hideCitation()
  previewGeneration++
  attachmentDrawer.close()
  libraryOpen.value = false
  selectedDocumentId.value = ''
  selectedEvidence.value = ''
  previewDocument.value = null
  previewLoading.value = false
  previewFailed.value = false
}

async function closeSourcePanel() {
  const trigger = citationTrigger
  closePreview()
  await nextTick()
  if (trigger?.isConnected && visible.value) {
    restoringCitationFocus = true
    trigger.focus({ preventScroll: true })
    restoringCitationFocus = false
  }
}

function cancelCitationClose() {
  clearTimeout(citationCloseTimer)
  citationCloseTimer = undefined
}

function hideCitation() {
  cancelCitationClose()
  citationFloat.value.visible = false
  citationFloat.value.content = ''
}

function scheduleCitationClose() {
  cancelCitationClose()
  citationCloseTimer = setTimeout(hideCitation, 120)
}

function citationLink(event: Event): HTMLAnchorElement | null {
  return event.target instanceof Element ? event.target.closest('a[href^="#example-source/"]') : null
}

function showCitation(link: HTMLAnchorElement) {
  if (!visible.value) return
  const source = resolveExampleCitation(link.getAttribute('href') || '', publications.value)
  if (!source) {
    hideCitation()
    return
  }
  cancelCitationClose()
  const rect = link.getBoundingClientRect()
  citationFloat.value = { visible: true, type: 'kb', title: source.document.filename,
    content: `${source.evidence.location}\n${t('patentExamples.evidenceSummary')}\n${source.evidence.summary}`,
    top: Math.max(8, rect.bottom + 240 > innerHeight ? rect.top - 230 : rect.bottom + 8) + scrollY,
    left: Math.max(8, Math.min(rect.left, innerWidth - 316)) + scrollX,
    url: '', loading: false, error: '',
  }
}

function hoverCitation(event: MouseEvent) {
  const link = citationLink(event)
  if (link) showCitation(link)
}

function focusCitation(event: FocusEvent) {
  if (restoringCitationFocus) return
  const link = citationLink(event)
  if (link) showCitation(link)
}

function leaveCitation(event: MouseEvent | FocusEvent) {
  const target = event.relatedTarget
  if (target instanceof Element && target.closest('.citation-example, .chat-citation-float')) return
  scheduleCitationClose()
}

async function load() {
  const current = ++generation
  example.value = null
  failed.value = false
  loading.value = false
  closePreview()
  citationTrigger = null
  if (!visible.value || !auth.effectiveTenantId) return
  loading.value = true
  try {
    const response = await getPatentExample(String(route.params.exampleId || ''))
    if (current !== generation) return
    if (response.data.workspace_mode !== props.mode) {
      await router.replace(`${workspaceHome(response.data.workspace_mode)}/examples/${response.data.id}`)
      return
    }
    example.value = response.data
    await nextTick()
    if (current === generation && scrollBox.value) scrollBox.value.scrollTop = 0
  } catch {
    if (current === generation) failed.value = true
  } finally {
    if (current === generation) loading.value = false
  }
}

async function preview(id: string) {
  if (!example.value || !visible.value) return
  const current = ++previewGeneration
  selectedDocumentId.value = id
  previewDocument.value = null
  previewFailed.value = false
  previewLoading.value = true
  try {
    const response = await getPatentExampleDocument(example.value.id, id)
    if (current === previewGeneration && visible.value) {
      previewDocument.value = response.data
      await nextTick()
      if (current === previewGeneration && selectedEvidence.value) {
        const target = [...(previewContainer.value?.querySelectorAll<HTMLElement>('[data-evidence-id]') || [])]
          .find(el => el.dataset.evidenceId === selectedEvidence.value)
        if (target && previewContainer.value) previewContainer.value.scrollTop = target.offsetTop - previewContainer.value.offsetTop - 12
      }
    }
  } catch {
    if (current === previewGeneration) previewFailed.value = true
  } finally {
    if (current === previewGeneration) previewLoading.value = false
  }
}

function openLibrary(event?: Event) {
  if (!visible.value || !example.value) return
  if (!libraryOpen.value && event?.currentTarget instanceof HTMLElement) citationTrigger = event.currentTarget
  closePreview()
  libraryOpen.value = true
}

function openPublication(id: string, evidence = '') {
  const doc = publications.value.find(doc => doc.id === id)
  if (!doc || !visible.value || (evidence && !doc.publication?.evidence.some(item => item.id === evidence))) return
  hideCitation()
  libraryOpen.value = true
  selectedEvidence.value = evidence
  void preview(id)
}

// Intercept only this case's static source links; never invoke business citation APIs.
function openCitation(event: MouseEvent) {
  const link = citationLink(event)
  const href = link?.getAttribute('href') || ''
  if (!href.startsWith('#example-source/')) return
  event.preventDefault()
  event.stopPropagation()
  const source = resolveExampleCitation(href, publications.value)
  if (source) {
    citationTrigger = link
    openPublication(source.document.id, source.evidence.id)
  }
}

function onKeydown(event: KeyboardEvent) {
  if (event.key !== 'Escape' || event.defaultPrevented) return
  hideCitation()
  if (libraryOpen.value) {
    void closeSourcePanel()
    hideCitation()
    event.preventDefault()
  }
}

// Reuse real attachment cards, but resolve their preview only through the example API.
watch(() => attachmentDrawer.target.value, target => {
  if (target && target.sessionId === example.value?.id) void preview(target.attachmentId)
})
watch(() => [props.mode, route.params.exampleId, auth.effectiveTenantId, visible.value], load, { immediate: true, flush: 'sync' })
const refreshAccess = () => { void workspace.load(true).catch(() => undefined) }
onMounted(() => {
  window.addEventListener('focus', refreshAccess)
  window.addEventListener('keydown', onKeydown)
  window.addEventListener('resize', hideCitation)
})
onBeforeUnmount(() => {
  generation++
  closePreview()
  window.removeEventListener('focus', refreshAccess)
  window.removeEventListener('keydown', onKeydown)
  window.removeEventListener('resize', hideCitation)
})
</script>

<style scoped lang="less">
.example-chat { position: relative; display: flex; flex: 1; min-width: 0; min-height: 0; height: 100%; overflow: hidden; color: var(--td-text-color-primary); }
.example-main { display: flex; flex: 1; flex-direction: column; min-width: 0; min-height: 0; height: 100%; }
.example-source-panel { display: flex; flex-direction: column; flex: 0 0 44%; min-width: 320px; max-width: 640px; border-left: 1px solid var(--td-component-stroke); background: var(--td-bg-color-container); }
.source-panel-header { display: flex; align-items: center; justify-content: space-between; flex-shrink: 0; min-height: var(--app-chat-header-height); gap: 8px; padding: 0 12px; border-bottom: 1px solid var(--td-component-stroke); font-size: var(--app-text-sm); box-sizing: border-box; }
.source-panel-body { flex: 1; min-height: 0; overflow: auto; overscroll-behavior-y: contain; padding: 16px; }
.publication-file { display: flex; align-items: center; gap: 10px; padding: 12px 16px; border-bottom: 1px solid var(--td-component-stroke); font-size: var(--app-text-sm); }
.chat-topbar { display: flex; align-items: center; gap: 8px; flex: 0 0 var(--app-chat-header-height); width: 100%; min-width: 0; padding: 0 12px 0 20px; box-sizing: border-box; border-bottom: 1px solid var(--td-component-stroke); background: var(--td-bg-color-container); }
.example-badge { margin-left: auto; flex-shrink: 0; color: var(--td-text-color-secondary); font-size: var(--app-text-sm); }
.chat_scroll_box { flex: 1; min-height: 0; width: 100%; overflow-y: auto; overscroll-behavior-y: contain; }
.msg_list { display: flex; flex-direction: column; gap: 16px; max-width: 1000px; width: 100%; margin: 0 auto; padding: 24px 20px; box-sizing: border-box; }
.message-row { display: flex; flex-direction: column; width: 100%; min-width: 0; }
.example-footer { width: 100%; max-width: 1000px; box-sizing: border-box; padding: 12px 20px; color: var(--td-text-color-secondary); font-size: var(--app-text-sm); line-height: 1.6; text-align: center; }
.example-information { max-width: min(480px, 85vw); max-height: 65vh; overflow: auto; font-size: var(--app-text-md); line-height: 1.6; }
.example-information pre { white-space: pre-wrap; overflow-wrap: anywhere; font-size: var(--app-text-sm); }
.preview-hint { color: var(--td-text-color-secondary); font-size: var(--app-text-sm); }
.example-library-label { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; max-width: 200px; }
.library-heading { display: flex; align-items: center; gap: 8px; padding: 16px 0; }
.example-document-row { display: flex; align-items: center; gap: 14px; width: 100%; padding: 16px 8px; border: 0; border-bottom: 1px solid var(--td-component-stroke); background: transparent; color: inherit; text-align: left; cursor: pointer; font: inherit; }
.example-document-row:hover { background: var(--td-bg-color-container-hover); }
.example-document-row:focus-visible { outline: 2px solid var(--td-brand-color); outline-offset: -2px; }
.document-name { display: flex; flex: 1; min-width: 0; flex-direction: column; gap: 6px; overflow-wrap: anywhere; }
.document-name > span, .document-status { color: var(--td-text-color-secondary); font-size: var(--app-text-sm); }
.document-status { display: flex; gap: 4px; align-items: center; flex-shrink: 0; }
.publication-details { overflow-wrap: anywhere; line-height: 1.7; }
.publication-details dl { display: grid; grid-template-columns: minmax(70px, auto) 1fr; gap: 8px 20px; padding: 16px; border-radius: var(--app-radius-md); background: var(--td-bg-color-secondarycontainer); }
.publication-details dt { color: var(--td-text-color-secondary); }
.publication-details dd { margin: 0; }
.external-source { display: inline-flex; gap: 6px; align-items: center; color: var(--td-brand-color); }
.evidence-summary { padding: 12px; margin-top: 8px; border-left: 3px solid var(--td-component-stroke); }
.evidence-summary p { margin: 6px 0 0; }
.evidence-summary.is-active { border-color: var(--td-brand-color); background: var(--td-brand-color-light); }
:deep(.citation-example) { max-width: 110px; color: var(--td-text-color-secondary) !important; background: var(--td-bg-color-secondarycontainer); text-decoration: none !important; cursor: pointer; }
:deep(.citation-example .citation-text) { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
:deep(.citation-example:hover) { background: var(--td-bg-color-container-hover); }
:deep(.citation-example:focus-visible) { outline: 2px solid var(--td-brand-color); outline-offset: 2px; }
:deep(.doc-md) { max-height: none; overflow: visible; }
@media (max-width: 900px) {
  .example-source-panel { position: absolute; inset: 0; z-index: 20; width: 100%; min-width: 0; max-width: none; border-left: 0; }
  .has-source-panel .example-main { visibility: hidden; }
}
@media (max-width: 680px) {
  .chat-topbar { padding-left: 12px; }
  .example-library-label { display: none; }
  .msg_list { padding: 16px 12px; }
  .example-footer { padding: 10px 12px; }
  .example-document-row { flex-wrap: wrap; gap: 10px; }
  .document-status { margin-left: 46px; }
}
</style>
