import assert from 'node:assert/strict'
import test, { type TestContext } from 'node:test'
import { readFileSync } from 'node:fs'
import * as vue from 'vue'
import * as structured from './structuredDocument.ts'
import * as anchors from './commentAnchors.ts'
import * as documentLinks from '../../documents/documentLinks.ts'
import { fixture, flush } from '../../../test-utils/vueFixture.ts'
import * as drafts from '../../documents/documentDrafts.ts'

function memoryStorage(): Storage {
  const values = new Map<string, string>()
  return { get length() { return values.size }, key: i => [...values.keys()][i] ?? null,
    getItem: key => values.get(key) ?? null, setItem: (key, value) => { values.set(key, value) },
    removeItem: key => { values.delete(key) }, clear: () => values.clear() }
}
const scope = { userId: 'me', tenantId: '1', kbId: 'kb', documentId: 'doc' }
async function setup(t: TestContext, storage = memoryStorage()) {
  const f = fixture(t, { storage })
  const draftHook = f.evaluate(readFileSync(new URL('../../documents/useDocumentDraft.ts', import.meta.url), 'utf8'), {
    vue, './documentDrafts': drafts, '../knowledge/browse/structuredDocument': structured,
  })
  const autosaveHook = f.evaluate(readFileSync(new URL('../../documents/useDocumentAutosave.ts', import.meta.url), 'utf8'), { vue })
  const source = { id: 'doc', title: 'Title', type: 'manual', parse_status: 'draft',
    metadata: { content: '# Original', version: 2 } }
  const getKnowledgeDetails = t.mock.fn(async (_id: string): Promise<any> => ({ success: true, document_schema_version: 1, data: structuredClone(source) }))
  const updateKnowledgeContent = t.mock.fn(async (_id: string, payload: any): Promise<any> => ({ success: true, data: {
    ...source, title: payload.title, metadata: { content: payload.content, version: payload.base_version + 1, ...(payload.document ? { document: payload.document } : {}) },
  } }))
  const confirmDiscardChanges = t.mock.fn(async () => false)
  const error = t.mock.fn()
  const openLinkedDocument = t.mock.fn()
  const page = f.component(new URL('./DocReader.vue', import.meta.url), {
    'vue-i18n': { useI18n: () => ({ t: (key: string) => key }) },
    'tdesign-vue-next': { MessagePlugin: { error, success() {} } },
    '@/stores/auth': { useAuthStore: () => ({ user: { id: 'me' }, effectiveTenantId: 1 }) },
    '@/views/documents/useDocumentDraft': draftHook,
    '@/views/documents/useDocumentAutosave': autosaveHook,
    '@/views/documents/unsavedChanges': { confirmDiscardChanges },
    '@/api/knowledge-base': { getKnowledgeDetails, updateKnowledgeContent },
    '@/utils/mergedContent': { mergeChunks: () => '' },
    '@/utils/markdownPipeline': { processMarkdown: (content: string) => content },
    '@/utils/security': { sanitizeHTML: (html: string) => html }, './structuredDocument': structured, '@/utils/mermaidViewer': {}, '@/utils/mermaidShared': {},
    '@/utils/filePreview': {}, '@/utils/index': { formatStringDate: () => '' },
    '../knowledgeDownloadFileName': {}, './docIcon': {}, './richDocument.less': {}, './commentAnchors': anchors, '@/views/documents/documentLinks': documentLinks,
  }, { doc: { ...source, file_type: 'md', file_name: 'Title.md', folder_path: '', updated_at: '' },
    kbId: 'kb', workspace: true, canEdit: true, onOpenLinkedDocument: openLinkedDocument })
  await flush()
  return { ...f, ...page, source, getKnowledgeDetails, updateKnowledgeContent, confirmDiscardChanges, error, openLinkedDocument }
}

test('automatic persistence stays in edit mode and saves against the current version', async t => {
  const f = await setup(t)
  f.vm.startEdit()
  f.vm.editContent = '# Updated'
  await flush()
  t.mock.timers.tick(1800)
  await flush()
  assert.equal(f.updateKnowledgeContent.mock.callCount(), 1)
  assert.deepEqual(structuredClone(f.updateKnowledgeContent.mock.calls[0].arguments), ['doc', {
    title: 'Title', content: '# Updated', base_version: 2,
  }])
  assert.equal(f.vm.editing, true)
  assert.equal(f.vm.editBaseVersion, 3)
  assert.equal(f.vm.hasUnsavedChanges, false)
  assert.equal(f.storage.length, 0)
  await f.vm.saveEdit()
  assert.equal(f.updateKnowledgeContent.mock.callCount(), 1)
  assert.equal(f.vm.editing, false)
})

