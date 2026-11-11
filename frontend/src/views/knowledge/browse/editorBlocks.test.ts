import assert from 'node:assert/strict'
import { before, test } from 'node:test'
import { JSDOM } from 'jsdom'
import { Editor, type JSONContent } from '@tiptap/core'
import StarterKit from '@tiptap/starter-kit'
import { TableRow, TableCell, TableHeader } from '@tiptap/extension-table'
import { TaskList } from '@tiptap/extension-list'
import { AllSelection, TextSelection } from '@tiptap/pm/state'
import { GapCursor } from '@tiptap/pm/gapcursor'
import { CellSelection } from '@tiptap/pm/tables'
import { createEditorBlocks, runBlockAction, moveTopLevelBlock, topLevelBlocks, selectedTopLevelBlock, selectedBlockRange, resolveBlockRange, convertedBlock, canBlockAction } from './editorBlocks.ts'
import { RichTable, RichTaskItem, Highlight, TextColor, Callout } from './richDocumentExtensions.ts'
import { createEditorImage } from './editorImage.ts'
import { createEditorOutline } from './editorOutline.ts'
import { documentMarkdown, type StructuredDocument } from './structuredDocument.ts'
import { withinDocumentLimits } from './editorDocumentLimits.ts'

before(() => {
  const dom = new JSDOM('<html><body></body></html>', { url: 'http://localhost', pretendToBeVisual: true })
  Object.assign(globalThis, { window: dom.window, document: dom.window.document, Node: dom.window.Node,
    HTMLElement: dom.window.HTMLElement, DOMParser: dom.window.DOMParser,
    requestAnimationFrame: dom.window.requestAnimationFrame.bind(dom.window), cancelAnimationFrame: dom.window.cancelAnimationFrame.bind(dom.window) })
  dom.window.Range.prototype.getClientRects = () => [] as unknown as DOMRectList
  dom.window.Range.prototype.getBoundingClientRect = () => ({ left: 0, top: 0, right: 0, bottom: 0, width: 0, height: 0 } as DOMRect)
})
const paragraph = (text: string) => ({ type: 'paragraph', content: [{ type: 'text', text }] })
const mixed = [
  paragraph('Before'),
  { type: 'heading', attrs: { level: 2 }, content: [{ type: 'text', text: 'Heading', marks: [{ type: 'bold' }] }] },
  { type: 'image', attrs: { src: 'resource://synthetic-image', width: 320, height: null, align: 'right' } },
  { type: 'table', content: [{ type: 'tableRow', content: [{ type: 'tableCell', attrs: { colwidth: [160] }, content: [paragraph('Cell')] }] }] },
  { type: 'bulletList', content: [{ type: 'listItem', content: [paragraph('One')] }, { type: 'listItem', content: [paragraph('Two')] }] },
  { type: 'codeBlock', attrs: { language: 'javascript' }, content: [{ type: 'text', text: 'const keep = true' }] },
  paragraph('After'),
]
function setup(content: any[] = mixed) {
  let blocked = false, closed = 0, limits = 0
  const opened: number[] = [], outlines: string[][] = []
  const e = new Editor({ element: document.body.appendChild(document.createElement('div')),
    extensions: [StarterKit, TaskList, RichTaskItem.configure({ nested: true }), createEditorImage(() => {}),
      RichTable.configure({ resizable: false }), TableRow, TableCell, TableHeader, Highlight, TextColor, Callout,
      createEditorBlocks({ label: 'Block actions', blocked: () => blocked, open: pos => opened.push(pos), close: () => closed++, limit: () => limits++ }),
      createEditorOutline(items => outlines.push(items.map(item => item.text)), () => {})],
    content: { type: 'doc', content } })
  return { e, opened, outlines, block: (value: boolean) => { blocked = value }, closed: () => closed, limits: () => limits }
}
const snapshot = (e: Editor) => structuredClone(e.getJSON())
const blocks = (e: Editor) => topLevelBlocks(e.state.doc)

