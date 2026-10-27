import { ref, watch } from 'vue'
import { defineStore } from 'pinia'
import { useAuthStore } from '@/stores/auth'
import { deletePatentWorkspaceData, getPatentWorkspaceConfig, updatePatentWorkspaceConfig } from '@/api/patent-workspace'
import type { PatentWorkspaceConfig } from '@/api/patent-workspace'

export const usePatentWorkspaceStore = defineStore('patentWorkspace', () => {
  const auth = useAuthStore()
  const enabled = ref(false)
  const examplesEnabled = ref(false)
  const loaded = ref(false)
  const loading = ref(false)
  const saving = ref(false)
  const loadError = ref<unknown>(null)
  let generation = 0
  let pending: Promise<void> | null = null

  function reset() {
    generation++
    enabled.value = false
    examplesEnabled.value = false
    loaded.value = false
    loading.value = false
    saving.value = false
    loadError.value = null
    pending = null
  }

  function load(force = false): Promise<void> {
    if (saving.value) return Promise.resolve()
    if (!auth.effectiveTenantId || (!force && loaded.value)) return Promise.resolve()
    if (pending) return pending
    const tenantId = auth.effectiveTenantId
    const current = ++generation
    loading.value = true
    loadError.value = null
    const request = (async () => {
      try {
        const response = await getPatentWorkspaceConfig()
        if (current !== generation || tenantId !== auth.effectiveTenantId) return
        enabled.value = response.data?.enabled === true
        examplesEnabled.value = response.data?.examples_enabled !== false
        loaded.value = true
      } catch (error) {
        if (current === generation && tenantId === auth.effectiveTenantId) {
          enabled.value = false
          examplesEnabled.value = false
          loadError.value = error
        }
        throw error
      } finally {
        if (current === generation) {
          loading.value = false
          pending = null
        }
      }
    })()
    pending = request
    return request
  }

  async function setConfig(config: Partial<PatentWorkspaceConfig>) {
    const current = ++generation
    const tenantId = auth.effectiveTenantId
    pending = null
    loading.value = false
    saving.value = true
    try {
      const response = await updatePatentWorkspaceConfig(config)
      if (current !== generation || tenantId !== auth.effectiveTenantId) return
      enabled.value = response.data?.enabled === true
      examplesEnabled.value = response.data?.examples_enabled !== false
      loaded.value = true
      loadError.value = null
    } finally {
      if (current === generation) saving.value = false
    }
  }

  const setEnabled = (value: boolean) => setConfig({ enabled: value })
  const setExamplesEnabled = (value: boolean) => setConfig({ examples_enabled: value })

  async function deleteData() {
    const tenantId = auth.effectiveTenantId
    await deletePatentWorkspaceData()
    if (tenantId === auth.effectiveTenantId) window.dispatchEvent(new CustomEvent('patent-workspace-data-deleted'))
  }

  watch(() => auth.effectiveTenantId, () => {
    reset()
    if (auth.effectiveTenantId) void load().catch(() => undefined)
  })

  return { enabled, examplesEnabled, loaded, loading, saving, loadError, load, setEnabled, setExamplesEnabled, deleteData, reset }
})
