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
import { TableMap, CellSelection } from '@tiptap/pm/tables'
import type { TablePasteProblem } from './editorTablePaste.ts'

before(() => {
  const dom = new JSDOM('<html><body></body></html>', { url: 'http://localhost', pretendToBeVisual: true })
  Object.assign(globalThis, { window: dom.window, document: dom.window.document, Node: dom.window.Node,
    HTMLElement: dom.window.HTMLElement, DOMParser: dom.window.DOMParser,
    ClipboardEvent: class extends dom.window.Event { clipboardData = null },
    requestAnimationFrame: dom.window.requestAnimationFrame.bind(dom.window), cancelAnimationFrame: dom.window.cancelAnimationFrame.bind(dom.window) })
})
function editor(onTablePasteRejected: (problem: TablePasteProblem) => void = () => {}) {
  return new Editor({ element: document.body.appendChild(document.createElement('div')), extensions: [
    StarterKit, RichClipboard.configure({ onTablePasteRejected }), Highlight, TextColor, RichTable.configure({ resizable: false }), TableRow, TableCell, TableHeader,
    createEditorToolkit({ placeholder: () => '', uploading: () => '', onFiles: () => { throw new Error('HTML must not be replaced by preview image') }, onKeyDown: () => false }),
    Markdown.configure({ html: false, transformPastedText: true }),
  ] })
}
function snapshot(e: Editor): StructuredDocument { return { schema_version: 1, content: e.getJSON() as StructuredDocument['content'] } }

function grid(e: Editor) {
  const table = e.state.doc.firstChild!, map = TableMap.get(table)
  return Array.from({ length: map.height }, (_, y) => Array.from({ length: map.width }, (_, x) =>
    table.nodeAt(map.map[y * map.width + x]!)!.textContent))
}
function setTable(e: Editor, rows = 3, cols = 3) {
  e.commands.setContent({ type: 'doc', content: [{ type: 'table', content: Array.from({ length: rows }, (_, y) => ({
    type: 'tableRow', content: Array.from({ length: cols }, (_, x) => ({
      type: y ? 'tableCell' : 'tableHeader', attrs: { colwidth: [120 + x * 20] },
      content: [{ type: 'paragraph', content: [{ type: 'text', text: `${y}:${x}` }] }],
    })),
  })) }] })
}
function selectCell(e: Editor, row: number, col: number, endRow?: number, endCol?: number) {
  const map = TableMap.get(e.state.doc.firstChild!)
  const pos = 1 + map.map[row * map.width + col]!
  if (endRow !== undefined) e.commands.setCellSelection({ anchorCell: pos, headCell: 1 + map.map[endRow * map.width + endCol!]! })
  else e.commands.setTextSelection(pos + 2)
}
function paste(e: Editor, text: string, html = '') {
  const event = new window.Event('paste', { bubbles: true, cancelable: true })
  Object.assign(event, { clipboardData: { files: [], getData: (type: string) => type === 'text/plain' ? text : type === 'text/html' ? html : '' } })
  e.view.dom.dispatchEvent(event)
  return event
}

test('spreadsheet paste fills existing cells, preserves headers and widths, and undoes as one isolated action', () => {
  const e = editor()
  try {
    setTable(e)
    selectCell(e, 0, 1)
    e.commands.insertContent('before paste')
    const before = snapshot(e)
    assert.equal(paste(e, 'Name\tNote\n"A\tB"\t"line1\nline2"').defaultPrevented, true)
    assert.deepEqual(grid(e), [['0:0', 'Name', 'Note'], ['1:0', 'A\tB', 'line1line2'], ['2:0', '2:1', '2:2']])
    const table = e.state.doc.firstChild!
    assert.equal(table.child(0).child(1).type.name, 'tableHeader')
    assert.deepEqual(table.child(0).child(1).attrs.colwidth, [140])
    assert.deepEqual(table.child(1).child(2).attrs.colwidth, [160])
    assert.equal(table.child(1).child(2).childCount, 2)
    const after = snapshot(e)
    assert.equal(e.commands.undo(), true); assert.deepEqual(snapshot(e), before)
    assert.equal(e.commands.redo(), true); assert.deepEqual(snapshot(e), after)
    selectCell(e, 2, 2); e.commands.insertContent('after paste')
    assert.equal(e.commands.undo(), true); assert.deepEqual(snapshot(e), after)
    assert.equal(e.commands.undo(), true); assert.deepEqual(snapshot(e), before)
  } finally { e.destroy() }
})

