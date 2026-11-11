import assert from 'node:assert/strict'
import { before, test } from 'node:test'
import { JSDOM } from 'jsdom'
import { Editor, type JSONContent } from '@tiptap/core'
import StarterKit from '@tiptap/starter-kit'
import { createEditorOutline, editorHeadings, jumpToEditorHeading, editorSections, editorOutlineKey, moveEditorSection } from './editorOutline.ts'
import { TableRow, TableCell, TableHeader } from '@tiptap/extension-table'
import { RichTable } from './richDocumentExtensions.ts'
import { createEditorImage } from './editorImage.ts'
import type { OutlineItem } from './types.ts'

before(() => {
  const dom = new JSDOM('<html><body></body></html>', { url: 'http://localhost', pretendToBeVisual: true })
  Object.assign(globalThis, { window: dom.window, document: dom.window.document, Node: dom.window.Node,
    HTMLElement: dom.window.HTMLElement, DOMParser: dom.window.DOMParser,
    requestAnimationFrame: dom.window.requestAnimationFrame.bind(dom.window), cancelAnimationFrame: dom.window.cancelAnimationFrame.bind(dom.window) })
  dom.window.Range.prototype.getClientRects = () => [] as unknown as DOMRectList
  dom.window.Range.prototype.getBoundingClientRect = () => ({ left: 0, top: 0, right: 0, bottom: 0, width: 0, height: 0 } as DOMRect)
})
function setup(content: string | JSONContent = '<p>Introduction</p><h1>Same</h1><p>Body</p><h2>Same</h2><p>Last body</p>') {
  const lists: OutlineItem[][] = [], events: OutlineItem[][] = [], active: number[] = []
  let blocked = false
  const e = new Editor({ element: document.body.appendChild(document.createElement('div')), content,
    extensions: [StarterKit, RichTable, TableRow, TableCell, TableHeader, createEditorImage(() => {}),
      createEditorOutline(items => { events.push(items); lists.push(items.map(({ text, level }) => ({ text, level }))) }, index => active.push(index), () => blocked)] })
  return { e, lists, events, active, block(value: boolean) { blocked = value; e.view.dispatch(e.state.tr) } }
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

const p = (text: string): JSONContent => ({ type: 'paragraph', content: [{ type: 'text', text }] })
const h = (level: number, text: string): JSONContent => ({ type: 'heading', attrs: { level }, ...(text ? { content: [{ type: 'text', text, marks: [{ type: 'bold' }] }] } : {}) })
const snapshot = (e: Editor) => structuredClone(e.getJSON())
function move(e: Editor, index: number, direction: 'up' | 'down') {
  return moveEditorSection(e, { index, direction, token: editorOutlineKey.getState(e.state)!.token })
}
const sectionsContent: JSONContent = { type: 'doc', content: [
  p('Preface'), h(1, 'Same'), p('A introduction'), h(2, 'Child A'), p('A body'),
  { type: 'table', content: [{ type: 'tableRow', content: [{ type: 'tableCell', attrs: { colwidth: [160] }, content: [p('Whole table')] }] }] },
  h(3, 'Subheading'), { type: 'image', attrs: { src: 'resource://fixture-image', width: 240, align: 'right' } },
  h(2, 'Child B'), p('B body'),
  { type: 'blockquote', content: [h(2, 'Nested quote heading'), p('Quote body')] },
  h(1, 'Same'), p('Second chapter'), h(2, ''), p('Empty-heading section'),
  h(1, 'Last'), p('Last body'),
] }

test('section bounds include deeper headings and containers, while only same-parent equal-level peers can move', () => {
  const { e, events } = setup(sectionsContent)
  try {
    const sections = editorSections(e.state.doc), heads = editorHeadings(e.state.doc)
    assert.equal(sections.length, heads.length - 1)
    assert.equal(sections[0]!.to, sections[4]!.from)
    assert.equal(sections[0]!.down, 4)
    assert.equal(sections[1]!.to, sections[3]!.from)
    assert.equal(sections[1]!.down, 3)
    assert.equal(sections[3]!.up, 1)
    assert.equal(sections[2]!.up, undefined)
    assert.equal(sections[5]!.up, undefined)
    const items = events.at(-1)!
    assert.deepEqual(items[0]!.section, { token: editorOutlineKey.getState(e.state)!.token, up: false, down: true })
    assert.equal(items[4]!.section, undefined)
    assert.equal(items[6]!.text, '')
    assert.equal(move(e, 4, 'up'), false)
    assert.equal(move(e, 6, 'up'), false)
  } finally { e.destroy() }
})

test('whole chapter movement preserves child headings, tables, images, quotes and preface with exact independent undo/redo', () => {
  const { e, lists, active } = setup(sectionsContent)
  try {
    const before = snapshot(e), original = before.content!, boundaries = editorSections(e.state.doc)
    const start = boundaries[0]!.from, end = boundaries[0]!.to
    const moved = e.state.doc.slice(start, end).content.toJSON()
    assert.equal(move(e, 0, 'down'), true)
    assert.equal(e.state.doc.firstChild!.textContent, 'Preface')
    assert.deepEqual(snapshot(e).content, [...original.slice(0, 1), ...original.slice(11, 15), ...original.slice(1, 11), ...original.slice(15)])
    const current = editorSections(e.state.doc).find(s => s.from === editorHeadings(e.state.doc)[2]!.pos)!
    assert.deepEqual(e.state.doc.slice(current.from, current.to).content.toJSON(), moved)
    assert.equal(e.state.selection.$from.parent.type.name, 'heading')
    assert.equal(e.state.selection.$from.parentOffset, 0)
    assert.equal(document.activeElement, e.view.dom)
    assert.equal(active.at(-1), 2)
    assert.deepEqual(lists.at(-1)!.map(i => i.text), ['Same', '', 'Same', 'Child A', 'Subheading', 'Child B', 'Nested quote heading', 'Last'])
    const after = snapshot(e)
    assert.equal(e.commands.undo(), true); assert.deepEqual(snapshot(e), before)
    assert.equal(e.commands.redo(), true); assert.deepEqual(snapshot(e), after)
    e.commands.undo()
    assert.equal(move(e, 5, 'up'), true)
    assert.deepEqual(snapshot(e), after)
  } finally { e.destroy() }
})

test('moving a child section includes its descendants and leaves it under the same parent', () => {
  const { e } = setup(sectionsContent)
  try {
    const before = snapshot(e), nodes = before.content!
    assert.equal(move(e, 1, 'down'), true)
    assert.deepEqual(snapshot(e).content, [...nodes.slice(0, 3), ...nodes.slice(8, 11), ...nodes.slice(3, 8), ...nodes.slice(11)])
    assert.equal(move(e, 3, 'down'), false)
    e.commands.undo(); assert.deepEqual(snapshot(e), before)
    assert.equal(move(e, 3, 'up'), true)
    e.commands.undo(); assert.deepEqual(snapshot(e), before)
  } finally { e.destroy() }
})

test('skipped levels and nested headings cannot move across a parent boundary or split a container', () => {
  const { e } = setup({ type: 'doc', content: [h(1, 'Root'), h(3, 'Skipped'), p('Keep'), h(2, 'New child'),
    h(3, 'Nested child'), p('Body'), h(2, 'Peer'), p('Peer body'), h(1, 'Next root'), h(3, 'Another parent'), p('End')] })
  try {
    const before = snapshot(e)
    for (const [index, direction] of [[1, 'down'], [3, 'up'], [6, 'up']] as const) assert.equal(move(e, index, direction), false)
    assert.deepEqual(snapshot(e), before)
    assert.equal(move(e, 2, 'down'), true)
    assert.deepEqual(editorHeadings(e.state.doc).map(item => item.text), ['Root', 'Skipped', 'Peer', 'New child', 'Nested child', 'Next root', 'Another parent'])
  } finally { e.destroy() }
})

test('old, foreign, malformed and busy outline events cannot modify the current document', () => {
  const { e, events, block } = setup(sectionsContent), other = setup(sectionsContent).e
  try {
    const token = editorOutlineKey.getState(e.state)!.token
    const request = { index: 0, direction: 'down' as const, token }
    const initialEvents = events.length
    e.commands.setTextSelection(2)
    assert.equal(events.length, initialEvents)
    e.commands.insertContentAt(1, 'Changed ')
    const before = snapshot(e)
    assert.equal(moveEditorSection(e, request), false)
    assert.equal(moveEditorSection(other, request), false)
    for (const index of [-1, 0.5, NaN, 100]) assert.equal(move(e, index, 'down'), false)
    block(true)
    assert.ok(events.at(-1)!.every(item => !item.section))
    assert.equal(move(e, 0, 'down'), false)
    block(false)
    assert.ok(events.at(-1)!.some(item => item.section))
    Object.defineProperty(e.view, 'composing', { value: true, configurable: true })
    assert.equal(move(e, 0, 'down'), false)
    Object.defineProperty(e.view, 'composing', { value: false, configurable: true })
    e.setEditable(false)
    assert.equal(move(e, 0, 'down'), false)
    assert.ok(events.at(-1)!.every(item => !item.section))
    assert.deepEqual(snapshot(e), before)
  } finally { e.destroy(); other.destroy() }
  assert.equal(moveEditorSection(e, { index: 0, direction: 'down', token: 1 }), false)
})

test('outline movement metadata never enters stored content and is rebuilt on reload', () => {
  const { e } = setup(sectionsContent)
  try {
    const before = snapshot(e)
    assert.equal(move(e, 0, 'down'), true)
    const saved = snapshot(e), other = setup(saved).e
    try {
      assert.deepEqual(snapshot(other), saved)
      assert.doesNotMatch(JSON.stringify(saved), /"token"|sectionTools|"section"|"parent"/)
      assert.notEqual(editorOutlineKey.getState(other.state)!.token, editorOutlineKey.getState(e.state)!.token)
      assert.equal(move(other, 2, 'up'), true)
      assert.deepEqual(snapshot(other), before)
    } finally { other.destroy() }
  } finally { e.destroy() }
})

test('moving a section at the document limit refuses a trailing-paragraph overflow without changing content or history', () => {
  const { e } = setup({ type: 'doc', content: [...Array.from({ length: 9996 }, () => p('x')),
    h(1, 'A'), { type: 'image', attrs: { src: 'resource://fixture-limit' } }, h(1, 'B'), p('last')] })
  try {
    const before = snapshot(e)
    let limits = 0
    assert.equal(moveEditorSection(e, { index: 1, direction: 'up', token: editorOutlineKey.getState(e.state)!.token }, () => limits++), false)
    assert.equal(limits, 1)
    assert.deepEqual(snapshot(e), before)
    assert.equal(e.commands.undo(), false)
  } finally { e.destroy() }
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
