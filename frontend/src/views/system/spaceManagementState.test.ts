import assert from 'node:assert/strict'
import test from 'node:test'

import type { TenantInfo } from '@/api/tenant'

import {
  isEnterableSpace,
  kbListFrom,
  spaceKbKey,
  toSpaceRows,
} from './spaceManagementState'

// These cover the boundary between the two space-overview endpoints and the
// table. Both payloads are typed loosely at the HTTP boundary, so the helpers
// have to hold up against shapes the compiler never sees — a null, an object
// where an array was promised, an entry missing its id.

const tenant = (over: Partial<TenantInfo>): TenantInfo =>
  ({ id: 1, name: 'Space', created_at: '2026-01-01T00:00:00Z', updated_at: '', ...over }) as TenantInfo

test('toSpaceRows maps workspace entries onto rows', () => {
  const rows = toSpaceRows([
    tenant({ id: 3, name: 'Gamma', description: 'third', created_at: '2026-03-03T00:00:00Z' }),
    tenant({ id: 1, name: 'Alpha' }),
  ])
  assert.deepEqual(rows, [
    { id: 1, name: 'Alpha', description: '', createdAt: '2026-01-01T00:00:00Z' },
    { id: 3, name: 'Gamma', description: 'third', createdAt: '2026-03-03T00:00:00Z' },
  ])
})

test('toSpaceRows sorts by id so the table is stable across refreshes', () => {
  const rows = toSpaceRows([tenant({ id: 9 }), tenant({ id: 2 }), tenant({ id: 5 })])
  assert.deepEqual(rows.map((r) => r.id), [2, 5, 9])
})

test('toSpaceRows drops entries the actions could not use', () => {
  // A row without a positive id is a dead row: both "enter this workspace"
  // and the knowledge-base fetch key off the id, so rendering it would only
  // produce a confusing 400. Also guards against the string ids the auth
  // API uses elsewhere leaking into this list.
  const rows = toSpaceRows([
    tenant({ id: 0 }),
    tenant({ id: -4 }),
    tenant({ id: Number.NaN }),
    tenant({ id: 7, name: 'Kept' }),
  ])
  assert.deepEqual(rows.map((r) => r.id), [7])
})

test('toSpaceRows tolerates a missing or malformed payload', () => {
  assert.deepEqual(toSpaceRows(undefined), [])
  assert.deepEqual(toSpaceRows(null), [])
  // Not an array — the error envelope shape, say.
  assert.deepEqual(toSpaceRows({ items: [] } as unknown as TenantInfo[]), [])
})

test('toSpaceRows falls back to the id when a workspace has no name', () => {
  const rows = toSpaceRows([tenant({ id: 12, name: '' })])
  assert.equal(rows[0].name, '#12')
})

test('isEnterableSpace rejects rows without a usable id', () => {
  assert.equal(isEnterableSpace({ id: 4, name: 'a', description: '', createdAt: '' }), true)
  assert.equal(isEnterableSpace({ id: 0, name: 'a', description: '', createdAt: '' }), false)
  assert.equal(isEnterableSpace(null), false)
  assert.equal(isEnterableSpace(undefined), false)
})

test('kbListFrom unwraps the array payload and rejects everything else', () => {
  const kb = { id: 'kb-1', name: 'KB', description: '', tenant_id: '9' }
  assert.deepEqual(kbListFrom([kb]), [kb])
  // The endpoint returns the array directly under `data`; a wrapped shape
  // must not silently render as "no results, but here is a table".
  assert.deepEqual(kbListFrom({ items: [kb] }), [])
  assert.deepEqual(kbListFrom(undefined), [])
  assert.deepEqual(kbListFrom(null), [])
})

test('spaceKbKey is stable per workspace', () => {
  assert.equal(spaceKbKey(42), '42')
  assert.equal(spaceKbKey(42), spaceKbKey(42))
  assert.notEqual(spaceKbKey(42), spaceKbKey(43))
})
