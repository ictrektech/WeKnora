<template>
  <div class="api-docs-settings">
    <div class="section-header">
      <h2>{{ $t('tenant.api.docLabel') }}</h2>
      <p>{{ $t('tenant.api.docDescription') }}</p>
    </div>
    <div class="api-docs-layout">
      <aside class="api-docs-nav">
        <t-input v-model="search" clearable :placeholder="$t('tenant.api.docLabel')" />
        <button v-for="doc in filteredDocs" :key="doc.slug" type="button"
          :class="{ active: selectedSlug === doc.slug }" @click="selectedSlug = doc.slug">
          {{ doc.title }}
        </button>
      </aside>
      <article class="api-docs-reader" v-if="selectedDoc">
        <h3>{{ selectedDoc.title }}</h3>
        <div class="api-docs-markdown" v-html="selectedHtml" @click="onLinkClick" />
      </article>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import { marked } from 'marked'
import { sanitizeMarkdownHTML } from '@/utils/security'

const modules = {
  ...import.meta.glob('../../../../website-docs/04-api/*.md', {
    eager: true,
    query: '?raw',
    import: 'default',
  }),
  ...import.meta.glob('../../../../docs/api/*.md', {
    eager: true,
    query: '?raw',
    import: 'default',
  }),
} as Record<string, string>
const docs = Object.entries(modules).map(([path, raw]) => {
  const fileName = path.split('/').pop() || path
  return {
    slug: fileName.replace(/\.md$/, ''),
    title: raw.match(/^#\s+(.+)$/m)?.[1]?.trim() || fileName,
    sourcePath: path.replace(/^\.\.\/\.\.\/\.\.\/\.\.\//, ''),
    raw,
  }
}).sort((a, b) => a.slug === 'vos-external-api' ? -1 : b.slug === 'vos-external-api' ? 1 : a.title.localeCompare(b.title))
const slugs = new Set(docs.map((doc) => doc.slug))
const search = ref('')
const selectedSlug = ref(docs[0]?.slug || '')
const filteredDocs = computed(() => docs.filter((doc) =>
  `${doc.title} ${doc.raw}`.toLowerCase().includes(search.value.trim().toLowerCase()),
))
const selectedDoc = computed(() => docs.find((doc) => doc.slug === selectedSlug.value) || filteredDocs.value[0])
function resolveRepoMarkdownPath(sourcePath: string, href: string) {
  if (href.startsWith('/') || href.startsWith('#') || /^[a-z][a-z0-9+.-]*:/i.test(href)) return ''
  const segments = sourcePath.split('/').slice(0, -1)
  for (const segment of href.split('/')) {
    if (segment === '..') segments.pop()
    else if (segment && segment !== '.') segments.push(segment)
  }
  return segments.join('/')
}
const selectedHtml = computed(() => {
  const raw = selectedDoc.value?.raw || ''
  const linked = raw.replace(/\]\(([^)]+\.md)(#[^)]+)?\)/g, (match, href: string, hash = '') => {
    const slug = (href.split('/').pop() || '').replace(/\.md$/, '')
    if (slugs.has(slug)) return `](#api-doc:${slug}${hash || ''})`
    const repoPath = resolveRepoMarkdownPath(selectedDoc.value?.sourcePath || '', href)
    return repoPath
      ? `](https://github.com/ictrektech/WeKnora/blob/main/${repoPath}${hash || ''})`
      : match
  })
  return sanitizeMarkdownHTML(marked.parse(linked, { async: false, gfm: true }) as string)
})
function onLinkClick(event: MouseEvent) {
  const anchor = (event.target as HTMLElement)?.closest('a')
  const href = anchor?.getAttribute('href') || ''
  if (href.startsWith('https://github.com/ictrektech/WeKnora/blob/main/')) {
    event.preventDefault()
    window.open(href, '_blank', 'noopener,noreferrer')
    return
  }
  if (!href.startsWith('#api-doc:')) return
  const slug = href.slice('#api-doc:'.length).split('#')[0]
  if (!slugs.has(slug)) return
  event.preventDefault()
  selectedSlug.value = slug
}
</script>

<style scoped lang="less">
.api-docs-settings { height: 100%; min-height: 480px; }
.section-header { margin-bottom: 20px; }
.section-header h2 { margin: 0 0 6px; font-size: 20px; }
.section-header p { margin: 0; color: var(--td-text-color-secondary); }
.api-docs-layout { display: grid; grid-template-columns: 220px minmax(0, 1fr); gap: 20px; min-height: 440px; }
.api-docs-nav { display: flex; flex-direction: column; gap: 4px; overflow: auto; max-height: 70vh; }
.api-docs-nav button { padding: 9px 10px; text-align: left; border: 0; border-radius: 6px; background: transparent; color: var(--td-text-color-primary); cursor: pointer; }
.api-docs-nav button:hover, .api-docs-nav button.active { background: var(--td-brand-color-light); color: var(--td-brand-color); }
.api-docs-reader { min-width: 0; max-height: 70vh; overflow: auto; padding: 0 12px 24px; }
.api-docs-reader h3 { margin-top: 0; }
.api-docs-markdown { overflow-wrap: anywhere; line-height: 1.65; }
.api-docs-markdown :deep(pre) { overflow: auto; padding: 12px; background: var(--td-bg-color-secondarycontainer); }
.api-docs-markdown :deep(table) { display: block; overflow-x: auto; border-collapse: collapse; }
.api-docs-markdown :deep(td), .api-docs-markdown :deep(th) { padding: 6px; border: 1px solid var(--td-component-stroke); }
@media (max-width: 700px) { .api-docs-layout { grid-template-columns: 1fr; } .api-docs-nav { max-height: 180px; } }
</style>
