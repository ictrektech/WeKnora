import {
  clearSessionMessages,
  delSession,
  pinSession,
  unpinSession,
  updateSession,
} from '@/api/chat'

export const SESSION_MUTATION_EVENT = 'weknora:session-mutation'

/** The sidebar row created optimistically before the next list request. */
export interface SessionMutationSession {
  id: string
  title?: string
  path?: string
  isNoTitle?: boolean
  created_at?: string
  updated_at?: string
  is_pinned?: boolean
  pinned_at?: string | null
  im_platform?: string
  description?: string
  user_id?: string
  workspace_mode?: string
}

export interface SessionMutationPatch {
  title?: string
  is_pinned?: boolean
  pinned_at?: string | null
}

export interface SessionMutationDetail {
  sessionId: string
  created?: boolean
  session?: SessionMutationSession
  patch?: SessionMutationPatch
  messagesCleared?: boolean
  removed?: boolean
}

export function notifySessionMutation(detail: SessionMutationDetail): void {
  if (typeof window === 'undefined') return
  window.dispatchEvent(new CustomEvent<SessionMutationDetail>(SESSION_MUTATION_EVENT, { detail }))
}

export function notifySessionCreated(session: SessionMutationSession): void {
  if (!session?.id) return
  notifySessionMutation({ sessionId: session.id, created: true, session })
}

function ensureSuccess(response: any): any {
  if (!response?.success) {
    throw new Error(response?.message || 'session mutation failed')
  }
  return response
}

export async function renameSession(
  sessionId: string,
  title: string,
  description = '',
): Promise<any> {
  const response = ensureSuccess(await updateSession(sessionId, { title, description }))
  const nextTitle = response.data?.title || title
  notifySessionMutation({ sessionId, patch: { title: nextTitle } })
  return response.data
}

export async function setSessionPinned(sessionId: string, pinned: boolean): Promise<void> {
  const response = ensureSuccess(pinned ? await pinSession(sessionId) : await unpinSession(sessionId))
  notifySessionMutation({
    sessionId,
    patch: {
      is_pinned: pinned,
      pinned_at: pinned ? new Date().toISOString() : null,
    },
  })
}

export async function clearSession(sessionId: string): Promise<void> {
  ensureSuccess(await clearSessionMessages(sessionId))
  notifySessionMutation({ sessionId, messagesCleared: true })
}

export async function removeSession(sessionId: string): Promise<void> {
  ensureSuccess(await delSession(sessionId))
  notifySessionMutation({ sessionId, removed: true })
}