test('move and duplicate preserve complete tables, images, lists and code with independent undo/redo', () => {
  const { e } = setup()
  try {
    for (const index of [2, 3, 4, 5]) {
      const source = blocks(e)[index]!, before = snapshot(e), node = source.node.toJSON()
      assert.equal(runBlockAction(e.view, source.pos, 'up'), true)
      assert.deepEqual(e.state.doc.child(index - 1).toJSON(), node)
      assert.equal(e.commands.undo(), true); assert.deepEqual(snapshot(e), before)
      assert.equal(runBlockAction(e.view, blocks(e)[index]!.pos, 'duplicate'), true)
      assert.deepEqual(e.state.doc.child(index + 1).toJSON(), node)
      const duplicate = snapshot(e)
      assert.equal(e.commands.undo(), true); assert.deepEqual(snapshot(e), before)
      assert.equal(e.commands.redo(), true); assert.deepEqual(snapshot(e), duplicate)
      e.commands.undo()
    }
    assert.equal(e.state.doc.child(2).attrs.src, 'resource://synthetic-image')
  } finally { e.destroy() }
})

test('moves only accept top-level boundaries and support the beginning and end of the document', () => {
  const { e } = setup([paragraph('A'), paragraph('B'), paragraph('C')])
  try {
    const before = snapshot(e)
    assert.equal(runBlockAction(e.view, 0, 'up'), false)
    assert.equal(runBlockAction(e.view, blocks(e).at(-1)!.pos, 'down'), false)
    assert.equal(moveTopLevelBlock(e.view, 1, e.state.doc.content.size), false)
    assert.equal(moveTopLevelBlock(e.view, 0, 2), false)
    assert.equal(moveTopLevelBlock(e.view, 0, blocks(e)[1]!.pos), false)
    assert.deepEqual(snapshot(e), before)
    assert.equal(moveTopLevelBlock(e.view, 0, e.state.doc.content.size), true)
    assert.deepEqual(e.state.doc.content.content.map(n => n.textContent), ['B', 'C', 'A'])
    assert.equal(moveTopLevelBlock(e.view, blocks(e).at(-1)!.pos, 0), true)
    assert.deepEqual(snapshot(e), before)
    runBlockAction(e.view, 0, 'down')
    assert.deepEqual(e.state.doc.content.content.map(n => n.textContent), ['B', 'A', 'C'])
  } finally { e.destroy() }
})

test('deleting the final block leaves a valid empty paragraph and undo restores the whole node', () => {
  const { e } = setup([{ type: 'paragraph', content: [{ type: 'text', text: 'Last block', marks: [{ type: 'bold' }] }] }])
  try {
    const before = snapshot(e)
    assert.equal(runBlockAction(e.view, 0, 'delete'), true)
    assert.deepEqual(e.getJSON().content, [{ type: 'paragraph' }])
    e.commands.undo(); assert.deepEqual(snapshot(e), before)
    assert.equal(e.state.doc.type.validContent(e.state.doc.content), true)
  } finally { e.destroy() }
})

test('deleting complete complex blocks is independently undoable, including a sole image with the editor trailing paragraph', () => {
  const { e } = setup()
  try {
    for (const index of [2, 3, 4, 5]) {
      const before = snapshot(e)
      assert.equal(runBlockAction(e.view, blocks(e)[index]!.pos, 'delete'), true)
      assert.equal(e.state.doc.childCount, before.content!.length - 1)
      assert.equal(e.commands.undo(), true)
      assert.deepEqual(snapshot(e), before)
    }
    e.commands.setContent({ type: 'doc', content: [mixed[2]] })
    // StarterKit appends a writable trailing paragraph after non-text final nodes.
    const before = snapshot(e)
    assert.equal(before.content!.length, 2)
    assert.equal(runBlockAction(e.view, 0, 'delete'), true)
    assert.deepEqual(e.getJSON().content, [{ type: 'paragraph' }])
    assert.equal(e.commands.undo(), true)
    assert.deepEqual(snapshot(e), before)
  } finally { e.destroy() }
})

