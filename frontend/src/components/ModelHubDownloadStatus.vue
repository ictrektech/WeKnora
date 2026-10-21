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
      <span v-else>{{ Math.round(item.task.progress * 100) }}%</span>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { isVosApp } from '@/i18n/vosLocale'
import { listHubModels, listHubTasks, retryHubTask, type HubTask } from '@/api/modelHub'

const models = ref<string[]>([])
const tasks = ref<HubTask[]>([])
const connectionError = ref(false)
let timer: number | undefined
let refreshing = false

const pending = computed(() => tasks.value
  .filter((task) => task.model_id.startsWith('ollama://')
    && task.phase !== 'READY' && !models.value.includes(task.model_id))
  .map((task) => ({ name: task.model_id.slice('ollama://'.length), task })))
const visible = computed(() => pending.value.length > 0)

async function refresh() {
  if (refreshing) return
  refreshing = true
  try {
    const [hubModels, hubTasks] = await Promise.all([listHubModels(), listHubTasks()])
    connectionError.value = false
    models.value = hubModels.filter((model) => model.status === 'ready').map((model) => model.model_id)
    tasks.value = hubTasks
    if (pending.value.length === 0 && timer) {
      window.clearInterval(timer)
      timer = undefined
    }
  } catch {
    // Only previously observed tasks justify a Model Hub connection warning.
    connectionError.value = true
  } finally {
    refreshing = false
  }
}

function monitorDownloads() {
  void refresh()
  if (!timer) timer = window.setInterval(() => { void refresh() }, 2000)
}

onMounted(() => {
  if (!isVosApp()) return
  window.addEventListener('weknora:model-hub-pull', monitorDownloads)
})
onUnmounted(() => {
  if (timer) window.clearInterval(timer)
  window.removeEventListener('weknora:model-hub-pull', monitorDownloads)
})
</script>

<style scoped>
.model-hub-download { position: fixed; z-index: 2000; right: 20px; bottom: 20px; width: min(360px, calc(100vw - 40px)); padding: 14px 16px; border: 1px solid var(--td-component-stroke); border-radius: 10px; background: var(--td-bg-color-container); box-shadow: 0 8px 28px #0002; font-size: 13px; }
.download-row { display: flex; justify-content: space-between; gap: 12px; margin-top: 8px; }
.download-row .error { color: var(--td-error-color); overflow-wrap: anywhere; }
</style>
