import { computed, onBeforeUnmount, ref, watch } from 'vue'

/** Optional, identity-scoped display preferences; never store document content. */
export function useWorkspaceLayout(scope: () => { user: string; tenant: string | number; kb: string } | null) {
  const width = ref<number | null>(null)
  const outline = ref<boolean | null>(null)
  const resizing = ref(false)
  const key = computed(() => {
    const value = scope()
    return value ? 'weknora:workspace-layout:v1:' + [value.user, String(value.tenant), value.kb].map(encodeURIComponent).join(':') : ''
  })
  let startX = 0, startWidth = 0, previousWidth: number | null = null, pointer: number | null = null
  const viewport = ref(window.innerWidth)
  const maxWidth = computed(() => Math.max(208, Math.min(480, viewport.value - 420)))
  const clamp = (value: number) => Math.round(Math.max(208, Math.min(maxWidth.value, value)))
  const sidebarWidth = computed(() => width.value == null ? (viewport.value <= 760 ? 208 : 272) : clamp(width.value))
  const style = computed(() => ({ '--workspace-sidebar-width': `${sidebarWidth.value}px` }))
  function save() {
    try { if (key.value) localStorage.setItem(key.value, JSON.stringify({ width: width.value, outline: outline.value })) } catch { /* Optional preferences. */ }
  }
  watch(key, () => {
    resizing.value = false; pointer = null; width.value = null; outline.value = null
    try {
      const value = JSON.parse(key.value ? localStorage.getItem(key.value) || '{}' : '{}')
      if (typeof value?.width === 'number' && Number.isFinite(value.width)) width.value = Math.max(208, Math.min(480, value.width))
      if (typeof value?.outline === 'boolean') outline.value = value.outline
    } catch { /* Ignore unavailable or damaged preferences. */ }
  }, { immediate: true })
  function start(event: PointerEvent) {
    if (event.button !== 0 || resizing.value) return
    const target = event.currentTarget as HTMLElement
    target.setPointerCapture(event.pointerId)
    startX = event.clientX; startWidth = sidebarWidth.value; previousWidth = width.value
    pointer = event.pointerId; resizing.value = true
    event.preventDefault()
  }
  function move(event: PointerEvent) {
    if (resizing.value && pointer === event.pointerId) width.value = clamp(startWidth + event.clientX - startX)
  }
  function finish(event: PointerEvent, cancel = false) {
    if (!resizing.value || pointer !== event.pointerId) return
    resizing.value = false; pointer = null
    if (cancel) width.value = previousWidth
    else save()
  }
  function reset() { width.value = null; save() }
  function keydown(event: KeyboardEvent) {
    if (event.key === 'Home') reset()
    else if (event.key === 'ArrowLeft' || event.key === 'ArrowRight') {
      width.value = clamp(sidebarWidth.value + (event.key === 'ArrowLeft' ? -16 : 16)); save()
    } else return
    event.preventDefault()
  }
  function setOutline(value: boolean) { outline.value = value; save() }
  const resize = () => { viewport.value = window.innerWidth }
  window.addEventListener('resize', resize)
  onBeforeUnmount(() => window.removeEventListener('resize', resize))
  return { style, sidebarWidth, maxWidth, resizing, outline, setOutline, start, move, finish, reset, keydown }
}
