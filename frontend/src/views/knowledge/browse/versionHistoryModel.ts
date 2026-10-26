// 版本历史抽屉的纯逻辑（P3/P4）：与组件分离便于 node:test 直接覆盖。
import { diffLines } from 'diff'
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

// ---------- P4：统一 diff ----------

export interface DiffLine {
  kind: 'added' | 'removed' | 'context'
  text: string
}

// 对比模式下的勾选状态：base 是较旧一侧，target 是较新一侧。
export interface CompareSelection {
  base: number | null
  target: number | null
}

// 勾选/取消勾选一个版本，维护「恰好两个」的选择集；自动保证 base < target。
// 返回 null 表示重置为预览模式（选择集被清空）。
export function toggleCompareVersion(
  sel: CompareSelection,
  version: number,
): CompareSelection {
  // 已选中 → 取消勾选。
  if (sel.base === version) return { base: sel.target, target: null }
  if (sel.target === version) return { base: sel.base, target: null }
  if (sel.base == null) return { base: version, target: null }
  if (sel.target == null) {
    const [base, target] = sel.base < version ? [sel.base, version] : [version, sel.base]
    return { base, target }
  }
  // 两个都已选 → 再点第三个视为换 target（保留 base）。
  const [base, target] = sel.base < version ? [sel.base, version] : [version, sel.target]
  return { base, target }
}

// 基于 Markdown 源文本的统一行 diff。jsdiff 会把结尾换行造成的变化单独报成
// 一段空行差异，先归一化掉一个尾部换行再比；每个 part 的行数组丢掉末尾空段。
export function buildUnifiedDiff(oldText: string, newText: string): DiffLine[] {
  const normalize = (s: string) => (s.endsWith('\n') ? s.slice(0, -1) : s)
  const parts = diffLines(normalize(oldText), normalize(newText))
  const lines: DiffLine[] = []
  for (const part of parts) {
    const rows = part.value.split('\n')
    if (rows.length > 0 && rows[rows.length - 1] === '') rows.pop()
    for (const text of rows) {
      lines.push({ kind: part.added ? 'added' : part.removed ? 'removed' : 'context', text })
    }
  }
  return lines
}
