import assert from 'node:assert/strict'
import test, { type TestContext } from 'node:test'
import { fixture, flush } from '../../../test-utils/vueFixture.ts'
import * as uuid from '../../../utils/randomUUID.ts'

async function setup(t: TestContext, overrides: Record<string, unknown> = {}) {
  const f = fixture(t)
  const list = t.mock.fn(async (..._args: any[]) => ({ success: true, data: { items: [], next_offset: 0, has_more: false } }))
  const create = t.mock.fn(async (..._args: any[]) => ({ success: true, data: { id: 'posted' } }))
  const mutate = t.mock.fn(async (..._args: any[]) => ({ success: true }))
  const page = f.component(new URL('./DocumentComments.vue', import.meta.url), {
    'vue-i18n': { useI18n: () => ({ t: (key: string) => key }) },
    'tdesign-vue-next': { DialogPlugin: {} }, '@/utils/index': {}, '@/utils/randomUUID': uuid,
    '@/api/knowledge-base': { listDocumentComments: list, createDocumentComment: create, mutateDocumentComment: mutate, ...overrides },
  }, { visible: true, knowledgeId: 'doc', userId: 'me', canEdit: true, contentVersion: 2, anchor: null })
  await flush()
  return { ...f, ...page, list, create, mutate }
}
test('posting captures a stable request ID and preserves body and quote on a failed request', async t => {
  let fail = true
  const calls: any[][] = []
  const createDocumentComment = async (...args: any[]) => { calls.push(args); if (fail) throw new Error('offline'); return { success: true } }
  const f = await setup(t, { createDocumentComment })
  const quote = { quote: 'Selected', prefix: '', suffix: '', content_version: 2 }
  f.vm.body = '  My comment  '
  f.vm.draftAnchor = quote
  await f.vm.submit()
  assert.equal(f.vm.body, '  My comment  ')
  assert.equal(f.vm.error, 'offline')
  assert.equal(f.vm.hasDraft, true)
  fail = false
  await f.vm.submit()
  assert.equal(calls.length, 2)
  assert.equal(calls[0][1].client_id, calls[1][1].client_id)
  assert.equal(calls[1][1].body, 'My comment')
  assert.equal(calls[1][1].quote, 'Selected')
  assert.equal(f.vm.body, '')
  assert.equal(f.vm.draftAnchor, null)
})
test('changed text gets a new retry ID; replies never carry root quote anchors', async t => {
  const calls: any[][] = []
  const f = await setup(t, { createDocumentComment: async (...args: any[]) => { calls.push(args); throw new Error('offline') } })
  f.vm.body = 'First'
  await f.vm.submit()
  f.vm.body = 'Edited'
  await f.vm.chooseReply({ id: 'root', author_name: 'Author' })
  await f.vm.submit()
  assert.notEqual(calls[0][1].client_id, calls[1][1].client_id)
  assert.equal(calls[1][1].parent_id, 'root')
  assert.equal(calls[1][1].quote, undefined)
})
test('stale mutation failure does not alter thread state and does not clear an unsent draft', async t => {
  const f = await setup(t, { mutateDocumentComment: async () => { throw new Error('conflict') } })
  f.vm.body = 'Unsent'
  const root = { id: 'root', revision: 2, resolved: false }
  await f.vm.mutate(root, 'resolve')
  assert.equal(root.resolved, false)
  assert.equal(f.vm.body, 'Unsent')
  assert.equal(f.vm.error, 'conflict')
})
test('an unmounted panel ignores late list responses', async t => {
  let release!: (value: any) => void
  const f = await setup(t, { listDocumentComments: () => new Promise(resolve => { release = resolve }) })
  f.close()
  release({ success: true, data: { items: [{ id: 'late' }], next_offset: 1, has_more: false } })
  await flush()
  assert.equal(f.vm.items.length, 0)
})
test('read-only users cannot submit or mutate, even through component methods', async t => {
  const f = await setup(t)
  f.vm.$.props.canEdit = false
  f.vm.body = 'Attempt'
  await f.vm.submit()
  await f.vm.mutate({ id: 'root', revision: 1 }, 'resolve')
  assert.equal(f.create.mock.callCount(), 0)
  assert.equal(f.mutate.mock.callCount(), 0)
})

test('a newer refresh supersedes a pending list and stale data cannot overwrite the current status', async t => {
  let release!: (value: any) => void
  let calls = 0
  const f = await setup(t, { listDocumentComments: () => ++calls === 1 ? new Promise(resolve => { release = resolve }) : Promise.resolve({ success: true, data: { items: [{ id: 'current' }], next_offset: 1, has_more: false } }) })
  await f.vm.load(false)
  assert.equal(f.vm.items[0].id, 'current')
  release({ success: true, data: { items: [{ id: 'stale' }], next_offset: 1, has_more: false } })
  await flush()
  assert.equal(f.vm.items[0].id, 'current')
})
test('comment actions do not race an ongoing list refresh', async t => {
  const f = await setup(t)
  f.vm.loading = true
  await f.vm.mutate({ id: 'root', revision: 1 }, 'resolve')
  assert.equal(f.mutate.mock.callCount(), 0)
})

test('HTTP origins without crypto.randomUUID can send comments and keep the same UUID for retries', async t => {
  const getRandomValues = globalThis.crypto.getRandomValues.bind(globalThis.crypto)
  t.mock.getter(globalThis, 'crypto', () => ({ getRandomValues }) as Crypto)
  assert.equal(typeof crypto.randomUUID, 'undefined')
  const calls: any[][] = []
  const f = await setup(t, { createDocumentComment: async (...args: any[]) => { calls.push(args); throw new Error('offline') } })
  f.vm.body = 'HTTP comment'
  await f.vm.submit()
  await f.vm.submit()
  assert.equal(calls.length, 2)
  assert.match(calls[0][1].client_id, /^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/)
  assert.equal(calls[0][1].client_id, calls[1][1].client_id)
  assert.equal(f.vm.error, 'offline')
})
