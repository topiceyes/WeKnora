export interface EditorMark { type: string; attrs?: Record<string, any> }
export interface EditorNode { type: string; attrs?: Record<string, any>; content?: EditorNode[]; marks?: EditorMark[]; text?: string }
export interface StructuredDocument { schema_version: 1; content: EditorNode }

export function readStructuredDocument(value: unknown): StructuredDocument | null {
  if (value == null) return null
  const doc = typeof value === 'string' ? JSON.parse(value) : value as StructuredDocument
  if (doc.schema_version !== 1 || doc.content?.type !== 'doc') throw new Error('Unsupported document schema')
  return doc
}
/** Stable equality includes formatting-only edits and is insensitive to JSON key order. */
export function documentFingerprint(document: StructuredDocument | null | undefined): string {
  const sorted = (value: any): any => Array.isArray(value) ? value.map(sorted) : value && typeof value === 'object'
    ? Object.fromEntries(Object.keys(value).sort().map(key => [key, sorted(value[key])])) : value
  return document ? JSON.stringify(sorted(document)) : ''
}
export function sameDocument(a: StructuredDocument | null | undefined, b: StructuredDocument | null | undefined) {
  return documentFingerprint(a) === documentFingerprint(b)
}
const escapeText = (value: string) => value.replace(/\\/g, '\\\\').replace(/[`*_[\]|#]/g, '\\$&').replace(/</g, '&lt;').replace(/>/g, '&gt;')
const destination = (value: string) => value.replace(/ /g, '%20').replace(/\(/g, '%28').replace(/\)/g, '%29')
const ticks = (value: string, min: number) => '`'.repeat(Math.max(min, ...[...value.matchAll(/`+/g)].map(match => match[0].length + 1)))

export function tableGrid(node: EditorNode): { cells: string[][]; widths: number[] } {
  const rows = node.content || []
  if (rows.length > 500) throw new Error('Table too large')
  const cells = rows.map(() => Array<string>(100).fill(''))
  const occupied = rows.map(() => Array<boolean>(100).fill(false))
  const widths: number[] = []
  let width = 0
  rows.forEach((row, y) => {
    let x = 0
    for (const cell of row.content || []) {
      while (x < 100 && occupied[y]![x]) x++
      const cs = cell.attrs?.colspan ?? 1, rs = cell.attrs?.rowspan ?? 1
      if (!Number.isInteger(cs) || !Number.isInteger(rs) || cs < 1 || rs < 1 || x + cs > 100 || y + rs > rows.length) throw new Error('Invalid table span')
      for (let yy = y; yy < y + rs; yy++) for (let xx = x; xx < x + cs; xx++) {
        if (occupied[yy]![xx]) throw new Error('Overlapping cells')
        occupied[yy]![xx] = true
      }
      cells[y]![x] = nodeMarkdown(cell).trim().replace(/\s+/g, ' ')
      for (let col = 0; col < cs; col++) if (cell.attrs?.colwidth?.[col]) widths[x + col] = cell.attrs.colwidth[col]
      x += cs
      width = Math.max(width, x)
    }
  })
  if (occupied.some(row => row.slice(0, width).some(value => !value))) throw new Error('Ragged table')
  return { cells: cells.map(row => row.slice(0, width)), widths: Array.from({ length: width }, (_, index) => widths[index] || 0) }
}

export function nodeMarkdown(node: EditorNode): string {
  const a = node.attrs || {}
  // These containers render children below. Pre-rendering them too makes
  // deeply nested lists/tables take exponential time.
  let body = ['table', 'codeBlock', 'bulletList', 'orderedList', 'taskList'].includes(node.type) ? '' : (node.content || []).map(nodeMarkdown).join('')
  switch (node.type) {
    case 'text':
      body = escapeText(node.text || '')
      for (const mark of node.marks || []) {
        if (mark.type === 'bold') body = `**${body}**`
        else if (mark.type === 'italic') body = `*${body}*`
        else if (mark.type === 'strike') body = `~~${body}~~`
        else if (mark.type === 'code') { const delimiter = ticks(node.text || '', 1); body = `${delimiter} ${node.text} ${delimiter}` }
        else if (mark.type === 'link') body = `[${body}](${destination(mark.attrs?.href || '')})`
      }
      return body
    case 'paragraph': case 'tableCell': case 'tableHeader': return body + '\n\n'
    case 'heading': return '#'.repeat(a.level || 1) + ' ' + body + '\n\n'
    case 'hardBreak': return '  \n'
    case 'horizontalRule': return '---\n\n'
    case 'image': return `![${escapeText(a.alt || '')}](${destination(a.src || '')})\n\n`
    case 'inlineMath': return `$${a.latex || ''}$`
    case 'blockMath': return `$$\n${a.latex || ''}\n$$\n\n`
    case 'codeBlock': {
      const code = (node.content || []).map(child => child.text || '').join(''), delimiter = ticks(code, 3)
      return `${delimiter}${a.language || ''}\n${code}\n${delimiter}\n\n`
    }
    case 'blockquote': case 'callout':
      body = body.trim()
      if (node.type === 'callout') body = `[!${String(a.kind || 'info').toUpperCase()}]\n${body}`
      return '> ' + body.replace(/\n/g, '\n> ') + '\n\n'
    case 'bulletList': case 'orderedList': case 'taskList':
      return (node.content || []).map((child, index) => {
        const prefix = node.type === 'orderedList' ? `${(a.start || 1) + index}. ` : node.type === 'taskList' ? `- [${child.attrs?.checked ? 'x' : ' '}] ` : '- '
        return prefix + nodeMarkdown(child).trim().replace(/\n/g, '\n' + ' '.repeat(prefix.length)) + '\n'
      }).join('') + '\n'
    case 'table': {
      const { cells } = tableGrid(node)
      return cells.map((row, index) => `| ${row.join(' | ')} |\n` + (index === 0 ? `| ${row.map(() => '---').join(' | ')} |\n` : '')).join('') + '\n'
    }
    default: return body
  }
}
export function documentMarkdown(document: StructuredDocument): string { return nodeMarkdown(document.content).trim() }

const escapeHTML = (value: unknown) => String(value ?? '').replace(/[&<>"']/g, char => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' })[char]!)
const validColor = (value: unknown) => typeof value === 'string' && /^#[\da-f]{6}$/i.test(value)
const safeURL = (value: unknown, image = false) => {
  if (typeof value !== 'string' || /[\x00\r\n\t\\]/.test(value)) return false
  if (value.startsWith('/')) return !value.startsWith('//')
  if (!image && value.startsWith('#')) return true
  return /^(https?|resource|storage|local|minio|cos|tos|s3|oss|ks3|obs):\/\/[^/]+/i.test(value) || (!image && /^(mailto|tel):\S+/i.test(value))
}
/** HTML is generated from a fixed schema, then passed to the existing sanitizer by the caller. */
export function documentHTML(document: StructuredDocument, renderMarkdown: (markdown: string) => string): string {
  const render = (node: EditorNode, depth = 0): string => {
    if (depth > 64) throw new Error('Document too deeply nested')
    const a = node.attrs || {}
    const body = (node.content || []).map(child => render(child, depth + 1)).join('')
    switch (node.type) {
      case 'doc': return body
      case 'text': {
        let text = escapeHTML(node.text)
        for (const mark of node.marks || []) {
          if (mark.type === 'bold') text = `<strong>${text}</strong>`
          else if (mark.type === 'italic') text = `<em>${text}</em>`
          else if (mark.type === 'strike') text = `<s>${text}</s>`
          else if (mark.type === 'code') text = `<code>${text}</code>`
          else if (mark.type === 'underline') text = `<u>${text}</u>`
          else if (mark.type === 'link' && safeURL(mark.attrs?.href)) text = `<a href="${escapeHTML(mark.attrs!.href)}"${typeof mark.attrs?.title === 'string' ? ` title="${escapeHTML(mark.attrs.title)}"` : ''} target="_blank" rel="noopener noreferrer">${text}</a>`
          else if (mark.type === 'highlight' && validColor(mark.attrs?.color)) text = `<mark style="background-color:${mark.attrs!.color};color:inherit">${text}</mark>`
          else if (mark.type === 'textColor' && validColor(mark.attrs?.color)) text = `<span style="color:${mark.attrs!.color}">${text}</span>`
        }
        return text
      }
      case 'paragraph': return `<p>${body}</p>`
      case 'heading': { const level = Math.max(1, Math.min(4, Number(a.level) || 1)); return `<h${level}>${body}</h${level}>` }
      case 'blockquote': return `<blockquote>${body}</blockquote>`
      case 'bulletList': return `<ul${a.tight ? ' class="rich-document-list--tight"' : ''}>${body}</ul>`
      case 'orderedList': return `<ol start="${Number(a.start) || 1}"${a.tight ? ' class="rich-document-list--tight"' : ''}>${body}</ol>`
      case 'listItem': return `<li>${body}</li>`
      case 'taskList': return `<ul class="rich-document-tasks">${body}</ul>`
      case 'taskItem': return `<li class="rich-document-task"><span class="rich-document-task-state" aria-label="${a.checked ? 'checked' : 'unchecked'}">${a.checked ? '☑' : '☐'}</span>${body}</li>`
      case 'hardBreak': return '<br>'
      case 'horizontalRule': return '<hr>'
      case 'image': return safeURL(a.src, true) ? `<img src="${escapeHTML(a.src)}" alt="${escapeHTML(a.alt)}" title="${escapeHTML(a.title)}">` : ''
      case 'codeBlock': case 'blockMath': case 'inlineMath': {
        const html = renderMarkdown(nodeMarkdown(node))
        return node.type === 'inlineMath' ? html.replace(/^<p>|<\/p>\s*$/g, '') : html
      }
      case 'callout': {
        const kind = ['info', 'tip', 'warning', 'danger'].includes(a.kind) ? a.kind : 'info'
        return `<div class="rich-document-callout rich-document-callout--${kind}">${body}</div>`
      }
      case 'table': {
        const { widths } = tableGrid(node)
        const cols = widths.some(Boolean) ? `<colgroup>${widths.map(width => `<col${width ? ` style="width:${width}px"` : ''}>`).join('')}</colgroup>` : ''
        return `<div class="rich-document-table"><table${cols ? ' style="table-layout:fixed"' : ''}>${cols}<tbody>${body}</tbody></table></div>`
      }
      case 'tableRow': return `<tr>${body}</tr>`
      case 'tableCell': case 'tableHeader': {
        const tag = node.type === 'tableHeader' ? 'th' : 'td'
        const align = ['left', 'center', 'right', 'justify'].includes(a.align) ? ` style="text-align:${a.align}"` : ''
        return `<${tag} colspan="${Math.max(1, Math.min(100, Number(a.colspan) || 1))}" rowspan="${Math.max(1, Math.min(100, Number(a.rowspan) || 1))}"${align}>${body}</${tag}>`
      }
      default: throw new Error(`Unsupported document node: ${node.type}`)
    }
  }
  return render(document.content)
}
