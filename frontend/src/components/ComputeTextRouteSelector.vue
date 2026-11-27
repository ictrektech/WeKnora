<template>
  <div v-if="shouldRender" class="compute-route-selector">
    <div class="compute-route-header">
      <label class="compute-route-label">{{ label }}</label>
      <p class="compute-route-desc">{{ description }}</p>
    </div>

    <div class="compute-route-controls">
      <t-select
        :value="primaryKey"
        clearable
        filterable
        :placeholder="placeholder"
        style="width: 100%;"
        @change="onPrimaryChange"
      >
        <t-option
          v-for="opt in optionList"
          :key="opt.key"
          :value="opt.key"
          :label="opt.label"
        >
          <div class="compute-option">
            <span class="compute-option__model">{{ opt.modelLabel }}</span>
            <span class="compute-option__node">{{ opt.nodeLabel }}</span>
          </div>
        </t-option>
      </t-select>
    </div>

    <div v-if="targets.length > 0" class="compute-route-details">
      <div v-if="fallbackOptions.length > 0 || targets.length > 1" class="compute-route-fallbacks">
        <div v-if="targets.length > 1" class="compute-route-list">
          <div v-for="(item, idx) in targets" :key="targetKey(item)" class="compute-route-item">
            <span class="compute-route-order">#{{ idx + 1 }}</span>
            <span class="compute-route-item-label">{{ formatTargetLabel(item) }}</span>
            <div class="compute-route-item-actions">
              <t-button size="small" variant="text" :disabled="idx === 0" @click="moveTarget(idx, -1)">
                {{ t('system.docReaderRouting.up') }}
              </t-button>
              <t-button size="small" variant="text" :disabled="idx === targets.length - 1" @click="moveTarget(idx, 1)">
                {{ t('system.docReaderRouting.down') }}
              </t-button>
              <t-button size="small" variant="text" @click="removeTarget(idx)">
                {{ t('system.docReaderRouting.remove') }}
              </t-button>
            </div>
          </div>
        </div>
        <div v-if="fallbackOptions.length > 0" class="compute-route-add">
          <t-select
            :value="pendingFallbackKey"
            clearable
            filterable
            :placeholder="t('system.docReaderRouting.addFallbackTargetPlaceholder')"
            style="flex: 1;"
            @change="onAddFallback"
          >
            <t-option
              v-for="opt in fallbackOptions"
              :key="opt.key"
              :value="opt.key"
              :label="opt.label"
            />
          </t-select>
        </div>
      </div>

      <label class="compute-route-checkbox">
        <input
          type="checkbox"
          :checked="fallbackToLocal"
          @change="onFallbackToLocalChange(($event.target as HTMLInputElement).checked)"
        />
        <span>{{ t('system.docReaderRouting.fallbackToLocal') }}</span>
      </label>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import type {
  ComputeTextNodeModels,
  ComputeTextRouteConfig,
  ComputeTextTarget,
} from '@/api/system/computeText'

interface Props {
  modelValue?: ComputeTextRouteConfig | null
  computeNodes?: ComputeTextNodeModels[]
  label: string
  description: string
  placeholder: string
}

const props = withDefaults(defineProps<Props>(), {
  modelValue: null,
  computeNodes: () => [],
})

const emit = defineEmits<{
  'update:modelValue': [value: ComputeTextRouteConfig | null]
}>()

const { t } = useI18n()
const pendingFallbackKey = ref('')

const targets = computed<ComputeTextTarget[]>(() => props.modelValue?.targets || [])
const fallbackToLocal = computed<boolean>(() => Boolean(props.modelValue?.fallback_to_local))

const shouldRender = computed(() =>
  (props.computeNodes && props.computeNodes.length > 0) || targets.value.length > 0,
)

function targetKey(target: Pick<ComputeTextTarget, 'node_origin' | 'model_id'>): string {
  return `${target.node_origin}||${target.model_id}`
}

function parseKey(key: string): { node_origin: string; model_id: string } | null {
  const sep = key.indexOf('||')
  if (sep <= 0) return null
  return {
    node_origin: key.slice(0, sep),
    model_id: key.slice(sep + 2),
  }
}

