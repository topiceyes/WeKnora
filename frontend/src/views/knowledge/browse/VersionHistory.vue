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
            :class="{ 'version-history__item--active': selectedVersion === item.version.version }"
            role="button"
            tabindex="0"
            @click="select(item)"
            @keydown.enter="select(item)"
          >
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
                {{ item.version.created_by || t('knowledgeBase.browse.history.unknownUser') }}
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
        <div v-if="loadingDetail" class="version-history__state">
          <t-skeleton animation="gradient" :row-col="[1, 1, 1]" />
        </div>
        <div
          v-else-if="detailHtml"
          ref="previewRef"
          class="version-history__preview-body md-content"
          v-html="detailHtml"
        ></div>
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
import { canRestoreVersion, toVersionListItems, type VersionListItem } from './versionHistoryModel'

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

const items = computed(() => toVersionListItems(versions.value))

function close() {
  emit('update:visible', false)
}

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
