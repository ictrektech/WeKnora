export type WorkspaceMode = 'platform' | 'legal_assistant' | 'patent_search' | 'patent_writing'
export type PatentMode = 'patent_search' | 'patent_writing'

export function isPatentMode(mode: unknown): mode is PatentMode {
  return mode === 'patent_search' || mode === 'patent_writing'
}

export function workspaceHome(mode: unknown): string {
  if (mode === 'legal_assistant') return '/platform/legal-assistant'
  if (mode === 'patent_search') return '/platform/patent/search'
  if (mode === 'patent_writing') return '/platform/patent/writing'
  return '/platform/creatChat'
}

export function workspaceChatPath(mode: unknown, id: string): string {
  return mode === 'platform' || !mode ? `/platform/chat/${id}` : `${workspaceHome(mode)}/chat/${id}`
}

export function routeWorkspaceMode(name: unknown): WorkspaceMode {
  if (typeof name !== 'string') return 'platform'
  if (name.startsWith('patentSearch')) return 'patent_search'
  if (name.startsWith('patentWriting')) return 'patent_writing'
  if (name.startsWith('legalAssistant')) return 'legal_assistant'
  return 'platform'
}

export function isPatentRoute(name: unknown): boolean {
  return isPatentMode(routeWorkspaceMode(name)) || name === 'patentWorkspace'
}
