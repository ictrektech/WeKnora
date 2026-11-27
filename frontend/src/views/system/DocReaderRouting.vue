<template>
  <div class="routing-panel">
    <t-loading v-if="loading" />
    <p v-else-if="!config" role="status">{{ loadError || t('system.docReaderRouting.unavailable') }}</p>
    <template v-else>
      <section v-if="sharing" class="sharing-section">
        <h3>{{ t('system.docReaderRouting.shareTitle') }}</h3>
        <p>{{ t('system.docReaderRouting.shareDescription') }}</p>
        <div class="routing-controls">
          <label>
            <input type="checkbox" :checked="sharing.enabled" :disabled="busy" @change="setSharing(!sharing.enabled)" />
            {{ t('system.docReaderRouting.shareEnabled') }}
          </label>
          <span>{{ t('system.docReaderRouting.pairedCount', { count: sharing.paired_nodes }) }}</span>
          <t-button variant="outline" :loading="issuing" :disabled="busy || !sharing.enabled"
            @click="issueCode">{{ t('system.docReaderRouting.generateCode') }}</t-button>
          <t-popconfirm v-if="sharing.paired_nodes" :content="t('system.docReaderRouting.revokeConfirm')" @confirm="revokeAll">
            <t-button variant="text" :disabled="busy">{{ t('system.docReaderRouting.revoke') }}</t-button>
          </t-popconfirm>
        </div>
        <div v-if="issued" class="issued-code">
          <label>
            {{ t('system.docReaderRouting.validUntil', { time: new Date(issued.expires_at).toLocaleTimeString() }) }}
            <textarea :value="issued.code" rows="2" readonly spellcheck="false" />
          </label>
          <t-button variant="outline" :disabled="busy" @click="copyCode">{{ t('system.docReaderRouting.copyCode') }}</t-button>
        </div>
        <div v-if="sharing.document_processing !== undefined" class="routing-controls">
          <label>
            {{ t('system.docReaderRouting.documentVisionModel') }}
            <select v-model="visionModelId" :disabled="busy || loadingVisionModels || !visionModels.length">
              <option value="" disabled>{{ t('system.docReaderRouting.selectVisionModel') }}</option>
              <option v-if="configuredVisionModelMissing" :value="sharing.document_model_id" disabled>
                {{ sharing.document_model_id }}
              </option>
              <option v-for="model in visionModels" :key="model.id" :value="model.id">
                {{ model.display_name || model.name }}
              </option>
            </select>
          </label>
          <t-button variant="outline" :disabled="busy || !sharing.enabled || !canSaveVisionModel"
            @click="enableDocumentProcessing">
            {{ t(sharing.document_processing ? 'system.docReaderRouting.saveVisionModel' : 'system.docReaderRouting.shareDocumentProcessing') }}
          </t-button>
          <t-button v-if="sharing.document_processing" variant="text" :disabled="busy"
            @click="updateSharing({ document_processing: false })">
            {{ t('system.docReaderRouting.stopDocumentProcessing') }}
          </t-button>
          <span v-if="visionModelFeedback" role="status">{{ visionModelFeedback }}</span>
        </div>
        <div v-if="sharing.text_inference !== undefined" class="text-sharing-block">
          <div class="text-sharing-header">
            <strong>{{ t('system.docReaderRouting.textInferenceTitle') }}</strong>
            <span class="text-sharing-desc">{{ t('system.docReaderRouting.textInferenceDescription') }}</span>
          </div>
          <div v-if="chatModels.length" class="text-model-list">
            <label v-for="model in chatModels" :key="model.id" class="text-model-checkbox">
              <input
                type="checkbox"
                :value="model.id || ''"
                :checked="selectedTextModelIds.includes(model.id || '')"
                :disabled="busy || loadingChatModels"
                @change="toggleTextModel(model.id || '', ($event.target as HTMLInputElement).checked)"
              />
              <span>{{ model.display_name || model.name }}</span>
            </label>
          </div>
          <div class="routing-controls">
            <t-button variant="outline" :disabled="busy || !sharing.enabled || !canSaveTextModels"
              @click="enableTextInference">
              {{ t(sharing.text_inference ? 'system.docReaderRouting.saveTextModels' : 'system.docReaderRouting.shareTextInference') }}
            </t-button>
            <t-button v-if="sharing.text_inference" variant="text" :disabled="busy"
              @click="updateSharing({ text_inference: false })">
              {{ t('system.docReaderRouting.stopTextInference') }}
            </t-button>
            <span v-if="textModelFeedback" role="status">{{ textModelFeedback }}</span>
          </div>
        </div>
      </section>
      <h3>{{ t('system.docReaderRouting.useTitle') }}</h3>
      <p v-if="!saved?.nodes.length">{{ t('system.docReaderRouting.firstNodeHint') }}</p>
      <form class="node-form" @submit.prevent="pair">
        <label>
          {{ t('system.docReaderRouting.pairCode') }}
          <textarea v-model.trim="pairingCode" rows="2" maxlength="4096" required spellcheck="false" :disabled="busy" />
        </label>
        <t-button type="submit" variant="outline" :loading="pairing" :disabled="busy || !pairingCode">
          {{ t('system.docReaderRouting.pairNode') }}
        </t-button>
      </form>
      <div v-if="config.nodes.length" class="routing-controls">
        <label>
          <input v-model="config.enabled" type="checkbox" :disabled="busy || !config.nodes.length" />
          {{ t('system.docReaderRouting.enabled') }}
        </label>
        <label>
          {{ t('system.docReaderRouting.strategy') }}
          <select v-model="config.strategy" :disabled="busy || !config.nodes.length">
            <option value="order">{{ t('system.docReaderRouting.order') }}</option>
            <option value="weight">{{ t('system.docReaderRouting.weighted') }}</option>
          </select>
        </label>
      </div>
      <ol v-if="config.nodes.length" class="node-list">
        <li v-for="(node, index) in config.nodes" :key="node.origin">
          <span class="node-address">{{ node.origin === 'local' ? t('system.docReaderRouting.local') : node.origin }}</span>
          <label v-if="node.origin !== 'local' && (documentProcessingNodes[node.origin] || node.document_processing)">
            <input v-model="node.document_processing" type="checkbox" :disabled="busy" />
            {{ t('system.docReaderRouting.useDocumentProcessing') }}
          </label>
          <label v-if="config.strategy === 'weight'">
            {{ t('system.docReaderRouting.weight') }}
            <input v-model.number="node.weight" type="number" min="1" max="100" step="1" :disabled="busy" />
          </label>
          <div class="node-actions">
            <t-button v-if="node.origin !== 'local'" size="small" variant="text" :disabled="busy"
              @click="checkNode(node)">{{ t('system.docReaderRouting.check') }}</t-button>
            <t-button size="small" variant="text" :disabled="busy || index === 0"
              @click="move(index, -1)">{{ t('system.docReaderRouting.up') }}</t-button>
            <t-button size="small" variant="text" :disabled="busy || index === config.nodes.length - 1"
              @click="move(index, 1)">{{ t('system.docReaderRouting.down') }}</t-button>
            <t-button size="small" variant="text" :disabled="busy"
              @click="removeNode(index)">{{ t('system.docReaderRouting.remove') }}</t-button>
          </div>
          <div v-if="node.origin !== 'local' && node.text_models?.length" class="node-text-models">
            <span class="node-text-models-label">{{ t('system.docReaderRouting.authorizedTextModels') }}:</span>
            <span v-for="m in node.text_models" :key="m.id" class="node-model-pill">
              {{ m.display_name || m.name || m.id }}
            </span>
          </div>
        </li>
      </ol>
      <t-button v-if="config.nodes.length || saved?.nodes.length" variant="outline" :disabled="busy || config.nodes.length >= 16 || config.nodes.some(node => node.origin === 'local')"
        @click="config.nodes.push({ origin: 'local', weight: 1 })">{{ t('system.docReaderRouting.addLocal') }}</t-button>

      <div v-if="config.nodes.length || saved?.nodes.length" class="routing-controls">
        <t-button :loading="saving" :disabled="busy || !dirty" @click="save">{{ t('system.docReaderRouting.save') }}</t-button>
      </div>
      <p v-if="feedback" role="status">{{ feedback }}</p>
    </template>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { get, post, put } from '@/utils/request'
