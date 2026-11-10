import assert from 'node:assert/strict'
import test, { type TestContext } from 'node:test'
import * as vue from 'vue'
import * as structured from '../knowledge/browse/structuredDocument.ts'
import { readFileSync } from 'node:fs'
import { fixture, flush } from '../../test-utils/vueFixture.ts'
import { canEditDocumentBook } from './workspaceModel.ts'
import * as draftStore from './documentDrafts.ts'

async function setup(t: TestContext, initialQuery: Record<string, string> = {}) {
  const f = fixture(t)
  const draftHook = f.evaluate(readFileSync(new URL('./useDocumentDraft.ts', import.meta.url), 'utf8'), {
    vue, './documentDrafts': draftStore, '../knowledge/browse/structuredDocument': structured,
  })
  const visits = f.evaluate(readFileSync(new URL('./useDocumentVisits.ts', import.meta.url), 'utf8'), { vue })
  const route = vue.reactive<any>({ params: { kbId: 'kb' }, query: initialQuery, fullPath: '/platform/documents/kb' })
  const push = t.mock.fn(async (_location: any) => {})
  const replace = t.mock.fn(async (_location: any) => {})
  const guards: { leave?: () => Promise<boolean>; update?: (...args: any[]) => any } = {}
  const auth = vue.reactive({ user: { id: 'me' }, effectiveTenantId: 1, currentTenantName: 'Team', hasRole: () => true })
  const getKnowledgeBaseById = t.mock.fn(async (id: string): Promise<any> => ({ success: true, data: { id, name: id, tenant_id: 1 } }))
  const getKnowledgeTree = t.mock.fn(async (id: string): Promise<any> => ({ success: true, data: {
    folders: [], total_document_count: 2, root_document_count: 2,
    documents: [{ id: `${id}-first`, title: 'First' }, { id: `${id}-second`, title: 'Second' }],
  } }))
  const createManualKnowledge = t.mock.fn(async (_id: string, _data: any): Promise<any> => ({ success: true, data: { id: 'new-doc' } }))
  const confirmDiscardChanges = t.mock.fn(async () => false)
  const error = t.mock.fn()
  const page = f.component(new URL('./DocumentWorkspace.vue', import.meta.url), {
    'vue-router': { useRoute: () => route, useRouter: () => ({ push, replace }),
      onBeforeRouteLeave: (fn: any) => { guards.leave = fn }, onBeforeRouteUpdate: (fn: any) => { guards.update = fn } },
    'vue-i18n': { useI18n: () => ({ t: (key: string) => key }) },
    'tdesign-vue-next': { MessagePlugin: { error, success() {} } },
    '@/stores/auth': { useAuthStore: () => auth },
    '@/stores/organization': { useOrganizationStore: () => ({ getKBPermission: () => null,
      fetchSharedKnowledgeBases: async () => [] }) },
    '@/api/knowledge-base': { getKnowledgeBaseById, getKnowledgeTree, createManualKnowledge,
      listKnowledgeBases: async () => ({ data: [{ id: 'kb', type: 'document' }, { id: 'faq', type: 'faq' }] }) },
    './workspaceModel': { canEditDocumentBook },
    './unsavedChanges': { confirmDiscardChanges },
    './useDocumentDraft': draftHook, './useDocumentVisits': visits,
    '../knowledge/browse/structuredDocument': structured,
  })
  await flush()
  return { ...page, ...f, route, push, replace, guards, auth, getKnowledgeBaseById, getKnowledgeTree,
    createManualKnowledge, confirmDiscardChanges, error }
}

test('opens a book directly and follows document deep links and browser history', async t => {
  const f = await setup(t, { doc: 'kb-second' })
  assert.equal(f.vm.selectedDoc.id, 'kb-second')
  f.route.query = { doc: 'kb-first' }
  await flush()
  assert.equal(f.vm.selectedDoc.id, 'kb-first')
  f.route.query = { doc: 'missing' }
  await flush()
  assert.equal(f.vm.selectedDoc, null)
  assert.equal(f.getKnowledgeBaseById.mock.callCount(), 1)
  assert.deepEqual(Array.from(f.vm.books, (book: any) => book.id), ['kb'])
})

test('late responses cannot replace a newly selected knowledge base', async t => {
  const f = await setup(t)
  let settle!: (value: any) => void
  f.getKnowledgeBaseById.mock.mockImplementation(id => id === 'slow'
    ? new Promise(resolve => { settle = resolve }) : Promise.resolve({ success: true, data: { id, tenant_id: 1 } }))
  f.route.params.kbId = 'slow'
  await flush()
  f.route.params.kbId = 'fast'
  await flush()
  settle({ success: true, data: { id: 'slow', tenant_id: 1 } })
  await flush()
  assert.equal(f.vm.book.id, 'fast')
  assert.equal(f.vm.selectedDoc.id, 'fast-first')
})

