import { isVosApp } from '@/i18n/vosLocale'

export type HubTask = {
  task_id: string
  model_id: string
  progress: number
  phase: 'PENDING' | 'DOWNLOADING' | 'VERIFYING' | 'READY' | 'FAILED'
  error_msg?: string | null
}
export type HubModel = { model_id: string; status: 'ready' | 'pulling' | 'failed' }

const base = '/api/com.ictrek.model-hub/api/v1'

export function hubModelId(name: string): string {
  const trimmed = name.trim()
  return `ollama://${trimmed.includes(':') ? trimmed : `${trimmed}:latest`}`
}

export function isModelHubUrl(value: string): boolean {
  try {
    const url = new URL(value)
    return isVosApp() && /^model-hub-ollama-(qa|embedding|rerank)$/.test(url.hostname)
  } catch {
    return false
  }
}

async function hubFetch<T>(path: string, options?: RequestInit): Promise<T> {
  const response = await fetch(`${base}${path}`, {
    credentials: 'same-origin',
    ...options,
  })
  if (!response.ok) throw new Error(`Model Hub HTTP ${response.status}`)
  return response.json() as Promise<T>
}

export function listHubModels(): Promise<HubModel[]> {
  return hubFetch('/models?status=all')
}

export function listHubTasks(): Promise<HubTask[]> {
  return hubFetch('/tasks')
}

export async function retryHubTask(taskId: string): Promise<void> {
  await hubFetch(`/tasks/${encodeURIComponent(taskId)}/retry`, { method: 'POST' })
  window.dispatchEvent(new Event('weknora:model-hub-pull'))
}

export async function pullHubModel(name: string): Promise<string> {
  const modelId = hubModelId(name)
  const models = await listHubModels()
  if (models.some((model) => model.model_id === modelId && model.status === 'ready')) return `ready:${name.trim()}`
  const tasks = await listHubTasks()
  const running = tasks.find((task) => task.model_id === modelId && !['READY', 'FAILED'].includes(task.phase))
  if (running) return running.task_id
  try {
    const result = await hubFetch<{ task_id: string }>('/models/pull', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ model_id: modelId, source: 'ollama', name: name.trim() }),
    })
    window.dispatchEvent(new Event('weknora:model-hub-pull'))
    return result.task_id
  } catch (error) {
    // Model Hub returns 409 when another browser or its startup preload already
    // created the same download. The shared task list remains the source of truth.
    if (!(error instanceof Error) || !error.message.includes('409')) throw error
    const concurrent = (await listHubTasks()).find((task) => task.model_id === modelId)
    if (concurrent) return concurrent.task_id
    throw error
  }
}

export async function getHubTask(taskId: string): Promise<HubTask | null> {
  if (taskId.startsWith('ready:')) return null
  return hubFetch(`/tasks/detail/${encodeURIComponent(taskId)}`)
}
