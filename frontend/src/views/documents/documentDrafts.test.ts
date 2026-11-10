import assert from 'node:assert/strict'
import test from 'node:test'
import { draftKey, readDocumentDrafts, removeDocumentDraft, writeDocumentDraft } from './documentDrafts.ts'

export function memoryStorage(): Storage {
  const values = new Map<string, string>()
  return { get length() { return values.size }, key: index => [...values.keys()][index] ?? null,
    getItem: key => values.get(key) ?? null, setItem: (key, value) => { values.set(key, value) },
    removeItem: key => { values.delete(key) }, clear: () => values.clear() }
}
const scope = { userId: 'user', tenantId: '1', kbId: 'kb', documentId: 'doc' }

test('recovery copies remain isolated by user, active workspace, book, document and editor', () => {
  const storage = memoryStorage()
  const body = { title: 'Title', content: '# Body', baseVersion: 2 }
  writeDocumentDraft(storage, scope, 'tab-1', body, 10)
  writeDocumentDraft(storage, scope, 'tab-2', { ...body, content: 'Other tab' }, 20)
  for (const [field, value] of Object.entries(scope)) {
    const other = { ...scope, [field]: value + 'other' }
    assert.deepEqual(readDocumentDrafts(storage, other), [])
  }
  const drafts = readDocumentDrafts(storage, scope)
  assert.equal(drafts.length, 2)
  assert.equal(drafts[0]!.content, 'Other tab')
  assert.equal(drafts[1]!.content, '# Body')
  assert.notEqual(draftKey({ ...scope, userId: 'a:b', tenantId: 'c' }, 'writer'),
    draftKey({ ...scope, userId: 'a', tenantId: 'b:c' }, 'writer'))
})

test('corrupt drafts are ignored, and deleting a recovered copy never removes a newer tab edit', () => {
  const storage = memoryStorage()
  const draft = writeDocumentDraft(storage, scope, 'tab', { title: 'A', content: '', baseVersion: 0 }, 10)
  storage.setItem(draftKey(scope, 'broken'), '{broken')
  storage.setItem(draftKey(scope, 'invalid'), JSON.stringify({ ...draft, writer: 'invalid', content: {}, baseVersion: -1 }))
  assert.equal(readDocumentDrafts(storage, scope).length, 1)
  writeDocumentDraft(storage, scope, 'tab', { title: 'A', content: 'newer', baseVersion: 0 }, 20)
  assert.equal(removeDocumentDraft(storage, draft), false)
  assert.equal(readDocumentDrafts(storage, scope)[0]!.content, 'newer')
  assert.equal(removeDocumentDraft(storage, readDocumentDrafts(storage, scope)[0]!), true)
})

test('quota errors reach the caller instead of pretending the draft was saved', () => {
  const storage = memoryStorage()
  storage.setItem = () => { throw new Error('quota') }
  assert.throws(() => writeDocumentDraft(storage, scope, 'tab', { title: '', content: 'Keep', baseVersion: 0 }), /quota/)
})

test('new-document folder destinations survive recovery and protect a newer folder selection from deletion', () => {
 const storage=memoryStorage(), first=writeDocumentDraft(storage,scope,'writer',{title:'Draft',content:'Body',baseVersion:0,folderPath:'Guides'},10)
 assert.equal(readDocumentDrafts(storage,scope)[0]!.folderPath,'Guides')
 writeDocumentDraft(storage,scope,'writer',{title:'Draft',content:'Body',baseVersion:0,folderPath:'Other'},10)
 assert.equal(removeDocumentDraft(storage,first),false)
})
