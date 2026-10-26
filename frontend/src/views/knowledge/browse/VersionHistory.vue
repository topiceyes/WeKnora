<template>
  <t-drawer
    :visible="visible"
    :header="t('knowledgeBase.browse.history.title')"
    size="560px"
    placement="right"
    :footer="false"
    destroy-on-close
    @close="close"
  >
    <div class="version-history">
      <div class="version-history__mode">
        <t-radio-group v-model="viewMode" size="small" variant="outline">
          <t-radio-button value="preview">{{ t('knowledgeBase.browse.history.preview') }}</t-radio-button>
          <t-radio-button value="compare">{{ t('knowledgeBase.browse.history.compare') }}</t-radio-button>
        </t-radio-group>
        <span v-if="viewMode === 'compare' && compareSel.base != null" class="version-history__compare-label">
          v{{ compareSel.base }} → v{{ compareSel.target ?? '?' }}
        </span>
      </div>

      <div class="version-history__list">
        <div v-if="loadingList" class="version-history__state">
          <t-skeleton animation="gradient" :row-col="[1, 1, 1]" />
        </div>
        <div v-else-if="loadError" class="version-history__state version-history__state--hint">
          <p>{{ loadError }}</p>
          <t-button size="small" variant="outline" @click="loadList">
            {{ t('knowledgeBase.browse.retry') }}
          </t-button>
        </div>
        <div v-else-if="!items.length" class="version-history__state version-history__state--hint">
          <t-icon name="history" size="28px" />
          <p>{{ t('knowledgeBase.browse.history.empty') }}</p>
        </div>
        <template v-else>
          <div
            v-for="item in items"
            :key="item.version.id"
            class="version-history__item"
            :class="{
              'version-history__item--active': viewMode === 'preview' && selectedVersion === item.version.version,
              'version-history__item--comparing': viewMode === 'compare' && isComparePicked(item.version.version),
            }"
            role="button"
            tabindex="0"
            @click="viewMode === 'compare' ? toggleCompare(item) : select(item)"
            @keydown.enter="viewMode === 'compare' ? toggleCompare(item) : select(item)"
          >
            <t-checkbox
              v-if="viewMode === 'compare'"
              :checked="isComparePicked(item.version.version)"
              @click.stop
              @change="() => toggleCompare(item)"
            />
            <span class="version-history__pill">v{{ item.version.version }}</span>
            <div class="version-history__item-main">
              <div class="version-history__item-title">
                <span class="version-history__item-name">{{ item.version.title || '-' }}</span>
                <span v-if="item.isCurrent" class="version-history__current">
                  {{ t('knowledgeBase.browse.history.current') }}
                </span>
              </div>
              <div class="version-history__item-meta">
                {{ formatStringDate(item.version.created_at) }} ·
                {{ item.version.creator_name || item.version.created_by || t('knowledgeBase.browse.history.unknownUser') }}
              </div>
            </div>
            <t-button
              v-if="canRestoreVersion(item, canEdit)"
              size="small"
              variant="outline"
              theme="warning"
              :loading="restoring"
              @click.stop="confirmRestore(item)"
            >
              {{ t('knowledgeBase.browse.history.restore') }}
            </t-button>
          </div>
        </template>
      </div>

      <div class="version-history__preview">
        <!-- 对比模式：统一 diff（文本插值渲染，天然无注入面） -->
        <template v-if="viewMode === 'compare'">
          <div v-if="diffLoading" class="version-history__state">
            <t-skeleton animation="gradient" :row-col="[1, 1, 1]" />
          </div>
          <div
            v-else-if="compareSel.base == null || compareSel.target == null"
            class="version-history__state version-history__state--hint"
          >
            <p>{{ t('knowledgeBase.browse.history.compareHint') }}</p>
          </div>
          <div v-else-if="!hasDiffChanges" class="version-history__state version-history__state--hint">
            <p>{{ t('knowledgeBase.browse.history.noDifference') }}</p>
          </div>
          <div v-else class="version-history__diff">
            <div
              v-for="(line, i) in diffLines"
              :key="i"
              class="version-history__diff-line"
              :class="`version-history__diff-line--${line.kind}`"
            >
              <span class="version-history__diff-sign">
                {{ line.kind === 'added' ? '+' : line.kind === 'removed' ? '-' : '' }}
              </span>
              <span class="version-history__diff-text">{{ line.text }}</span>
            </div>
          </div>
        </template>

        <!-- 预览模式：渲染版全文 -->
        <template v-else>
          <div v-if="loadingDetail" class="version-history__state">
            <t-skeleton animation="gradient" :row-col="[1, 1, 1]" />
          </div>
          <div
            v-else-if="detailHtml"
            ref="previewRef"
            class="version-history__preview-body md-content"
            v-html="detailHtml"
          ></div>
        </template>
      </div>
    </div>
  </t-drawer>
</template>

