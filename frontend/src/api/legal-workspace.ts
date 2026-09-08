import { del, get, put } from '@/utils/request'

/** Workspace-level visibility and lifecycle settings for the legal workspace. */
export interface LegalWorkspaceConfig {
  enabled: boolean
}

export interface LegalWorkspaceDataDeletionResult {
  success: boolean
  deleted_reviews?: number
  deleted_documents?: number
}

export function getLegalWorkspaceConfig() {
  return get<{ success: boolean; data: LegalWorkspaceConfig }>(
    '/api/v1/tenants/kv/legal-workspace-config',
  )
}

export function updateLegalWorkspaceConfig(enabled: boolean) {
  return put<{ success: boolean; data: LegalWorkspaceConfig }>(
    '/api/v1/tenants/kv/legal-workspace-config',
    { enabled },
  )
}

/** Permanently removes all contract-review records and source documents in the active workspace. */
export function deleteLegalWorkspaceData() {
  return del<LegalWorkspaceDataDeletionResult>('/api/v1/legal-workspace-data')
}
