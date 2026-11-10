import assert from 'node:assert/strict'
import { before, test } from 'node:test'
import { JSDOM } from 'jsdom'
import { Editor } from '@tiptap/core'
import StarterKit from '@tiptap/starter-kit'
import { createEditorImage } from './editorImage.ts'
import { imageAlignment, imageDimension, imageLayoutStyle } from './imageLayout.ts'
import { documentHTML, documentMarkdown, sameDocument, type StructuredDocument } from './structuredDocument.ts'

before(() => {
  const dom = new JSDOM('<html><body></body></html>', { url: 'http://localhost', pretendToBeVisual: true })
  Object.assign(globalThis, { window: dom.window, document: dom.window.document, Node: dom.window.Node,
    HTMLElement: dom.window.HTMLElement, DOMParser: dom.window.DOMParser,
    requestAnimationFrame: dom.window.requestAnimationFrame.bind(dom.window), cancelAnimationFrame: dom.window.cancelAnimationFrame.bind(dom.window) })
})
function editor(attrs: Record<string, unknown> = {}, hydrate: (root: HTMLElement) => void = () => {}) {
  return new Editor({ element: document.body.appendChild(document.createElement('div')),
    extensions: [StarterKit, createEditorImage(hydrate)],
    content: { type: 'doc', content: [{ type: 'image', attrs: { src: 'resource://synthetic-image', alt: 'Example', ...attrs } }] } })
}
function snapshot(e: Editor): StructuredDocument { return { schema_version: 1, content: e.getJSON() as StructuredDocument['content'] } }
function handles(e: Editor) {
  const handle = e.view.dom.querySelector<HTMLButtonElement>('.editor-image__resize')!
  const frame = e.view.dom.querySelector<HTMLElement>('.editor-image__frame')!
  const img = frame.querySelector('img')!
  handle.setPointerCapture = () => {}
  img.getBoundingClientRect = () => ({ width: Number.parseFloat(frame.style.width) || 400, height: 200 } as DOMRect)
  const pointer = (type: string, x: number, id = 1) => {
    const event = new window.Event(type, { bubbles: true, cancelable: true })
    Object.assign(event, { button: 0, clientX: x, pointerId: id }); handle.dispatchEvent(event)
  }
  return { handle, frame, img, pointer }
}

test('image layout accepts only bounded dimensions and fixed alignment values', () => {
  for (const value of [0, -1, 1.5, 4097, Infinity, '320', '1px', null]) assert.equal(imageDimension(value), null)
  assert.equal(imageDimension(1), 1); assert.equal(imageDimension(4096), 4096)
  assert.equal(imageAlignment('center'), 'center'); assert.equal(imageAlignment('right'), 'right')
  const style = imageLayoutStyle({ width: '1px;position:fixed', align: 'right;background:url(evil)' })
  assert.doesNotMatch(style, /fixed|evil/)
  assert.match(style, /margin-left:0;margin-right:auto/)
})

test('image dimensions and alignment round-trip through structured reload and sanitized HTML attributes', () => {
  const e = editor({ width: 320, height: 160, align: 'center' })
  try {
    const saved = snapshot(e), html = e.getHTML()
    assert.match(html, /width:\s*320px/); assert.match(html, /margin-left:\s*auto;\s*margin-right:\s*auto/)
    const copy = editor()
    try {
      copy.commands.setContent(html)
      assert.equal(copy.getAttributes('image').width, 320)
      const image = copy.state.doc.firstChild!
      assert.equal(image.attrs.align, 'center'); assert.equal(image.attrs.height, 160)
      copy.commands.setContent(saved.content)
      assert.deepEqual(copy.state.doc.firstChild!.toJSON(), saved.content.content![0])
    } finally { copy.destroy() }
    assert.match(documentHTML(saved, s => s), /width="320".*width:320px/)
    const changed = structuredClone(saved)
    changed.content.content![0]!.attrs!.align = 'right'
    assert.equal(documentMarkdown(changed), documentMarkdown(saved))
    assert.equal(sameDocument(changed, saved), false)
    e.commands.setContent('<img src="https://example.invalid/image.png" width="99999" height="-2" style="width:99%;margin-left:auto;margin-right:auto" data-image-align="center">')
    assert.equal(e.state.doc.firstChild!.attrs.width, null)
    assert.equal(e.state.doc.firstChild!.attrs.height, null)
    assert.equal(e.state.doc.firstChild!.attrs.align, 'center')
  } finally { e.destroy() }
})

