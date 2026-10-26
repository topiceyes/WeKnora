import { describe, it } from 'node:test'
import assert from 'node:assert/strict'

import {
  buildUnifiedDiff,
  canRestoreVersion,
  toggleCompareVersion,
  toVersionListItems,
  type CompareSelection,
} from './versionHistoryModel'
import type { KnowledgeContentVersionSummary } from '@/api/knowledge-base'

function v(version: number): KnowledgeContentVersionSummary {
  return {
    id: `snap-${version}`,
    knowledge_id: 'doc',
    version,
    title: `标题${version}`,
    created_by: 'caller',
    created_at: '2026-10-23T00:00:00Z',
  }
}

describe('toVersionListItems', () => {
  it('标记最大版本号为当前版本', () => {
    const items = toVersionListItems([v(1), v(3), v(2)])
    assert.deepEqual(items.map((i) => i.isCurrent), [false, true, false])
  })

  it('空列表安全返回', () => {
    assert.deepEqual(toVersionListItems([]), [])
  })
})

describe('canRestoreVersion', () => {
  it('当前版本或无编辑权限不可恢复', () => {
    const [current, past] = [
      { version: v(2), isCurrent: true },
      { version: v(1), isCurrent: false },
    ]
    assert.equal(canRestoreVersion(current, true), false)
    assert.equal(canRestoreVersion(past, true), true)
    assert.equal(canRestoreVersion(past, false), false)
  })
})

describe('toggleCompareVersion', () => {
  it('从空开始依次选中两个，自动保证 base < target', () => {
    let sel: CompareSelection = { base: null, target: null }
    sel = toggleCompareVersion(sel, 3)
    assert.deepEqual(sel, { base: 3, target: null })
    // 后选更小的版本号 → 交换，base 仍是较旧一侧。
    sel = toggleCompareVersion(sel, 1)
    assert.deepEqual(sel, { base: 1, target: 3 })
  })

  it('取消勾选清空对应槽位，全空回到 null 语义', () => {
    let sel: CompareSelection = { base: 1, target: 3 }
    sel = toggleCompareVersion(sel, 3)
    assert.deepEqual(sel, { base: 1, target: null })
    sel = toggleCompareVersion(sel, 1)
    assert.deepEqual(sel, { base: null, target: null })
  })

  it('两个已选时再点第三个视为更换 target', () => {
    const sel = { base: 1, target: 3 }
    assert.deepEqual(toggleCompareVersion(sel, 5), { base: 1, target: 5 })
    // 新点的小于 base → 新点成为 base，原 target 保持较新。
    assert.deepEqual(toggleCompareVersion(sel, 0), { base: 0, target: 3 })
  })
})

describe('buildUnifiedDiff', () => {
  it('增删改与上下文行分类正确', () => {
    // diffLines 把连续变更聚成整块（第二行+第三行一起删、三行一起增），
    // 与常见统一 diff 视图的呈现一致。
    const lines = buildUnifiedDiff('第一行\n第二行\n第三行\n', '第一行\n第二行改\n第三行\n新行\n')
    assert.deepEqual(
      lines.map((l) => `${l.kind}:${l.text}`),
      [
        'context:第一行',
        'removed:第二行',
        'removed:第三行',
        'added:第二行改',
        'added:第三行',
        'added:新行',
      ],
    )
  })

  it('空旧文本全部为新增，空新文本全部为删除', () => {
    assert.deepEqual(
      buildUnifiedDiff('', 'a\nb\n').map((l) => l.kind),
      ['added', 'added'],
    )
    assert.deepEqual(
      buildUnifiedDiff('a\nb\n', '').map((l) => l.kind),
      ['removed', 'removed'],
    )
  })

  it('完全相同全部为上下文，且不产生空行伪影', () => {
    const lines = buildUnifiedDiff('a\nb\n', 'a\nb\n')
    assert.deepEqual(lines, [
      { kind: 'context', text: 'a' },
      { kind: 'context', text: 'b' },
    ])
  })

  it('尾部换行差异被归一化，不报成空行增删', () => {
    const lines = buildUnifiedDiff('a\nb', 'a\nb\n')
    assert.deepEqual(lines.map((l) => l.kind), ['context', 'context'])
  })

  it('多 hunk 顺序保持原文顺序', () => {
    const lines = buildUnifiedDiff('a\nb\nc\nd\ne\n', 'a\nB\nc\nd\nE\n')
    const seq = lines.map((l) => l.kind)
    // a 上下文 → b 删/B 增 → c/d 上下文 → e 删/E 增
    assert.deepEqual(seq, ['context', 'removed', 'added', 'context', 'context', 'removed', 'added'])
  })
})
