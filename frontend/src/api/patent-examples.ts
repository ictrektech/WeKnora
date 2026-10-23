import { get } from '@/utils/request'
import type { PatentMode } from '@/utils/workspaceMode'

export interface PatentExampleDocument {
  id: string
  filename: string
  source: string
  publication?: {
    title: string
    applicant: string
    application_date: string
    publication_date: string
    evidence: Array<{ id: string; location: string; summary: string }>
  }
}
export interface PatentExample {
  id: string
  workspace_mode: PatentMode
  title: string
  description: string
  model: string
  recorded_at: string
  limitations: string
  attribution?: string
  documents: PatentExampleDocument[]
  messages?: Array<{
    role: 'user' | 'assistant'
    content: string
    agent_duration_ms?: number
    events?: Array<Record<string, unknown> & { type: string; content?: string }>
  }>
}

const base = '/api/v1/patent-workspace/examples'
export const listPatentExamples = (mode: PatentMode) =>
  get<{ success: boolean; data: { version: string; examples: PatentExample[] } }>(`${base}?workspace_mode=${mode}`)
export const getPatentExample = (id: string) =>
  get<{ success: boolean; data: PatentExample }>(`${base}/${encodeURIComponent(id)}`)
export const getPatentExampleDocument = (id: string, documentId: string) =>
  get<{ success: boolean; data: { id: string; filename: string; content: string; document: PatentExampleDocument } }>(`${base}/${encodeURIComponent(id)}/documents/${encodeURIComponent(documentId)}`)
