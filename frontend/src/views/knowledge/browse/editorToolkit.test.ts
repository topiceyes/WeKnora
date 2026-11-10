import assert from 'node:assert/strict'
import { before, test } from 'node:test'
import { JSDOM } from 'jsdom'
import { Editor } from '@tiptap/core'
import { StarterKit } from '@tiptap/starter-kit'
import { Image } from '@tiptap/extension-image'
import { Table, TableRow, TableCell, TableHeader } from '@tiptap/extension-table'
import { Markdown } from 'tiptap-markdown'
import { createEditorToolkit, editorToolkitKey, findTextMatches, normalizeTableHeaders, slashQuery } from './editorToolkit.ts'
import { createEditorImage } from './editorImage.ts'

before(() => {
  const dom = new JSDOM('<html><body></body></html>', { url: 'http://localhost/' })
  Object.assign(globalThis, { window: dom.window, document: dom.window.document, Node: dom.window.Node,
    HTMLElement: dom.window.HTMLElement, DOMParser: dom.window.DOMParser })
})

function makeEditor(content: string | object = '', onFiles = (_files: File[], _pos: number) => {}) {
  return new Editor({ element: document.createElement('div'), extensions: [StarterKit, Image,
    Table, TableRow, TableHeader, TableCell,
    createEditorToolkit({ placeholder: () => 'Write here', uploading: () => 'Uploading', onFiles, onKeyDown: () => true }),
    Markdown.configure({ html: false, transformPastedText: true })], content })
}

test('literal search crosses formatting marks, respects block boundaries and Unicode offsets', () => {
  const editor = makeEditor('前文 **hello** world 😀\n\nhello world')
  try {
    const matches = findTextMatches(editor.state.doc, 'hello world')
    assert.equal(matches.length, 2)
    for (const match of matches) assert.equal(editor.state.doc.textBetween(match.from, match.to), 'hello world')
    assert.deepEqual(findTextMatches(editor.state.doc, '😀hello'), [])
    assert.deepEqual(findTextMatches(editor.state.doc, ''), [])
    const emoji = findTextMatches(editor.state.doc, '😀')[0]!
    assert.equal(emoji.to - emoji.from, 2)
  } finally { editor.destroy() }
})

test('search decorations track edits and replace-all is a single undoable transaction', () => {
  const editor = makeEditor('hello **world** hello')
  try {
    editor.view.dispatch(editor.state.tr.setMeta(editorToolkitKey, { query: 'hello' }))
    const matches = editorToolkitKey.getState(editor.state)!.matches
    const tr = editor.state.tr
    for (const match of [...matches].reverse()) tr.insertText('team', match.from, match.to)
    editor.view.dispatch(tr)
    assert.equal(editor.getText(), 'team world team')
    assert.equal(editorToolkitKey.getState(editor.state)!.matches.length, 0)
    assert.equal(editor.commands.undo(), true)
    assert.equal(editor.getText(), 'hello world hello')
    assert.equal(editorToolkitKey.getState(editor.state)!.matches.length, 2)
    assert.match((editor.storage as any).markdown.getMarkdown(), /\*\*world\*\*/)
  } finally { editor.destroy() }
})

test('slash commands only match the beginning of a paragraph, not URLs, code or selections', () => {
  for (const [content, expected] of [['/table', 'table'], ['正文 /table', null], ['https://example.com', null], ['```text\n/table\n```', null]] as const) {
    const editor = makeEditor(content)
    try {
      editor.commands.setTextSelection(editor.state.doc.content.size - 1)
      assert.equal(slashQuery(editor.state)?.query ?? null, expected)
      editor.commands.setTextSelection({ from: 1, to: Math.min(3, editor.state.doc.content.size - 1) })
      assert.equal(slashQuery(editor.state), null)
    } finally { editor.destroy() }
  }
})

test('upload anchors follow typing and completed images preserve the intended location', () => {
  const editor = makeEditor('before after')
  try {
    editor.view.dispatch(editor.state.tr.setMeta(editorToolkitKey, { addUpload: { id: 'img', pos: 8 } }).setMeta('addToHistory', false))
    editor.commands.insertContentAt(1, 'new ')
    assert.equal(editorToolkitKey.getState(editor.state)!.uploads.get('img'), 12)
    const pos = editorToolkitKey.getState(editor.state)!.uploads.get('img')!
    editor.view.dispatch(editor.state.tr.replaceWith(pos, pos, editor.schema.nodes.image!.create({ src: '/example.png', alt: 'Screenshot' }))
      .setMeta(editorToolkitKey, { removeUpload: 'img' }))
    const markdown = (editor.storage as any).markdown.getMarkdown()
    assert.match(markdown, /new before/)
    assert.match(markdown, /!\[Screenshot\]\(\/example.png\)/)
    assert.ok(markdown.indexOf('before') < markdown.indexOf('![Screenshot]'))
    assert.ok(markdown.indexOf('![Screenshot]') < markdown.indexOf('after'))
    assert.equal(editorToolkitKey.getState(editor.state)!.uploads.size, 0)
  } finally { editor.destroy() }
})

