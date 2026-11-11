import { Fragment, type Node as PMNode } from '@tiptap/pm/model'
import { TableMap } from '@tiptap/pm/tables'

/** Match the existing structured-document storage bounds before an atomic edit. */
export function withinDocumentLimits(doc: PMNode, reserveTrailingParagraph = false) {
  // StarterKit adds a writable paragraph after a non-paragraph final block.
  // Include that appended transaction in the limit before dispatching the edit.
  if (reserveTrailingParagraph && doc.lastChild?.type.name !== 'paragraph') {
    doc = doc.copy(doc.content.append(Fragment.from(doc.type.schema.nodes.paragraph!.create())))
  }
  let count = 0, valid = true
  const visit = (node: PMNode, depth: number) => {
    if (!valid) return
    if (++count > 20_000 || depth > 64) { valid = false; return }
    if (['cell', 'header_cell'].includes(node.type.spec.tableRole) && (node.attrs.colspan > 100 || node.attrs.rowspan > 100)) {
      valid = false; return
    }
    if (node.type.spec.tableRole === 'table') {
      const map = TableMap.get(node)
      // Column widths are reconciled by the table plugin's appended transaction.
      if (map.width > 100 || map.height > 500 || map.problems?.some(problem => problem.type !== 'colwidth mismatch')) { valid = false; return }
    }
    node.forEach(child => visit(child, depth + 1))
  }
  visit(doc, 0)
  if (!valid) return false
  const json = JSON.stringify({ schema_version: 1, content: doc.toJSON() })
    .replace(/[<>&\u2028\u2029]/g, char => '\\u' + char.charCodeAt(0).toString(16).padStart(4, '0'))
  return new TextEncoder().encode(json).length <= (2 << 20)
}
