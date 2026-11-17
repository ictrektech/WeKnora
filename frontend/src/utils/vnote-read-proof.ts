import { acquireVOSFastpathToken, getVOSFastpathPlatform } from './vos-fastpath'
import { getVOSAccessTokenForIframeSSO } from './vos-sso'
import { getApiBaseUrl } from './api-base'

let cached: { identity: string; token: string; until: number } | null = null
let pending: Promise<string | null> | null = null

// Human proof lives only in memory. The server independently checks current
// VOS status and stable account binding for each request that reads V-Note data.
async function currentProof(): Promise<string | null> {
  const identity = typeof localStorage === 'undefined' ? '' : localStorage.getItem('weknora_token') || ''
  if (cached?.identity === identity && cached.until > Date.now()) return cached.token
  if (pending) return pending
  pending = (async () => {
    if (getVOSFastpathPlatform()) {
      try {
        const result = await acquireVOSFastpathToken()
        if (result?.access_token) {
          cached = { identity, token: result.access_token, until: Date.now() + Math.max(1, Math.min(result.expires_in, 300) - 15) * 1000 }
          return result.access_token
        }
      } catch { /* A missing human proof leaves private reads unavailable. */ }
    }
    return getVOSAccessTokenForIframeSSO()
  })().finally(() => { pending = null })
  return pending
}

export async function getVOSReadHeaders(url: string, baseURL = ''): Promise<Record<string, string>> {
  if (typeof window === 'undefined') return {}
  try {
    const absolute = /^([a-z][a-z\d+.-]*:)?\/\//i.test(url)
    const combined = absolute ? url : `${baseURL.replace(/\/$/, '')}/${url.replace(/^\/+/, '')}`
    const target = new URL(combined, window.location.origin)
    const prefix = `${getApiBaseUrl().replace(/\/$/, '')}/api/v1/`
    // Never forward a platform proof to downloads, arbitrary URLs, or another app.
    if (target.origin !== window.location.origin || !target.pathname.startsWith(prefix)) return {}
    const proof = await currentProof()
    return proof ? { 'X-VOS-User-Proof': proof } : {}
  } catch { return {} }
}