function formatNodeHost(origin: string): string {
  if (origin === 'local') return t('system.docReaderRouting.localExecution')
  return origin.replace(/^https?:\/\//, '')
}

interface SelectOption {
  key: string
  node_origin: string
  model_id: string
  modelLabel: string
  nodeLabel: string
  label: string
}

const optionList = computed<SelectOption[]>(() => {
  const seen = new Set<string>()
  const opts: SelectOption[] = []
  for (const node of props.computeNodes || []) {
    const nodeLabel = formatNodeHost(node.origin)
    for (const model of node.models || []) {
      const key = targetKey({ node_origin: node.origin, model_id: model.id })
      if (seen.has(key)) continue
      seen.add(key)
      const modelLabel = model.display_name?.trim() || model.name || model.id
      opts.push({
        key,
        node_origin: node.origin,
        model_id: model.id,
        modelLabel,
        nodeLabel,
        label: `${modelLabel} (${nodeLabel})`,
      })
    }
  }
  // Keep any already-saved targets selectable/visible even if a node is temporarily offline.
  for (const saved of targets.value) {
    const key = targetKey(saved)
    if (seen.has(key)) continue
    seen.add(key)
    const nodeLabel = formatNodeHost(saved.node_origin)
    const modelLabel = saved.model_name?.trim() || saved.model_id
    opts.push({
      key,
      node_origin: saved.node_origin,
      model_id: saved.model_id,
      modelLabel,
      nodeLabel,
      label: `${modelLabel} (${nodeLabel})`,
    })
  }
  return opts
})

const primaryKey = computed(() => (targets.value.length > 0 ? targetKey(targets.value[0]) : ''))

const fallbackOptions = computed(() => {
  const chosen = new Set(targets.value.map(targetKey))
  return optionList.value.filter(opt => !chosen.has(opt.key))
})

function formatTargetLabel(item: ComputeTextTarget): string {
  const found = optionList.value.find(opt => opt.key === targetKey(item))
  if (found) return found.label
  return `${item.model_name || item.model_id} (${formatNodeHost(item.node_origin)})`
}

function emitRoute(nextTargets: ComputeTextTarget[], nextFallback: boolean) {
  if (!nextTargets.length) {
    emit('update:modelValue', { targets: [], fallback_to_local: false })
    return
  }
  emit('update:modelValue', {
    targets: nextTargets,
    fallback_to_local: nextFallback,
  })
}

function onPrimaryChange(val: string | number | undefined) {
  const key = typeof val === 'string' ? val : ''
  if (!key) {
    emitRoute([], false)
    return
  }
  const parsed = parseKey(key)
  if (!parsed) return
  const matched = optionList.value.find(opt => opt.key === key)
  const primary: ComputeTextTarget = {
    node_origin: parsed.node_origin,
    model_id: parsed.model_id,
    model_name: matched?.modelLabel || '',
  }
  const rest = targets.value.slice(1).filter(t => targetKey(t) !== key)
  emitRoute([primary, ...rest], fallbackToLocal.value)
}

function onAddFallback(val: string | number | undefined) {
  const key = typeof val === 'string' ? val : ''
  pendingFallbackKey.value = ''
  if (!key) return
  const parsed = parseKey(key)
  if (!parsed) return
  if (targets.value.some(t => targetKey(t) === key)) return
  const matched = optionList.value.find(opt => opt.key === key)
  emitRoute([
    ...targets.value,
    {
      node_origin: parsed.node_origin,
      model_id: parsed.model_id,
      model_name: matched?.modelLabel || '',
    },
  ], fallbackToLocal.value)
}

function moveTarget(index: number, delta: number) {
  const next = [...targets.value]
  const targetIdx = index + delta
  if (targetIdx < 0 || targetIdx >= next.length) return
  const [moved] = next.splice(index, 1)
  next.splice(targetIdx, 0, moved)
  emitRoute(next, fallbackToLocal.value)
}

function removeTarget(index: number) {
  const next = targets.value.filter((_, i) => i !== index)
  emitRoute(next, next.length > 0 ? fallbackToLocal.value : false)
}

function onFallbackToLocalChange(checked: boolean) {
  emitRoute([...targets.value], checked)
}
</script>

<style scoped lang="less">
.compute-route-selector {
  display: grid;
  gap: 8px;
  width: 100%;
}

.compute-route-label {
  font-size: var(--app-text-md);
  font-weight: 500;
  color: var(--td-text-color-primary);
  display: block;
}

.compute-route-desc {
  margin: 2px 0 0;
  font-size: var(--app-text-sm);
  color: var(--td-text-color-secondary);
  line-height: 1.5;
}

.compute-route-details {
  display: grid;
  gap: 8px;
}

.compute-route-fallbacks {
  display: grid;
  gap: 6px;
}

.compute-route-list {
  display: grid;
  gap: 4px;
}

.compute-route-item {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 4px 8px;
  background: var(--td-bg-color-secondarycontainer);
  border-radius: var(--app-radius-xs);
  font-size: var(--app-text-sm);
}

.compute-route-order {
  color: var(--td-text-color-secondary);
  font-variant-numeric: tabular-nums;
}

.compute-route-item-label {
  flex: 1;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.compute-route-item-actions {
  display: flex;
  align-items: center;
  gap: 4px;
}

.compute-route-add {
  display: flex;
  gap: 8px;
}

.compute-route-checkbox {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  font-size: var(--app-text-sm);
  color: var(--td-text-color-secondary);
  cursor: pointer;
}

.compute-option {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;

  &__model {
    font-weight: 500;
  }

  &__node {
    font-size: var(--app-text-xs);
    color: var(--td-text-color-secondary);
  }
}
</style>
