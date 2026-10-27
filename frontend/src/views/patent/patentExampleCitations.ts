import type { PatentExampleDocument } from '@/api/patent-examples'

export function resolveExampleCitation(href: string, documents: PatentExampleDocument[]) {
  const match = /^#example-source\/([\w-]+)\/([\w-]+)$/.exec(href)
  if (!match) return null
  const document = documents.find(doc => doc.id === match[1] && doc.publication)
  const evidence = document?.publication?.evidence.find(item => item.id === match[2])
  return document && evidence ? { document, evidence } : null
}

const escapeHtml = (value: string) => value.replace(/[&<>"']/g, char => ({
  '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;',
})[char]!)

// Keep the recording unchanged; only its reviewed source links become file pills.
export function formatExampleCitations(content: string, documents: PatentExampleDocument[]) {
  return content.replace(/\[[^\]\n]+\]\((#example-source\/[^\s)]+)\)/g, (original, href: string) => {
    const source = resolveExampleCitation(href, documents)
    if (!source) return original
    const filename = escapeHtml(source.document.filename)
    const label = escapeHtml(`${source.document.filename}: ${source.evidence.location}`)
    return `<a class="citation citation-example" href="${escapeHtml(href)}" aria-label="${label}"><span class="citation-icon citation-icon--book" aria-hidden="true"></span><span class="citation-text">${filename}</span></a>`
  })
}