<script setup lang="ts">
import { computed, nextTick, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { DialogPlugin, MessagePlugin } from 'tdesign-vue-next'
import {
  getKnowledgeContentVersion,
  listKnowledgeContentVersions,
  restoreKnowledgeContentVersion,
  type KnowledgeContentVersionSummary,
} from '@/api/knowledge-base'
import { processMarkdown } from '@/utils/markdownPipeline'
import { hydrateProtectedFileImages } from '@/utils/security'
import { formatStringDate } from '@/utils/index'
import {
  buildUnifiedDiff,
  canRestoreVersion,
  toVersionListItems,
  toggleCompareVersion,
  type CompareSelection,
  type DiffLine,
  type VersionListItem,
} from './versionHistoryModel'

const props = defineProps<{
  visible: boolean
  knowledgeId: string
  kbId?: string
  canEdit: boolean
}>()

const emit = defineEmits<{
  (e: 'update:visible', value: boolean): void
  (e: 'restored'): void
}>()

const { t } = useI18n()

const versions = ref<KnowledgeContentVersionSummary[]>([])
const loadingList = ref(false)
const loadError = ref('')
const selectedVersion = ref(0)
const detailHtml = ref('')
const loadingDetail = ref(false)
const restoring = ref(false)
const previewRef = ref<HTMLElement | null>(null)
// 代际令牌：快速连点版本或切文档时，丢弃过期的异步响应。
let loadGeneration = 0

// ---------- 对比模式（P4） ----------
const viewMode = ref<'preview' | 'compare'>('preview')
const compareSel = ref<CompareSelection>({ base: null, target: null })
const diffLines = ref<DiffLine[]>([])
const diffLoading = ref(false)
// 版本号 → 原文内容缓存：预览请求顺带填充，对比模式零成本复用。
const rawCache = ref<Record<number, string>>({})
let diffGeneration = 0

const items = computed(() => toVersionListItems(versions.value))

// 两版完全相同时 buildUnifiedDiff 返回全 context 行（非空数组），「内容
// 一致」提示要按「没有任何 added/removed 行」判断，不能按行数为空。
const hasDiffChanges = computed(() => diffLines.value.some((line) => line.kind !== 'context'))

function close() {
  emit('update:visible', false)
}

function isComparePicked(version: number): boolean {
  return compareSel.value.base === version || compareSel.value.target === version
}

function toggleCompare(item: VersionListItem) {
  compareSel.value = toggleCompareVersion(compareSel.value, item.version.version)
}

// 确保拿到某版本原文：缓存命中直接返回，否则补一次详情请求。
async function ensureRawContent(version: number, generation: number): Promise<string> {
  const cached = rawCache.value[version]
  if (cached != null) return cached
  const res = await getKnowledgeContentVersion(props.knowledgeId, version)
  if (generation !== diffGeneration) return ''
  const content = (res as any)?.data?.content || ''
  rawCache.value = { ...rawCache.value, [version]: content }
  return content
}

// 选齐两个版本后计算统一 diff（base 较旧 → target 较新）。
watch([viewMode, compareSel], async () => {
  if (viewMode.value !== 'compare') return
  const { base, target } = compareSel.value
  if (base == null || target == null) {
    diffLines.value = []
    return
  }
  const generation = ++diffGeneration
  diffLoading.value = true
  try {
    const [oldText, newText] = await Promise.all([
      ensureRawContent(base, generation),
      ensureRawContent(target, generation),
    ])
    if (generation !== diffGeneration) return
    diffLines.value = buildUnifiedDiff(oldText, newText)
  } catch (e: any) {
    if (generation !== diffGeneration) return
    MessagePlugin.error(e?.message || t('knowledgeBase.browse.history.loadFailed'))
  } finally {
    if (generation === diffGeneration) diffLoading.value = false
  }
})

async function loadList() {
  const generation = ++loadGeneration
  loadingList.value = true
  loadError.value = ''
  try {
    const res = await listKnowledgeContentVersions(props.knowledgeId)
    if (generation !== loadGeneration) return
    versions.value = (res as any)?.data || []
    loadingList.value = false // select() 会推进代际，先落位 loading
    // 默认选中当前版本（列表第一条，version 最大）。
    const first = items.value[0]
    if (first) {
      select(first)
    } else {
      selectedVersion.value = 0
      detailHtml.value = ''
    }
  } catch (e: any) {
    if (generation !== loadGeneration) return
    loadError.value = e?.message || t('knowledgeBase.browse.history.loadFailed')
  } finally {
    if (generation === loadGeneration) loadingList.value = false
  }
}

async function select(item: VersionListItem) {
  if (selectedVersion.value === item.version.version && detailHtml.value) return
  const generation = ++loadGeneration
  selectedVersion.value = item.version.version
  loadingDetail.value = true
  detailHtml.value = ''
  try {
    const res = await getKnowledgeContentVersion(props.knowledgeId, item.version.version)
    if (generation !== loadGeneration) return
    const content = (res as any)?.data?.content || ''
    rawCache.value = { ...rawCache.value, [item.version.version]: content }
    detailHtml.value = processMarkdown(content)
    await nextTick()
    await hydrateProtectedFileImages(
      previewRef.value,
      props.kbId ? { mode: 'knowledgeBase', kbId: props.kbId } : undefined,
    )
  } catch (e: any) {
    if (generation !== loadGeneration) return
    MessagePlugin.error(e?.message || t('knowledgeBase.browse.history.loadFailed'))
  } finally {
    if (generation === loadGeneration) loadingDetail.value = false
  }
}

function confirmRestore(item: VersionListItem) {
  const ver = item.version.version
  const dialog = DialogPlugin.confirm({
    header: t('knowledgeBase.browse.history.restore'),
    body: t('knowledgeBase.browse.history.restoreConfirm', { ver }),
    confirmBtn: t('common.confirm'),
    cancelBtn: t('common.cancel'),
    theme: 'warning',
    onConfirm: async () => {
      dialog.destroy()
      restoring.value = true
      try {
        await restoreKnowledgeContentVersion(props.knowledgeId, ver)
        MessagePlugin.success(t('knowledgeBase.browse.history.restoreSuccess', { ver }))
        emit('restored')
        close()
      } catch (e: any) {
        MessagePlugin.error(e?.message || t('knowledgeBase.browse.history.restoreFailed'))
      } finally {
        restoring.value = false
      }
    },
    onCancel: () => dialog.destroy(),
  })
}

watch(
  () => [props.visible, props.knowledgeId],
  ([visible]) => {
    if (visible) {
      selectedVersion.value = 0
      detailHtml.value = ''
      viewMode.value = 'preview'
      compareSel.value = { base: null, target: null }
      diffLines.value = []
      rawCache.value = {}
      loadList()
    }
  },
  { immediate: true },
)
</script>

<style scoped lang="less">
.version-history {
  display: flex;
  flex-direction: column;
  height: 100%;
  min-height: 0;
}

.version-history__mode {
  display: flex;
  align-items: center;
  gap: 12px;
  padding-bottom: 10px;
}

.version-history__compare-label {
  font-size: var(--app-text-xs);
  color: var(--td-text-color-secondary);
  font-family: 'SF Mono', Consolas, Menlo, monospace;
}

.version-history__list {
  flex: 0 0 auto;
  max-height: 45%;
  overflow-y: auto;
  border-bottom: 1px solid var(--td-component-border);
}

.version-history__item {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 10px 4px;
  cursor: pointer;
  border-radius: var(--app-radius-md);

  &:hover {
    background: var(--td-bg-color-container-hover);
  }

  &--active {
    background: var(--td-bg-color-container-select);
  }

  &--comparing {
    background: var(--td-brand-color-light);
  }
}

.version-history__pill {
  flex: 0 0 auto;
  padding: 1px 8px;
  border-radius: var(--app-radius-pill);
  background: var(--td-bg-color-secondarycontainer);
  color: var(--td-text-color-secondary);
  font-size: var(--app-text-xs);
}

.version-history__item-main {
  flex: 1 1 auto;
  min-width: 0;
}

.version-history__item-title {
  display: flex;
  align-items: center;
  gap: 6px;
}

.version-history__item-name {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-size: var(--app-text-sm);
  color: var(--td-text-color-primary);
}

.version-history__current {
  flex: 0 0 auto;
  padding: 0 6px;
  border-radius: var(--app-radius-pill);
  background: var(--td-success-color-light);
  color: var(--td-success-color);
  font-size: var(--app-text-xs);
}

.version-history__item-meta {
  font-size: var(--app-text-xs);
  color: var(--td-text-color-secondary);
}

.version-history__preview {
  flex: 1 1 auto;
  min-height: 0;
  overflow-y: auto;
  padding-top: 12px;
}

.version-history__preview-body {
  font-size: var(--app-text-sm);
}

.version-history__diff {
  border: 1px solid var(--td-component-border);
  border-radius: var(--app-radius-md);
  overflow: hidden;
  font-family: 'SF Mono', Consolas, Menlo, monospace;
  font-size: var(--app-text-xs);
  line-height: 1.7;
}

.version-history__diff-line {
  display: flex;
  gap: 6px;
  padding: 0 8px;

  &--added {
    background: var(--td-success-color-light);

    .version-history__diff-sign,
    .version-history__diff-text {
      color: var(--td-success-color);
    }
  }

  &--removed {
    background: var(--td-error-color-light);

    .version-history__diff-sign,
    .version-history__diff-text {
      color: var(--td-error-color);
    }
  }
}

.version-history__diff-sign {
  flex: 0 0 1em;
  text-align: center;
  user-select: none;
}

.version-history__diff-text {
  flex: 1 1 auto;
  min-width: 0;
  white-space: pre-wrap;
  word-break: break-all;
  color: var(--td-text-color-primary);
}

.version-history__state {
  padding: 24px 4px;

  &--hint {
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 8px;
    color: var(--td-text-color-secondary);
    font-size: var(--app-text-sm);
  }
}
</style>
