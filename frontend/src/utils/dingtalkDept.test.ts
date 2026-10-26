// 钉钉部门树构建：平铺快照 → 带深度的 DFS 序列。
// 供 PlatformUsers 与 DingTalkImportDialog 共用，回归孤儿重挂与虚拟根。
import assert from 'node:assert/strict'
import test from 'node:test'

import { buildDeptTree, DINGTALK_ROOT_DEPT_ID } from './dingtalkDept'

test('buildDeptTree renders virtual root and depth-first order', () => {
  const tree = buildDeptTree([
    { dept_id: 100, name: '技术部', parent_id: 1 },
    { dept_id: 110, name: '后端组', parent_id: 100 },
    { dept_id: 200, name: '市场部', parent_id: 1 },
  ], '全公司')
  assert.deepEqual(
    tree.map((n) => [n.dept_id, n.depth]),
    [[1, 0], [100, 1], [110, 2], [200, 1]],
  )
})

test('buildDeptTree reattaches orphan subtrees under the virtual root', () => {
  // 应用通讯录权限被范围限定时，parent 可能不在可见列表内。
  const tree = buildDeptTree([
    { dept_id: 300, name: '孤儿部', parent_id: 999 },
  ], '全公司')
  assert.deepEqual(
    tree.map((n) => [n.dept_id, n.parent_id, n.depth]),
    [[1, 0, 0], [300, 1, 1]],
  )
})

test('buildDeptTree keeps unknown names visible as #id placeholders', () => {
  const tree = buildDeptTree([{ dept_id: 100, name: '技术部', parent_id: 1 }], '全公司')
  assert.equal(tree[0].name, '全公司')
  assert.equal(tree[0].dept_id, DINGTALK_ROOT_DEPT_ID)
  assert.equal(tree[1].name, '技术部')
})

test('buildDeptTree handles empty snapshot (root only)', () => {
  const tree = buildDeptTree([], '全公司')
  assert.equal(tree.length, 1)
  assert.equal(tree[0].dept_id, 1)
})
