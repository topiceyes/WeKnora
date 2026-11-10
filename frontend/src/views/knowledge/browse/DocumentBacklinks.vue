<template>
  <aside class="document-backlinks" :aria-label="t('documentDiscovery.backlinks')">
    <div class="document-backlinks__heading"><strong>{{ t('documentDiscovery.backlinks') }}</strong><t-button size="small" variant="text" :disabled="loading" @click="load(false)">{{ t('documentDiscovery.refresh') }}</t-button></div>
    <p class="document-backlinks__hint">{{ t('documentDiscovery.backlinkScope') }}</p>
    <p v-if="error" role="alert">{{ error }}</p>
    <t-loading v-if="loading && !items.length" size="small" />
    <p v-else-if="!items.length && !error" class="document-backlinks__hint">{{ t('documentDiscovery.noBacklinks') }}</p>
    <a v-for="item in items" :key="item.id" :href="documentLink(item.id)" class="document-backlinks__item">
      <span>{{ item.title }}</span><small>{{ item.folder_path || t('documentNavigation.root') }}</small>
    </a>
    <t-button v-if="hasMore" size="small" variant="outline" :loading="loading" @click="load(true)">{{ t('documentDiscovery.more') }}</t-button>
  </aside>
</template>
<script setup lang="ts">
import { onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { documentBacklinks, type DocumentSearchItem } from '@/api/knowledge-base'
import { documentLink } from '@/views/documents/documentLinks'
const props = defineProps<{ knowledgeId: string }>()
const { t } = useI18n()
const items = ref<DocumentSearchItem[]>([]), loading = ref(false), error = ref(''), offset = ref(0), hasMore = ref(false)
let generation = 0
async function load(append = false) {
  if (!props.knowledgeId) return
  const ticket = ++generation, id = props.knowledgeId
  loading.value = true; error.value = ''
  try {
    const result: any = await documentBacklinks(id, append ? offset.value : 0)
    if (ticket !== generation) return
    if (!result?.success || !result.data) throw new Error(result?.message || t('documentDiscovery.loadFailed'))
    items.value = append ? [...new Map<string, DocumentSearchItem>([...items.value, ...result.data.items].map((item: DocumentSearchItem) => [item.id, item])).values()] : result.data.items
    offset.value = result.data.next_offset; hasMore.value = result.data.has_more
  } catch (e: any) { if (ticket === generation) error.value = e?.message || t('documentDiscovery.loadFailed') }
  finally { if (ticket === generation) loading.value = false }
}
watch(() => props.knowledgeId, () => { items.value = []; hasMore.value = false; void load(false) }, { immediate: true })
onBeforeUnmount(() => { generation++ })
</script>
<style scoped lang="less">
.document-backlinks { margin-top: 48px; padding-top: 20px; border-top: 1px solid var(--td-component-stroke); }
.document-backlinks__heading { display: flex; justify-content: space-between; align-items: center; }
.document-backlinks__hint, .document-backlinks small { font-size: var(--app-text-xs); color: var(--td-text-color-secondary); }
.document-backlinks__item { display: flex; justify-content: space-between; gap: 12px; padding: 10px 0; color: var(--td-brand-color); overflow-wrap: anywhere; }
</style>
