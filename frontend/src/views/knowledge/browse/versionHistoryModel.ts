// 版本历史抽屉的纯逻辑（P3）：与组件分离便于 node:test 直接覆盖。
import type { KnowledgeContentVersionSummary } from '@/api/knowledge-base'

export interface VersionListItem {
  version: KnowledgeContentVersionSummary
  // 当前版本恒为最大版本号（快照存的是保存后的新状态）。
  isCurrent: boolean
}

export function toVersionListItems(versions: KnowledgeContentVersionSummary[]): VersionListItem[] {
  const max = versions.reduce((m, v) => Math.max(m, v.version), 0)
  return versions.map((version) => ({ version, isCurrent: max > 0 && version.version === max }))
}

// 只有非当前版本且有编辑权限才可恢复。
export function canRestoreVersion(item: VersionListItem, canEdit: boolean): boolean {
  return canEdit && !item.isCurrent
}
