<template>
  <main class="document-link-resolver">
    <t-loading v-if="loading" />
    <template v-else>
      <t-icon name="file-search" size="36px" />
      <p role="alert">{{ error }}</p>
      <t-button variant="outline" @click="resolve">{{ t('documentDiscovery.retry') }}</t-button>
      <RouterLink :to="{ name: 'documentWorkspace' }">{{ t('documentWorkspace.allBooks') }}</RouterLink>
    </template>
  </main>
</template>
<script setup lang="ts">
import { onBeforeUnmount, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { getKnowledgeDetails } from '@/api/knowledge-base'
import { documentLinkPrefix, linkedDocumentID } from './documentLinks'
const route = useRoute(), router = useRouter()
const { t } = useI18n()
const loading = ref(false), error = ref('')
let generation = 0
async function resolve() {
  const ticket = ++generation
  const id = String(route.params.documentId || '')
  loading.value = true
  error.value = ''
  try {
    if (!linkedDocumentID(documentLinkPrefix + id)) throw new Error()
    const result: any = await getKnowledgeDetails(id)
    if (ticket !== generation) return
    if (!result?.success || !result.data?.knowledge_base_id) throw new Error()
    await router.replace({ name: 'documentWorkspace', params: { kbId: result.data.knowledge_base_id }, query: { doc: id } })
  } catch { if (ticket === generation) error.value = t('documentDiscovery.unavailable') }
  finally { if (ticket === generation) loading.value = false }
}
watch(() => route.params.documentId, resolve, { immediate: true })
onBeforeUnmount(() => { generation++ })
</script>
<style scoped lang="less">
.document-link-resolver { min-height: 100vh; display: flex; flex-direction: column; align-items: center; justify-content: center; gap: 16px; background: var(--td-bg-color-container); color: var(--td-text-color-secondary); }
</style>
