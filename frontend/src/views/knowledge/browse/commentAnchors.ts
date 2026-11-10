import type { DocumentCommentAnchor } from '@/api/knowledge-base'

// Match the exact quote and context only in the version it was selected from.
// A changed version or ambiguous text never silently points to another passage.
export function locateCommentQuote(text: string, anchor: DocumentCommentAnchor, version: number): number | null {
  if (!anchor.quote || version !== anchor.content_version) return null
  let found: number | null = null
  let cursor = 0
  while (cursor <= text.length - anchor.quote.length) {
    const index = text.indexOf(anchor.quote, cursor)
    if (index < 0) break
    if (text.slice(Math.max(0, index - anchor.prefix.length), index) === anchor.prefix &&
      text.slice(index + anchor.quote.length, index + anchor.quote.length + anchor.suffix.length) === anchor.suffix) {
      if (found !== null) return null
      found = index
    }
    cursor = index + 1
  }
  return found
}
export function captureCommentQuote(root: HTMLElement, selection: Selection | null, version: number): DocumentCommentAnchor | null {
  if (!selection || selection.isCollapsed || selection.rangeCount !== 1) return null
  const range = selection.getRangeAt(0)
  if (!root.contains(range.startContainer) || !root.contains(range.endContainer)) return null
  const before = range.cloneRange()
  before.selectNodeContents(root)
  before.setEnd(range.startContainer, range.startOffset)
  const index = before.toString().length
  const quote = range.toString()
  if (!quote.trim() || [...quote].length > 1000) return null
  const text = root.textContent || ''
  // Context truncation is by codepoint so it never splits a surrogate pair.
  return { quote, prefix: [...text.slice(0, index)].slice(-64).join(''),
    suffix: [...text.slice(index + quote.length)].slice(0, 64).join(''), content_version: version }
}
export function selectCommentQuote(root: HTMLElement, anchor: DocumentCommentAnchor, version: number): boolean {
  const index = locateCommentQuote(root.textContent || '', anchor, version)
  if (index === null) return false
  const walker = document.createTreeWalker(root, NodeFilter.SHOW_TEXT)
  const range = document.createRange()
  let offset = 0
  let started = false
  for (let node = walker.nextNode(); node; node = walker.nextNode()) {
    const end = offset + (node.textContent?.length || 0)
    if (!started && index >= offset && index < end) { range.setStart(node, index - offset); started = true }
    if (started && index + anchor.quote.length <= end) {
      range.setEnd(node, index + anchor.quote.length - offset)
      window.getSelection()?.removeAllRanges()
      window.getSelection()?.addRange(range)
      const element = range.startContainer.parentElement
      element?.scrollIntoView({ behavior: 'smooth', block: 'center' })
      return true
    }
    offset = end
  }
  return false
}
