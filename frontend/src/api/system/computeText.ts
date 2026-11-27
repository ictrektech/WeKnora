import { get } from '@/utils/request'

export interface ComputeTextModelInfo {
  id: string
  name: string
  display_name?: string
  context_window?: number
}

export interface ComputeTextNodeModels {
  origin: string
  models: ComputeTextModelInfo[]
}

export interface ComputeTextTarget {
  node_origin: string
  model_id: string
  model_name?: string
}

export interface ComputeTextRouteConfig {
  targets?: ComputeTextTarget[]
  fallback_to_local?: boolean
}

export interface ComputeTextModelsResponse {
  available: boolean
  nodes: ComputeTextNodeModels[]
}

export async function listComputeTextModels(): Promise<ComputeTextModelsResponse> {
  try {
    const res = await get<ComputeTextModelsResponse>('/api/v1/system/compute-text-models')
    return {
      available: Boolean(res?.available),
      nodes: Array.isArray(res?.nodes) ? res.nodes : [],
    }
  } catch {
    return { available: false, nodes: [] }
  }
}
