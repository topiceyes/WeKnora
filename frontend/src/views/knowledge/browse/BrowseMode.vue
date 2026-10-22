<template>
  <div class="browse-mode">
    <BrowseDocTree
      :tree="tree"
      :selected-doc-id="selectedDocId"
      :loading="loading"
      @select="selectDoc"
      @refresh="loadTree"
    />
    <DocReader
      ref="readerRef"
      :doc="selectedDoc"
      :kb-id="kbId"
      :can-download="canDownload"
      @open-source-doc="(id) => emit('open-source-doc', id)"
      @outline-change="(items) => (outlineItems = items)"
      @outline-active="(index) => (outlineActive = index)"
    />
    <DocOutline
      v-model:collapsed="outlineCollapsed"
      :items="outlineItems"
      :active-index="outlineActive"
      @jump="(index) => readerRef?.scrollToHeading(index)"
    />
  </div>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { getKnowledgeTree, type KnowledgeTree, type KnowledgeTreeDocument } from '@/api/knowledge-base'
import BrowseDocTree from './BrowseDocTree.vue'
import DocReader from './DocReader.vue'
import DocOutline from './DocOutline.vue'
import type { OutlineItem } from './types'

const props = withDefaults(defineProps<{
  kbId: string
  canDownload?: boolean
}>(), {
  canDownload: false,
})

const emit = defineEmits<{
  'open-source-doc': [knowledgeId: string]
}>()

const route = useRoute()
const router = useRouter()

const tree = ref<KnowledgeTree | null>(null)
const loading = ref(false)
const selectedDocId = ref('')
const outlineItems = ref<OutlineItem[]>([])
const outlineActive = ref(-1)
const outlineCollapsed = ref(false)
const readerRef = ref<InstanceType<typeof DocReader> | null>(null)

const selectedDoc = computed<KnowledgeTreeDocument | null>(() =>
  tree.value?.documents?.find((doc) => doc.id === selectedDocId.value) ?? null,
)

// 选中文档同步到 ?doc=，刷新/分享链接可还原（沿用 ?knowledge_id= 的先例，
// 但独立于它——?knowledge_id= 仍归「管理」视图的详情抽屉使用）。
const selectDoc = (docId: string) => {
  if (docId === selectedDocId.value) return
  selectedDocId.value = docId
}

watch(selectedDocId, (docId) => {
  const query = { ...route.query }
  if (docId) {
    query.doc = docId
  } else {
    delete query.doc
  }
  router.replace({ query })
})

const loadTree = async () => {
  if (!props.kbId) return
  loading.value = true
  try {
    const res: any = await getKnowledgeTree(props.kbId)
    const data = res?.data
    tree.value = data?.folders ? (data as KnowledgeTree) : { ...(data ?? {}), folders: [], documents: [] }
    const docs = tree.value?.documents ?? []
    // 当前选中失效（被删除/移动）或尚未选中时，落到第一篇文档。
    if (!docs.some((doc) => doc.id === selectedDocId.value)) {
      const initial = route.query.doc as string
      selectedDocId.value = docs.some((doc) => doc.id === initial) ? initial : (docs[0]?.id ?? '')
    }
  } catch (error) {
    console.error('[BrowseMode] failed to load knowledge tree:', error)
  } finally {
    loading.value = false
  }
}

watch(() => props.kbId, (kbId, oldKbId) => {
  if (kbId !== oldKbId) {
    tree.value = null
    selectedDocId.value = ''
  }
  loadTree()
}, { immediate: true })
</script>

<style scoped lang="less">
// 外层 .knowledge-layout 已提供水平内边距与列向 flex，这里只占满剩余高度。
.browse-mode {
  flex: 1;
  min-height: 0;
  display: flex;
  padding-bottom: 16px;
  box-sizing: border-box;
}
</style>
