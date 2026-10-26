/**
 * 钉钉部门树构建（本地快照平铺列表 → 带深度的展示序列）。
 * 供系统设置「钉钉通讯录」页与共享空间「从钉钉组织架构添加」对话框共用。
 */

export interface DingTalkDeptRow {
  dept_id: number
  name: string
  parent_id: number
}

export interface DeptNode {
  dept_id: number
  name: string
  parent_id: number
  depth: number
}

// DINGTALK_ROOT_DEPT_ID 钉钉默认的公司根节点 id（不落库，前端虚拟根）。
export const DINGTALK_ROOT_DEPT_ID = 1

/**
 * buildDeptTree 把平铺部门列表展开成带 depth 的深度优先序列。
 * 根部门（dept_id=1）作为树顶（名称用 rootLabel），其余按 parent_id 展开；
 * 孤儿部门（parent 不在可见列表内，如应用通讯录权限被范围限定）直接
 * 挂到根下，避免整棵子树不可见。
 */
export function buildDeptTree(departments: DingTalkDeptRow[], rootLabel: string): DeptNode[] {
  const byParent = new Map<number, number[]>()
  for (const d of departments) {
    const list = byParent.get(d.parent_id) ?? []
    list.push(d.dept_id)
    byParent.set(d.parent_id, list)
  }
  const names = new Map(departments.map((d) => [d.dept_id, d.name]))
  const out: DeptNode[] = [{ dept_id: DINGTALK_ROOT_DEPT_ID, name: rootLabel, parent_id: 0, depth: 0 }]
  const placed = new Set([DINGTALK_ROOT_DEPT_ID])
  const walk = (parent: number, depth: number) => {
    for (const id of byParent.get(parent) ?? []) {
      out.push({ dept_id: id, name: names.get(id) || `#${id}`, parent_id: parent, depth })
      placed.add(id)
      walk(id, depth + 1)
    }
  }
  walk(DINGTALK_ROOT_DEPT_ID, 1)
  for (const d of departments) {
    if (!placed.has(d.dept_id)) {
      out.push({ dept_id: d.dept_id, name: d.name, parent_id: DINGTALK_ROOT_DEPT_ID, depth: 1 })
    }
  }
  return out
}
