import assert from 'node:assert/strict'
import test from 'node:test'
import { createDocumentReadCache } from './documentReadCache.ts'

test('hover and click share the in-flight request; revisits expire and retry failures', async () => {
  let calls = 0, now = 0
  let resolve!: (value: string) => void
  const cache = createDocumentReadCache(async () => { calls++; return new Promise<string>(done => { resolve = done }) }, { now: () => now, ttl: 15 })
  const hover = cache.load('a', true)!
  const click = cache.load('a')!
  assert.equal(hover, click)
  await Promise.resolve(); assert.equal(calls, 1)
  resolve('body'); await click
  assert.equal(cache.peek('a'), 'body')
  assert.equal(await cache.load('a'), 'body'); assert.equal(calls, 1)
  now = 15; assert.equal(cache.peek('a'), undefined)
  const next = cache.load('a')!; await Promise.resolve(); resolve('new'); await next
  assert.equal(calls, 2)
  let attempts = 0
  const failing = createDocumentReadCache(async () => { if (++attempts === 1) throw new Error('403'); return 'allowed' })
  await assert.rejects(failing.load('a')!, /403/)
  assert.equal(await failing.load('a'), 'allowed')
})

test('scope invalidation prevents old requests from repopulating the cache or deleting newer pending reads', async () => {
  const resolvers: ((value: string) => void)[] = []
  const cache = createDocumentReadCache(() => new Promise<string>(resolve => { resolvers.push(resolve) }))
  const old = cache.load('same')!; await Promise.resolve()
  cache.clear()
  const current = cache.load('same')!; await Promise.resolve()
  resolvers[0]!('old'); await old
  assert.equal(cache.peek('same'), undefined)
  assert.equal(cache.load('same'), current)
  resolvers[1]!('new'); await current
  assert.equal(cache.peek('same'), 'new')
})

test('bounded LRU and payload budget; speculative reads yield to foreground navigation', async () => {
  const cache = createDocumentReadCache(async key => key, { maxEntries: 2, maxChars: 10 })
  await cache.load('a'); await cache.load('b'); cache.peek('a'); await cache.load('c')
  assert.equal(cache.peek('b'), undefined); assert.equal(cache.peek('a'), 'a')
  await cache.load('too large to retain')
  assert.equal(cache.peek('too large to retain'), undefined)
  const pending = createDocumentReadCache(() => new Promise<string>(() => {}))
  pending.load('a', true); pending.load('b', true)
  assert.equal(pending.load('c', true), undefined)
  assert.ok(pending.load('selected'))
})