import { listModels, type ModelConfig } from '@/api/model'

interface TextModelInfo {
  id: string
  name: string
  display_name?: string
  context_window?: number
}

interface Node {
  origin: string
  weight: number
  document_processing?: boolean
  text_models?: TextModelInfo[]
}

interface RoutingConfig {
  revision: number
  enabled: boolean
  strategy: 'order' | 'weight'
  nodes: Node[]
}

interface ProbeResult {
  origin: string
  version: string
  engines: { Name: string; Available: boolean }[]
  capabilities?: string[]
  text_models?: TextModelInfo[]
}

interface Sharing {
  enabled: boolean
  paired_nodes: number
  document_processing?: boolean
  document_model_id?: string
  text_inference?: boolean
  text_model_ids?: string[]
}

type SharingUpdate = {
  enabled?: boolean
  revoke?: boolean
  document_processing?: boolean
  document_model_id?: string
  text_inference?: boolean
  text_model_ids?: string[]
}

interface PairingCode {
  code: string
  expires_at: string
}

const sharingEndpoint = '/api/v1/system/admin/docreader-sharing'
const endpoint = '/api/v1/system/admin/docreader-routing'
const { t } = useI18n()
const config = ref<RoutingConfig>()
const loading = ref(false)
const saving = ref(false)
const probing = ref(false)
const pairing = ref(false)
const issuing = ref(false)
const updatingShare = ref(false)
const busy = computed(() => loading.value || saving.value || probing.value || pairing.value || issuing.value || updatingShare.value)
const loadError = ref('')
const feedback = ref('')
const sharing = ref<Sharing>()
const visionModels = ref<ModelConfig[]>([])
const visionModelId = ref('')
const loadingVisionModels = ref(false)
const visionModelFeedback = ref('')
const chatModels = ref<ModelConfig[]>([])
const selectedTextModelIds = ref<string[]>([])
const loadingChatModels = ref(false)
const textModelFeedback = ref('')
const configuredVisionModelMissing = computed(() => Boolean(sharing.value?.document_model_id)
  && !visionModels.value.some(model => model.id === sharing.value?.document_model_id))
