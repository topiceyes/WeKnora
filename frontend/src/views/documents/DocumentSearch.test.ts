import assert from 'node:assert/strict'
import test, { type TestContext } from 'node:test'
import { fixture, flush } from '../../test-utils/vueFixture.ts'
async function setup(t: TestContext, searchDocuments: (...args: any[]) => Promise<any>) {
  const f = fixture(t)
  const page = f.component(new URL('./DocumentSearch.vue', import.meta.url), {
    'vue-i18n': { useI18n: () => ({ t: (key: string) => key }) },
    '@/api/knowledge-base': { searchDocuments },
  }, { visible: true, kbId: 'kb' })
  await flush()
  return { ...f, ...page }
}
const response = (id: string, more = false) => ({ success: true, data: { items: [{ id, title: id }], next_offset: 1, has_more: more } })
test('search debounces input and ignores older results after a new query', async t => {
  let release!: (value: any) => void
  const calls: any[] = []
  const f = await setup(t, async (...args) => { calls.push(args); return args[1] === 'older' ? new Promise(resolve => { release = resolve }) : response(args[1] || 'recent') })
  assert.equal(f.vm.items[0].id, 'recent')
  f.vm.query = 'older'; await flush(); t.mock.timers.tick(250); await flush()
  f.vm.query = 'newer'; await flush(); t.mock.timers.tick(250); await flush()
  assert.equal(f.vm.items[0].id, 'newer')
  release(response('stale')); await flush()
  assert.equal(f.vm.items[0].id, 'newer')
  assert.equal(f.vm.appliedQuery, 'newer')
  assert.equal(calls.length, 3)
})
test('failed search has a retry and cannot select results for an unsent query', async t => {
  let fail = false
  const f = await setup(t, async () => { if (fail) throw new Error('offline'); return response('doc') })
  fail = true; f.vm.query = 'query'; await flush(); t.mock.timers.tick(250); await flush()
  assert.equal(f.vm.error, 'offline')
  assert.equal(f.vm.items.length, 0)
  fail = false; await f.vm.load(false)
  assert.equal(f.vm.error, '')
  assert.equal(f.vm.items[0].id, 'doc')
  f.vm.query = 'unsent'; await flush()
  f.vm.select({ id: 'doc' })
  assert.equal(f.vm.$.emitted?.select, undefined)
})
test('closed search ignores late responses and pagination deduplicates overlapping pages', async t => {
  let release!: (value: any) => void
  let deferred = false
  const f = await setup(t, async () => deferred ? new Promise(resolve => { release = resolve }) : response('doc', true))
  await f.vm.load(true)
  assert.equal(f.vm.items.length, 1)
  deferred = true
  const pending = f.vm.load(false)
  f.vm.$.props.visible = false; await flush()
  release(response('late')); await pending
  assert.equal(f.vm.items.length, 0)
})
