import { onBeforeUnmount, onMounted, ref, watch } from 'vue'

export type AutosaveOutcome = 'saved' | 'error' | 'conflict' | 'blocked'

export function useDocumentAutosave(options: {
  enabled: () => boolean
  dirty: () => boolean
  busy: () => boolean
  revision: () => unknown
  save: () => Promise<AutosaveOutcome>
  delay?: number
  minimumInterval?: number
}) {
  const state = ref<'idle' | 'pending' | 'saving' | AutosaveOutcome>('idle')
  const savedAt = ref(0)
  let timer: ReturnType<typeof setTimeout> | null = null
  let lastSave = 0
  let disposed = false
  let paused = false
  const stop = () => { if (timer) clearTimeout(timer); timer = null }
  function schedule() {
    stop()
    if (disposed || paused || !options.enabled()) return
    if (!options.dirty()) { state.value = savedAt.value ? 'saved' : 'idle'; return }
    if (options.busy() || state.value === 'saving') return
    state.value = 'pending'
    const wait = Math.max(options.delay ?? 1800, lastSave + (options.minimumInterval ?? 10000) - Date.now())
    timer = setTimeout(() => { void save() }, wait)
  }
  async function save(): Promise<AutosaveOutcome> {
    stop()
    if (disposed || paused || !options.enabled() || !options.dirty() || options.busy() || state.value === 'saving') return 'blocked'
    state.value = 'saving'
    lastSave = Date.now()
    let outcome: AutosaveOutcome
    try { outcome = await options.save() } catch { outcome = 'error' }
    if (disposed) return outcome
    state.value = outcome
    if (outcome === 'saved') { savedAt.value = Date.now(); schedule() }
    // Failures do not retry on every keystroke: wait for an explicit retry or connectivity recovery.
    else if (outcome === 'error' || outcome === 'conflict') paused = true
    return outcome
  }
  function resume() { paused = false; state.value = 'idle'; schedule() }
  function pause() { paused = true; stop() }
  watch(() => [options.enabled(), options.dirty(), options.busy(), options.revision()], schedule)
  const onOnline = () => { if (state.value === 'error') resume() }
  onMounted(() => window.addEventListener('online', onOnline))
  onBeforeUnmount(() => { disposed = true; stop(); window.removeEventListener('online', onOnline) })
  return { state, savedAt, save, pause, resume, stop }
}