const canSaveVisionModel = computed(() => visionModels.value.some(model => model.id === visionModelId.value)
  && (!sharing.value?.document_processing || sharing.value?.document_model_id !== visionModelId.value))
const canSaveTextModels = computed(() => {
  const validSelected = selectedTextModelIds.value.filter(id => chatModels.value.some(m => m.id === id))
  if (!validSelected.length) return false
  if (!sharing.value?.text_inference) return true
  const current = [...(sharing.value?.text_model_ids || [])].sort()
  const next = [...validSelected].sort()
  return JSON.stringify(current) !== JSON.stringify(next)
})
const issued = ref<PairingCode>()
const pairingCode = ref('')
const saved = ref<RoutingConfig>()
const documentProcessingNodes = ref<Record<string, boolean>>({})
const dirty = computed(() => JSON.stringify(config.value) !== JSON.stringify(saved.value))

function setConfig(value?: RoutingConfig) {
  config.value = value
  saved.value = value && JSON.parse(JSON.stringify(value))
}

function describeError(error: unknown) {
  if (error && typeof error === 'object' && 'message' in error && typeof error.message === 'string') return error.message
  return t('system.docReaderRouting.error')
}

function describeRoutingError(error: unknown) {
  if (error && typeof error === 'object' && '$httpStatus' in error && error.$httpStatus === 409) {
    return t('system.docReaderRouting.conflict')
  }
  return describeError(error)
}

async function load() {
  loading.value = true
  feedback.value = ''
  loadError.value = ''
  try {
    const [routing, provider] = await Promise.all([
      get<{ config?: RoutingConfig }>(endpoint),
      get<{ sharing?: Sharing }>(sharingEndpoint),
    ])
    setConfig(routing.config)
    sharing.value = provider.sharing
    issued.value = undefined
    if (provider.sharing?.document_processing !== undefined || provider.sharing?.text_inference !== undefined) {
      void loadShareableModels()
    }
  } catch (error) {
    loadError.value = describeError(error)
    feedback.value = loadError.value
  } finally {
    loading.value = false
  }
}

