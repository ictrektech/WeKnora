import {
  browserPreferredLocale,
  BUILT_IN_DEFAULT,
  normalizeSupportedLocale,
  type SupportedLocale,
} from './resolveDefaultLocale'

const VOS_LOCALE_KEY = 'preferences-locale'
const VOS_LOCALE_PREFIXES = ['VIVIBIT-', 'vben-web-antd-']
let bridgeLocale: SupportedLocale | null = null

function normalize(value: unknown): SupportedLocale | null {
  return typeof value === 'string'
    ? normalizeSupportedLocale(value) || browserPreferredLocale([value])
    : null
}

function manualLocale(): SupportedLocale | null {
  return normalize(localStorage.getItem('locale'))
}

export function isVosApp(): boolean {
  return typeof window !== 'undefined' && (
    window.location.pathname.includes('/app/com.ictrek.hybrag/')
    || (window.__HYBRAG_BASE_PATH__ || '').includes('/app/com.ictrek.hybrag/')
  )
}

export function readVosLocale(): SupportedLocale | null {
  if (!isVosApp()) return null
  if (bridgeLocale) return bridgeLocale
  try {
    const keys = Array.from({ length: localStorage.length }, (_, index) => localStorage.key(index))
      .filter((key): key is string => !!key && isVosLocaleKey(key))
      .sort((a, b) => b.localeCompare(a, undefined, { numeric: true }))
    for (const key of keys) {
      const locale = parseStoredLocale(localStorage.getItem(key))
      if (locale) return locale
    }
    return null
  } catch {
    return null
  }
}

function isVosLocaleKey(key: string): boolean {
  return key === VOS_LOCALE_KEY
    || VOS_LOCALE_PREFIXES.some((prefix) => key.startsWith(prefix) && key.endsWith(`-${VOS_LOCALE_KEY}`))
}

function parseStoredLocale(stored: string | null): SupportedLocale | null {
  if (!stored) return null
  try {
    const value = JSON.parse(stored)
    const locale = value?.value ?? value
    return normalize(locale)
  } catch {
    return null
  }
}

export function followsVosLocale(): boolean {
  return isVosApp() && followsAutomaticLocale()
}

export function followsAutomaticLocale(): boolean {
  return !manualLocale()
}

export function initialAppLocale(deploymentDefault: SupportedLocale): SupportedLocale {
  const manual = manualLocale()
  if (manual) return manual
  const vosLocale = followsVosLocale() ? readVosLocale() : null
  if (vosLocale) return vosLocale
  return browserPreferredLocale() || deploymentDefault || BUILT_IN_DEFAULT
}

export function saveAppLocale(
  locale: SupportedLocale | 'auto',
  deploymentDefault: SupportedLocale = BUILT_IN_DEFAULT,
): SupportedLocale {
  if (locale === 'auto') {
    localStorage.removeItem('locale')
    return initialAppLocale(deploymentDefault)
  }
  localStorage.setItem('locale', locale)
  return locale
}

export function listenForVosLocaleChange(apply: (locale: SupportedLocale) => void): () => void {
  let stopped = false
  let connected = false
  let revision = 0
  let timer: ReturnType<typeof setInterval> | undefined
  let unsubscribe: (() => void) | undefined

  const receiveLocale = (value: unknown) => {
    const locale = normalize(value)
    if (!locale || stopped) return
    bridgeLocale = locale
    if (followsVosLocale()) apply(locale)
  }
  const onLocaleChange = (data: { locale?: unknown }) => {
    revision += 1
    receiveLocale(data?.locale)
  }

  const connect = () => {
    const platform = window.vos_platform
    if (!platform?.getState || !platform.broadcast?.on) return false
    const snapshotRevision = revision
    try {
      const dispose = platform.broadcast.on('locale-change', onLocaleChange)
      unsubscribe = typeof dispose === 'function'
        ? dispose
        : () => platform.broadcast?.off?.('locale-change', onLocaleChange)
      connected = true
    } catch {
      return false
    }
    // Subscribe first; a slow initial snapshot must not overwrite a newer event.
    void Promise.resolve().then(() => platform.getState!()).then((state) => {
      if (revision === snapshotRevision) receiveLocale(state?.locale)
    }).catch(() => { /* Keep the fallback locale; later events can still update it. */ })
    return true
  }

  const listener = (event: StorageEvent) => {
    if (connected || !event.key || !isVosLocaleKey(event.key) || !followsVosLocale()) return
    const locale = parseStoredLocale(event.newValue)
    if (locale) apply(locale)
  }
  window.addEventListener('storage', listener)
  if (isVosApp() && !connect() && window.parent !== window) {
    timer = setInterval(() => {
      if (connect()) clearInterval(timer)
    }, 250)
  }
  return () => {
    stopped = true
    clearInterval(timer)
    unsubscribe?.()
    bridgeLocale = null
    window.removeEventListener('storage', listener)
  }
}
