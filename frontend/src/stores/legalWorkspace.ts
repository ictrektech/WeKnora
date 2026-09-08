import { computed, ref, watch } from 'vue'
import { defineStore } from 'pinia'

import {
  deleteLegalWorkspaceData,
  getLegalWorkspaceConfig,
  updateLegalWorkspaceConfig,
  type LegalWorkspaceConfig,
  type LegalWorkspaceDataDeletionResult,
} from '@/api/legal-workspace'
import { useAuthStore } from '@/stores/auth'

/**
 * Tenant-scoped legal workspace state shared by the sidebar and Settings.
 *
 * The optimistic default is enabled to preserve the behavior of existing
 * deployments until the first config response arrives. The server remains
 * authoritative for both feature access and destructive operations.
 */
export const useLegalWorkspaceStore = defineStore('legalWorkspace', () => {
  const authStore = useAuthStore()
  const enabled = ref(true)
  const loaded = ref(false)
  const loading = ref(false)
  const saving = ref(false)
  const loadedTenantId = ref<number | null>(null)
  const loadError = ref<unknown>(null)
  let loadPromise: Promise<void> | null = null
  let loadPromiseTenantId: number | null = null
  let loadGeneration = 0

  const config = computed<LegalWorkspaceConfig>(() => ({ enabled: enabled.value }))

  async function load(force = false): Promise<void> {
    const tenantId = authStore.effectiveTenantId
    if (!tenantId) return
    if (!force && loaded.value && loadedTenantId.value === tenantId) return
    if (loadPromise && loadPromiseTenantId === tenantId) return loadPromise

    loading.value = true
    loadError.value = null
    const generation = ++loadGeneration
    const request = (async () => {
      try {
        const response = await getLegalWorkspaceConfig()
        // A tenant switch or a forced refresh may have superseded this
        // request. Never let a late response change the active tenant's flag.
        if (generation !== loadGeneration || authStore.effectiveTenantId !== tenantId) return
        // Missing config on an older deployment means the compatibility
        // default: existing contract reviews remain available.
        enabled.value = response.data?.enabled !== false
        loadedTenantId.value = tenantId
        loaded.value = true
      } catch (error) {
        if (generation !== loadGeneration || authStore.effectiveTenantId !== tenantId) throw error
        loadError.value = error
        // Fail open for a read-only navigation preference. Contract review
        // API authorization still belongs to the backend.
        enabled.value = true
        loadedTenantId.value = tenantId
        loaded.value = true
        throw error
      } finally {
        if (generation === loadGeneration) {
          loading.value = false
          loadPromise = null
          loadPromiseTenantId = null
        }
      }
    })()
    loadPromise = request
    loadPromiseTenantId = tenantId
    return request
  }

  async function setEnabled(value: boolean): Promise<LegalWorkspaceConfig> {
    const previous = enabled.value
    saving.value = true
    enabled.value = value
    try {
      const response = await updateLegalWorkspaceConfig(value)
      enabled.value = response.data?.enabled !== false
      loaded.value = true
      loadedTenantId.value = authStore.effectiveTenantId
      return response.data || { enabled: enabled.value }
    } catch (error) {
      enabled.value = previous
      throw error
    } finally {
      saving.value = false
    }
  }

  async function deleteData(): Promise<LegalWorkspaceDataDeletionResult> {
    return deleteLegalWorkspaceData()
  }

  function reset() {
    enabled.value = true
    loaded.value = false
    loading.value = false
    saving.value = false
    loadedTenantId.value = null
    loadError.value = null
    loadPromiseTenantId = null
    loadGeneration += 1
    loadPromise = null
  }

  watch(
    () => authStore.effectiveTenantId,
    (tenantId, previousTenantId) => {
      if (tenantId === previousTenantId) return
      reset()
      if (tenantId) void load().catch(() => undefined)
    },
  )

  return {
    enabled,
    loaded,
    loading,
    saving,
    loadError,
    config,
    load,
    setEnabled,
    deleteData,
    reset,
  }
})
