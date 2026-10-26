<template>
  <div class="doc-reader">
    <div v-if="!doc" class="doc-reader__placeholder">
      <t-icon name="file-paste" size="40px" />
      <p>{{ t('knowledgeBase.browse.pickDocument') }}</p>
    </div>

    <template v-else>
      <header class="doc-reader__header">
        <div class="doc-reader__title-row">
          <t-icon :name="browseDocIcon(doc)" size="20px" class="doc-reader__doc-icon" />
          <h1 class="doc-reader__title" :title="displayTitle">{{ displayTitle }}</h1>
        </div>
        <div class="doc-reader__meta">
          <span v-if="detail?.updated_at || doc.updated_at">
            {{ t('knowledgeBase.browse.updatedAt', { time: displayTime }) }}
          </span>
        </div>
        <div class="doc-reader__actions">
          <t-tooltip v-if="canEditNow && !editing" :content="t('knowledgeBase.browse.edit')" placement="bottom">
            <button type="button" class="doc-reader__action" :aria-label="t('knowledgeBase.browse.edit')"
              @click="startEdit">
              <t-icon name="edit" size="16px" />
            </button>
          </t-tooltip>
          <template v-if="!editing">
          <t-tooltip v-if="doc.type !== 'faq'" :content="t('knowledgeBase.browse.history.title')" placement="bottom">
            <button type="button" class="doc-reader__action" :aria-label="t('knowledgeBase.browse.history.title')"
              @click="historyOpen = true">
              <t-icon name="history" size="16px" />
            </button>
          </t-tooltip>
          <t-tooltip v-if="canPreviewOriginal" :content="t('knowledgeBase.browse.originalPreview')" placement="bottom">
            <button type="button" class="doc-reader__action" :aria-label="t('knowledgeBase.browse.originalPreview')"
              @click="previewOpen = true">
              <t-icon name="file-search" size="16px" />
            </button>
          </t-tooltip>
          <t-tooltip :content="t('knowledgeBase.browse.aiViewTip')" placement="bottom">
            <button type="button" class="doc-reader__action" :aria-label="t('knowledgeBase.browse.aiView')"
              @click="emit('open-source-doc', doc.id)">
              <t-icon name="data-search" size="16px" />
            </button>
          </t-tooltip>
          <t-tooltip v-if="canDownload && doc.type === 'file'" :content="t('knowledgeBase.browse.download')" placement="bottom">
            <button type="button" class="doc-reader__action" :aria-label="t('knowledgeBase.browse.download')"
              :disabled="downloading" @click="downloadFile">
              <t-icon :name="downloading ? 'loading' : 'download'" size="16px" />
            </button>
          </t-tooltip>
          </template>
        </div>
      </header>

      <div v-if="indexStale && !editing" class="doc-reader__stale">
        <t-icon name="notification-filled" size="14px" />
        <span class="doc-reader__stale-text">{{ t('knowledgeBase.browse.staleBanner') }}</span>
        <t-button v-if="canEdit" size="small" theme="warning" variant="outline" :loading="relearning" @click="relearn">
          {{ relearning ? t('knowledgeBase.browse.learning') : t('knowledgeBase.browse.relearn') }}
        </t-button>
      </div>

      <div v-if="editing" class="doc-reader__edit">
        <t-input v-model="editTitle" class="doc-reader__edit-title" :maxlength="200" clearable />
        <RichEditor v-model="editContent" :kb-id="kbId" class="doc-reader__edit-editor" />
        <div class="doc-reader__edit-actions">
          <t-button variant="outline" :disabled="saving" @click="cancelEdit">
            {{ t('knowledgeBase.browse.cancel') }}
          </t-button>
          <t-button theme="primary" :loading="saving" @click="saveEdit">
            {{ t('knowledgeBase.browse.save') }}
          </t-button>
        </div>
      </div>

      <div v-else ref="scrollRef" class="doc-reader__scroll" @scroll.passive="onScroll">
        <div v-if="loading" class="doc-reader__state">
          <t-skeleton animation="gradient" :row-col="skeletonRows" />
        </div>

        <div v-else-if="loadError" class="doc-reader__state doc-reader__state--hint">
          <t-icon name="error-circle" size="32px" />
          <p>{{ loadError }}</p>
          <t-button size="small" variant="outline" @click="loadContent">
            {{ t('knowledgeBase.browse.retry') }}
          </t-button>
        </div>

        <div v-else-if="statusHint" class="doc-reader__state doc-reader__state--hint">
          <t-icon :name="statusIcon" size="32px" />
          <p>{{ statusHint }}</p>
        </div>

        <article v-else class="doc-reader__article">
          <div ref="contentRef" class="md-content" v-html="renderedHtml"></div>
        </article>
      </div>
    </template>

    <t-dialog
      v-model:visible="previewOpen"
      :header="displayTitle"
      width="80%"
      :footer="false"
      destroy-on-close
    >
      <div class="doc-reader__preview-body">
        <DocumentPreview
          v-if="previewOpen && doc"
          :knowledge-id="doc.id"
          :file-type="doc.file_type"
          :file-name="displayTitle"
          :active="previewOpen"
          fill-height
        />
      </div>
    </t-dialog>

    <VersionHistory
      v-if="doc"
      v-model:visible="historyOpen"
      :knowledge-id="doc.id"
      :kb-id="kbId"
      :can-edit="canEdit"
      @restored="onRestored"
    />
  </div>
