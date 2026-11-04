import assert from 'node:assert/strict'
import { test } from 'node:test'
import type { TenantMember } from '@/api/tenant/members'
import {
  filterTransferCandidates,
  formatTransferFileSize,
  transferCandidateLabel,
  transferHasBlockers,
} from './ownershipTransfer'

function member(overrides: Partial<TenantMember> & { user_id: string }): TenantMember {
  return {
    email: `${overrides.user_id}@example.com`,
    username: overrides.user_id,
    role: 'contributor',
    status: 'active',
    joined_at: '2026-01-01T00:00:00Z',
    ...overrides,
  }
}

test('excludes the current owner — self-transfer is rejected by the backend anyway', () => {
  const members = [member({ user_id: 'alice' }), member({ user_id: 'bob' })]
  const result = filterTransferCandidates(members, 'alice')
  assert.deepEqual(result.map((m) => m.user_id), ['bob'])
})

test('keeps only active members: invited and suspended cannot take over', () => {
  const members = [
    member({ user_id: 'active' }),
    member({ user_id: 'invited', status: 'invited' }),
    member({ user_id: 'suspended', status: 'suspended' }),
  ]
  const result = filterTransferCandidates(members, '')
  assert.deepEqual(result.map((m) => m.user_id), ['active'])
})

test('keeps viewers — the backend promotes them to contributor during the transfer', () => {
  const members = [member({ user_id: 'viewer', role: 'viewer' })]
  const result = filterTransferCandidates(members, '')
  assert.deepEqual(result.map((m) => m.user_id), ['viewer'])
})

test('treats a workspace-owned knowledge base (no owner) as having no exclusions', () => {
  const members = [member({ user_id: 'alice' }), member({ user_id: 'bob' })]
  assert.equal(filterTransferCandidates(members, '').length, 2)
  assert.equal(filterTransferCandidates(members, undefined).length, 2)
})

test('a missing plan or any blocker disables submission', () => {
  assert.equal(transferHasBlockers(null), true)
  assert.equal(transferHasBlockers(undefined), true)
  assert.equal(transferHasBlockers({ blockers: [] }), false)
  assert.equal(
    transferHasBlockers({ blockers: [{ code: 'im_channel_bound' }] }),
    true,
  )
})

test('file size is rendered with a compact ASCII unit', () => {
  assert.deepEqual(formatTransferFileSize(0), { value: '0', unit: 'B' })
  assert.deepEqual(formatTransferFileSize(undefined), { value: '0', unit: 'B' })
  assert.deepEqual(formatTransferFileSize(900), { value: '900', unit: 'B' })
  assert.deepEqual(formatTransferFileSize(1536), { value: '1.5', unit: 'KB' })
  assert.deepEqual(formatTransferFileSize(681 * 1024 * 1024), { value: '681', unit: 'MB' })
  assert.deepEqual(formatTransferFileSize(2 * 1024 * 1024 * 1024), { value: '2', unit: 'GB' })
})

test('label prefers username, falls back to email, then the raw user id', () => {
  assert.equal(transferCandidateLabel(member({ user_id: 'u1', username: 'Alice' })), 'Alice')
  assert.equal(
    transferCandidateLabel(member({ user_id: 'u2', username: '', email: 'bob@example.com' })),
    'bob@example.com',
  )
  assert.equal(
    transferCandidateLabel(member({ user_id: 'u3', username: '  ', email: '' })),
    'u3',
  )
})