test('deleting an upload anchor cancels late image insertion', () => {
  const editor = makeEditor('before after')
  try {
    editor.view.dispatch(editor.state.tr.setMeta(editorToolkitKey, { addUpload: { id: 'img', pos: 8 } }))
    editor.view.dispatch(editor.state.tr.delete(1, 12))
    assert.equal(editorToolkitKey.getState(editor.state)!.uploads.has('img'), false)
  } finally { editor.destroy() }
})

test('multiple upload anchors at one position keep image order after paragraph splitting', () => {
  const editor = makeEditor('before after')
  try {
    for (const id of ['one', 'two']) editor.view.dispatch(editor.state.tr.setMeta(editorToolkitKey, { addUpload: { id, pos: 8 } }))
    for (const id of ['one', 'two']) {
      const pos = editorToolkitKey.getState(editor.state)!.uploads.get(id)
      assert.notEqual(pos, undefined)
      editor.view.dispatch(editor.state.tr.replaceWith(pos!, pos!, editor.schema.nodes.image!.create({ src: `/${id}.png`, alt: id }))
        .setMeta(editorToolkitKey, { removeUpload: id }))
    }
    const markdown = (editor.storage as any).markdown.getMarkdown()
    assert.ok(markdown.indexOf('/one.png') < markdown.indexOf('/two.png'))
    assert.ok(markdown.indexOf('/two.png') < markdown.indexOf('after'))
  } finally { editor.destroy() }
})

test('keyboard tools leave IME composition events to the input method', () => {
  const editor = makeEditor('/')
  try {
    const plugin = editorToolkitKey.get(editor.state)!
    const event = { key: 'Enter', isComposing: true } as KeyboardEvent
    assert.equal(plugin.props.handleKeyDown?.call(plugin, { editable: true, composing: true } as any, event), false)
  } finally { editor.destroy() }
})

test('image paste is handled once; ordinary text paste and read-only paste fall through', () => {
  const calls: { files: File[]; pos: number }[] = []
  const editor = makeEditor('', (files, pos) => calls.push({ files, pos }))
  try {
    const file = new window.File(['image'], 'example.png', { type: 'image/png' }) as File
    const event = { clipboardData: { files: [file], getData: () => '' }, preventDefault() {} } as unknown as ClipboardEvent
    let handled = false
    editor.view.someProp('handlePaste', handler => { handled = !!handler(editor.view, event, null as any); return handled })
    assert.equal(handled, true)
    assert.equal(calls.length, 1)
    assert.equal(calls[0]!.files[0]!.name, 'example.png')
    const ordinary = { clipboardData: { files: [], getData: () => 'plain text' }, preventDefault() {} } as unknown as ClipboardEvent
    const toolkit = editorToolkitKey.get(editor.state)!
    assert.equal(toolkit.props.handlePaste?.call(toolkit, editor.view, ordinary, null as any), false)
    editor.setEditable(false)
    editor.view.someProp('handlePaste', handler => handler(editor.view, event, null as any))
    assert.equal(calls.length, 1)
  } finally { editor.destroy() }
})

test('inserting above and deleting the header keeps a Markdown table rather than dropping its cells', () => {
  const editor = makeEditor('| Name | Count |\n| --- | --- |\n| Apple | 3 |')
  try {
    editor.commands.setTextSelection(4)
    editor.commands.addRowBefore()
    editor.view.dispatch(normalizeTableHeaders(editor.state))
    let markdown = (editor.storage as any).markdown.getMarkdown()
    assert.doesNotMatch(markdown, /\[table\]/)
    assert.match(markdown, /Name/)
    assert.match(markdown, /Apple/)
    editor.commands.setTextSelection(4)
    editor.commands.deleteRow()
    editor.view.dispatch(normalizeTableHeaders(editor.state))
    markdown = (editor.storage as any).markdown.getMarkdown()
    assert.match(markdown, /\| Name \| Count \|/)
    assert.match(markdown, /\| Apple \| 3 \|/)
    const reloaded = makeEditor(markdown)
    try { assert.equal(reloaded.getText().trim(), editor.getText().trim()) } finally { reloaded.destroy() }
  } finally { editor.destroy() }
})

test('authenticated image previews persist stable resource references rather than temporary blob URLs', () => {
  const editor = new Editor({ element: document.createElement('div'), content: '![Screenshot](resource://file-1)',
    extensions: [StarterKit, createEditorImage(root => root.querySelector('img')!.setAttribute('src', 'blob:preview-only')),
      Markdown.configure({ html: false })] })
  try {
    assert.equal(editor.view.dom.querySelector('img')?.getAttribute('src'), 'blob:preview-only')
    assert.equal((editor.storage as any).markdown.getMarkdown(), '![Screenshot](resource://file-1)')
    assert.equal(editor.getJSON().content?.[0]?.attrs?.src, 'resource://file-1')
  } finally { editor.destroy() }
})