async function loadShareableModels() {
  loadingVisionModels.value = true
  loadingChatModels.value = true
  visionModelFeedback.value = ''
  textModelFeedback.value = ''
  try {
    const all = (await listModels()).filter(model => model.id && model.status === 'active')
    visionModels.value = all.filter(model =>
      model.type === 'VLLM' || (model.type === 'KnowledgeQA' && model.parameters?.supports_vision === true))
    visionModelId.value = sharing.value?.document_model_id
      || visionModels.value.find(model => model.default_for?.includes('VLLM'))?.id
      || ''
    if (!visionModels.value.length) visionModelFeedback.value = t('system.docReaderRouting.noVisionModels')

    chatModels.value = all.filter(model => model.type === 'KnowledgeQA')
    selectedTextModelIds.value = [...(sharing.value?.text_model_ids || [])]
    if (!chatModels.value.length) textModelFeedback.value = t('system.docReaderRouting.noTextModels')
  } catch (error) {
    const msg = describeError(error)
    visionModelFeedback.value = msg
    textModelFeedback.value = msg
  } finally {
    loadingVisionModels.value = false
    loadingChatModels.value = false
  }
}

function toggleTextModel(id: string, checked: boolean) {
  const next = new Set(selectedTextModelIds.value)
  if (checked) next.add(id)
  else next.delete(id)
  selectedTextModelIds.value = [...next]
}

function enableTextInference() {
  if (!canSaveTextModels.value) return
  const validSelected = selectedTextModelIds.value.filter(id => chatModels.value.some(m => m.id === id))
  return updateSharing({ text_inference: true, text_model_ids: validSelected })
}

async function pair() {
  const draft = config.value
  const baseline = saved.value
  if (busy.value || !draft || !baseline || !pairingCode.value) return
  const hadDraft = dirty.value
  pairing.value = true
  feedback.value = ''
  try {
    const result = await post<{ config: RoutingConfig; node: ProbeResult }>(endpoint + '/pair', {
      code: pairingCode.value, revision: baseline.revision, enable_if_empty: true,
    })
    documentProcessingNodes.value[result.node.origin] = result.node.capabilities?.includes('document_processing') ?? false
    setConfig(result.config)
    const added = result.config.nodes.find(node => node.origin === result.node.origin)
    if (hadDraft && added) {
      // Apply the saved node and new revision while retaining unrelated draft edits.
      const index = draft.nodes.findIndex(node => node.origin === added.origin)
      const nodes = [...draft.nodes]
      if (index < 0) nodes.push(added)
      else nodes[index] = { ...added, weight: nodes[index].weight, document_processing: nodes[index].document_processing }
      config.value = {
        ...result.config,
        enabled: draft.enabled === baseline.enabled ? result.config.enabled : draft.enabled,
        strategy: draft.strategy === baseline.strategy ? result.config.strategy : draft.strategy,
        nodes,
      }
    }
    pairingCode.value = ''
    feedback.value = t('system.docReaderRouting.pairSuccess', { address: result.node.origin })
    if (dirty.value) feedback.value += ' ' + t('system.docReaderRouting.unsavedRouting')
  } catch (error) {
    feedback.value = describeRoutingError(error)
  } finally {
    pairing.value = false
  }
}

async function issueCode() {
  issuing.value = true
  feedback.value = ''
  try {
    issued.value = await post<PairingCode>(sharingEndpoint + '/code', { origin: window.location.origin })
  } catch (error) {
    issued.value = undefined
    feedback.value = describeError(error)
  } finally {
    issuing.value = false
  }
}

async function copyCode() {
  if (!issued.value) return
  try {
    await navigator.clipboard.writeText(issued.value.code)
    feedback.value = t('system.docReaderRouting.copied')
  } catch {
    feedback.value = t('system.docReaderRouting.manualCopy')
  }
}

async function updateSharing(value: SharingUpdate) {
  updatingShare.value = true
  feedback.value = ''
  try {
    const result = await put<{ sharing: Sharing }>(sharingEndpoint, value)
    sharing.value = result.sharing
    selectedTextModelIds.value = [...(result.sharing.text_model_ids || [])]
    issued.value = undefined
  } catch (error) {
    feedback.value = describeError(error)
  } finally {
    updatingShare.value = false
  }
}

function enableDocumentProcessing() {
  if (!canSaveVisionModel.value) return
  return updateSharing({ document_processing: true, document_model_id: visionModelId.value })
}

