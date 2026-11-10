import { Extension } from '@tiptap/core'
import { Plugin } from '@tiptap/pm/state'
import { Fragment, Slice } from '@tiptap/pm/model'
import type { EditorProps } from '@tiptap/pm/view'

/** Restrict persisted CSS to opaque colors accepted by the document schema. */
export function clipboardColor(value: string): string | null {
  const color = value.trim().toLowerCase()
  if (/^#[\da-f]{6}$/.test(color)) return color
  if (/^#[\da-f]{3}$/.test(color)) return '#' + [...color.slice(1)].map(c => c + c).join('')
  const rgb = /^rgba?\(\s*(\d+)\s*,\s*(\d+)\s*,\s*(\d+)(?:\s*,\s*(1|1\.0+))?\s*\)$/.exec(color)
  if (rgb && rgb.slice(1, 4).every(c => Number(c) <= 255)) return '#' + rgb.slice(1, 4).map(c => Number(c).toString(16).padStart(2, '0')).join('')
  // Browser color parsing resolves named colors. Never
  // carry arbitrary CSS, theme-dependent colors or transparency into storage.
  if (!/^[a-z]+$/.test(color) || ['inherit', 'initial', 'unset', 'revert', 'transparent', 'currentcolor'].includes(color)) return null
  const probe = document.createElement('span')
  probe.style.color = color
  if (!probe.style.color || !document.body) return null
  document.body.append(probe)
  const resolved = window.getComputedStyle(probe).color
  probe.remove()
  return resolved !== color ? clipboardColor(resolved) : null
}

function pixelWidth(value: string | null): number | null {
  const match = /^\s*(\d+(?:\.\d+)?)\s*(px|pt)?\s*$/i.exec(value || '')
  if (!match) return null
  const width = Math.round(Number(match[1]) * (match[2]?.toLowerCase() === 'pt' ? 4 / 3 : 1))
  return width > 0 && width <= 4096 ? width : null
}

/** Normalize inert clipboard HTML before ProseMirror applies its fixed schema. */
export function normalizeClipboardHTML(html: string, images?: { label: string; onUnsupported: () => void }): string {
  const doc = new DOMParser().parseFromString(html, 'text/html')
  doc.querySelectorAll('script,style,link,meta,iframe,object,embed,svg,form').forEach(node => node.remove())
  doc.querySelectorAll<HTMLElement>('*').forEach(node => {
    for (const attr of [...node.attributes]) if (/^on/i.test(attr.name)) node.removeAttribute(attr.name)
    if (node.tagName === 'FONT' && node.getAttribute('color')) node.style.color = node.getAttribute('color')!
    for (const property of ['color', 'background-color']) {
      const color = clipboardColor(node.style.getPropertyValue(property))
      node.style.removeProperty(property)
      if (color) node.style.setProperty(property, color)
    }
    // Word emits namespace-prefixed elements that otherwise swallow text.
    if (node.tagName.toLowerCase() === 'o:p' && !node.textContent?.trim()) node.remove()
  })
  let unsupportedImage = false
  doc.querySelectorAll<HTMLImageElement>('img').forEach(img => {
    // Copying from our authenticated reader must retain the stable reference,
    // not the temporary blob used to display it in the source browser tab.
    const src = img.getAttribute('data-protected-src') || img.getAttribute('src') || ''
    let valid = false
    if (src && src.length <= 4096 && !/[\x00\r\n\t\\]/.test(src)) {
      if (src.startsWith('/')) valid = !src.startsWith('//')
      else {
        try {
          const url = new URL(src)
          valid = ['http:', 'https:', 'resource:', 'storage:', 'local:', 'minio:', 'cos:', 'tos:', 's3:', 'oss:', 'ks3:', 'obs:'].includes(url.protocol) && !!url.host
        } catch { /* Local files and transient blob/data URLs require upload. */ }
      }
    }
    if (valid) img.setAttribute('src', src)
    else {
      unsupportedImage = true
      img.replaceWith(doc.createTextNode(`[${img.alt || images?.label || 'Image'}]`))
    }
  })
  if (unsupportedImage) images?.onUnsupported()
  for (const table of doc.querySelectorAll<HTMLTableElement>('table')) {
    const cols: (number | null)[] = []
    table.querySelectorAll<HTMLTableColElement>('colgroup > col').forEach(col => {
      if (col.closest('table') !== table) return
      const width = pixelWidth(col.style.width) || pixelWidth(col.getAttribute('width'))
      for (let i = 0; i < Math.min(col.span || 1, 100); i++) cols.push(width)
    })
    const occupied: number[] = []
    for (const row of Array.from(table.rows)) {
      let x = 0
      for (const cell of Array.from(row.cells)) {
        while ((occupied[x] || 0) > 0) x++
        const span = Math.min(cell.colSpan || 1, 100)
        const explicit = cell.getAttribute('colwidth')?.split(',').map(Number)
        const total = pixelWidth(cell.style.width) || pixelWidth(cell.getAttribute('width'))
        const widths = explicit?.length === span && explicit.every(w => Number.isInteger(w) && w >= 0 && w <= 4096)
          ? explicit : Array.from({ length: span }, (_, i) => cols[x + i] || (total ? Math.round(total / span) : 0))
        cell.removeAttribute('colwidth')
        if (widths.some(Boolean)) cell.setAttribute('colwidth', widths.join(','))
        for (let i = 0; i < span; i++) occupied[x + i] = Math.min(cell.rowSpan || 1, 100)
        x += span
      }
      for (let i = 0; i < occupied.length; i++) occupied[i] = Math.max(0, (occupied[i] || 0) - 1)
    }
  }
  return doc.body.innerHTML
}

/** Excel/Sheets TSV supports quoted tabs, line breaks and escaped quotes. */
export function clipboardTable(text: string): string[][] | null {
  if (!text.includes('\t') || text.length > 500_000) return null
  const rows: string[][] = [], row: string[] = []
  let value = '', quoted = false, closed = false
  const input = text.replace(/\r\n?/g, '\n')
  for (let i = 0; i < input.length; i++) {
    const c = input[i]!
    if (quoted) {
      if (c === '"') {
        if (input[i + 1] === '"') { value += '"'; i++ }
        else { quoted = false; closed = true }
      } else value += c
    } else if (c === '"' && value === '' && !closed) quoted = true
    else if (c === '\t' || c === '\n') {
      row.push(value); value = ''; closed = false
      if (row.length > 100) return null
      if (c === '\n') { rows.push(row.splice(0)); if (rows.length > 500) return null }
    } else {
      if (closed) return null
      value += c
    }
  }
  if (quoted) return null
  if (value || row.length || !input.endsWith('\n')) { row.push(value); rows.push(row) }
  if (rows.length < 2 || rows.length > 500 || rows[0]!.length < 2 || rows[0]!.length > 100 || rows.some(r => r.length !== rows[0]!.length)) return null
  return rows
}

export const RichClipboard = Extension.create({
  name: 'richClipboard', priority: 1100,
  addOptions() { return { imageLabel: 'Image', onUnsupportedImage: () => {} } },
  addProseMirrorPlugins() {
    return [new Plugin({ props: {
      transformPastedHTML: html => normalizeClipboardHTML(html, { label: this.options.imageLabel, onUnsupported: this.options.onUnsupportedImage }),
      // ProseMirror's someProp dispatcher accepts null to continue to the next
      // parser (as tiptap-markdown does), though its public type omits null.
      clipboardTextParser: ((text, context, plainText, view) => {
        if (plainText || context.parent.type.spec.code) return null
        for (let depth = context.depth; depth > 0; depth--) {
          if (['tableCell', 'tableHeader'].includes(context.node(depth).type.name)) return null
        }
        const rows = clipboardTable(text), s = view.state.schema.nodes
        if (!rows || !s.table || !s.tableRow || !s.tableCell || !s.paragraph) return null
        const table = s.table.create(null, rows.map(row => s.tableRow!.create(null, row.map(text => {
          const content = text.split('\n').map(line => s.paragraph!.create(null, line ? view.state.schema.text(line) : null))
          return s.tableCell!.create(null, content)
        }))))
        return new Slice(Fragment.from(table), 0, 0)
      }) as NonNullable<EditorProps['clipboardTextParser']>,
    } })]
  },
})