test('typing during a save survives the response and gets backed up with the new baseline version', async t => {
  const f = await setup(t)
  let settle!: (value: any) => void
  f.updateKnowledgeContent.mock.mockImplementation(() => new Promise(resolve => { settle = resolve }))
  f.vm.startEdit()
  f.vm.editContent = '# Sent'
  await flush()
  t.mock.timers.tick(1800)
  await flush()
  f.vm.editContent = '# Typed while saving'
  settle({ success: true, data: { ...f.source, metadata: { content: '# Sent', version: 3 } } })
  await flush()
  assert.equal(f.vm.editContent, '# Typed while saving')
  assert.equal(f.vm.editBaseVersion, 3)
  assert.equal(f.vm.hasUnsavedChanges, true)
  const local = drafts.readDocumentDrafts(f.storage, scope)[0]!
  assert.equal(local.content, '# Typed while saving')
  assert.equal(local.baseVersion, 3)
})

test('409 pauses automatic writes, preserves the draft and closing the dialog cannot overwrite', async t => {
  const f = await setup(t)
  f.updateKnowledgeContent.mock.mockImplementation(async () => { throw { status: 409 } })
  f.vm.startEdit()
  f.vm.editContent = '# My version'
  await flush()
  t.mock.timers.tick(1800)
  await flush()
  assert.equal(f.vm.saveConflict, true)
  assert.equal(f.vm.conflictOpen, false)
  assert.equal(drafts.readDocumentDrafts(f.storage, scope)[0]!.content, '# My version')
  f.vm.editContent += '\nStill editing'
  await flush()
  t.mock.timers.tick(20000)
  assert.equal(f.updateKnowledgeContent.mock.callCount(), 1)
  await f.vm.saveEdit()
  assert.equal(f.vm.conflictOpen, true)
  f.vm.conflictOpen = false
  assert.equal(f.updateKnowledgeContent.mock.callCount(), 1)
  assert.equal(f.vm.editContent, '# My version\nStill editing')
})

test('an old recovered draft keeps its original base and requires conflict resolution before saving', async t => {
  const storage = memoryStorage()
  const draft = drafts.writeDocumentDraft(storage, scope, 'previous', { title: 'Old draft', content: '# Recovered', baseVersion: 1 })
  const f = await setup(t, storage)
  assert.equal(f.vm.localDraft.candidates.value.length, 1)
  f.vm.restoreDraft(draft)
  assert.equal(f.vm.editContent, '# Recovered')
  assert.equal(f.vm.editBaseVersion, 1)
  assert.equal(f.vm.saveConflict, true)
  await flush()
  t.mock.timers.tick(20000)
  assert.equal(f.updateKnowledgeContent.mock.callCount(), 0)
  assert.equal(await f.vm.requestLeave(), false)
  assert.equal(storage.length, 2)
})

test('a network failure keeps recovery content and a manual save still reports success explicitly', async t => {
  const f = await setup(t)
  f.updateKnowledgeContent.mock.mockImplementationOnce(async () => { throw new Error('offline') })
  f.vm.startEdit()
  f.vm.editContent = '# Offline edits'
  await flush()
  t.mock.timers.tick(1800)
  await flush()
  assert.equal(f.vm.autosave.state.value, 'error')
  assert.equal(f.vm.editContent, '# Offline edits')
  assert.equal(drafts.readDocumentDrafts(f.storage, scope)[0]!.content, '# Offline edits')
  await f.vm.saveEdit()
  assert.equal(f.vm.editing, false)
  assert.equal(f.storage.length, 0)
})

function highlighted(color: string): structured.StructuredDocument {
  return { schema_version: 1, content: { type: 'doc', content: [{ type: 'paragraph', content: [
    { type: 'text', text: 'Same text', marks: [{ type: 'highlight', attrs: { color } }] },
  ] }] } }
}

