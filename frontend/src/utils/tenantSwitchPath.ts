export const TENANT_SWITCH_FALLBACK_PATH = '/platform/knowledge-bases'

/**
 * Return a safe same-origin platform URL after a tenant switch. Most callers
 * omit destination and land on the KB list; resource deep-links may opt in to
 * a concrete `/platform/...` path. Raw backslashes are rejected because
 * browsers can normalize them as URL separators.
 */
export function tenantSwitchTargetPath(
  _currentPath: string,
  destination?: string,
): string {
  if (
    typeof destination !== 'string'
    || !destination.startsWith('/platform/')
    || destination.includes('\\')
    || /[\u0000-\u001f\u007f]/.test(destination)
  ) {
    return TENANT_SWITCH_FALLBACK_PATH
  }

  try {
    const base = new URL('https://tenant-switch.invalid')
    const target = new URL(destination, base)
    // URL parsing normalizes dot segments, including percent-encoded ones.
    // Re-check the normalized pathname so `/platform/../login` cannot escape
    // the platform route namespace even though its raw prefix looked valid.
    if (target.origin !== base.origin || !target.pathname.startsWith('/platform/')) {
      return TENANT_SWITCH_FALLBACK_PATH
    }
    return `${target.pathname}${target.search}${target.hash}`
  } catch {
    return TENANT_SWITCH_FALLBACK_PATH
  }
}