test('compatible conversions retain inline formatting and content, and update the live outline', () => {
  const { e, outlines } = setup([{ type: 'paragraph', content: [{ type: 'text', text: 'Marked', marks: [
    { type: 'bold' }, { type: 'textColor', attrs: { color: '#1677ff' } }, { type: 'link', attrs: { href: '/platform/documents/open/11111111-1111-4111-8111-111111111111' } },
  ] }] }])
  try {
    const inline = e.state.doc.firstChild!.content.toJSON()
    assert.equal(runBlockAction(e.view, 0, 'heading2'), true)
    assert.deepEqual(outlines.at(-1), ['Marked'])
    assert.deepEqual(e.state.doc.firstChild!.content.toJSON(), inline)
    assert.equal(runBlockAction(e.view, 0, 'bulletList'), true)
    assert.deepEqual(e.state.doc.firstChild!.firstChild!.firstChild!.content.toJSON(), inline)
    assert.equal(runBlockAction(e.view, 0, 'blockquote'), true)
    assert.deepEqual(e.state.doc.firstChild!.firstChild!.content.toJSON(), inline)
    assert.equal(runBlockAction(e.view, 0, 'paragraph'), true)
    assert.deepEqual(e.state.doc.firstChild!.content.toJSON(), inline)
    assert.deepEqual(outlines.at(-1), [])
    const saved = snapshot(e), other = setup([paragraph('x')]).e
    try { other.commands.setContent(saved); assert.deepEqual(snapshot(other), saved) } finally { other.destroy() }
  } finally { e.destroy() }
})

test('flat lists unwrap completely while nested lists, tables, code and images cannot be flattened', () => {
  const { e } = setup()
  try {
    const list = blocks(e)[4]!
    assert.equal(runBlockAction(e.view, list.pos, 'heading3'), true)
    assert.equal(e.state.doc.child(4).textContent, 'One'); assert.equal(e.state.doc.child(5).textContent, 'Two')
    assert.equal(e.state.doc.child(5).attrs.level, 3)
    e.commands.undo()
    for (const i of [2, 3, 5]) assert.equal(convertedBlock(blocks(e)[i]!.node, 'paragraph', e.schema), null)
    const nested = e.schema.nodes.bulletList!.create(null, e.schema.nodes.listItem!.create(null, [e.schema.nodes.paragraph!.create(null, e.schema.text('Parent')), list.node]))
    assert.equal(convertedBlock(nested, 'paragraph', e.schema), null)
    assert.equal(convertedBlock(list.node, 'bulletList', e.schema), null)
  } finally { e.destroy() }
})

test('a selection inside a table or nested list resolves to its outer block; decorations never enter stored content', () => {
  const { e } = setup()
  try {
    const table = blocks(e)[3]!
    e.commands.setTextSelection(table.pos + 4)
    assert.equal(selectedTopLevelBlock(e.state), table.pos)
    assert.equal(e.view.dom.querySelectorAll('.editor-block-handle').length, e.state.doc.childCount)
    assert.doesNotMatch(e.getHTML(), /editor-block|draggable/)
    assert.doesNotMatch(JSON.stringify(e.getJSON()), /editor-block|Block actions/)
    assert.match(documentMarkdown({ schema_version: 1, content: e.getJSON() as StructuredDocument['content'] }), /Cell/)
  } finally { e.destroy() }
})

function dragData() {
  const data = new Map<string, string>()
  return { get types() { return [...data.keys()] }, effectAllowed: '', dropEffect: '', setData: (key: string, value: string) => data.set(key, value), getData: (key: string) => data.get(key) || '' }
}
function event(target: Element, type: string, data: ReturnType<typeof dragData>, y = 9999) {
  const ev = new window.Event(type, { bubbles: true, cancelable: true })
  Object.assign(ev, { dataTransfer: data, clientY: y, clientX: 50, relatedTarget: null })
  target.dispatchEvent(ev); return ev
}

test('handle drag uses an editor-local token, shows a drop boundary, moves atomically and rejects foreign data', () => {
  const a = setup([paragraph('A'), paragraph('B'), paragraph('C')]), b = setup([paragraph('Other')])
  try {
    const data = dragData(), before = snapshot(a.e)
    event(a.e.view.dom.querySelector('.editor-block-handle')!, 'dragstart', data)
    event(a.e.view.dom, 'dragover', data)
    assert.ok(a.e.view.dom.querySelector('.editor-block-drop'))
    event(b.e.view.dom, 'drop', data)
    assert.equal(b.e.state.doc.firstChild!.textContent, 'Other')
    event(a.e.view.dom, 'drop', data)
    assert.deepEqual(a.e.state.doc.content.content.map(n => n.textContent), ['B', 'C', 'A'])
    assert.equal(a.e.view.dom.querySelector('.editor-block-drop'), null)
    a.e.commands.undo(); assert.deepEqual(snapshot(a.e), before)
    const forged = dragData(); forged.setData('application/x-weknora-block', 'forged')
    event(a.e.view.dom, 'drop', forged)
    assert.deepEqual(snapshot(a.e), before)
  } finally { a.e.destroy(); b.e.destroy() }
})

