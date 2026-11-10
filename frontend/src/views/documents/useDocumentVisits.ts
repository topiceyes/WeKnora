import { computed, onMounted, ref, watch } from 'vue'

/** Browser-only navigation preferences; never grant access from cached IDs. */
export function useDocumentVisits(scope: () => { user: string; tenant: string | number; kb: string } | null) {
  const favorites = ref<string[]>([]), recent = ref<string[]>([])
  const key = computed(() => { const s = scope(); return s ? 'weknora:document-visits:v1:' + [s.user, String(s.tenant), s.kb].map(encodeURIComponent).join(':') : '' })
  function load() {
    favorites.value = []; recent.value = []
    if (!key.value) return
    try { const v = JSON.parse(localStorage.getItem(key.value) || '{}'); const ids = (value: unknown) => Array.isArray(value) ? [...new Set(value.filter((id): id is string => typeof id === 'string'))].slice(0, 200) : []
      favorites.value = ids(v.favorites); recent.value = ids(v.recent)
    } catch { /* Broken or unavailable preferences must not block navigation. */ }
  }
  function save() { try { if (key.value) localStorage.setItem(key.value, JSON.stringify({ favorites: favorites.value, recent: recent.value })) } catch { /* Preferences are optional, not document drafts. */ } }
  function visit(id: string) { recent.value = [id, ...recent.value.filter(value => value !== id)].slice(0, 50); save() }
  function toggle(id: string) { favorites.value = favorites.value.includes(id) ? favorites.value.filter(v => v !== id) : [...favorites.value, id].slice(-200); save() }
  watch(key, load, { immediate: true })
  onMounted(load)
  return { favorites, recent, visit, toggle }
}
