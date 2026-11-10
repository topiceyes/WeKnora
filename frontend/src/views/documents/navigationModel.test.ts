import assert from 'node:assert/strict'
import test from 'node:test'
import { directItems, folderOptions, folderNameValid, reorderedKeys, orderItems } from './navigationModel.ts'
import type { KnowledgeTree } from '@/api/knowledge-base'
const tree: KnowledgeTree = { root_document_count: 2, total_document_count: 3, navigation_revision: 2,
  folders: [{ path: 'A', name: 'A', document_count: 0, total_count: 1, children: [{ path: 'A/sub', name: 'sub', document_count: 1, total_count: 1 }] }, { path: 'B', name: 'B', document_count: 0, total_count: 0 }],
  documents: [{ id: 'a', title: 'Alpha', folder_path: '', updated_at: '2026-11-01' }, { id: 'b', title: 'Beta', folder_path: '', updated_at: '2026-11-09' }, { id: 'c', title: 'Child', folder_path: 'A/sub' }] as any,
  sibling_orders: { '': ['d:b', 'f:B', 'd:a', 'f:A'], 'A': ['f:A/sub'] } }
test('mixed sibling order is shared, deterministic and switchable without changing saved order', () => {
  const items = directItems(tree, '')
  assert.deepEqual(items.map(v => v.kind === 'doc' ? v.id : v.path), ['b', 'B', 'a', 'A'])
  assert.deepEqual(orderItems(items, tree, '', 'name').map(v => v.kind === 'doc' ? v.id : v.path), ['A', 'B', 'a', 'b'])
  assert.deepEqual(orderItems(items, tree, '', 'updated').map(v => v.kind === 'doc' ? v.id : v.path), ['A', 'B', 'b', 'a'])
  assert.deepEqual(reorderedKeys(tree, items[1]!, -1), ['f:B', 'd:b', 'd:a', 'f:A'])
  assert.equal(reorderedKeys(tree, items[0]!, -1), null)
  assert.deepEqual(tree.sibling_orders![''], ['d:b', 'f:B', 'd:a', 'f:A'])
})
test('moving folder destinations exclude the source and descendants, path lengths count bytes', () => {
  assert.deepEqual(folderOptions(tree, 'A').map(o => o.value), ['B'])
  assert.equal(folderNameValid('设计', 'A'), true)
  for (const name of ['', '..', 'a/b', 'a\\b', '<bad>', '中'.repeat(43)]) assert.equal(folderNameValid(name, ''), false)
  assert.equal(folderNameValid('leaf', Array(16).fill('parent').join('/')), false)
})
