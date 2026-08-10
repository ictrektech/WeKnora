import { sha256 } from 'js-sha256'

export interface VOSFastpathTokenSet {
  access_token: string
  token_type: 'Bearer'
  expires_in: number
  refresh_token?: string
  id_token?: string
  scope?: string
}

interface VOSPlatformOAuth2 {
  authorize(params: {
    client_id: string
    response_type: 'code'
    scope: string
    state: string
    code_challenge: string
    code_challenge_method: 'S256'
    nonce?: string
  }): Promise<{ code: string; state: string; redirect_uri?: string }>
  token(params: {
    grant_type: 'authorization_code'
    code: string
    code_verifier: string
    client_id: string
    client_secret?: string
  }): Promise<VOSFastpathTokenSet>
}

interface VOSPlatform {
  version?: string
  mode?: string
  api?: {
    v1000?: {
      oauth2?: VOSPlatformOAuth2
    }
  }
}

declare global {
  interface Window {
    vos_platform?: VOSPlatform
  }
}

const DEFAULT_CLIENT_ID = 'com.ictrek.hybrag'
const DEFAULT_SCOPE = 'openid profile email'
const DETECT_TIMEOUT_MS = 3000

function base64urlEncode(bytes: Uint8Array): string {
  let raw = ''
  for (let i = 0; i < bytes.length; i += 1) {
    raw += String.fromCharCode(bytes[i])
  }
  return btoa(raw).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '')
}

function randomBase64url(byteLength = 32): string {
  const bytes = new Uint8Array(byteLength)
  crypto.getRandomValues(bytes)
  return base64urlEncode(bytes)
}

function computeCodeChallenge(verifier: string): string {
  return base64urlEncode(new Uint8Array(sha256.array(verifier)))
}

function getClientID(): string {
  return import.meta.env.VITE_VOS_OIDC_CLIENT_ID || DEFAULT_CLIENT_ID
}

function getScope(): string {
  return import.meta.env.VITE_VOS_OIDC_SCOPE || DEFAULT_SCOPE
}

export function getVOSFastpathPlatform(): VOSPlatform | null {
  if (typeof window === 'undefined') return null
  const platform = window.vos_platform
  if (!platform?.api?.v1000?.oauth2?.authorize || !platform.api.v1000.oauth2.token) {
    return null
  }
  return platform
}

export async function waitForVOSFastpathPlatform(timeoutMs = DETECT_TIMEOUT_MS): Promise<VOSPlatform | null> {
  const existing = getVOSFastpathPlatform()
  if (existing) return existing
  if (typeof window === 'undefined' || window.parent === window) return null

  const start = Date.now()
  return new Promise((resolve) => {
    const timer = window.setInterval(() => {
      const platform = getVOSFastpathPlatform()
      if (platform) {
        window.clearInterval(timer)
        resolve(platform)
        return
      }
      if (Date.now() - start >= timeoutMs) {
        window.clearInterval(timer)
        resolve(null)
      }
    }, 50)
  })
}

export async function acquireVOSFastpathToken(): Promise<VOSFastpathTokenSet | null> {
  const platform = await waitForVOSFastpathPlatform()
  const oauth2 = platform?.api?.v1000?.oauth2
  if (!oauth2) return null

  const clientID = getClientID()
  const verifier = randomBase64url()
  const state = randomBase64url()
  const nonce = randomBase64url()
  const challenge = computeCodeChallenge(verifier)

  const authResp = await oauth2.authorize({
    client_id: clientID,
    response_type: 'code',
    scope: getScope(),
    state,
    code_challenge: challenge,
    code_challenge_method: 'S256',
    nonce,
  })

  if (authResp.state !== state) {
    throw new Error('VOS OIDC state mismatch')
  }

  const tokenResp = await oauth2.token({
    grant_type: 'authorization_code',
    code: authResp.code,
    code_verifier: verifier,
    client_id: clientID,
  })

  return tokenResp?.access_token ? tokenResp : null
}
