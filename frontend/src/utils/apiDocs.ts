export type ApiDocItem = {
  slug: string
  fileName: string
  title: string
  sourcePath: string
  raw: string
}

// Stable slugs keep the selected chapter and internal links when VOS changes language.
export function localizedApiDocs(modules: Record<string, string>, locale: string): ApiDocItem[] {
  const language = locale.toLowerCase().startsWith('zh') ? 'zh' : 'en'
  const selected = new Map<string, ApiDocItem>()
  for (const [path, raw] of Object.entries(modules)) {
    const fileName = path.split('/').pop() || path
    if (fileName.endsWith('.zh-TW.md')) continue
    const english = fileName.endsWith('.en.md')
    if (english !== (language === 'en')) continue
    const slug = fileName.replace(/(?:\.en)?\.md$/, '')
    selected.set(slug, {
      slug, fileName, raw,
      title: raw.match(/^#\s+(.+)$/m)?.[1]?.trim() || slug,
      sourcePath: path.replace(/^(?:\.\.\/){4}/, ''),
    })
  }
  const first = ['vos-external-api', 'README', '01-api-overview', 'vos-external-api-validation']
  const weight = (slug: string) => { const i = first.indexOf(slug); return i < 0 ? first.length : i }
  return [...selected.values()].sort((a, b) => weight(a.slug) - weight(b.slug) || a.slug.localeCompare(b.slug))
}