test('changed documents, drag cancellation, uploads, and readonly state cannot move a stale block', () => {
  const { e, block, opened } = setup([paragraph('A'), paragraph('B')])
  try {
    const data = dragData()
    let handle = e.view.dom.querySelector('.editor-block-handle')!
    event(handle, 'dragstart', data)
    e.commands.insertContentAt(1, 'Changed ')
    const before = snapshot(e)
    event(e.view.dom, 'drop', data); assert.deepEqual(snapshot(e), before)
    handle = e.view.dom.querySelector('.editor-block-handle')!
    event(handle, 'dragstart', data); event(e.view.dom, 'dragover', data); event(handle, 'dragend', data)
    event(e.view.dom, 'drop', data); assert.deepEqual(snapshot(e), before)
    block(true); handle.dispatchEvent(new window.MouseEvent('click', { bubbles: true }))
    assert.deepEqual(opened, [])
    assert.equal(event(handle, 'dragstart', data).defaultPrevented, true)
    block(false); e.setEditable(false)
    assert.equal(runBlockAction(e.view, 0, 'duplicate'), false)
    event(handle, 'dragstart', data); event(e.view.dom, 'drop', data)
    assert.deepEqual(snapshot(e), before)
  } finally { e.destroy() }
})

test('oversized duplication is rejected before dispatch and does not add an undo step', () => {
  const { e } = setup([paragraph('a'.repeat(1_100_000))])
  try {
    let limits = 0
    const before = snapshot(e)
    assert.equal(runBlockAction(e.view, 0, 'duplicate', () => limits++), false)
    assert.equal(limits, 1); assert.deepEqual(snapshot(e), before)
    assert.equal(e.commands.undo(), false)
  } finally { e.destroy() }
})

test('partial and backwards text selections expand to whole outer blocks; boundary endpoints exclude the next block', () => {
  const { e } = setup()
  try {
    const first = blocks(e)[1]!, last = blocks(e)[4]!
    e.commands.setTextSelection({ from: first.pos + 2, to: last.pos + 4 })
    const range = { from: first.pos, to: last.pos + last.node.nodeSize }
    assert.deepEqual(selectedBlockRange(e.state), range)
    assert.deepEqual(selectedBlockRange(e.state, blocks(e)[2]!.pos), range)
    assert.deepEqual(selectedBlockRange(e.state, 0), { from: 0, to: blocks(e)[0]!.node.nodeSize })
    e.view.dispatch(e.state.tr.setSelection(TextSelection.create(e.state.doc, last.pos + 4, first.pos + 2)))
    assert.deepEqual(selectedBlockRange(e.state), range)
    e.view.dispatch(e.state.tr.setSelection(new AllSelection(e.state.doc)))
    assert.deepEqual(selectedBlockRange(e.state), { from: 0, to: e.state.doc.content.size })
    // A selection ending exactly at a boundary must not pick the next image.
    e.commands.setNodeSelection(first.pos)
    assert.deepEqual(selectedBlockRange(e.state), { from: first.pos, to: first.pos + first.node.nodeSize })
    const table = blocks(e)[3]!
    e.view.dispatch(e.state.tr.setSelection(CellSelection.create(e.state.doc, table.pos + 2)))
    assert.deepEqual(selectedBlockRange(e.state), { from: table.pos, to: table.pos + table.node.nodeSize })
    e.view.dispatch(e.state.tr.setSelection(new GapCursor(e.state.doc.resolve(e.state.doc.content.size))))
    assert.deepEqual(selectedBlockRange(e.state), { from: blocks(e).at(-1)!.pos, to: e.state.doc.content.size })
    assert.equal(resolveBlockRange(e.state.doc, { from: first.pos + 1, to: range.to }), null)
    assert.equal(resolveBlockRange(e.state.doc, { from: range.from, to: range.to - 1 }), null)
    assert.equal(resolveBlockRange(e.state.doc, { from: range.to, to: range.from }), null)
  } finally { e.destroy() }
})

