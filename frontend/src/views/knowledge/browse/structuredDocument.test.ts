import assert from 'node:assert/strict'
import { before, test } from 'node:test'
import { readFileSync } from 'node:fs'
import { JSDOM } from 'jsdom'
import { Editor, generateJSON } from '@tiptap/core'
import { StarterKit } from '@tiptap/starter-kit'
import { TaskList } from '@tiptap/extension-list'
import { TableRow, TableCell, TableHeader } from '@tiptap/extension-table'
import { createEditorImage } from './editorImage.ts'
import { Markdown } from 'tiptap-markdown'
import { Callout, Highlight, RichTable, RichTaskItem, TextColor } from './richDocumentExtensions.ts'
import { documentHTML, documentMarkdown, readStructuredDocument, sameDocument, type StructuredDocument } from './structuredDocument.ts'
import { buildBlockMathExtension, buildInlineMathExtension } from './editorMathExtensions.ts'

const cases: { name: string; document: StructuredDocument; markdown: string }[] = JSON.parse(readFileSync(
  new URL('../../../../../testdata/structured-documents.json', import.meta.url), 'utf8'))

before(() => {
  const dom = new JSDOM('<html><body></body></html>', { url: 'http://localhost', pretendToBeVisual: true })
  Object.assign(globalThis, { document: dom.window.document, window: dom.window, Node: dom.window.Node,
    HTMLElement: dom.window.HTMLElement, DOMParser: dom.window.DOMParser, localStorage: dom.window.localStorage,
    requestAnimationFrame: dom.window.requestAnimationFrame.bind(dom.window), cancelAnimationFrame: dom.window.cancelAnimationFrame.bind(dom.window) })
})
function extensions() {
  return [StarterKit.configure({ heading: { levels: [1, 2, 3, 4] } }), TaskList, RichTaskItem.configure({ nested: true }),
      createEditorImage(() => {}), Highlight, TextColor, Callout, RichTable.configure({ resizable: false }), TableRow, TableCell, TableHeader,
      buildBlockMathExtension(() => {}), buildInlineMathExtension(() => {}), Markdown.configure({ html: false })]
}
function editor(document: StructuredDocument) {
  return new Editor({ element: globalThis.document.createElement('div'), content: document.content,
    enableContentCheck: true, extensions: extensions() })
}

for (const example of cases) test(`shared Markdown projection and rich reload: ${example.name}`, () => {
  assert.equal(documentMarkdown(example.document), example.markdown)
  const e = editor(example.document)
  try {
    const saved: StructuredDocument = { schema_version: 1, content: e.getJSON() as StructuredDocument['content'] }
    const reloaded = editor(JSON.parse(JSON.stringify(saved)))
    try {
      assert.equal(sameDocument({ schema_version: 1, content: reloaded.getJSON() as any }, saved), true)
      assert.equal(documentMarkdown(saved), example.markdown)
    } finally { reloaded.destroy() }
  } finally { e.destroy() }
})

