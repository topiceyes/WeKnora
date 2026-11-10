import assert from 'node:assert/strict'
import test from 'node:test'
import { canEditDocumentBook } from './workspaceModel.ts'

const admin = { id: 'me', tenantId: 1, admin: true, contributor: true }

test('a local admin cannot upgrade a read-only cross-workspace book', () => {
  assert.equal(canEditDocumentBook({ id: 'kb', tenant_id: 2, my_permission: 'viewer' }, admin, 'editor'), false)
  assert.equal(canEditDocumentBook({ id: 'kb', tenant_id: 2 }, admin), false)
  assert.equal(canEditDocumentBook({ id: 'kb', tenant_id: 1 }, admin), true)
})

test('editing follows the explicit effective grant and honors the creator role floor', () => {
  assert.equal(canEditDocumentBook({ id: 'kb', tenant_id: 2, my_permission: 'editor' }, admin), true)
  assert.equal(canEditDocumentBook({ id: 'kb', creator_id: 'me', my_permission: 'viewer' }, admin), true)
  assert.equal(canEditDocumentBook({ id: 'kb', creator_id: 'me', my_permission: 'viewer' },
    { ...admin, admin: false, contributor: false }), false)
})