test('mixed block groups duplicate and delete atomically with complete resources, marks, tables and nested content', () => {
  const { e } = setup()
  try {
    const from = blocks(e)[1]!.pos, to = blocks(e)[5]!.pos
    const range = { from, to }, before = snapshot(e), originals = before.content!.slice(1, 5)
    assert.equal(runBlockAction(e.view, range, 'duplicate'), true)
    assert.deepEqual(snapshot(e).content!.slice(5, 9), originals)
    assert.equal(e.state.doc.childCount, 11)
    e.commands.undo(); assert.deepEqual(snapshot(e), before)
    assert.equal(runBlockAction(e.view, range, 'delete'), true)
    assert.deepEqual(snapshot(e).content, [before.content![0], ...before.content!.slice(5)])
    assert.equal(e.commands.undo(), true); assert.deepEqual(snapshot(e), before)
    assert.equal(e.commands.redo(), true)
    assert.equal(e.state.doc.childCount, 3)
  } finally { e.destroy() }
})

test('moving a group swaps one neighbour while retaining order, and refuses interior or malformed destinations', () => {
  const { e } = setup([paragraph('A'), paragraph('B'), paragraph('C'), paragraph('D'), paragraph('E')])
  try {
    const before = snapshot(e), range = { from: blocks(e)[1]!.pos, to: blocks(e)[3]!.pos }
    assert.equal(canBlockAction(e.state, range, 'up'), true)
    assert.equal(canBlockAction(e.state, { from: 0, to: range.to }, 'up'), false)
    assert.equal(canBlockAction(e.state, { from: range.from, to: e.state.doc.content.size }, 'down'), false)
    for (const destination of [range.from, blocks(e)[2]!.pos, range.to, 2]) {
      assert.equal(moveTopLevelBlock(e.view, range, destination), false)
      assert.deepEqual(snapshot(e), before)
    }
    assert.equal(runBlockAction(e.view, range, 'up'), true)
    assert.deepEqual(e.state.doc.content.content.map(n => n.textContent), ['B', 'C', 'A', 'D', 'E'])
    e.commands.undo()
    assert.equal(runBlockAction(e.view, range, 'down'), true)
    assert.deepEqual(e.state.doc.content.content.map(n => n.textContent), ['A', 'D', 'B', 'C', 'E'])
    e.commands.undo()
    assert.equal(moveTopLevelBlock(e.view, range, e.state.doc.content.size), true)
    assert.deepEqual(e.state.doc.content.content.map(n => n.textContent), ['A', 'D', 'E', 'B', 'C'])
    e.commands.undo(); assert.deepEqual(snapshot(e), before)
  } finally { e.destroy() }
})

test('select all can duplicate or clear the document in one undo step; multi-block conversion never flattens content', () => {
  const { e } = setup()
  try {
    const before = snapshot(e), range = { from: 0, to: e.state.doc.content.size }
    assert.equal(runBlockAction(e.view, range, 'heading2'), false)
    assert.deepEqual(snapshot(e), before)
    assert.equal(runBlockAction(e.view, range, 'duplicate'), true)
    assert.deepEqual(snapshot(e).content, [...before.content!, ...before.content!])
    e.commands.undo(); assert.deepEqual(snapshot(e), before)
    assert.equal(runBlockAction(e.view, range, 'delete'), true)
    assert.deepEqual(e.getJSON().content, [{ type: 'paragraph' }])
    assert.equal(e.commands.undo(), true); assert.deepEqual(snapshot(e), before)
  } finally { e.destroy() }
})

test('multi-block highlighting follows the selection, never enters stored content, and clears on collapse', () => {
  const { e } = setup([paragraph('A'), paragraph('B'), paragraph('C')])
  try {
    const before = snapshot(e)
    e.commands.setTextSelection({ from: 1, to: 5 })
    assert.equal(e.view.dom.querySelectorAll('.editor-block-range').length, 2)
    assert.deepEqual(snapshot(e), before)
    assert.doesNotMatch(e.getHTML(), /editor-block-range/)
    assert.equal(e.commands.undo(), false)
    e.commands.setTextSelection(1)
    assert.equal(e.view.dom.querySelector('.editor-block-range'), null)
  } finally { e.destroy() }
})

