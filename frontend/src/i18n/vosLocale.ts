import {
  browserTraditionalLocale,
  BUILT_IN_DEFAULT,
  normalizeSupportedLocale,
  type SupportedLocale,
} from './resolveDefaultLocale'

const VOS_LOCALE_KEY = 'preferences-locale'
const VOS_LOCALE_PREFIXES = ['VIVIBIT-', 'vben-web-antd-']

function normalize(value: unknown): SupportedLocale | null {
  return typeof value === 'string' ? normalizeSupportedLocale(value) : null
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
  return isVosApp() && !manualLocale() && !browserTraditionalLocale()
}

export function followsAutomaticLocale(): boolean {
  return !manualLocale()
}

export function initialAppLocale(deploymentDefault: SupportedLocale): SupportedLocale {
  const manual = manualLocale()
  if (manual) return manual
  const browserLocale = browserTraditionalLocale()
  if (browserLocale) return browserLocale
  const vosLocale = followsVosLocale() ? readVosLocale() : null
  if (vosLocale) return vosLocale
  return deploymentDefault || BUILT_IN_DEFAULT
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
  const listener = (event: StorageEvent) => {
    if (!event.key || !isVosLocaleKey(event.key) || !followsVosLocale()) return
    const locale = parseStoredLocale(event.newValue)
    if (locale) apply(locale)
  }
  window.addEventListener('storage', listener)
  return () => window.removeEventListener('storage', listener)
}