</template>

<script setup lang="ts">
import { computed, defineAsyncComponent, nextTick, onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { DialogPlugin, MessagePlugin } from 'tdesign-vue-next'
import {
  downKnowledgeDetails,
  getKnowledgeDetails,
  listAllKnowledgeChunks,
  relearnKnowledge,
  updateKnowledgeContent,
  type KnowledgeTreeDocument,
} from '@/api/knowledge-base'
import { mergeChunks } from '@/utils/mergedContent'
import { processMarkdown } from '@/utils/markdownPipeline'
import { hydrateProtectedFileImages } from '@/utils/security'
import { openMermaidFullscreen } from '@/utils/mermaidViewer'
import { runMermaidOnDivs } from '@/utils/mermaidShared'
import { resolveFilePreviewExt, isKnownPreviewableExt } from '@/utils/filePreview'
import { formatStringDate } from '@/utils/index'
import { resolveKnowledgeDownloadFileName } from '../knowledgeDownloadFileName'
import DocumentPreview from '@/components/document-preview.vue'
import { browseDocIcon } from './docIcon'
import type { OutlineItem } from './types'

// 编辑器与版本历史是重依赖（tiptap / diff），首次使用时才拉 chunk，
// KB 首屏不背这个包。
const RichEditor = defineAsyncComponent(() => import('./RichEditor.vue'))
const VersionHistory = defineAsyncComponent(() => import('./VersionHistory.vue'))

const props = withDefaults(defineProps<{
  doc: KnowledgeTreeDocument | null
  kbId: string
  canDownload?: boolean
  /** 当前用户对该知识库有编辑权限（Editor+），控制编辑/重新学习入口。 */
  canEdit?: boolean
}>(), {
  canDownload: false,
  canEdit: false,
})

const emit = defineEmits<{
  'open-source-doc': [knowledgeId: string]
  'outline-change': [items: OutlineItem[]]
  'outline-active': [index: number]
  /** 保存或重新学习改变了文档（updated_at / index_stale / parse_status），树需要刷新。 */
  'doc-updated': []
}>()

const { t } = useI18n()

const detail = ref<any>(null)
const markdown = ref('')
const loading = ref(false)
const loadError = ref('')
const downloading = ref(false)
const previewOpen = ref(false)
const historyOpen = ref(false)
const scrollRef = ref<HTMLElement | null>(null)
const contentRef = ref<HTMLElement | null>(null)

const skeletonRows = [
  { width: '60%', height: '24px' },
  { width: '100%', height: '14px' },
  { width: '100%', height: '14px' },
  { width: '92%', height: '14px' },
  { width: '96%', height: '14px' },
  { width: '70%', height: '14px' },
]

const displayTitle = computed(() =>
  props.doc?.title || props.doc?.file_name || detail.value?.title || t('knowledgeBase.untitledDocument'),
)

const displayTime = computed(() => {
  const raw = detail.value?.updated_at || props.doc?.updated_at
  return raw ? formatStringDate(new Date(raw)) : ''
})

// 与 doc-content.vue 的 canPreview 一致：file 类型且扩展名可预览（音频除外，
// 音频在阅读器里只展示转写文本）。
const canPreviewOriginal = computed(() => {
  if (!props.doc || props.doc.type !== 'file') return false
  const ft = resolveFilePreviewExt(props.doc.title, props.doc.file_type)
  if (!ft) return false
  if (['mp3', 'wav', 'm4a', 'flac', 'ogg'].includes(ft)) return false
  return isKnownPreviewableExt(ft)
})

// 未解析完成 / 解析失败 / 无正文时的占位提示；为空字符串则正常渲染正文。
// 正文优先：manual 文档的正文存在 metadata 里，与索引状态无关；重新解析
// 失败的旧文档也仍有历史分片可读。只有拿不到正文时才按状态给提示。
const statusHint = computed(() => {
  if (!props.doc) return ''
  if (loading.value || loadError.value) return ''
  if (markdown.value) return ''
  const status = detail.value?.parse_status || props.doc.parse_status
  if (status === 'pending' || status === 'processing') {
    return t('knowledgeBase.browse.parseProcessing')
  }
  if (status === 'failed') {
    return detail.value?.error_message || t('knowledgeBase.browse.parseFailed')
  }
  return t('knowledgeBase.browse.emptyContent')
})

const statusIcon = computed(() => {
  const status = detail.value?.parse_status || props.doc?.parse_status
  if (status === 'failed') return 'error-circle'
  if (status === 'pending' || status === 'processing') return 'loading'
  return 'file-paste'
})

const renderedHtml = computed(() => processMarkdown(markdown.value))

// manual 文档的全文存在 knowledge.metadata.content 里；file/url 文档被在线
// 编辑后也会把编辑文本写进同一字段（metadata 可能是对象或 JSON 字符串）。
const parseMetadataObject = (metadata: any): Record<string, any> | null => {
  if (!metadata) return null
  try {
    const parsed = typeof metadata === 'string' ? JSON.parse(metadata) : metadata
    if (parsed && typeof parsed === 'object') return parsed
  } catch (error) {
    console.warn('[DocReader] Failed to parse metadata:', error)
  }
  return null
}

// 正文已编辑但尚未重新学习：阅读器展示新文本，AI 检索仍是旧索引。
const indexStale = computed(() => parseMetadataObject(detail.value?.metadata)?.index_stale === true)

const isInFlightStatus = (status?: string) =>
  status === 'pending' || status === 'processing' || status === 'finalizing'

let loadGeneration = 0

const loadContent = async () => {
  const doc = props.doc
  if (!doc) return
  const generation = ++loadGeneration
  loading.value = true
  loadError.value = ''
  markdown.value = ''
  detail.value = null
  try {
    const result: any = await getKnowledgeDetails(doc.id)
    if (generation !== loadGeneration) return
    const data = result?.data
    if (!result?.success || !data) {
      throw new Error(result?.message || t('knowledgeBase.browse.loadFailed'))
    }
    detail.value = data
    // 编辑内容（metadata.content）优先：manual 文档天然如此，file/url 文档
    // 编辑后也以它为准；未编辑的 file/url 仍回退到分片拼接。
    const edited = parseMetadataObject(data.metadata)?.content || ''
    if (edited) {
      markdown.value = edited
    } else if ((data.type || doc.type) === 'manual') {
      markdown.value = ''
    } else {
      const status = data.parse_status || doc.parse_status
      if (isInFlightStatus(status) || status === 'failed') {
        markdown.value = ''
      } else {
        const chunks = await listAllKnowledgeChunks(doc.id)
        if (generation !== loadGeneration) return
        markdown.value = mergeChunks(chunks.data)
      }
    }
  } catch (error: any) {
    if (generation !== loadGeneration) return
    console.error('[DocReader] load failed', error)
    loadError.value = error?.message || t('knowledgeBase.browse.loadFailed')
  } finally {
    if (generation === loadGeneration) {
      loading.value = false
    }
  }
}

// ---------- 在线编辑 / 重新学习 ----------

const editing = ref(false)
const editTitle = ref('')
const editContent = ref('')
const saving = ref(false)
const relearning = ref(false)
// 进入编辑时快照的 metadata.version（未编辑过的文档为 0），保存时作为
// base_version 做乐观并发检测；后端不匹配返回 409。
const editBaseVersion = ref(0)
let relearnTimer: ReturnType<typeof setInterval> | null = null

// 编辑入口只对「有权限 + 有正文 + 不在解析中」的文档开放；解析中的文档保存会
// 与 worker 抢写，后端同样会拒绝。
const canEditNow = computed(() => {
  if (!props.canEdit || !props.doc) return false
  // FAQ 是问答对结构不是 Markdown 文档，走 FAQ 管理编辑（后端同样拒绝）。
  if ((detail.value?.type || props.doc.type) === 'faq') return false
  if (loading.value || loadError.value || !markdown.value) return false
  const status = detail.value?.parse_status || props.doc.parse_status
  return !isInFlightStatus(status)
})

// 从 detail.metadata 同步编辑基线版本：有编辑内容取 version，否则 0。
const syncEditBaseVersion = () => {
  const meta = parseMetadataObject(detail.value?.metadata)
  editBaseVersion.value = meta?.content ? Number(meta?.version) || 1 : 0
}

const startEdit = () => {
  if (!canEditNow.value) return
  editTitle.value = detail.value?.title || props.doc?.title || ''
  editContent.value = markdown.value
  syncEditBaseVersion()
  editing.value = true
}

const cancelEdit = () => {
  editing.value = false
  editContent.value = ''
  editTitle.value = ''
}

// 保存冲突（409）：让用户选「加载最新」还是「强制覆盖」。tdesign confirm 弹窗
// 只有双按钮，cancel 槽位复用为破坏性的「强制覆盖」；直接关闭弹窗 = 什么都不
// 做，继续在旧基线上编辑。
const showSaveConflictDialog = (error: any) => {
  const details = error?.error?.details || error?.details
  const current = typeof details?.current_version === 'number' ? details.current_version : -1
  const dialog = DialogPlugin.confirm({
    header: t('knowledgeBase.browse.editor.conflictTitle'),
    body: t('knowledgeBase.browse.editor.conflictBody', { ver: current >= 0 ? current : '?' }),
    confirmBtn: t('knowledgeBase.browse.editor.conflictLoadLatest'),
    cancelBtn: {
      content: t('knowledgeBase.browse.editor.conflictOverwrite'),
      theme: 'danger',
      variant: 'outline',
    },
    theme: 'warning',
    onConfirm: async () => {
      dialog.destroy()
      // 用最新内容替换编辑器并留在编辑模式；用户未保存的修改会被覆盖（文案已说明）。
      await loadContent()
      editTitle.value = detail.value?.title || props.doc?.title || ''
      editContent.value = markdown.value
      syncEditBaseVersion()
      MessagePlugin.info(t('knowledgeBase.browse.editor.conflictLoaded'))
    },
    onCancel: () => {
      dialog.destroy()
      // 强制覆盖：不带 base_version 重发，以本地内容为准创建新版本。
      void saveEditInner(false)
    },
  })
}

const saveEditInner = async (withCas: boolean) => {
  const doc = props.doc
  if (!doc || saving.value) return
  if (!editContent.value.trim()) return
  saving.value = true
  try {
    const title = editTitle.value.trim()
    const result: any = await updateKnowledgeContent(doc.id, {
      title: title || undefined,
      content: editContent.value,
      base_version: withCas ? editBaseVersion.value : undefined,
    })
    const data = result?.data
    if (!result?.success || !data) {
      throw new Error(result?.message || t('knowledgeBase.browse.saveFailed'))
    }
    detail.value = data
    syncEditBaseVersion()
    markdown.value = parseMetadataObject(data.metadata)?.content || editContent.value
    editing.value = false
    MessagePlugin.success(t('knowledgeBase.browse.saveSuccess'))
    emit('doc-updated')
  } catch (error: any) {
    if (withCas && error?.status === 409) {
      showSaveConflictDialog(error)
      return
    }
    console.error('[DocReader] save failed', error)
    MessagePlugin.error(error?.message || t('knowledgeBase.browse.saveFailed'))
  } finally {
    saving.value = false
  }
}

const saveEdit = () => saveEditInner(true)

// 恢复历史版本后：刷新详情与正文渲染，并通知父级刷新文档树（标题/标记可能变化）。
const onRestored = async () => {
  const doc = props.doc
  if (!doc) return
  try {
    const resp: any = await getKnowledgeDetails(doc.id)
    if (resp?.success && resp.data) detail.value = resp.data
  } catch {
    // loadContent 仍能按现有 detail 渲染
  }
  await loadContent()
  emit('doc-updated')
}

const stopRelearnPolling = () => {
  if (relearnTimer) {
    clearInterval(relearnTimer)
    relearnTimer = null
  }
  relearning.value = false
}

const relearn = async () => {
  const doc = props.doc
  if (!doc || relearning.value) return
  relearning.value = true
  try {
    const result: any = await relearnKnowledge(doc.id)
    if (!result?.success) {
      throw new Error(result?.message || t('knowledgeBase.browse.relearnFailed'))
    }
    if (result.data) detail.value = result.data
    MessagePlugin.success(t('knowledgeBase.browse.relearnStarted'))
    emit('doc-updated')
    // 轮询解析状态直到落定：index_stale 解除、树的状态图标随之刷新。
    relearnTimer = setInterval(async () => {
      try {
        const resp: any = await getKnowledgeDetails(doc.id)
        const data = resp?.data
        if (!resp?.success || !data) return
        detail.value = data
        if (!isInFlightStatus(data.parse_status)) {
          stopRelearnPolling()
          emit('doc-updated')
        }
      } catch {
        // 单次轮询失败忽略，下一周期继续
      }
    }, 2500)
  } catch (error: any) {
    console.error('[DocReader] relearn failed', error)
    MessagePlugin.error(error?.message || t('knowledgeBase.browse.relearnFailed'))
    relearning.value = false
  }
}

onBeforeUnmount(() => {
  stopRelearnPolling()
})

// 文档切换：重置视图状态并重新加载。必须放在 cancelEdit/stopRelearnPolling
// 声明之后——immediate 首次执行时 const 尚在 TDZ，提前注册会抛
// ReferenceError 并吞掉 loadContent。
watch(() => props.doc?.id, () => {
  previewOpen.value = false
  cancelEdit()
  stopRelearnPolling()
  emit('outline-change', [])
  emit('outline-active', -1)
  if (scrollRef.value) scrollRef.value.scrollTop = 0
  loadContent()
}, { immediate: true })

const downloadFile = () => {
  if (!props.doc || downloading.value) return
  downloading.value = true
  downKnowledgeDetails(props.doc.id)
    .then((result: any) => {
      if (!result) return
      const url = URL.createObjectURL(result)
      const link = document.createElement('a')
      link.style.display = 'none'
      link.setAttribute('href', url)
      link.setAttribute('download', resolveKnowledgeDownloadFileName({
        id: props.doc!.id,
        title: props.doc!.title,
        file_name: props.doc!.file_name,
        type: props.doc!.type,
      }))
      document.body.appendChild(link)
      link.click()
      nextTick(() => {
        document.body.removeChild(link)
        URL.revokeObjectURL(url)
      })
    })
    .catch(() => {
      MessagePlugin.error(t('knowledgeBase.browse.downloadFailed'))
    })
    .finally(() => {
      downloading.value = false
    })
}

// ---------- 大纲提取与渲染后处理 ----------

let headingEls: HTMLElement[] = []

const handleMermaidClick = (e: Event) => {
  e.stopPropagation()
  const svg = (e.currentTarget as HTMLElement).querySelector('svg')
  if (svg) {
    openMermaidFullscreen(svg.outerHTML)
  }
}

const runPostRender = async () => {
  await nextTick()
  const root = contentRef.value
  if (!root) return

  // 大纲：给标题元素补锚点 id 并收集层级，交给右侧 DocOutline。
  const headings = Array.from(
    root.querySelectorAll<HTMLElement>('h1, h2, h3, h4, h5, h6'),
  )
  headings.forEach((el, index) => {
    el.id = `browse-heading-${index}`
  })
  headingEls = headings
  emit('outline-change', headings.map((el) => ({
    text: el.textContent || '',
    level: Number(el.tagName.slice(1)) || 1,
  })))

  await hydrateProtectedFileImages(
    root,
    props.kbId ? { mode: 'knowledgeBase', kbId: props.kbId } : undefined,
  )

  // Mermaid：经 mermaidShared 动态加载渲染（主题/明暗变量统一由它管理），
  // 渲染后绑定点击全屏（与 doc-content.vue 行为一致）。
  const mermaidEls = await runMermaidOnDivs(root)
  mermaidEls.forEach((el) => {
    el.style.cursor = 'pointer'
    el.removeEventListener('click', handleMermaidClick)
    el.addEventListener('click', handleMermaidClick)
  })

  onScroll()
}

watch(renderedHtml, () => {
  if (markdown.value) {
    runPostRender()
  }
}, { flush: 'post' })

// 滚动高亮：取滚动位置上方最后一个标题作为当前大纲项。
const onScroll = () => {
  const scrollEl = scrollRef.value
  if (!scrollEl || !headingEls.length) return
  const offset = scrollEl.scrollTop + 96
  let active = -1
  for (let i = 0; i < headingEls.length; i++) {
    if (headingEls[i].offsetTop <= offset) {
      active = i
    } else {
      break
    }
  }
  emit('outline-active', active)
}

const scrollToHeading = (index: number) => {
  const el = headingEls[index]
  const scrollEl = scrollRef.value
  if (!el || !scrollEl) return
  scrollEl.scrollTo({
    top: Math.max(el.offsetTop - 72, 0),
    behavior: 'smooth',
  })
}

defineExpose({ scrollToHeading })
</script>

<style scoped lang="less">
@import '@/components/css/markdown.less';

.doc-reader {
  flex: 1;
  min-width: 0;
  min-height: 0;
  display: flex;
  flex-direction: column;
}

.doc-reader__placeholder {
  flex: 1;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 12px;
  color: var(--td-text-color-placeholder);
  font-size: var(--app-text-md);
}

.doc-reader__header {
  flex-shrink: 0;
  display: flex;
  align-items: center;
  gap: 12px;
  min-height: 32px;
  padding-bottom: 12px;
  border-bottom: 1px solid var(--td-component-stroke);
}

.doc-reader__title-row {
  flex: 1;
  min-width: 0;
  display: flex;
  align-items: center;
  gap: 8px;
}

.doc-reader__doc-icon {
  flex: 0 0 auto;
  color: var(--td-text-color-secondary);
}

.doc-reader__title {
  margin: 0;
  font-size: var(--app-text-2xl);
  font-weight: 600;
  color: var(--td-text-color-primary);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.doc-reader__meta {
  flex-shrink: 0;
  font-size: var(--app-text-xs);
  color: var(--td-text-color-placeholder);
  white-space: nowrap;
}

.doc-reader__actions {
  flex-shrink: 0;
  display: flex;
  align-items: center;
  gap: 4px;
}

.doc-reader__action {
  width: 28px;
  height: 28px;
  padding: 0;
  border: 0;
  border-radius: var(--app-radius-sm);
  background: transparent;
  color: var(--td-text-color-secondary);
  display: inline-flex;
  align-items: center;
  justify-content: center;
  cursor: pointer;
  transition: background-color var(--app-motion-fast) ease, color var(--app-motion-fast) ease;

  &:hover:not(:disabled) {
    color: var(--td-brand-color);
    background: var(--td-bg-color-container-hover);
  }

  &:disabled {
    cursor: not-allowed;
    opacity: 0.6;
  }
}

.doc-reader__stale {
  flex-shrink: 0;
  display: flex;
  align-items: center;
  gap: 8px;
  margin-top: 12px;
  padding: 8px 12px;
  border: 1px solid var(--td-warning-color-light);
  border-radius: var(--app-radius-sm);
  background: var(--td-warning-color-light);
  color: var(--td-warning-color);
  font-size: var(--app-text-sm);
}

.doc-reader__stale-text {
  flex: 1;
  min-width: 0;
}

.doc-reader__edit {
  flex: 1;
  min-height: 0;
  display: flex;
  flex-direction: column;
  gap: 12px;
  padding-top: 12px;
}

.doc-reader__edit-title {
  flex-shrink: 0;
  max-width: 760px;
}

.doc-reader__edit-editor {
  flex: 1;
  min-height: 0;
}

.doc-reader__edit-actions {
  flex-shrink: 0;
  display: flex;
  justify-content: flex-end;
  gap: 8px;
}

.doc-reader__scroll {
  flex: 1;
  min-height: 0;
  overflow-y: auto;
  padding: 20px 24px 48px 4px;
  scrollbar-width: thin;

  &::-webkit-scrollbar {
    width: 4px;
  }

  &::-webkit-scrollbar-thumb {
    border-radius: var(--app-radius-xs);
    background: var(--td-scrollbar-color);
  }
}

.doc-reader__state {
  max-width: 760px;
  margin: 0 auto;
  padding: 24px 0;

  &--hint {
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 12px;
    padding-top: 96px;
    color: var(--td-text-color-placeholder);
    font-size: var(--app-text-md);
    text-align: center;
  }
}

// 语雀式阅读宽度：长行居中限宽，保证可读性。
.doc-reader__article {
  max-width: 760px;
  margin: 0 auto;
}

.doc-reader__preview-body {
  height: 70vh;
}
</style>