test('dragging a selected handle moves the whole group, while an unselected handle still moves only its block', () => {
  const { e } = setup([paragraph('A'), paragraph('B'), paragraph('C'), paragraph('D')])
  try {
    e.commands.setTextSelection({ from: 1, to: 5 })
    const before = snapshot(e), data = dragData()
    event(e.view.dom.querySelectorAll('.editor-block-handle')[1]!, 'dragstart', data)
    event(e.view.dom, 'dragover', data)
    event(e.view.dom, 'drop', data)
    assert.deepEqual(e.state.doc.content.content.map(n => n.textContent), ['C', 'D', 'A', 'B'])
    e.commands.undo(); assert.deepEqual(snapshot(e), before)
    e.commands.setTextSelection({ from: 1, to: 5 })
    event(e.view.dom.querySelectorAll('.editor-block-handle')[2]!, 'dragstart', data)
    event(e.view.dom, 'drop', data)
    assert.deepEqual(e.state.doc.content.content.map(n => n.textContent), ['A', 'B', 'D', 'C'])
  } finally { e.destroy() }
})

test('group operations respect readonly, composition and document size without partially changing content', () => {
  const { e } = setup([paragraph('a'.repeat(550_000)), paragraph('b'.repeat(550_000))])
  try {
    const before = snapshot(e), range = { from: 0, to: e.state.doc.content.size }
    let limit = 0
    assert.equal(runBlockAction(e.view, range, 'duplicate', () => limit++), false)
    assert.equal(limit, 1); assert.deepEqual(snapshot(e), before)
    assert.equal(e.commands.undo(), false)
    Object.defineProperty(e.view, 'composing', { value: true, configurable: true })
    assert.equal(runBlockAction(e.view, range, 'delete'), false)
    Object.defineProperty(e.view, 'composing', { value: false, configurable: true })
    e.setEditable(false)
    assert.equal(runBlockAction(e.view, range, 'delete'), false)
    assert.deepEqual(snapshot(e), before)
  } finally { e.destroy() }
})

test('block dragging works on HTTP origins without crypto.randomUUID', t => {
  const getRandomValues = globalThis.crypto.getRandomValues.bind(globalThis.crypto)
  t.mock.getter(globalThis, 'crypto', () => ({ getRandomValues }) as Crypto)
  assert.equal(typeof crypto.randomUUID, 'undefined')
  const { e } = setup([paragraph('A'), paragraph('B')])
  try {
    const data = dragData()
    event(e.view.dom.querySelector('.editor-block-handle')!, 'dragstart', data)
    assert.match(data.getData('application/x-weknora-block'), /^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/)
    event(e.view.dom, 'drop', data)
    assert.deepEqual(e.state.doc.content.content.map(n => n.textContent), ['B', 'A'])
  } finally { e.destroy() }
})

test('every mixed block range moves like a stable array splice, with no loss or splitting and exact undo', () => {
  const { e } = setup()
  try {
    const before = snapshot(e), original = before.content!
    const positions = blocks(e), destinations = [...positions.map(b => b.pos), e.state.doc.content.size]
    let verified = 0
    for (let start = 0; start < positions.length; start++) {
      for (let end = start + 1; end <= positions.length; end++) {
        const range = { from: positions[start]!.pos, to: end === positions.length ? e.state.doc.content.size : positions[end]!.pos }
        for (let index = 0; index < destinations.length; index++) {
          const allowed = index < start || index > end
          assert.equal(moveTopLevelBlock(e.view, range, destinations[index]!), allowed)
          if (allowed) {
            const expected: JSONContent[] = structuredClone(original), moved = expected.splice(start, end - start)
            expected.splice(index > end ? index - moved.length : index, 0, ...moved)
            if (expected.at(-1)!.type !== 'paragraph') expected.push({ type: 'paragraph' })
            assert.deepEqual(snapshot(e).content, expected)
            assert.equal(e.state.doc.type.validContent(e.state.doc.content), true)
            assert.equal(withinDocumentLimits(e.state.doc), true)
            assert.equal(e.commands.undo(), true)
          }
          assert.deepEqual(snapshot(e), before)
          verified++
        }
      }
    }
    assert.equal(verified, 224)
  } finally { e.destroy() }
})

test('capacity checks include the writable trailing paragraph appended after a move', () => {
  const { e } = setup([...Array.from({ length: 9998 }, () => paragraph('x')), mixed[2], paragraph('last')])
  try {
    assert.equal(withinDocumentLimits(e.state.doc), true)
    const before = snapshot(e)
    let limits = 0
    assert.equal(runBlockAction(e.view, blocks(e).at(-1)!.pos, 'up', () => limits++), false)
    assert.equal(limits, 1)
    assert.deepEqual(snapshot(e), before)
    assert.equal(e.commands.undo(), false)
  } finally { e.destroy() }
})
