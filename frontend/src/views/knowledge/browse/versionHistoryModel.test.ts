import { describe, it } from 'node:test'
import assert from 'node:assert/strict'

import { canRestoreVersion, toVersionListItems } from './versionHistoryModel'
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
