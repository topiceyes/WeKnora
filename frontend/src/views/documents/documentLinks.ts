export const documentLinkPrefix = '/platform/documents/open/'
const uuid = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/
export function documentLink(id: string): string {
  if (id.length !== 36 || !uuid.test(id)) throw new Error('Invalid document ID')
  return documentLinkPrefix + id
}
export function linkedDocumentID(href: string): string | null {
  if (!href.startsWith(documentLinkPrefix)) return null
  const id = href.slice(documentLinkPrefix.length)
  return id.length === 36 && uuid.test(id) ? id : null
}
export function highlightedParts(text: string, query: string): { text: string; match: boolean }[] {
  if (!query) return [{ text, match: false }]
  const parts: { text: string; match: boolean }[] = []
  const escaped = query.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
  const matches = text.matchAll(new RegExp(escaped, 'giu'))
  let cursor = 0
  for (const match of matches) {
    const index = match.index!
    if (index > cursor) parts.push({ text: text.slice(cursor, index), match: false })
    parts.push({ text: match[0], match: true })
    cursor = index + match[0].length
  }
  if (cursor < text.length) parts.push({ text: text.slice(cursor), match: false })
  return parts
}