test('navigation protects a new document and blocks leaving during a save', async t => {
  const f = await setup(t, { new: '1' })
  f.vm.newTitle = 'Unsaved'
  f.vm.newContent = 'Body'
  assert.equal(await f.guards.leave!(), false)
  assert.equal(f.vm.newContent, 'Body')
  f.confirmDiscardChanges.mock.mockImplementation(async () => true)
  assert.equal(await f.guards.leave!(), true)
  f.vm.saving = true
  assert.equal(await f.guards.leave!(), false)
})

test('navigation consults the active editor before switching documents', async t => {
  const f = await setup(t)
  f.vm.reader = { requestLeave: async () => false }
  assert.equal(await f.guards.update!(
    { params: { kbId: 'kb' }, query: { doc: 'kb-second' } },
    { params: { kbId: 'kb' }, query: { doc: 'kb-first' } },
  ), false)
})

test('new documents save without indexing and land directly on the saved document', async t => {
  const f = await setup(t, { new: '1' })
  f.vm.newTitle = 'New document'
  f.vm.newContent = '# Hello\n\nTeam content'
  await f.vm.saveNew()
  assert.deepEqual(structuredClone(Array.from(f.createManualKnowledge.mock.calls[0].arguments)), ['kb', {
    title: 'New document', content: '# Hello\n\nTeam content', status: 'draft',
  }])
  assert.equal(f.replace.mock.calls[0].arguments[0].query.doc, 'new-doc')
  assert.equal(f.vm.newContent, '')
})

test('failed saves retain the complete draft, and read-only books never submit writes', async t => {
  const f = await setup(t, { new: '1' })
  f.vm.newTitle = 'Draft'
  f.vm.newContent = 'Keep this text'
  f.createManualKnowledge.mock.mockImplementation(async () => { throw new Error('offline') })
  await f.vm.saveNew()
  assert.equal(f.vm.newContent, 'Keep this text')
  assert.equal(f.replace.mock.callCount(), 0)
  f.vm.book = { id: 'kb', tenant_id: 2, my_permission: 'viewer' }
  await f.vm.saveNew()
  assert.equal(f.createManualKnowledge.mock.callCount(), 1)
})

test('image uploads block navigation and saving until their references can be persisted', async t => {
  const f = await setup(t, { new: '1' })
  f.vm.newTitle = 'With image'
  f.vm.newContent = 'Image is uploading'
  f.vm.editorBusy = true
  assert.equal(await f.guards.leave!(), false)
  await f.vm.saveNew()
  assert.equal(f.createManualKnowledge.mock.callCount(), 0)
  f.vm.editorBusy = false
  await f.vm.saveNew()
  assert.equal(f.createManualKnowledge.mock.callCount(), 1)
})

test('new rich documents require server capability and persist the live editor snapshot', async t => {
  const f = await setup(t, { new: '1' })
  const document: structured.StructuredDocument = { schema_version: 1, content: { type: 'doc', content: [
    { type: 'callout', attrs: { kind: 'tip' }, content: [{ type: 'paragraph', content: [{ type: 'text', text: 'Keep structure' }] }] },
  ] } }
  f.vm.newTitle = 'Rich draft'
  f.vm.newContent = 'Keep structure'
  f.vm.newEditorRef = { snapshot: () => ({ content: structured.documentMarkdown(document), document }) }
  await f.vm.saveNew()
  assert.equal(f.createManualKnowledge.mock.callCount(), 0)
  assert.equal(f.replace.mock.callCount(), 0)
  f.vm.documentSchemaVersion = 1
  f.createManualKnowledge.mock.mockImplementation(async (_id, payload) => ({ success: true, data: {
    id: 'rich-doc', metadata: { content: payload.content, document: payload.document },
  } }))
  await f.vm.saveNew()
  assert.deepEqual(JSON.parse(JSON.stringify(f.createManualKnowledge.mock.calls[0].arguments[1].document)), document)
  assert.equal(f.replace.mock.calls[0].arguments[0].query.doc, 'rich-doc')
})

test('new documents keep the chosen folder on create and route to folder browsing without opening unrelated documents', async t => {
  const f=await setup(t,{new:'1',folder:'Guides'})
  f.vm.newTitle='Folder document';f.vm.newContent='Body'
  assert.equal(f.vm.newFolder,'Guides')
  await f.vm.saveNew()
  assert.equal(f.createManualKnowledge.mock.calls[0].arguments[1].folder_path,'Guides')
  f.route.query={folder:'Guides'};await flush()
  assert.equal(f.vm.selectedDoc,null)
  assert.equal(f.vm.folderView,true)
})
