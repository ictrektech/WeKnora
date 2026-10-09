export const SUPPORTED_LOCALES = ['zh-CN', 'zh-TW', 'en-US', 'ru-RU', 'ko-KR', 'ja-JP'] as const
export type SupportedLocale = (typeof SUPPORTED_LOCALES)[number]

export const BUILT_IN_DEFAULT: SupportedLocale = 'zh-CN'

export function normalizeSupportedLocale(value: string): SupportedLocale | null {
  const normalized = value.trim().replaceAll('_', '-')
  if ((SUPPORTED_LOCALES as readonly string[]).includes(normalized)) return normalized as SupportedLocale

  const [language, second, third] = normalized.toLowerCase().split('-')
  if (language !== 'zh') return null
  if (second === 'hans' || third === 'hans') return 'zh-CN'
  if (second === 'hant' || second === 'tw' || second === 'hk' || second === 'mo'
    || third === 'tw' || third === 'hk' || third === 'mo') return 'zh-TW'
  if (!second || second === 'cn' || second === 'sg') return 'zh-CN'
  return null
}

export function browserPreferredLocale(languages?: readonly string[]): SupportedLocale | null {
  const preferred = languages ?? (typeof navigator === 'undefined'
    ? []
    : navigator.languages?.length ? navigator.languages : [navigator.language])
  for (const tag of preferred) {
    if (!tag?.trim()) continue
    const normalized = tag.trim().replaceAll('_', '-')
    const locale = normalizeSupportedLocale(normalized)
    if (locale) return locale
    const language = normalized.split('-')[0].toLowerCase()
    const fallback = SUPPORTED_LOCALES.find((supported) => supported.split('-')[0] === language)
    if (fallback) return fallback
  }
  return null
}

/** Resolve deployment default locale before any user preference is applied. */
export function resolveDefaultLocale(
  runtimeLocale?: string | null,
  buildTimeLocale?: string | null,
): SupportedLocale {
  const raw = runtimeLocale || buildTimeLocale || BUILT_IN_DEFAULT
  const candidate = typeof raw === 'string' ? raw.trim() : BUILT_IN_DEFAULT
  return normalizeSupportedLocale(candidate) || BUILT_IN_DEFAULT
}
