// Tenant-switch navigation helper.
//
// Switching the active tenant normally lands on the platform's KB list. A
// small number of explicit resource flows may provide a validated internal
// deep-link. Both paths use a full navigation so every store / SSE / request
// is reset under the new tenant.

import { updateMyPreferences } from '@/api/auth'
import { tenantSwitchTargetPath } from './tenantSwitchPath'

export { tenantSwitchTargetPath } from './tenantSwitchPath'

/**
 * Perform the post-switch hard navigation, optionally to a validated platform
 * deep-link. A full navigation resets tenant-scoped stores, SSE and requests.
 */
export function navigateAfterTenantSwitch(destination?: string): void {
  window.location.href = tenantSwitchTargetPath(window.location.pathname, destination)
}

// 切换成功后的 toast 跨 hard reload 传递：调用方在 reload 前把信息塞进
// sessionStorage，App.vue 启动时 consume 一次再弹出。直接在 reload 前调
// NotifyPlugin 会被刷掉，根本来不及看清。
const PENDING_TOAST_KEY = 'weknora_pending_tenant_switch_toast'

export interface PendingTenantSwitchToast {
  name: string
  /** Human-readable role label, e.g. "所有者" / "Owner". Drives the role chip text. */
  role?: string
  /** Raw role enum, e.g. "owner". Drives the role chip color + icon. */
  roleEnum?: string
}

export function stashTenantSwitchToast(payload: PendingTenantSwitchToast): void {
  try {
    sessionStorage.setItem(PENDING_TOAST_KEY, JSON.stringify(payload))
  } catch {
    // sessionStorage 写失败（隐私模式等）就静默放弃，toast 是锦上添花
  }
}

/**
 * Run the back half of an in-app workspace switch: stash the post-reload
 * toast, best-effort persist the "last active workspace" preference, then
 * hard-navigate to the workspace's landing page.
 *
 * Callers are expected to have already called
 * authStore.setSelectedTenant(...) — that writes the localStorage override
 * `utils/request.ts` reads to attach X-Tenant-ID — and to close any open
 * dropdown first.
 *
 * Extracted so the sidebar TenantSelector and the platform space overview
 * run the *same* sequence. The subtle parts worth not duplicating:
 *   - the toast must be stashed rather than shown (a hard reload would eat it),
 *   - switching to the home workspace must persist `null` (clears the
 *     preference) while a peer workspace persists its id,
 *   - the persist is raced against a 500ms budget so the navigation isn't
 *     held hostage by a slow preference write.
 */
export function switchToTenant(options: {
  tenantId: number
  /** Display name for the post-reload toast. */
  name: string
  /** True when the target is the user's home workspace. */
  isHomeTenant: boolean
  /** Human-readable role label, e.g. "所有者" / "Owner". Omitted when the
   *  caller has no membership row (cross-space visitors). */
  roleLabel?: string
  /** Raw role enum, e.g. "owner". Drives the toast's chip colour. */
  roleEnum?: string
  /** Optional same-origin `/platform/...` resource deep-link. */
  destination?: string
}): void {
  const { tenantId, name, isHomeTenant, roleLabel, roleEnum, destination } = options
  stashTenantSwitchToast({ name, role: roleLabel, roleEnum })
  const persist = persistLastActiveTenantPreference(isHomeTenant ? null : tenantId)
  Promise.race([persist, new Promise((r) => setTimeout(r, 500))])
    .finally(() => navigateAfterTenantSwitch(destination))
}

export function consumePendingTenantSwitchToast(): PendingTenantSwitchToast | null {
  try {
    const raw = sessionStorage.getItem(PENDING_TOAST_KEY)
    if (!raw) return null
    sessionStorage.removeItem(PENDING_TOAST_KEY)
    const parsed = JSON.parse(raw)
    if (parsed && typeof parsed.name === 'string') {
      return {
        name: parsed.name,
        role: typeof parsed.role === 'string' ? parsed.role : undefined,
        roleEnum: typeof parsed.roleEnum === 'string' ? parsed.roleEnum : undefined,
      }
    }
    return null
  } catch {
    return null
  }
}

/**
 * Persist the user's "last active tenant" preference server-side so a
 * fresh login (new device, new refresh token, cleared browser) drops
 * them back into this workspace instead of always bouncing to their
 * home tenant.
 *
 * Conventions:
 *   - Pass the target tenant id when switching to a peer tenant.
 *   - Pass `null` (which sends `0`) when switching back to the home
 *     tenant — that clears the preference and reverts the user to the
 *     "always start at home" default.
 *
 * Fire-and-forget: callers usually trigger a full-page reload right
 * after; this returns the in-flight promise so callers can race it
 * against a small budget if they want best-effort completion before
 * navigation. Failures are logged but never thrown — losing one
 * persist is recoverable on the next switch.
 */
export function persistLastActiveTenantPreference(
  tenantId: number | null,
): Promise<void> {
  const payload = { last_active_tenant_id: tenantId == null ? 0 : tenantId }
  return updateMyPreferences(payload)
    .then((res) => {
      if (!res.success) {
        console.warn('persistLastActiveTenantPreference: server rejected update', res.message)
      }
    })
    .catch((err) => {
      console.warn('persistLastActiveTenantPreference: network/serialization error', err)
    })
}
