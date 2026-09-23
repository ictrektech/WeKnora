import { BUILT_IN_DEFAULT, SUPPORTED_LOCALES, type SupportedLocale } from './resolveDefaultLocale'

const VOS_LOCALE_KEY = 'preferences-locale'
const MODE_KEY = 'weknora-locale-mode'

function supported(value: unknown): value is SupportedLocale {
  return typeof value === 'string' && (SUPPORTED_LOCALES as readonly string[]).includes(value)
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
    const stored = localStorage.getItem(VOS_LOCALE_KEY)
    const value = stored ? JSON.parse(stored) : null
    const locale = value?.value ?? value
    return supported(locale) ? locale : null
  } catch {
    return null
  }
}

export function followsVosLocale(): boolean {
  return isVosApp() && localStorage.getItem(MODE_KEY) !== 'manual'
}

export function initialAppLocale(deploymentDefault: SupportedLocale): SupportedLocale {
  const vosLocale = followsVosLocale() ? readVosLocale() : null
  if (vosLocale) return vosLocale
  const saved = localStorage.getItem('locale')
  return supported(saved) ? saved : deploymentDefault || BUILT_IN_DEFAULT
}

export function saveAppLocale(locale: SupportedLocale | 'auto'): SupportedLocale {
  if (locale === 'auto') {
    localStorage.removeItem(MODE_KEY)
    return readVosLocale() || initialAppLocale(BUILT_IN_DEFAULT)
  }
  localStorage.setItem(MODE_KEY, 'manual')
  localStorage.setItem('locale', locale)
  return locale
}

export function listenForVosLocaleChange(apply: (locale: SupportedLocale) => void): () => void {
  const listener = (event: StorageEvent) => {
    if (event.key !== VOS_LOCALE_KEY || !followsVosLocale()) return
    const locale = readVosLocale()
    if (locale) apply(locale)
  }
  window.addEventListener('storage', listener)
  return () => window.removeEventListener('storage', listener)
}
