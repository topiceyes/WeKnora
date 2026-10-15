import type { KnowledgeBaseInfo } from '@/api/auth'
import type { TenantInfo } from '@/api/tenant'

// spaceManagementState.ts — pure helpers behind the platform space overview.
//
// Split out of SpaceManagement.vue so they can be unit-tested with the repo's
// node:test stack, the same way modelCatalogState.ts / createUserDialogState.ts
// are. The component keeps only the loading flags and the rendering.
//
// Everything here is defensive about its input on purpose: these values come
// straight off the HTTP boundary (listAllTenants / listSpaceKnowledgeBases),
// where the runtime shape is only as trustworthy as the server's response
// shape. A malformed entry must degrade to "missing row", never to a crash
// or to a row that looks clickable but can't be acted on.

export interface SpaceRow {
  id: number
  name: string
  description: string
  createdAt: string
}

/**
 * Map the `/tenants/all` payload onto table rows.
 *
 * Entries without a positive numeric id are dropped rather than rendered:
 * both the "enter this space" action and the per-space knowledge-base fetch
 * key off the id, so a row without one is a dead row that would only produce
 * a confusing 400.
 *
 * Sorted by id ascending so the table is stable across refreshes (the server
 * makes no ordering promise).
 */
export function toSpaceRows(raw: readonly TenantInfo[] | null | undefined): SpaceRow[] {
  if (!Array.isArray(raw)) return []
  const rows: SpaceRow[] = []
  for (const item of raw) {
    if (!item) continue
    const id = Number(item.id)
    if (!Number.isFinite(id) || id <= 0) continue
    rows.push({
      id,
      name: typeof item.name === 'string' && item.name ? item.name : `#${id}`,
      description: typeof item.description === 'string' ? item.description : '',
      createdAt: typeof item.created_at === 'string' ? item.created_at : '',
    })
  }
  return rows.sort((a, b) => a.id - b.id)
}

/** Whether the "enter this space" action can run for a row. */
export function isEnterableSpace(row: SpaceRow | null | undefined): boolean {
  return !!row && Number.isFinite(row.id) && row.id > 0
}

/**
 * Unwrap the `/system/admin/spaces/:id/knowledge-bases` payload.
 *
 * That endpoint returns the array directly under `data` (same builder as the
 * ordinary KB list), not wrapped in `data.items`. Anything else — an error
 * envelope, a null, a shape we don't recognise — yields an empty list so the
 * panel shows its empty state instead of throwing during render.
 */
export function kbListFrom(data: unknown): KnowledgeBaseInfo[] {
  return Array.isArray(data) ? (data as KnowledgeBaseInfo[]) : []
}

/** Cache key for the per-space knowledge-base list. */
export function spaceKbKey(spaceId: number): string {
  return String(spaceId)
}
