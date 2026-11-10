import { onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { documentFingerprint } from '../knowledge/browse/structuredDocument'
import { draftPrefix, readDocumentDrafts, removeDocumentDraft, writeDocumentDraft,
  type DraftContent, type DraftScope, type StoredDraft } from './documentDrafts'

/** Local recovery copies are isolated by account, active workspace, book, document and editor session. */
export function useDocumentDraft(options: {
  scope: () => DraftScope | null
  value: () => DraftContent
  active: () => boolean
  dirty: () => boolean
}) {
  const writer = globalThis.crypto?.randomUUID?.() ?? `${Date.now()}-${Math.random().toString(36).slice(2)}`
  const candidates = ref<StoredDraft[]>([])
  const storageError = ref(false)
  const savedAt = ref(0)
  let own: StoredDraft | null = null
  let recovered: StoredDraft | null = null
  let timer: ReturnType<typeof setTimeout> | null = null
  let pending: { scope: DraftScope; value: DraftContent } | null = null
  const cancelTimer = () => { if (timer) clearTimeout(timer); timer = null }
  function refresh() {
    const scope = options.scope()
    candidates.value = []
    if (!scope) return
    try {
      candidates.value = readDocumentDrafts(localStorage, scope).filter(draft => draft.writer !== writer)
      if (!pending) storageError.value = false
    } catch { storageError.value = true }
  }
  function flush() {
    cancelTimer()
    if (!pending) return
    const { scope, value } = pending
    try {
      own = writeDocumentDraft(localStorage, scope, writer, value)
      pending = null
      savedAt.value = own.savedAt
      storageError.value = false
    } catch { storageError.value = true }
  }
  function clear() {
    cancelTimer()
    pending = null
    try {
      if (own) removeDocumentDraft(localStorage, own)
      if (recovered) removeDocumentDraft(localStorage, recovered)
      own = null
      recovered = null
      savedAt.value = 0
      storageError.value = false
    } catch { storageError.value = true }
    refresh()
  }
  function discard(draft: StoredDraft) {
    try { removeDocumentDraft(localStorage, draft) } catch { storageError.value = true }
    refresh()
  }
  function adopt(draft: StoredDraft) {
    recovered = draft
    // Keep the source until the restored content has been saved to the server or explicitly discarded.
    candidates.value = candidates.value.filter(item => item.key !== draft.key)
  }
  watch(() => {
    const scope = options.scope()
    return scope ? draftPrefix(scope) : ''
  }, () => { flush(); pending = null; own = null; recovered = null; savedAt.value = 0; refresh() }, { immediate: true, flush: 'sync' })
  watch(() => [options.active(), options.dirty(), options.value().title, options.value().content, options.value().baseVersion, options.value().folderPath, documentFingerprint(options.value().document)], () => {
    cancelTimer()
    const scope = options.scope()
    if (scope && options.active() && options.dirty()) {
      pending = { scope: { ...scope }, value: { ...options.value() } }
      timer = setTimeout(flush, 250)
    } else pending = null
  }, { flush: 'sync' })
  const onStorage = (event: StorageEvent) => {
    const scope = options.scope()
    if (scope && (!event.key || event.key.startsWith(draftPrefix(scope)))) refresh()
  }
  const onHidden = () => { if (document.visibilityState === 'hidden') flush() }
  onMounted(() => {
    window.addEventListener('pagehide', flush)
    window.addEventListener('storage', onStorage)
    document.addEventListener('visibilitychange', onHidden)
  })
  onBeforeUnmount(() => {
    flush()
    window.removeEventListener('pagehide', flush)
    window.removeEventListener('storage', onStorage)
    document.removeEventListener('visibilitychange', onHidden)
  })
  return { candidates, storageError, savedAt, refresh, flush, clear, discard, adopt }
}
