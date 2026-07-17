import SecureLS from 'secure-ls'

type MaybeVOSWindow = Window & {
  __VOS_APP_CONTEXT__?: {
    accessToken?: string
    token?: string
    user?: unknown
  }
  __VOS_ACCESS_TOKEN__?: string
}

const VOS_ACCESS_STORE_SUFFIX = '-core-access'
const VOS_DEFAULT_STORE_SECRET = 'Vv123456vV'

function parseJSON(value: unknown): any | null {
  if (!value) return null
  if (typeof value === 'object') return value
  if (typeof value !== 'string') return null
  try {
    return JSON.parse(value)
  } catch {
    return null
  }
}

function tokenFromAccessStore(raw: unknown): string | null {
  const data = parseJSON(raw)
  const token = data?.accessToken || data?.access_token || data?.token
  return typeof token === 'string' && token.trim() ? token.trim() : null
}

function tokenFromInjectedContext(): string | null {
  if (typeof window === 'undefined') return null
  const w = window as MaybeVOSWindow
  const token = w.__VOS_APP_CONTEXT__?.accessToken ||
    w.__VOS_APP_CONTEXT__?.token ||
    w.__VOS_ACCESS_TOKEN__
  return typeof token === 'string' && token.trim() ? token.trim() : null
}

function tokenFromPlainStore(): string | null {
  if (typeof localStorage === 'undefined') return null

  const directKeys = [
    'core-access',
    'VIVIBIT-core-access',
  ]
  for (const key of directKeys) {
    const token = tokenFromAccessStore(localStorage.getItem(key))
    if (token) return token
  }

  for (let i = 0; i < localStorage.length; i += 1) {
    const key = localStorage.key(i)
    if (!key || !key.endsWith(VOS_ACCESS_STORE_SUFFIX)) continue
    const token = tokenFromAccessStore(localStorage.getItem(key))
    if (token) return token
  }
  return null
}

function tokenFromSecureStore(): string | null {
  if (typeof localStorage === 'undefined') return null
  const secret = import.meta.env.VITE_VOS_STORE_SECURE_KEY || VOS_DEFAULT_STORE_SECRET

  for (let i = 0; i < localStorage.length; i += 1) {
    const key = localStorage.key(i)
    if (!key || !key.endsWith(VOS_ACCESS_STORE_SUFFIX)) continue
    const namespace = key.slice(0, -VOS_ACCESS_STORE_SUFFIX.length)
    if (!namespace) continue
    try {
      const ls = new SecureLS({
        encodingType: 'aes',
        encryptionSecret: secret,
        isCompression: true,
        metaKey: `${namespace}-secure-meta`,
      })
      const token = tokenFromAccessStore(ls.get(key))
      if (token) return token
    } catch {
      // Different VOS versions may use different store keys or secrets.
      // Ignore and keep probing so the temporary adapter can coexist with
      // the future official app-user context.
    }
  }
  return null
}

export function getVOSAccessTokenForIframeSSO(): string | null {
  return tokenFromInjectedContext() || tokenFromPlainStore() || tokenFromSecureStore()
}