test('one selected cell anchors the complete matrix and grows rows and columns without touching surrounding values', () => {
  const e = editor()
  try {
    setTable(e, 2, 2); selectCell(e, 1, 1, 1, 1)
    paste(e, 'A\tB\nC\tD')
    assert.deepEqual(grid(e), [['0:0', '0:1', ''], ['1:0', 'A', 'B'], ['', 'C', 'D']])
    assert.equal(TableMap.get(e.state.doc.firstChild!).problems, null)
    let tables = 0
    e.state.doc.descendants(node => { if (node.type.name === 'table') tables++ })
    assert.equal(tables, 1)
    const saved = snapshot(e)
    const reloaded = editor()
    try { reloaded.commands.setContent(saved.content); assert.deepEqual(snapshot(reloaded), saved) } finally { reloaded.destroy() }
    assert.match(documentMarkdown(saved), /A.*B/)
    assert.match(documentHTML(saved, x => x), /<td[^>]*><p>D<\/p>/)
  } finally { e.destroy() }
})

test('single rows, single columns, trailing empty cells and formula-like strings remain literal cell values', () => {
  const e = editor()
  try {
    setTable(e); selectCell(e, 0, 0); paste(e, '=SUM(A1:A2)\t+1\t')
    assert.deepEqual(grid(e)[0], ['=SUM(A1:A2)', '+1', ''])
    selectCell(e, 0, 2); paste(e, 'first\r\nsecond\r\nthird\r\n')
    assert.deepEqual(grid(e).map(row => row[2]), ['first', 'second', 'third'])
    assert.deepEqual(clipboardTable('single\trow', true), [['single', 'row']])
    assert.deepEqual(clipboardTable('a\nb\n', true), [['a'], ['b']])
  } finally { e.destroy() }
})

test('expanding a header row retains header types and existing alignment', () => {
  const e = editor()
  try {
    setTable(e, 2, 2); selectCell(e, 0, 1)
    e.commands.setCellAttribute('align', 'right')
    paste(e, 'A\tB\tC')
    assert.deepEqual(grid(e)[0], ['0:0', 'A', 'B', 'C'])
    const row = e.state.doc.firstChild!.child(0)
    row.forEach(cell => assert.equal(cell.type.name, 'tableHeader'))
    assert.equal(row.child(1).attrs.align, 'right')
    assert.equal(TableMap.get(e.state.doc.firstChild!).problems, null)
  } finally { e.destroy() }
})

test('rectangular selection accepts matching matrices and single values but rejects clipping or implicit repetition', () => {
  const errors: TablePasteProblem[] = [], e = editor(problem => errors.push(problem))
  try {
    setTable(e); selectCell(e, 0, 0, 1, 1)
    const before = snapshot(e), selection = e.state.selection.toJSON()
    paste(e, 'A\tB\tC\nD\tE\tF')
    assert.deepEqual(errors, ['selection']); assert.deepEqual(snapshot(e), before)
    assert.deepEqual(e.state.selection.toJSON(), selection)
    paste(e, 'one\ttwo')
    assert.deepEqual(errors, ['selection', 'selection']); assert.deepEqual(snapshot(e), before)
    paste(e, 'A\tB\nC\tD')
    assert.deepEqual(grid(e), [['A', 'B', '0:2'], ['C', 'D', '1:2'], ['2:0', '2:1', '2:2']])
    selectCell(e, 0, 0, 1, 1); paste(e, 'all')
    assert.deepEqual(grid(e).slice(0, 2).map(row => row.slice(0, 2)), [['all', 'all'], ['all', 'all']])
    selectCell(e, 0, 0, 1, 1); paste(e, 'bold', '<table><tr><td><b>bold</b></td></tr></table>')
    assert.deepEqual(grid(e).slice(0, 2).map(row => row.slice(0, 2)), [['bold', 'bold'], ['bold', 'bold']])
    assert.ok(e.state.doc.firstChild!.child(1).child(1).firstChild!.firstChild!.marks.some(mark => mark.type.name === 'bold'))
  } finally { e.destroy() }
})