test('read rendering preserves highlight, callout, merged cells and column widths', () => {
  const html = documentHTML(cases[0]!.document, value => value)
  assert.match(html, /<mark style="background-color:#fff3a3/)
  assert.match(html, /<span style="color:#1677ff/)
  assert.match(html, /rich-document-callout--warning/)
  const table = documentHTML(cases[1]!.document, value => value)
  assert.match(table, /colspan="2"/)
  assert.match(table, /width:160px/)
  assert.match(table, /width:240px/)
})

test('rendering treats hostile text as text and does not spread arbitrary attributes or unsafe URLs', () => {
  const doc: StructuredDocument = { schema_version: 1, content: { type: 'doc', content: [
    { type: 'paragraph', content: [{ type: 'text', text: '<img src=x onerror=alert(1)>', marks: [
      { type: 'link', attrs: { href: 'javascript:alert(1)', onclick: 'bad' } },
      { type: 'textColor', attrs: { color: 'red;background:url(evil)' } },
    ] }] }, { type: 'image', attrs: { src: 'javascript:evil', onerror: 'bad' } },
  ] } }
  const html = documentHTML(doc, value => value)
  assert.match(html, /&lt;img/)
  assert.doesNotMatch(html, /<img|<a|background:url|onclick=/)
  assert.throws(() => readStructuredDocument({ schema_version: 2, content: { type: 'doc' } }))
})

test('format-only edits change document identity while JSON key order does not', () => {
  const original = cases[0]!.document
  const changed = JSON.parse(JSON.stringify(original)) as StructuredDocument
  changed.content.content![1]!.content![0]!.marks![0]!.attrs!.color = '#c8f7c5'
  assert.equal(documentMarkdown(original), documentMarkdown(changed))
  assert.equal(sameDocument(original, changed), false)
  assert.equal(sameDocument(original, { content: original.content, schema_version: 1 }), true)
})

test('deeply nested lists project every item once', () => {
  let node: StructuredDocument['content'] = { type: 'paragraph', content: [{ type: 'text', text: 'leaf' }] }
  for (let level = 0; level < 24; level++) node = { type: 'bulletList', content: [{ type: 'listItem', content: [
    { type: 'paragraph', content: [{ type: 'text', text: 'level' }] }, node,
  ] }] }
  const markdown = documentMarkdown({ schema_version: 1, content: { type: 'doc', content: [node] } })
  assert.equal(markdown.match(/level/g)?.length, 24)
  assert.equal(markdown.match(/leaf/g)?.length, 1)
})

test('merged and multi-paragraph tables never serialize to a data-losing placeholder', () => {
  const e = editor(cases[1]!.document)
  try {
    const md = (e.storage as any).markdown.getMarkdown()
    assert.doesNotMatch(md, /\[table\]/)
    assert.match(md, /合并标题/)
    assert.match(md, /第一列/)
    const saved = e.getJSON() as StructuredDocument['content']
    assert.equal(saved.content?.[0]?.content?.[0]?.content?.[0]?.attrs?.colspan, 2)
  } finally { e.destroy() }
})

test('copy/paste HTML keeps supported colors and callouts in the editor schema', () => {
  const e = editor(cases[0]!.document)
  try {
    const html = e.getHTML()
    // Paste uses the schema's HTML parser; setContent(string) is overridden by
    // tiptap-markdown and intentionally treats strings as Markdown.
    e.commands.setContent(generateJSON(html, extensions()), { emitUpdate: false })
    assert.match(JSON.stringify(e.getJSON()), /"color":"#fff3a3"/)
    assert.match(JSON.stringify(e.getJSON()), /"type":"callout"/)
  } finally { e.destroy() }
})

test('task item view ignores generated checkbox mutations while content and checking remain editable', async () => {
  const e = editor(cases[3]!.document)
  try {
    let transactions = 0
    e.on('transaction', () => { transactions++ })
    e.commands.setContent(cases[3]!.document.content)
    await new Promise(resolve => setTimeout(resolve, 30))
    assert.ok(transactions < 10, 'node view decorations must not cause an observer/update loop')
    const checkbox = e.view.dom.querySelector('input') as HTMLInputElement
    checkbox.checked = false
    checkbox.dispatchEvent(new window.Event('change', { bubbles: true }))
    assert.equal((e.getJSON() as StructuredDocument['content']).content?.[0]?.content?.[0]?.attrs?.checked, false)
    e.commands.setTextSelection(3)
    e.commands.insertContent('Edited ')
    assert.match(e.getText(), /Edited/)
  } finally { e.destroy() }
})

test('actual Tiptap link JSON and shared server fixture agree on every attribute', () => {
  const sample = cases.find(c => c.name === 'document-link-with-tiptap-default-attributes')!
  const e = editor(sample.document)
  try {
    const link = (e.getJSON().content![0]!.content![0]!.marks || []).find(mark => mark.type === 'link')!
    const expected = sample.document.content.content![0]!.content![0]!.marks![0]!
    assert.deepEqual(JSON.parse(JSON.stringify(link.attrs)), expected.attrs)
    const rendered = documentHTML(sample.document, value => value)
    assert.ok(rendered.includes('title="接口说明与示例"'))
    assert.ok(!rendered.includes('title="null"'))
  } finally { e.destroy() }
})
