<template>
  <div v-if="visible" class="model-hub-download" role="status" aria-live="polite">
    <strong>{{ $t('modelHubDownload.title') }}</strong>
    <p v-if="connectionError" class="error">{{ $t('modelHubDownload.unavailable') }}</p>
    <div v-for="item in pending" :key="item.name" class="download-row">
      <span>{{ item.name }}</span>
      <span v-if="item.task?.phase === 'FAILED'" class="error">
        {{ item.task.error_msg || $t('modelHubDownload.failed') }}
        <button type="button" @click="retryHubTask(item.task.task_id)">{{ $t('modelHubDownload.retry') }}</button>
      </span>
      <span v-else-if="item.task">{{ Math.round(item.task.progress * 100) }}%</span>
      <span v-else>{{ $t('modelHubDownload.starting') }}</span>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { isVosApp } from '@/i18n/vosLocale'
import { hubModelId, listHubModels, listHubTasks, pullHubModel, retryHubTask, type HubTask } from '@/api/modelHub'

const required = ['qwen3.5:2b', 'bge-m3', 'qllama/bge-reranker-v2-m3:q8_0']
const models = ref<string[]>([])
const tasks = ref<HubTask[]>([])
const available = ref(false)
const connectionError = ref(false)
let timer: number | undefined
let refreshing = false

const pending = computed(() => {
  const names = new Map(required.map((name) => [hubModelId(name), name]))
  for (const task of tasks.value) {
    if (task.model_id.startsWith('ollama://') && !['READY'].includes(task.phase)) {
      if (!names.has(task.model_id)) names.set(task.model_id, task.model_id.slice('ollama://'.length))
    }
  }
  return [...names].filter(([id]) => !models.value.includes(id))
    .map(([id, name]) => ({ name, task: tasks.value.find((task) => task.model_id === id) }))
})
const visible = computed(() => available.value && pending.value.length > 0)

async function refresh() {
  if (refreshing) return
  refreshing = true
  try {
    const [hubModels, hubTasks] = await Promise.all([listHubModels(), listHubTasks()])
    available.value = true
    connectionError.value = false
    models.value = hubModels.filter((model) => model.status === 'ready').map((model) => model.model_id)
    tasks.value = hubTasks
    for (const name of required) {
      const id = hubModelId(name)
      if (!models.value.includes(id) && !hubTasks.some((task) => task.model_id === id)) {
        await pullHubModel(name)
      }
    }
  } catch {
    // Model Hub can still be starting when the VOS iframe loads.
    available.value = true
    connectionError.value = true
  } finally {
    refreshing = false
  }
}

onMounted(() => {
  if (!isVosApp()) return
  void refresh()
  timer = window.setInterval(() => { void refresh() }, 2000)
  window.addEventListener('weknora:model-hub-pull', refresh)
})
onUnmounted(() => {
  if (timer) window.clearInterval(timer)
  window.removeEventListener('weknora:model-hub-pull', refresh)
})
</script>

<style scoped>
.model-hub-download { position: fixed; z-index: 2000; right: 20px; bottom: 20px; width: min(360px, calc(100vw - 40px)); padding: 14px 16px; border: 1px solid var(--td-component-stroke); border-radius: 10px; background: var(--td-bg-color-container); box-shadow: 0 8px 28px #0002; font-size: 13px; }
.download-row { display: flex; justify-content: space-between; gap: 12px; margin-top: 8px; }
.download-row .error { color: var(--td-error-color); overflow-wrap: anywhere; }
</style>
