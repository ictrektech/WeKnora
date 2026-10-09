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

export function browserTraditionalLocale(languages?: readonly string[]): 'zh-TW' | null {
  const preferred = languages ?? (typeof navigator === 'undefined'
    ? []
    : navigator.languages?.length ? navigator.languages : [navigator.language])
  const tag = preferred.find(Boolean)?.trim().replaceAll('_', '-').toLowerCase()
  if (!tag || !tag.startsWith('zh-')) return null

  const subtags = tag.split('-')
  if (subtags.includes('hans')) return null
  return subtags.includes('hant') || subtags.includes('tw') || subtags.includes('hk') || subtags.includes('mo')
    ? 'zh-TW'
    : null
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
