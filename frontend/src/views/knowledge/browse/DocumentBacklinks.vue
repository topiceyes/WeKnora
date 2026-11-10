<template>
  <aside class="document-backlinks" :aria-label="t('documentDiscovery.backlinks')">
    <div class="document-backlinks__heading">
      <button type="button" class="document-backlinks__toggle" :aria-expanded="expanded" @click="open = !expanded">
        <t-icon :name="expanded ? 'chevron-down' : 'chevron-right'" size="14px" />
        <span>{{ t('documentDiscovery.backlinks') }}</span><small v-if="!loading && !error">{{ items.length }}{{ hasMore ? '+' : '' }}</small>
      </button>
      <t-button v-if="expanded || error" size="small" variant="text" :disabled="loading" @click="load(false)">{{ t('documentDiscovery.refresh') }}</t-button>
    </div>
    <template v-if="expanded">
    <p class="document-backlinks__hint">{{ t('documentDiscovery.backlinkScope') }}</p>
    <p v-if="error" role="alert">{{ error }}</p>
    <t-loading v-if="loading && !items.length" size="small" />
    <p v-else-if="!items.length && !error" class="document-backlinks__hint">{{ t('documentDiscovery.noBacklinks') }}</p>
    <a v-for="item in items" :key="item.id" :href="documentLink(item.id)" class="document-backlinks__item">
      <span>{{ item.title }}</span><small>{{ item.folder_path || t('documentNavigation.root') }}</small>
    </a>
    <t-button v-if="hasMore" size="small" variant="outline" :loading="loading" @click="load(true)">{{ t('documentDiscovery.more') }}</t-button>
    </template>
  </aside>
</template>
<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { documentBacklinks, type DocumentSearchItem } from '@/api/knowledge-base'
import { documentLink } from '@/views/documents/documentLinks'
const props = defineProps<{ knowledgeId: string }>()
const { t } = useI18n()
const items = ref<DocumentSearchItem[]>([]), loading = ref(false), error = ref(''), offset = ref(0), hasMore = ref(false)
const open = ref<boolean | null>(null)
const expanded = computed(() => open.value ?? (items.value.length > 0 || !!error.value))
let generation = 0
async function load(append = false) {
  if (!props.knowledgeId || (append && loading.value)) return
  const ticket = ++generation, id = props.knowledgeId
  loading.value = true; error.value = ''
  try {
    const result: any = await documentBacklinks(id, append ? offset.value : 0)
    if (ticket !== generation) return
    if (!result?.success || !Array.isArray(result.data?.items)) throw new Error(result?.message || t('documentDiscovery.loadFailed'))
    items.value = append ? [...new Map<string, DocumentSearchItem>([...items.value, ...result.data.items].map((item: DocumentSearchItem) => [item.id, item])).values()] : result.data.items
    offset.value = result.data.next_offset; hasMore.value = result.data.has_more
  } catch (e: any) { if (ticket === generation) error.value = e?.message || t('documentDiscovery.loadFailed') }
  finally { if (ticket === generation) loading.value = false }
}
watch(() => props.knowledgeId, () => { items.value = []; hasMore.value = false; offset.value = 0; open.value = null; error.value = ''; void load(false) }, { immediate: true })
onBeforeUnmount(() => { generation++ })
</script>
<style scoped lang="less">
.document-backlinks { margin-top: 28px; padding-top: 8px; border-top: 1px solid var(--td-component-stroke); }
.document-backlinks__heading { display: flex; justify-content: space-between; align-items: center; }
.document-backlinks__toggle { display: flex; align-items: center; gap: 6px; padding: 6px 0; background: transparent; color: var(--td-text-color-secondary); border: 0; cursor: pointer; font: inherit; font-size: var(--app-text-sm);
  &:focus-visible { outline: 2px solid var(--app-focus-border); outline-offset: 2px; }
}
.document-backlinks__hint, .document-backlinks small { font-size: var(--app-text-xs); color: var(--td-text-color-secondary); }
.document-backlinks__item { display: flex; justify-content: space-between; gap: 12px; padding: 10px 0; color: var(--td-brand-color); overflow-wrap: anywhere; }
</style>
