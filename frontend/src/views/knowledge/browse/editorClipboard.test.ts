import assert from 'node:assert/strict'
import { before, test } from 'node:test'
import { JSDOM } from 'jsdom'
import { Editor } from '@tiptap/core'
import StarterKit from '@tiptap/starter-kit'
import { TableRow, TableCell, TableHeader } from '@tiptap/extension-table'
import { Markdown } from 'tiptap-markdown'
import { Highlight, RichTable, TextColor } from './richDocumentExtensions.ts'
import { clipboardColor, clipboardTable, normalizeClipboardHTML, RichClipboard } from './editorClipboard.ts'
import { documentHTML, documentMarkdown, type StructuredDocument } from './structuredDocument.ts'
import { createEditorToolkit } from './editorToolkit.ts'

before(() => {
  const dom = new JSDOM('<html><body></body></html>', { url: 'http://localhost', pretendToBeVisual: true })
  Object.assign(globalThis, { window: dom.window, document: dom.window.document, Node: dom.window.Node,
    HTMLElement: dom.window.HTMLElement, DOMParser: dom.window.DOMParser,
    ClipboardEvent: class extends dom.window.Event { clipboardData = null },
    requestAnimationFrame: dom.window.requestAnimationFrame.bind(dom.window), cancelAnimationFrame: dom.window.cancelAnimationFrame.bind(dom.window) })
})
function editor() {
  return new Editor({ element: document.body.appendChild(document.createElement('div')), extensions: [
    StarterKit, RichClipboard, Highlight, TextColor, RichTable.configure({ resizable: false }), TableRow, TableCell, TableHeader,
    createEditorToolkit({ placeholder: () => '', uploading: () => '', onFiles: () => { throw new Error('HTML must not be replaced by preview image') }, onKeyDown: () => false }),
    Markdown.configure({ html: false, transformPastedText: true }),
  ] })
}
function snapshot(e: Editor): StructuredDocument { return { schema_version: 1, content: e.getJSON() as StructuredDocument['content'] } }

test('opaque CSS colors normalize without persisting CSS expressions or transparency', () => {
  assert.equal(clipboardColor('#F80'), '#ff8800')
  assert.equal(clipboardColor('rgb(22, 119, 255)'), '#1677ff')
  assert.equal(clipboardColor('navy'), '#000080')
  for (const value of ['rgba(1,2,3,0.5)', 'transparent', 'currentColor', 'var(--secret)', 'red; background:url(evil)', 'rgb(999,0,0)']) assert.equal(clipboardColor(value), null)
})

