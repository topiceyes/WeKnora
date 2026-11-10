import assert from 'node:assert/strict'
import test from 'node:test'
import * as vue from 'vue'
import { fixture } from '../../../test-utils/vueFixture.ts'
import { StarterKit } from '@tiptap/starter-kit'
import { TaskList } from '@tiptap/extension-list'
import * as table from '@tiptap/extension-table'
import * as state from '@tiptap/pm/state'
import * as history from '@tiptap/pm/history'
import { Markdown } from 'tiptap-markdown'
import * as documentLinks from '../../documents/documentLinks.ts'

test('editor setup registers menu watchers after the editor ref exists', t => {
  const f = fixture(t)
  const errors: unknown[] = []
  const consoleError = t.mock.method(console, 'error', (...args: unknown[]) => { errors.push(args) })
  let options: any
  const editorRef = vue.shallowRef<any>(undefined)
  const page = f.component(new URL('./RichEditor.vue', import.meta.url), {
    'vue-i18n': { useI18n: () => ({ t: (key: string) => key }) },
    'tdesign-vue-next': { MessagePlugin: {} },
    '@tiptap/vue-3': { useEditor: (value: any) => { options = value; return editorRef }, EditorContent: {} },
    '@tiptap/starter-kit': { StarterKit }, '@tiptap/extension-list': { TaskList },
    '@tiptap/extension-table': table, '@tiptap/pm/state': state, '@tiptap/pm/history': history,
    'tiptap-markdown': { Markdown }, 'katex/dist/katex.min.css': {},
    '@/api/knowledge-base': {}, '@/views/documents/documentLinks': documentLinks, '@/utils/security': {}, './richDocument.less': {},
    './MermaidCodeBlock': { MermaidCodeBlock: { configure: () => ({}) } },
  }, { modelValue: '', kbId: 'kb' })
  assert.equal(page.vm.slash, null)
  assert.equal(page.vm.currentBlock, 'paragraph')
  assert.equal(page.vm.characterCount, 0)
  assert.deepEqual(errors, [])
  assert.equal(consoleError.mock.callCount(), 0)
  const calls: unknown[] = []
  const bookmark = { resolve: () => ({ empty: true, from: 3, to: 3 }), map: () => ({ resolve: () => ({ empty: true, from: 7, to: 7 }) }) }
  const chain: any = { focus: () => chain, insertContentAt: (...args: unknown[]) => { calls.push(args); return chain }, run: () => true }
  editorRef.value = { state: { selection: { getBookmark: () => bookmark }, doc: {} }, chain: () => chain, destroy() {} }
  page.vm.openDocumentPicker()
  assert.equal(page.vm.documentPickerOpen, true)
  options.onTransaction({ transaction: { docChanged: true, mapping: {} } })
  page.vm.insertDocumentLink({ id: '12345678-1234-4234-8234-123456789abc', title: 'Target document' })
  assert.deepEqual(structuredClone(calls), [[7, [
    { type: 'text', text: 'Target document', marks: [{ type: 'link', attrs: { href: '/platform/documents/open/12345678-1234-4234-8234-123456789abc' } }] },
    { type: 'text', text: ' ' },
  ]]])
})
