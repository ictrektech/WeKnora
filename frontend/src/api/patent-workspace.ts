import { del, get, put } from '@/utils/request'

export interface PatentWorkspaceConfig { enabled: boolean; examples_enabled?: boolean }

export const getPatentWorkspaceConfig = () =>
  get<{ success: boolean; data: PatentWorkspaceConfig }>('/api/v1/tenants/kv/patent-workspace-config')

export const updatePatentWorkspaceConfig = (config: Partial<PatentWorkspaceConfig>) =>
  put<{ success: boolean; data: PatentWorkspaceConfig }>('/api/v1/tenants/kv/patent-workspace-config', config)

export const deletePatentWorkspaceData = () => del('/api/v1/patent-workspace-data')