test('real HTML paste keeps external nested colors, highlight, semantic lists and bold text', () => {
  const e = editor()
  try {
    assert.equal(e.view.pasteHTML('<h2>网页标题</h2><p style="color:navy"><b>外层颜色</b><span style="color:rgb(22,119,255);background-color:#ff0">重点</span></p><ul><li>第一条</li><li>第二条</li></ul>'), true)
    const doc = snapshot(e), json = JSON.stringify(doc)
    assert.match(json, /#000080/); assert.match(json, /#1677ff/); assert.match(json, /#ffff00/)
    assert.match(documentMarkdown(doc), /## 网页标题/)
    assert.match(documentMarkdown(doc), /\*\*外层颜色\*\*/)
    assert.match(documentMarkdown(doc), /- 第一条\n- 第二条/)
    assert.match(documentHTML(doc, s => s), /<mark style="background-color:#ffff00/)
    assert.equal(e.commands.undo(), true)
    assert.equal(e.getText(), '')
  } finally { e.destroy() }
})

test('Office table paste retains spans, point/pixel widths, alignment and multiple cell paragraphs', () => {
  const e = editor()
  try {
    e.view.pasteHTML('<table><colgroup><col style="width:90pt"><col width="200"></colgroup><tr><td colspan="2" style="text-align:center"><p>合并标题</p></td></tr><tr><td rowspan="2"><p>A1</p><p>A2</p></td><td><p>B1</p></td></tr><tr><td><p>B2</p></td></tr></table>')
    const table = snapshot(e).content.content![0]!
    assert.equal(table.type, 'table')
    assert.deepEqual(table.content![0]!.content![0]!.attrs!.colwidth, [120, 200])
    assert.equal(table.content![0]!.content![0]!.attrs!.colspan, 2)
    assert.equal(table.content![0]!.content![0]!.attrs!.align, 'center')
    assert.equal(table.content![1]!.content![0]!.attrs!.rowspan, 2)
    assert.deepEqual(table.content![2]!.content![0]!.attrs!.colwidth, [200])
    assert.match(documentMarkdown(snapshot(e)), /A1 A2/)
    assert.equal(e.commands.undo(), true)
    assert.equal(e.getText(), '')
  } finally { e.destroy() }
})

test('individual cell widths survive when no colgroup exists and nested tables use their own columns', () => {
  const normalized = new DOMParser().parseFromString(normalizeClipboardHTML('<table><tr><td style="width:144pt"><p>outer</p><table><colgroup><col width="70"></colgroup><tr><td>inner</td></tr></table></td><td width="160">right</td></tr></table>'), 'text/html')
  assert.equal(normalized.querySelector('td')!.getAttribute('colwidth'), '192')
  assert.equal(normalized.querySelector('td table td')!.getAttribute('colwidth'), '70')
})

test('TSV keeps quoted tabs, embedded newlines, escaped quotes and empty cells', () => {
  const text = 'Name\tNote\tCount\r\n"A\tB"\t"line1\nline2"\t\r\nC\t"Say ""yes"""\t3\r\n'
  assert.deepEqual(clipboardTable(text), [['Name', 'Note', 'Count'], ['A\tB', 'line1\nline2', ''], ['C', 'Say "yes"', '3']])
  const e = editor()
  try {
    const clipboard = { clipboardData: { files: [], getData: (type: string) => type === 'text/plain' ? text : '' } }
    const event = new window.Event('paste', { bubbles: true, cancelable: true })
    Object.assign(event, clipboard)
    e.view.dom.dispatchEvent(event)
    assert.equal(event.defaultPrevented, true)
    assert.equal(snapshot(e).content.content![0]!.type, 'table')
    assert.match(e.getText(), /line1/); assert.match(e.getText(), /line2/)
    assert.match(e.getText(), /Say "yes"/)
    assert.equal(e.commands.undo(), true)
    assert.equal(e.getText(), '')
    for (const value of ['single\trow', 'a\tb\nc', 'a\tb\n"unterminated\tc', 'a\tb\n"closed"bad\tc']) assert.equal(clipboardTable(value), null)
  } finally { e.destroy() }
})

test('plain-text paste, code blocks and pasting within a table never create nested spreadsheet tables', () => {
  const e = editor()
  try {
    const value = 'a\tb\nc\td'
    const parser = e.view.someProp('clipboardTextParser')!
    assert.equal(parser(value, e.state.selection.$from, true, e.view), null)
    e.commands.setCodeBlock()
    e.view.pasteText(value)
    assert.equal(snapshot(e).content.content![0]!.type, 'codeBlock')
    e.commands.setContent({ type: 'doc', content: [{ type: 'table', content: [{ type: 'tableRow', content: [{ type: 'tableCell', content: [{ type: 'paragraph' }] }] }] }] })
    e.commands.setTextSelection(4)
    e.view.pasteText(value)
    let tables = 0
    e.state.doc.descendants(node => { if (node.type.name === 'table') tables++ })
    assert.equal(tables, 1)
  } finally { e.destroy() }
})

test('mixed HTML and image clipboard retains the body instead of pasting only its preview', () => {
  const e = editor()
  try {
    const data = { files: [new window.File(['x'], 'preview.png', { type: 'image/png' })], getData: (type: string) => type === 'text/html' ? '<p>Complete document</p>' : type === 'text/plain' ? 'Complete document' : '' }
    assert.equal(e.view.pasteHTML(data.getData('text/html'), { clipboardData: data, preventDefault() {} } as unknown as ClipboardEvent), true)
    assert.equal(e.getText(), 'Complete document')
  } finally { e.destroy() }
})

test('pasted executable HTML and unsafe styles never enter the document', () => {
  const e = editor()
  try {
    e.view.pasteHTML('<style>p{color:red}</style><script>alert(1)</script><p onclick="bad()">Safe <span style="color:var(--secret)">text</span><a href="javascript:alert(1)">link</a></p>')
    assert.equal(e.getText(), 'Safe textlink')
    assert.doesNotMatch(JSON.stringify(snapshot(e)), /javascript:|onclick|alert|var\(/)
  } finally { e.destroy() }
})

test('protected reader images use stable references and unsupported embedded files retain descriptions', () => {
  let warnings = 0
  const html = normalizeClipboardHTML('<p>Before</p><img src="blob:display" data-protected-src="resource://file-123" alt="stored"><img src="file:///private/image.png" alt="Local chart"><img src="data:image/png;base64,eA==">', { label: 'Image', onUnsupported: () => { warnings++ } })
  assert.equal(warnings, 1)
  assert.match(html, /src="resource:\/\/file-123"/)
  assert.match(html, /\[Local chart\]/)
  assert.match(html, /\[Image\]/)
  assert.doesNotMatch(html, /src="(?:blob:|file:|data:)/)
})