test('pointer resize previews without content writes, commits once, and supports isolated undo/redo', () => {
  let hydrated = 0
  const e = editor({ width: 320, height: 160 }, root => {
    hydrated++; root.querySelector('img')!.setAttribute('src', 'blob:display-only')
  })
  try {
    e.commands.setNodeSelection(0)
    const h = handles(e), before = snapshot(e)
    assert.equal(h.handle.hidden, false)
    h.pointer('pointerdown', 100); h.pointer('pointermove', 180)
    assert.equal(h.frame.style.width, '400px'); assert.deepEqual(snapshot(e), before)
    h.pointer('pointerup', 180)
    assert.equal(e.state.doc.firstChild!.attrs.width, 400)
    assert.equal(e.state.doc.firstChild!.attrs.height, null)
    assert.equal(e.state.doc.firstChild!.attrs.src, 'resource://synthetic-image')
    assert.equal(h.img.getAttribute('src'), 'blob:display-only'); assert.equal(hydrated, 1)
    const after = snapshot(e)
    assert.equal(e.commands.undo(), true); assert.deepEqual(snapshot(e), before)
    assert.equal(e.commands.redo(), true); assert.deepEqual(snapshot(e), after)
    assert.equal(hydrated, 1)
  } finally { e.destroy() }
})

test('pointer cancellation, capture loss and Escape restore the previous size without saving', () => {
  const e = editor({ width: 200 })
  try {
    e.commands.setNodeSelection(0)
    const h = handles(e), before = snapshot(e)
    for (const cancel of ['pointercancel', 'lostpointercapture', 'Escape']) {
      h.pointer('pointerdown', 100); h.pointer('pointermove', 250)
      if (cancel === 'Escape') h.handle.dispatchEvent(new window.KeyboardEvent('keydown', { key: 'Escape', bubbles: true }))
      else h.pointer(cancel, 250)
      h.pointer('pointerup', 250)
      assert.equal(h.frame.style.width, '200px'); assert.deepEqual(snapshot(e), before)
    }
  } finally { e.destroy() }
})

test('clicking the resize handle without dragging keeps automatic sizing and creates no content change', () => {
  const e = editor()
  try {
    e.commands.setNodeSelection(0)
    const h = handles(e), before = snapshot(e)
    h.pointer('pointerdown', 100); h.pointer('pointerup', 100)
    assert.deepEqual(snapshot(e), before)
    assert.equal(e.state.doc.firstChild!.attrs.width, null)
  } finally { e.destroy() }
})

test('right and centered images resize from their visual anchor; keyboard resizing and Home preserve alignment', () => {
  for (const [align, end, width] of [['right', 50, 250], ['center', 150, 300]] as const) {
    const e = editor({ width: 200, align })
    try {
      e.commands.setNodeSelection(0)
      const h = handles(e)
      h.pointer('pointerdown', 100); h.pointer('pointermove', end); h.pointer('pointerup', end)
      assert.equal(e.state.doc.firstChild!.attrs.width, width)
      h.handle.dispatchEvent(new window.KeyboardEvent('keydown', { key: 'ArrowRight', bubbles: true }))
      assert.equal(e.state.doc.firstChild!.attrs.width, width + 16)
      h.handle.dispatchEvent(new window.KeyboardEvent('keydown', { key: 'Home', bubbles: true }))
      assert.equal(e.state.doc.firstChild!.attrs.width, null)
      assert.equal(e.state.doc.firstChild!.attrs.height, null)
      assert.equal(e.state.doc.firstChild!.attrs.align, align)
    } finally { e.destroy() }
  }
})

test('resize resolves the image position after adjacent editing, clamps to its container, and refuses readonly writes', () => {
  const e = editor({ width: 200 })
  try {
    e.commands.setNodeSelection(0)
    const h = handles(e)
    Object.defineProperty(h.frame.parentElement!.parentElement!, 'clientWidth', { value: 300 })
    h.pointer('pointerdown', 100); h.pointer('pointermove', 2000)
    e.view.dispatch(e.state.tr.insert(0, e.schema.nodes.paragraph!.create(null, e.schema.text('Keep later input'))))
    h.pointer('pointerup', 2000)
    assert.equal(e.state.doc.child(1).attrs.width, 300)
    assert.match(e.getText(), /Keep later input/)
    e.setEditable(false)
    const before = snapshot(e)
    h.pointer('pointerdown', 100); h.pointer('pointermove', 1); h.pointer('pointerup', 1)
    h.handle.dispatchEvent(new window.KeyboardEvent('keydown', { key: 'ArrowLeft', bubbles: true }))
    assert.deepEqual(snapshot(e), before)
  } finally { e.destroy() }
})