function setSharing(enabled: boolean) {
  return updateSharing({ enabled })
}

function revokeAll() {
  return updateSharing({ revoke: true })
}

async function checkNode(node: Node) {
  probing.value = true
  feedback.value = ''
  try {
    const result = await post<ProbeResult>(endpoint + '/probe', node)
    const canProcess = result.capabilities?.includes('document_processing') ?? false
    const canText = result.capabilities?.includes('text_inference') ?? false
    documentProcessingNodes.value[node.origin] = canProcess
    const discoveredModels = result.text_models || []
    node.text_models = discoveredModels
    const savedNode = saved.value?.nodes.find(n => n.origin === node.origin)
    if (savedNode) savedNode.text_models = JSON.parse(JSON.stringify(discoveredModels))
    if (canText && canProcess) {
      feedback.value = t('system.docReaderRouting.documentAndTextReady', {
        address: result.origin,
        count: discoveredModels.length,
      })
    } else if (canText) {
      feedback.value = t('system.docReaderRouting.textInferenceReady', {
        address: result.origin,
        count: discoveredModels.length,
      })
    } else if (canProcess) {
      feedback.value = t('system.docReaderRouting.documentProcessingReady', { address: result.origin })
    } else {
      feedback.value = t('system.docReaderRouting.nodeReady', { address: result.origin })
    }
  } catch (error) {
    feedback.value = describeError(error)
  } finally {
    probing.value = false
  }
}

function move(index: number, direction: number) {
  if (!config.value) return
  const nodes = config.value.nodes
  const selected = nodes[index]
  if (!selected || !nodes[index + direction]) return
  nodes.splice(index, 1)
  nodes.splice(index + direction, 0, selected)
}

function removeNode(index: number) {
  if (!config.value) return
  config.value.nodes.splice(index, 1)
  if (!config.value.nodes.length) config.value.enabled = false
}

async function save() {
  if (!config.value || !dirty.value) return
  saving.value = true
  feedback.value = ''
  try {
    const result = await put<{ config: RoutingConfig }>(endpoint, config.value)
    setConfig(result.config)
    feedback.value = t('system.docReaderRouting.saved')
  } catch (error) {
    feedback.value = describeRoutingError(error)
  } finally {
    saving.value = false
  }
}

onMounted(load)
</script>

<style scoped lang="less">
.routing-panel { display: grid; gap: 16px; }
.routing-controls, .node-actions, .issued-code, .node-list li { display: flex; align-items: center; gap: 12px; flex-wrap: wrap; }
.node-form { display: flex; align-items: end; gap: 12px; flex-wrap: wrap; }
.node-form label, .issued-code label { display: grid; gap: 6px; flex: 1; min-width: 220px; }
.sharing-section { display: grid; gap: 12px; border-bottom: 1px solid var(--td-component-border); padding-bottom: 16px; }
h3, p { margin: 0; }
textarea { resize: vertical; }
.node-list { padding-inline-start: 24px; }
.node-list li { margin-block: 8px; }
.node-address { min-width: 160px; overflow-wrap: anywhere; }
.text-sharing-block { display: grid; gap: 8px; padding-top: 8px; border-top: 1px dashed var(--td-component-border); }
.text-sharing-header { display: flex; flex-wrap: wrap; align-items: baseline; gap: 8px; }
.text-sharing-desc { font-size: var(--app-text-xs); color: var(--td-text-color-secondary); }
.text-model-list { display: flex; flex-wrap: wrap; gap: 10px 16px; }
.text-model-checkbox { display: inline-flex; align-items: center; gap: 6px; cursor: pointer; }
.node-text-models { flex-basis: 100%; display: flex; flex-wrap: wrap; align-items: center; gap: 6px; font-size: var(--app-text-xs); color: var(--td-text-color-secondary); }
.node-model-pill { padding: 1px 8px; border-radius: var(--app-radius-lg); background: var(--td-bg-color-secondarycontainer); color: var(--td-text-color-primary); }
input, select, textarea { background: var(--td-bg-color-container); color: var(--td-text-color-primary); border: 1px solid var(--td-component-border); border-radius: var(--app-radius-xs); padding: 5px 8px; }
input[type='number'] { width: 70px; }
</style>
