import type { KnowledgeFolderNode, KnowledgeTree, KnowledgeTreeDocument } from '@/api/knowledge-base'

export type NavigationItem = { kind: 'folder'; path: string; name: string } | { kind: 'doc'; id: string; title: string; folder_path: string }
export type NavigationSort = 'manual' | 'name' | 'updated'
export const parentPath = (path: string) => path.includes('/') ? path.slice(0, path.lastIndexOf('/')) : ''
export const itemKey = (item: NavigationItem) => item.kind === 'folder' ? `f:${item.path}` : `d:${item.id}`
export const itemParent = (item: NavigationItem) => item.kind === 'folder' ? parentPath(item.path) : item.folder_path || ''
export function allFolders(folders: KnowledgeFolderNode[]): KnowledgeFolderNode[] { return folders.flatMap(f => [f, ...allFolders(f.children || [])]) }
export function directItems(tree: KnowledgeTree | null, parent: string): NavigationItem[] {
  if (!tree) return []
  const items: NavigationItem[] = [
    ...allFolders(tree.folders || []).filter(f => parentPath(f.path) === parent).map(f => ({ kind: 'folder' as const, path: f.path, name: f.name })),
    ...tree.documents.filter(d => (d.folder_path || '') === parent).map(d => ({ kind: 'doc' as const, id: d.id, title: d.title || d.file_name, folder_path: d.folder_path || '' })),
  ]
  return orderItems(items, tree, parent, 'manual')
}
export function orderItems<T extends NavigationItem>(items: T[], tree: KnowledgeTree | null, parent: string, sort: NavigationSort): T[] {
  const rank = new Map((tree?.sibling_orders?.[parent] || []).map((key, index) => [key, index]))
  const docs = new Map((tree?.documents || []).map(d => [d.id, d]))
  return [...items].sort((a, b) => {
    if (sort === 'manual') {
      const delta = (rank.get(itemKey(a)) ?? Infinity) - (rank.get(itemKey(b)) ?? Infinity)
      if (!Number.isNaN(delta) && delta !== 0) return delta
    }
    if (a.kind !== b.kind) return a.kind === 'folder' ? -1 : 1
    if (sort === 'updated' && a.kind === 'doc' && b.kind === 'doc') {
      const delta = Date.parse(docs.get(b.id)?.updated_at || '') - Date.parse(docs.get(a.id)?.updated_at || '')
      if (Number.isFinite(delta) && delta) return delta
    }
    const label = (a.kind === 'folder' ? a.name : a.title).localeCompare(b.kind === 'folder' ? b.name : b.title, undefined, { numeric: true })
    return label || itemKey(a).localeCompare(itemKey(b))
  })
}
export function reorderedKeys(tree: KnowledgeTree, item: NavigationItem, direction: -1 | 1): string[] | null {
  const siblings = directItems(tree, itemParent(item)).map(itemKey), index = siblings.indexOf(itemKey(item)), target = index + direction
  if (index < 0 || target < 0 || target >= siblings.length) return null
  ;[siblings[index], siblings[target]] = [siblings[target]!, siblings[index]!]
  return siblings
}
export function folderOptions(tree: KnowledgeTree | null, moving?: string): { label: string; value: string }[] {
  return allFolders(tree?.folders || []).filter(f => !moving || f.path !== moving && !f.path.startsWith(moving + '/')).map(f => ({ label: f.path, value: f.path }))
}
export function folderNameValid(name: string, parent: string) {
  const value = name.trim(), bytes = new TextEncoder()
  const path = parent ? `${parent}/${value}` : value
  return !!value && !value.endsWith('.') && !/[\\/<>\x00-\x1f]/.test(value) && bytes.encode(value).length <= 128 && bytes.encode(path).length <= 1024 && path.split('/').length <= 16
}
export function activeFolder(tree: KnowledgeTree | null, doc: KnowledgeTreeDocument | null, selected: string): string {
  if (doc) return doc.folder_path || ''
  return allFolders(tree?.folders || []).some(f => f.path === selected) ? selected : ''
}