test('format-only edits autosave the structure and retain later colors during an in-flight save', async t => {
  const f = await setup(t)
  const original = highlighted('#fff3a3'), sent = highlighted('#c8f7c5'), latest = highlighted('#b5d9ff')
  f.vm.structuredDocument = original
  f.vm.markdown = 'Same text'
  f.vm.startEdit()
  let settle!: (value: any) => void
  f.updateKnowledgeContent.mock.mockImplementation(() => new Promise(resolve => { settle = resolve }))
  f.vm.onEditorContent({ content: 'Same text', document: sent })
  await flush()
  assert.equal(f.vm.hasUnsavedChanges, true)
  t.mock.timers.tick(1800)
  await flush()
  assert.deepEqual(JSON.parse(JSON.stringify(f.updateKnowledgeContent.mock.calls[0].arguments[1].document)), sent)
  f.vm.onEditorContent({ content: 'Same text', document: latest })
  settle({ success: true, data: { ...f.source, metadata: { content: 'Same text', version: 3, document: sent } } })
  await flush()
  assert.equal(structured.sameDocument(f.vm.editDocument, latest), true)
  assert.equal(structured.sameDocument(f.vm.baselineDocument, sent), true)
  assert.equal(f.vm.hasUnsavedChanges, true)
  const local = drafts.readDocumentDrafts(f.storage, scope)[0]!
  assert.equal(structured.sameDocument(local.document, latest), true)
  assert.equal(local.baseVersion, 3)
})

test('older backend capability prevents rich writes and missing structure receipts retain the draft', async t => {
  const f = await setup(t)
  f.vm.startEdit()
  f.vm.onEditorContent({ content: 'Same text', document: highlighted('#fff3a3') })
  f.vm.documentSchemaVersion = 0
  assert.equal(await f.vm.saveEditInner(true), 'error')
  assert.equal(f.updateKnowledgeContent.mock.callCount(), 0)
  assert.equal(drafts.readDocumentDrafts(f.storage, scope).length, 1)
  f.vm.documentSchemaVersion = 1
  f.updateKnowledgeContent.mock.mockImplementation(async () => ({ success: true, data: {
    ...f.source, metadata: { content: 'Same text', version: 3 },
  } }))
  assert.equal(await f.vm.saveEditInner(true), 'error')
  assert.equal(f.vm.editing, true)
  assert.equal(structured.sameDocument(drafts.readDocumentDrafts(f.storage, scope)[0]!.document, highlighted('#fff3a3')), true)
})

test('a recovered rich draft keeps formatting through explicit save', async t => {
  const storage = memoryStorage(), document = highlighted('#c8f7c5')
  const draft = drafts.writeDocumentDraft(storage, scope, 'previous', { title: 'Title', content: 'Same text', document, baseVersion: 2 })
  const f = await setup(t, storage)
  f.vm.restoreDraft(draft)
  assert.equal(structured.sameDocument(f.vm.editDocument, document), true)
  await f.vm.saveEdit()
  assert.equal(structured.sameDocument(f.vm.structuredDocument, document), true)
  assert.equal(storage.length, 0)
})

test('unsent discussion text participates in the document navigation guard', async t => {
  const f = await setup(t)
  let discarded = false
  f.vm.commentsRef = { hasDraft: true, busy: false, discard() { discarded = true } }
  assert.equal(await f.vm.requestLeave(), false)
  assert.equal(f.confirmDiscardChanges.mock.callCount(), 1)
  assert.equal(discarded, false)
  f.confirmDiscardChanges.mock.mockImplementation(async () => true)
  assert.equal(await f.vm.requestLeave(), true)
  assert.equal(discarded, true)
})
test('an in-flight discussion write blocks leaving the document', async t => {
  const f = await setup(t)
  f.vm.commentsRef = { hasDraft: true, busy: true }
  assert.equal(await f.vm.requestLeave(), false)
  assert.equal(f.confirmDiscardChanges.mock.callCount(), 0)
})

test('stable document links use guarded workspace navigation and preserve modified clicks', async t => {
  const f = await setup(t)
  const id = '12345678-1234-4234-8234-123456789abc'
  let prevented = 0
  const event = (href: string, ctrl = false) => ({ button: 0, ctrlKey: ctrl, target: { closest: () => ({ getAttribute: () => href }) }, preventDefault() { prevented++ } })
  f.vm.onDocumentLinkClick(event('/platform/documents/open/' + id))
  assert.equal(prevented, 1)
  assert.deepEqual(structuredClone(f.openLinkedDocument.mock.calls[0].arguments), [id])
  f.vm.onDocumentLinkClick(event('/platform/documents/open/' + id, true))
  f.vm.onDocumentLinkClick(event('https://external.invalid/platform/documents/open/' + id))
  assert.equal(prevented, 1)
})