test('HTML spreadsheet paste retains rich cell content while preserving destination widths and header types', () => {
  const e = editor()
  try {
    setTable(e); selectCell(e, 0, 0)
    paste(e, 'Bold\tRed\nC\tD', '<table><tr><td><b>Bold</b></td><td><span style="color:#ff0000">Red</span></td></tr><tr><td>C</td><td>D</td></tr></table>')
    assert.deepEqual(grid(e), [['Bold', 'Red', '0:2'], ['C', 'D', '1:2'], ['2:0', '2:1', '2:2']])
    assert.equal(e.state.doc.firstChild!.child(0).child(0).type.name, 'tableHeader')
    assert.deepEqual(e.state.doc.firstChild!.child(0).child(1).attrs.colwidth, [140])
    assert.match(JSON.stringify(snapshot(e)), /#ff0000/)
    assert.match(documentMarkdown(snapshot(e)), /\*\*Bold\*\*/)
  } finally { e.destroy() }
})

test('merged destination cells reject the whole paste without changes, while unrelated merged cells remain intact', () => {
  const errors: TablePasteProblem[] = [], e = editor(problem => errors.push(problem))
  try {
    setTable(e); selectCell(e, 0, 0, 0, 1); e.commands.mergeCells()
    selectCell(e, 0, 0)
    const before = snapshot(e)
    paste(e, 'A\tB\nC\tD')
    assert.deepEqual(errors, ['merged']); assert.deepEqual(snapshot(e), before)
    selectCell(e, 1, 1); paste(e, 'A\tB\nC\tD')
    assert.deepEqual(grid(e)[2], ['2:0', 'C', 'D'])
    assert.equal(e.state.doc.firstChild!.child(0).child(0).attrs.colspan, 2)
    assert.equal(TableMap.get(e.state.doc.firstChild!).problems, null)
  } finally { e.destroy() }
})

test('source merged cells remain valid when pasted into unmerged destinations', () => {
  const e = editor()
  try {
    setTable(e); selectCell(e, 0, 0)
    paste(e, '', '<table><tr><td colspan="2">merged</td></tr><tr><td>A</td><td>B</td></tr></table>')
    assert.equal(e.state.doc.firstChild!.child(0).child(0).attrs.colspan, 2)
    assert.deepEqual(e.state.doc.firstChild!.child(0).child(0).attrs.colwidth, [120, 140])
    assert.equal(TableMap.get(e.state.doc.firstChild!).problems, null)
    assert.doesNotThrow(() => documentMarkdown(snapshot(e)))
  } finally { e.destroy() }
})

test('native copied cell slices retain a rectangular selection and support reversed selections', () => {
  const source = editor(), target = editor()
  try {
    setTable(source); selectCell(source, 2, 2, 1, 1)
    assert.ok(source.state.selection instanceof CellSelection)
    const copied = source.view.serializeForClipboard(source.state.selection.content())
    setTable(target); selectCell(target, 1, 1, 0, 0)
    paste(target, copied.text, copied.dom.innerHTML)
    assert.deepEqual(grid(target), [['1:1', '1:2', '0:2'], ['2:1', '2:2', '1:2'], ['2:0', '2:1', '2:2']])
    assert.equal(TableMap.get(target.state.doc.firstChild!).problems, null)
  } finally { source.destroy(); target.destroy() }
})

test('plain paste, code inside a cell and a disabled editor bypass spreadsheet filling', () => {
  const e = editor()
  try {
    setTable(e); selectCell(e, 0, 0)
    e.view.dom.dispatchEvent(new window.KeyboardEvent('keydown', { key: 'Shift', keyCode: 16, shiftKey: true, bubbles: true }))
    paste(e, 'A\tB\nC\tD')
    e.view.dom.dispatchEvent(new window.KeyboardEvent('keyup', { key: 'Shift', keyCode: 16, bubbles: true }))
    assert.equal(grid(e)[0]![1], '0:1')
    setTable(e); selectCell(e, 0, 0); e.commands.setCodeBlock()
    paste(e, 'A\tB\nC\tD')
    assert.equal(grid(e)[0]![1], '0:1')
    assert.match(e.state.doc.firstChild!.child(0).child(0).textContent, /A\tB\nC\tD/)
    setTable(e); selectCell(e, 0, 0); e.setEditable(false)
    const before = snapshot(e)
    paste(e, 'A\tB\nC\tD')
    assert.deepEqual(snapshot(e), before)
  } finally { e.destroy() }
})

test('table dimension and document complexity limits reject atomically, including raw oversized TSV', () => {
  const errors: TablePasteProblem[] = [], e = editor(problem => errors.push(problem))
  try {
    setTable(e); selectCell(e, 2, 2)
    const before = snapshot(e)
    paste(e, Array.from({ length: 99 }, () => 'x').join('\t'))
    assert.deepEqual(errors, ['limit']); assert.deepEqual(snapshot(e), before)
    paste(e, Array.from({ length: 101 }, () => 'x').join('\t'))
    assert.deepEqual(errors, ['limit', 'limit']); assert.deepEqual(snapshot(e), before)
    paste(e, Array.from({ length: 501 }, () => 'x').join('\n'))
    paste(e, Array.from({ length: 250 }, () => Array.from({ length: 30 }, () => 'x').join('\t')).join('\n'))
    assert.deepEqual(errors, ['limit', 'limit', 'limit', 'limit']); assert.deepEqual(snapshot(e), before)
    paste(e, '', '<table><tr><td>' + '汉'.repeat(710_000) + '</td></tr></table>')
    assert.equal(errors.at(-1), 'limit'); assert.deepEqual(snapshot(e), before)
    paste(e, 'x\t' + 'y'.repeat(500_000))
    assert.equal(errors.length, 6); assert.deepEqual(snapshot(e), before)
    const tallMerge = '<table><tr><td rowspan="101">merged</td><td>top</td></tr>' + '<tr><td>row</td></tr>'.repeat(100) + '</table>'
    paste(e, '', tallMerge)
    assert.equal(errors.length, 7); assert.deepEqual(snapshot(e), before)
    assert.ok(e.state.selection.empty)
  } finally { e.destroy() }
})

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
