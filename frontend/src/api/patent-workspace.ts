import { del, get, put } from '@/utils/request'

export interface PatentWorkspaceConfig { enabled: boolean }

export const getPatentWorkspaceConfig = () =>
  get<{ success: boolean; data: PatentWorkspaceConfig }>('/api/v1/tenants/kv/patent-workspace-config')

export const updatePatentWorkspaceConfig = (enabled: boolean) =>
  put<{ success: boolean; data: PatentWorkspaceConfig }>('/api/v1/tenants/kv/patent-workspace-config', { enabled })

export const deletePatentWorkspaceData = () => del('/api/v1/patent-workspace-data')
