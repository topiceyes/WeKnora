import assert from 'node:assert/strict'
import { before, test } from 'node:test'
import { JSDOM } from 'jsdom'
import { Editor } from '@tiptap/core'
import StarterKit from '@tiptap/starter-kit'
import { createEditorOutline, editorHeadings, jumpToEditorHeading } from './editorOutline.ts'
import type { OutlineItem } from './types.ts'

before(() => {
  const dom = new JSDOM('<html><body></body></html>', { url: 'http://localhost', pretendToBeVisual: true })
  Object.assign(globalThis, { window: dom.window, document: dom.window.document, Node: dom.window.Node,
    HTMLElement: dom.window.HTMLElement, DOMParser: dom.window.DOMParser,
    requestAnimationFrame: dom.window.requestAnimationFrame.bind(dom.window), cancelAnimationFrame: dom.window.cancelAnimationFrame.bind(dom.window) })
  dom.window.Range.prototype.getClientRects = () => [] as unknown as DOMRectList
  dom.window.Range.prototype.getBoundingClientRect = () => ({ left: 0, top: 0, right: 0, bottom: 0, width: 0, height: 0 } as DOMRect)
})
function setup(content = '<p>Introduction</p><h1>Same</h1><p>Body</p><h2>Same</h2><p>Last body</p>') {
  const lists: OutlineItem[][] = [], active: number[] = []
  const e = new Editor({ element: document.body.appendChild(document.createElement('div')), content,
    extensions: [StarterKit, createEditorOutline(items => lists.push(items), index => active.push(index))] })
  return { e, lists, active }
}

test('outline initializes from the editor, includes nested and empty headings, and ignores Markdown-looking text', () => {
  const { e, lists, active } = setup('<p># not a heading</p><h1>Top</h1><blockquote><h3><strong>Nested</strong> 😀</h3></blockquote><h2></h2><pre><code>## code</code></pre>')
  try {
    assert.deepEqual(lists.at(-1), [{ text: 'Top', level: 1 }, { text: 'Nested 😀', level: 3 }, { text: '', level: 2 }])
    assert.equal(active.at(-1), -1)
    for (const item of editorHeadings(e.state.doc)) assert.equal(e.state.doc.nodeAt(item.pos)?.type.name, 'heading')
  } finally { e.destroy() }
  assert.deepEqual(lists.at(-1), []); assert.equal(active.at(-1), -1)
})

test('heading text, levels, insertion, deletion and undo update the live list', () => {
  const { e, lists } = setup()
  try {
    jumpToEditorHeading(e, 0)
    e.commands.insertContent('Renamed ')
    assert.equal(lists.at(-1)![0]!.text, 'Renamed Same')
    e.commands.setHeading({ level: 3 })
    assert.equal(lists.at(-1)![0]!.level, 3)
    e.commands.setParagraph()
    assert.deepEqual(lists.at(-1), [{ text: 'Same', level: 2 }])
    e.commands.undo()
    assert.equal(lists.at(-1)!.length, 2)
    e.commands.insertContentAt(0, '<h4>Inserted</h4>')
    assert.equal(lists.at(-1)![0]!.text, 'Inserted')
    const item = editorHeadings(e.state.doc)[0]!, node = e.state.doc.nodeAt(item.pos)!
    e.commands.deleteRange({ from: item.pos, to: item.pos + node.nodeSize })
    assert.notEqual(lists.at(-1)![0]!.text, 'Inserted')
  } finally { e.destroy() }
})

test('selection updates active section without re-emitting the list or modifying content', () => {
  const { e, lists, active } = setup()
  try {
    const before = e.getJSON(), scans = lists.length
    const headings = editorHeadings(e.state.doc)
    e.commands.setTextSelection(headings[0]!.pos + 1)
    assert.equal(active.at(-1), 0)
    e.commands.setTextSelection(headings[1]!.pos + 1)
    assert.equal(active.at(-1), 1)
    e.commands.setTextSelection(e.state.doc.content.size - 1)
    assert.equal(active.at(-1), 1)
    e.commands.setTextSelection(1)
    assert.equal(active.at(-1), -1)
    assert.equal(lists.length, scans)
    assert.deepEqual(e.getJSON(), before)
  } finally { e.destroy() }
})

test('jump resolves duplicate titles after preceding edits, focuses the caret and adds no undoable change', () => {
  const { e, active } = setup()
  try {
    e.commands.insertContentAt(1, 'Prefix ')
    const before = e.getJSON()
    jumpToEditorHeading(e, 1)
    assert.equal(e.state.selection.from, editorHeadings(e.state.doc)[1]!.pos + 1)
    assert.equal(active.at(-1), 1)
    assert.equal(document.activeElement, e.view.dom)
    assert.deepEqual(e.getJSON(), before)
    e.commands.undo()
    assert.equal(e.state.doc.firstChild!.textContent, 'Introduction')
    const pos = e.state.selection.from
    for (const invalid of [-1, 10, 0.5, NaN]) jumpToEditorHeading(e, invalid)
    assert.equal(e.state.selection.from, pos)
    e.setEditable(false); jumpToEditorHeading(e, 1)
    assert.equal(e.state.selection.from, pos)
  } finally { e.destroy() }
  assert.doesNotThrow(() => jumpToEditorHeading(e, 0))
})

test('external content reset replaces old positions and an empty document clears the active section', () => {
  const { e, lists, active } = setup()
  try {
    jumpToEditorHeading(e, 1)
    e.commands.setContent('<h4>Replacement</h4><p>new body</p>', { emitUpdate: false })
    assert.deepEqual(lists.at(-1), [{ text: 'Replacement', level: 4 }])
    jumpToEditorHeading(e, 0)
    assert.equal(e.state.selection.from, 1)
    e.commands.setContent('<p>Empty outline</p>', { emitUpdate: false })
    assert.deepEqual(lists.at(-1), []); assert.equal(active.at(-1), -1)
    assert.doesNotThrow(() => jumpToEditorHeading(e, 0))
  } finally { e.destroy() }
})
