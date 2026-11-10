import assert from 'node:assert/strict'
import test, { type TestContext } from 'node:test'
import { readFileSync } from 'node:fs'
import * as vue from 'vue'
import * as structured from '../knowledge/browse/structuredDocument.ts'
import { fixture, flush } from '../../test-utils/vueFixture.ts'
import * as draftStore from './documentDrafts.ts'
import type { AutosaveOutcome } from './useDocumentAutosave.ts'

function memoryStorage(): Storage {
  const values = new Map<string, string>()
  return { get length() { return values.size }, key: index => [...values.keys()][index] ?? null,
    getItem: key => values.get(key) ?? null, setItem: (key, value) => { values.set(key, value) },
    removeItem: key => { values.delete(key) }, clear: () => values.clear() }
}
const originalScope = { userId: 'me', tenantId: '1', kbId: 'kb', documentId: 'doc' }
function draftFixture(t: TestContext, storage = memoryStorage()) {
  const f = fixture(t, { storage })
  const { useDocumentDraft } = f.evaluate(readFileSync(new URL('./useDocumentDraft.ts', import.meta.url), 'utf8'), {
    vue, './documentDrafts': draftStore, '../knowledge/browse/structuredDocument': structured,
  })
  const state = vue.reactive({ scope: { ...originalScope }, title: 'A', content: '', version: 2, active: false, dirty: false })
  let draft: any
  const mounted = f.mount({ setup() { draft = useDocumentDraft({ scope: () => state.scope,
    value: () => ({ title: state.title, content: state.content, baseVersion: state.version }),
    active: () => state.active, dirty: () => state.dirty }); return {} } })
  return { ...f, ...mounted, state, draft, storage }
}

test('local copies debounce, flush on pagehide, and can be recovered after reopening', async t => {
  const f = draftFixture(t)
  f.state.active = true
  f.state.dirty = true
  f.state.content = 'First'
  t.mock.timers.tick(200)
  f.state.content = 'Latest'
  t.mock.timers.tick(100)
  assert.equal(f.storage.length, 0)
  f.window.dispatchEvent(new Event('pagehide'))
  assert.equal(draftStore.readDocumentDrafts(f.storage, originalScope)[0]!.content, 'Latest')
  f.close()
  const { useDocumentDraft } = f.evaluate(readFileSync(new URL('./useDocumentDraft.ts', import.meta.url), 'utf8'), {
    vue, './documentDrafts': draftStore, '../knowledge/browse/structuredDocument': structured,
  })
  let restored: any
  f.mount({ setup() { restored = useDocumentDraft({ scope: () => originalScope,
    value: () => ({ title: '', content: '', baseVersion: 0 }), active: () => false, dirty: () => false }); return {} } })
  assert.equal(restored.candidates.value[0].content, 'Latest')
})

test('draft switching captures its original scope and blocks cannot falsely report a saved copy', t => {
  const f = draftFixture(t)
  f.state.active = true
  f.state.dirty = true
  f.state.content = 'Old book body'
  f.state.scope = { ...originalScope, kbId: 'other' }
  assert.equal(draftStore.readDocumentDrafts(f.storage, originalScope)[0]!.content, 'Old book body')
  assert.equal(draftStore.readDocumentDrafts(f.storage, f.state.scope).length, 0)
  f.storage.setItem = () => { throw new Error('quota') }
  f.state.content = 'Still editable'
  f.draft.flush()
  assert.equal(f.draft.storageError.value, true)
  assert.equal(f.draft.savedAt.value, 0)
})

test('restored source survives failure and successful persistence removes only the exact recovered copy', t => {
  const storage = memoryStorage()
  const prior = draftStore.writeDocumentDraft(storage, originalScope, 'prior', { title: 'A', content: 'Recover', baseVersion: 1 })
  const f = draftFixture(t, storage)
  f.state.active = true
  f.state.dirty = true
  f.state.content = prior.content
  f.draft.adopt(prior)
  f.draft.flush()
  assert.equal(storage.length, 2)
  f.draft.clear()
  assert.equal(storage.length, 0)
})

async function autosaveFixture(t: TestContext) {
  const f = fixture(t)
  const { useDocumentAutosave } = f.evaluate(readFileSync(new URL('./useDocumentAutosave.ts', import.meta.url), 'utf8'), { vue })
  const state = vue.reactive({ enabled: true, dirty: false, busy: false, text: '' })
  const save = t.mock.fn(async (): Promise<AutosaveOutcome> => { state.dirty = false; return 'saved' })
  let autosave: any
  const mounted = f.mount({ setup() { autosave = useDocumentAutosave({ enabled: () => state.enabled,
    dirty: () => state.dirty, busy: () => state.busy, revision: () => state.text, save,
    delay: 1800, minimumInterval: 0 }); return {} } })
  return { ...f, ...mounted, state, save, autosave }
}

test('autosave waits for a pause in typing and for image uploads', async t => {
  const f = await autosaveFixture(t)
  f.state.dirty = true
  f.state.text = 'A'
  await flush()
  t.mock.timers.tick(1200)
  f.state.text = 'AB'
  await flush()
  t.mock.timers.tick(1200)
  assert.equal(f.save.mock.callCount(), 0)
  f.state.busy = true
  await flush()
  t.mock.timers.tick(10000)
  assert.equal(f.save.mock.callCount(), 0)
  f.state.busy = false
  await flush()
  t.mock.timers.tick(1800)
  await flush()
  assert.equal(f.save.mock.callCount(), 1)
  assert.equal(f.autosave.state.value, 'saved')
})

test('a failing autosave pauses until an explicit retry or an online event', async t => {
  const f = await autosaveFixture(t)
  f.save.mock.mockImplementation(async () => 'error')
  f.state.dirty = true
  f.state.text = 'Draft'
  await flush()
  t.mock.timers.tick(1800)
  await flush()
  assert.equal(f.autosave.state.value, 'error')
  f.state.text = 'Keep typing offline'
  await flush()
  t.mock.timers.tick(100000)
  assert.equal(f.save.mock.callCount(), 1)
  f.window.dispatchEvent(new Event('online'))
  t.mock.timers.tick(1800)
  await flush()
  assert.equal(f.save.mock.callCount(), 2)
})

test('version conflicts never resume on connectivity recovery, and disposal cancels queued saves', async t => {
  const f = await autosaveFixture(t)
  f.save.mock.mockImplementation(async () => 'conflict')
  f.state.dirty = true
  f.state.text = 'Conflict'
  await flush()
  t.mock.timers.tick(1800)
  await flush()
  f.window.dispatchEvent(new Event('online'))
  t.mock.timers.tick(100000)
  assert.equal(f.save.mock.callCount(), 1)
  f.autosave.resume()
  f.close()
  t.mock.timers.tick(100000)
  assert.equal(f.save.mock.callCount(), 1)
})

test('typing during a save queues the next snapshot without parallel writes', async t => {
  const f = await autosaveFixture(t)
  let settle!: (value: AutosaveOutcome) => void
  f.save.mock.mockImplementation(() => new Promise(resolve => { settle = resolve }))
  f.state.dirty = true
  f.state.text = 'First snapshot'
  await flush()
  t.mock.timers.tick(1800)
  await flush()
  f.state.text = 'New snapshot'
  await flush()
  t.mock.timers.tick(10000)
  assert.equal(f.save.mock.callCount(), 1)
  settle('saved')
  await flush()
  t.mock.timers.tick(1800)
  assert.equal(f.save.mock.callCount(), 2)
  settle('saved')
  await flush()
})
