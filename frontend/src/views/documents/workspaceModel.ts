import { permissionCanEditKB } from '../../utils/kbPermission'

export interface DocumentBook {
  id: string
  name?: string
  description?: string
  type?: string
  creator_id?: string
  tenant_id?: number | string
  my_permission?: string
  permission?: string
  isMine?: boolean
  org_name?: string
  knowledge_count?: number
  updated_at?: string
}

// A local admin role must never upgrade a read-only cross-workspace grant.
export function canEditDocumentBook(
  book: DocumentBook | null,
  user: { id?: string; tenantId?: number | string | null; contributor: boolean; admin: boolean },
  sharedPermission?: string | null,
): boolean {
  if (!book) return false
  if (book.creator_id && book.creator_id === user.id && user.contributor) return true
  const permission = book.my_permission || sharedPermission || book.permission
  if (permission) return permissionCanEditKB(permission)
  return !!book.tenant_id && String(book.tenant_id) === String(user.tenantId) && user.admin
}
