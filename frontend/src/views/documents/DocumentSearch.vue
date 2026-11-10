<template>
  <t-dialog v-if="visible" :visible="true" :header="t(picking ? 'documentDiscovery.insertLink' : 'documentDiscovery.search')"
    width="min(720px, 94vw)" :footer="false" :close-on-overlay-click="false" @close="emit('update:visible', false)">
    <div class="document-search">
      <t-input ref="input" v-model="query" clearable :maxlength="100" :placeholder="t('documentDiscovery.placeholder')"
        :aria-label="t('documentDiscovery.placeholder')" @enter="load(false)" @keydown.down.prevent="step(1)" @keydown.up.prevent="step(-1)">
        <template #prefix-icon><t-icon name="search" /></template>
      </t-input>
      <p class="document-search__hint">{{ t('documentDiscovery.scope') }}</p>
      <p v-if="error" class="document-search__error" role="alert">{{ error }} <t-button size="small" variant="text" @click="load(false)">{{ t('documentDiscovery.retry') }}</t-button></p>
      <t-loading v-if="loading && !items.length" size="small" />
      <p v-else-if="!items.length && !error" class="document-search__hint">{{ t('documentDiscovery.empty') }}</p>
      <div class="document-search__results" @keydown.down.prevent="step(1)" @keydown.up.prevent="step(-1)">
        <button v-for="(item, index) in items" :key="item.id" :ref="el => { if (el) buttons[index] = el as HTMLButtonElement }"
          type="button" class="document-search__result" :disabled="loading" @click="select(item)" @focus="active = index">
          <strong><template v-for="(part, i) in highlightedParts(item.title, appliedQuery)" :key="i"><mark v-if="part.match">{{ part.text }}</mark><span v-else>{{ part.text }}</span></template></strong>
          <span class="document-search__path">{{ item.folder_path || t('documentNavigation.root') }} · {{ t(item.match_field === 'body' ? 'documentDiscovery.bodyMatch' : 'documentDiscovery.titleMatch') }}</span>
          <span class="document-search__snippet"><template v-for="(part, i) in highlightedParts(item.snippet, appliedQuery)" :key="i"><mark v-if="part.match">{{ part.text }}</mark><span v-else>{{ part.text }}</span></template></span>
        </button>
      </div>
      <t-button v-if="hasMore" variant="outline" block :loading="loading" @click="load(true)">{{ t('documentDiscovery.more') }}</t-button>
    </div>
  </t-dialog>
</template>
<script setup lang="ts">
import { nextTick, onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { searchDocuments, type DocumentSearchItem } from '@/api/knowledge-base'
import { highlightedParts } from './documentLinks'
const props = defineProps<{ visible: boolean; kbId: string; picking?: boolean }>()
const emit = defineEmits<{ 'update:visible': [value: boolean]; select: [item: DocumentSearchItem] }>()
const { t } = useI18n()
const query = ref('')
const appliedQuery = ref('')
const items = ref<DocumentSearchItem[]>([])
const loading = ref(false)
const error = ref('')
const hasMore = ref(false)
const offset = ref(0)
const input = ref<{ focus: () => void } | null>(null)
const buttons = ref<HTMLButtonElement[]>([])
const active = ref(-1)
let generation = 0
let timer: ReturnType<typeof setTimeout> | undefined
let disposed = false
async function load(append = false) {
  if (!props.visible || !props.kbId || (append && loading.value)) return
  clearTimeout(timer)
  const ticket = ++generation, kb = props.kbId, value = query.value.trim()
  loading.value = true
  error.value = ''
  if (!append) { items.value = []; hasMore.value = false; active.value = -1; buttons.value = [] }
  try {
    const result: any = await searchDocuments(kb, value, append ? offset.value : 0)
    if (disposed || ticket !== generation || kb !== props.kbId || !props.visible) return
    if (!result?.success || !result.data) throw new Error(result?.message || t('documentDiscovery.loadFailed'))
    items.value = append ? [...new Map<string, DocumentSearchItem>([...items.value, ...result.data.items].map((item: DocumentSearchItem) => [item.id, item])).values()] : result.data.items
    offset.value = result.data.next_offset
    hasMore.value = result.data.has_more
    appliedQuery.value = value
  } catch (e: any) { if (!disposed && ticket === generation) error.value = e?.message || t('documentDiscovery.loadFailed') }
  finally { if (!disposed && ticket === generation) loading.value = false }
}
function select(item: DocumentSearchItem) {
  if (loading.value || appliedQuery.value !== query.value.trim()) return
  emit('select', item)
  emit('update:visible', false)
}
function step(delta: number) {
  if (!items.value.length || loading.value) return
  active.value = Math.min(items.value.length - 1, Math.max(0, active.value + delta))
  buttons.value[active.value]?.focus()
}
watch(query, () => {
  ++generation
  loading.value = true
  clearTimeout(timer)
  timer = setTimeout(() => { void load(false) }, 250)
})
watch(() => [props.visible, props.kbId] as const, async ([visible]) => {
  ++generation
  clearTimeout(timer)
  loading.value = false
  if (visible) { void load(false); await nextTick(); input.value?.focus() }
}, { immediate: true })
onBeforeUnmount(() => { disposed = true; generation++; clearTimeout(timer) })
</script>
<style scoped lang="less">
.document-search { display: flex; flex-direction: column; gap: 10px; }
.document-search__results { max-height: 52vh; overflow: auto; display: flex; flex-direction: column; gap: 6px; }
.document-search__result { display: flex; flex-direction: column; gap: 6px; padding: 12px; text-align: left; background: var(--td-bg-color-container); color: var(--td-text-color-primary); border: 1px solid var(--td-component-stroke); border-radius: var(--app-radius-md); cursor: pointer; overflow-wrap: anywhere; font: inherit; }
.document-search__result:hover, .document-search__result:focus-visible { border-color: var(--td-brand-color); background: var(--td-brand-color-light); }
.document-search__path, .document-search__hint { font-size: var(--app-text-xs); color: var(--td-text-color-secondary); }
.document-search__snippet { font-size: var(--app-text-sm); color: var(--td-text-color-secondary); line-height: 1.6; }
.document-search mark { background: var(--td-warning-color-light); color: var(--td-text-color-primary); }
.document-search__error { color: var(--td-error-color); }
</style>
