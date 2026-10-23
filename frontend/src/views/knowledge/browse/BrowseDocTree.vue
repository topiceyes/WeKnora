<template>
  <aside class="browse-doc-tree">
    <div class="browse-doc-tree__header">
      <span class="browse-doc-tree__title">{{ t('knowledgeBase.browse.treeTitle') }}</span>
      <span v-if="tree && !loading" class="browse-doc-tree__total">
        {{ t('knowledgeBase.folderTree.totalDocuments', { count: tree.total_document_count }) }}
      </span>
      <t-tooltip :content="t('knowledgeBase.browse.refresh')" placement="top">
        <button
          type="button"
          class="browse-doc-tree__icon-btn"
          :aria-label="t('knowledgeBase.browse.refresh')"
          @click="emit('refresh')"
        >
          <t-icon name="refresh" size="15px" />
        </button>
      </t-tooltip>
    </div>

    <div class="browse-doc-tree__body">
      <template v-if="loading && !tree">
        <div v-for="n in 8" :key="'tree-skel-' + n" class="browse-doc-tree__skeleton">
          <t-skeleton animation="gradient" :row-col="[{ width: '100%', height: '16px' }]" />
        </div>
      </template>
      <div v-else-if="!rows.length" class="browse-doc-tree__empty">
        {{ t('knowledgeBase.browse.emptyKb') }}
      </div>
      <template v-else>
        <div
          v-for="row in rows"
          :key="row.kind === 'folder' ? 'f:' + row.path : 'd:' + row.id"
          class="browse-tree-row"
          :class="{
            'is-folder': row.kind === 'folder',
            active: row.kind === 'doc' && row.id === selectedDocId,
          }"
          :style="{ '--browse-row-depth': row.depth }"
          :title="row.kind === 'folder' ? row.path : row.title"
          role="button"
          tabindex="0"
          @click="onRowClick(row)"
          @keydown.enter.self="onRowClick(row)"
          @keydown.space.prevent.self="onRowClick(row)"
        >
          <span
            v-if="row.kind === 'folder' && row.hasChildren"
            class="browse-tree-row__toggle"
            role="button"
            tabindex="0"
            :aria-expanded="isExpanded(row.path)"
            :aria-label="t(isExpanded(row.path)
              ? 'knowledgeBase.folderTree.collapseFolder'
              : 'knowledgeBase.folderTree.expandFolder')"
            @keydown.enter.stop.prevent="toggle(row.path)"
            @keydown.space.stop.prevent="toggle(row.path)"
            @click.stop="toggle(row.path)"
          >
            <t-icon :name="isExpanded(row.path) ? 'chevron-down' : 'chevron-right'" />
          </span>
          <span v-else class="browse-tree-row__toggle-placeholder" aria-hidden="true" />

          <t-icon
            :name="row.kind === 'folder'
              ? (isExpanded(row.path) ? 'folder-open' : 'folder')
              : browseDocIcon(row.doc)"
            class="browse-tree-row__icon"
            :class="{ 'is-doc': row.kind === 'doc' }"
          />
          <span class="browse-tree-row__label">{{ row.kind === 'folder' ? row.name : row.title }}</span>
          <t-icon
            v-if="row.kind === 'doc' && isDocProcessing(row.doc)"
            name="loading"
            class="browse-tree-row__status is-processing"
          />
          <t-tooltip v-else-if="row.kind === 'doc' && row.doc.parse_status === 'failed'"
            :content="t('knowledgeBase.browse.parseFailed')" placement="top">
            <t-icon name="error-circle" class="browse-tree-row__status is-failed" />
          </t-tooltip>
          <t-tooltip v-if="row.kind === 'doc' && row.doc.index_stale"
            :content="t('knowledgeBase.browse.staleDot')" placement="top">
            <span class="browse-tree-row__stale-dot" aria-hidden="true" />
          </t-tooltip>
        </div>
      </template>
    </div>
  </aside>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import type {
  KnowledgeFolderNode,
  KnowledgeTree,
  KnowledgeTreeDocument,
} from '@/api/knowledge-base'
import { folderAncestorPaths, ROOT_FOLDER_PATH } from '../folderTree'
import { browseDocIcon } from './docIcon'

type TreeRow =
  | { kind: 'folder'; path: string; name: string; depth: number; hasChildren: boolean }
  | { kind: 'doc'; id: string; title: string; depth: number; doc: KnowledgeTreeDocument }

const props = withDefaults(defineProps<{
  tree: KnowledgeTree | null
  selectedDocId?: string
  loading?: boolean
}>(), {
  selectedDocId: '',
  loading: false,
})

const emit = defineEmits<{
  select: [docId: string]
  refresh: []
}>()

const { t } = useI18n()

// Folders start expanded at the top level so the uploaded structure is visible
// without a click; ancestors of the selected document are kept expanded below.
const expanded = ref(new Set<string>([ROOT_FOLDER_PATH]))

const isExpanded = (path: string) => expanded.value.has(path)

const toggle = (path: string) => {
  const next = new Set(expanded.value)
  if (next.has(path)) next.delete(path)
  else next.add(path)
  expanded.value = next
}

const docsByFolder = computed(() => {
  const map = new Map<string, KnowledgeTreeDocument[]>()
  for (const doc of props.tree?.documents ?? []) {
    const key = doc.folder_path || ROOT_FOLDER_PATH
    if (!map.has(key)) map.set(key, [])
    map.get(key)!.push(doc)
  }
  return map
})

const isDocProcessing = (doc: KnowledgeTreeDocument) =>
  doc.parse_status === 'pending' || doc.parse_status === 'processing'

const rows = computed<TreeRow[]>(() => {
  const out: TreeRow[] = []
  const pushDocs = (path: string, depth: number) => {
    for (const doc of docsByFolder.value.get(path) ?? []) {
      out.push({ kind: 'doc', id: doc.id, title: doc.title || doc.file_name || doc.id, depth, doc })
    }
  }
  const walk = (folders: KnowledgeFolderNode[], depth: number) => {
    for (const folder of folders) {
      const childDocs = docsByFolder.value.get(folder.path)?.length ?? 0
      const hasChildren = (folder.children?.length ?? 0) > 0 || childDocs > 0
      out.push({ kind: 'folder', path: folder.path, name: folder.name, depth, hasChildren })
      if (hasChildren && expanded.value.has(folder.path)) {
        pushDocs(folder.path, depth + 1)
        walk(folder.children ?? [], depth + 1)
      }
    }
  }
  walk(props.tree?.folders ?? [], 0)
  // 根目录文档排在文件夹之后，与「管理」视图的目录浏览顺序一致。
  pushDocs(ROOT_FOLDER_PATH, 0)
  return out
})

const onRowClick = (row: TreeRow) => {
  if (row.kind === 'folder') {
    toggle(row.path)
  } else {
    emit('select', row.id)
  }
}

// Keep the selected document reachable: expand every folder above its path,
// both on first load and when the selection changes from elsewhere (deep link).
watch(
  () => [props.selectedDocId, props.tree] as const,
  () => {
    if (!props.selectedDocId || !props.tree) return
    const doc = props.tree.documents?.find((item) => item.id === props.selectedDocId)
    if (!doc?.folder_path) return
    const next = new Set(expanded.value)
    folderAncestorPaths(doc.folder_path).forEach((path) => next.add(path))
    expanded.value = next
  },
  { immediate: true },
)

// First load opens the top-level folders, mirroring KbFolderTree.
watch(
  () => props.tree,
  (tree) => {
    if (!tree?.folders?.length || expanded.value.size > 1) return
    const next = new Set(expanded.value)
    tree.folders.forEach((folder) => next.add(folder.path))
    expanded.value = next
  },
  { immediate: true },
)
</script>

<style scoped lang="less">
.browse-doc-tree {
  --browse-row-indent: 14px;
  display: flex;
  flex-direction: column;
  flex-shrink: 0;
  width: 232px;
  min-height: 0;
  padding-right: 12px;
  margin-right: 16px;
  border-right: 1px solid var(--td-component-stroke);
  box-sizing: border-box;
}

.browse-doc-tree__header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 6px;
  height: 32px;
  padding-bottom: 16px;
  flex-shrink: 0;
}

.browse-doc-tree__title {
  font-size: var(--app-text-md);
  font-weight: 500;
  color: var(--td-text-color-primary);
  white-space: nowrap;
}

.browse-doc-tree__total {
  flex: 1;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-size: var(--app-text-xs);
  color: var(--td-text-color-placeholder);
}

.browse-doc-tree__icon-btn {
  flex-shrink: 0;
  width: 24px;
  height: 24px;
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

  &:hover {
    color: var(--td-brand-color);
    background: var(--td-bg-color-container-hover);
  }
}

.browse-doc-tree__body {
  flex: 1;
  min-height: 0;
  overflow-y: auto;
  overflow-x: hidden;
  padding: 8px 0 12px;
  scrollbar-width: thin;

  &::-webkit-scrollbar {
    width: 4px;
  }

  &::-webkit-scrollbar-thumb {
    border-radius: var(--app-radius-xs);
    background: var(--td-scrollbar-color);
  }
}

.browse-doc-tree__skeleton {
  padding: 7px 8px;
}

.browse-doc-tree__empty {
  padding: 24px 8px;
  font-size: var(--app-text-md);
  color: var(--td-text-color-placeholder);
  text-align: center;
}

.browse-tree-row {
  display: flex;
  align-items: center;
  gap: 4px;
  box-sizing: border-box;
  width: 100%;
  height: 32px;
  margin-bottom: 2px;
  padding: 0 4px 0 calc(4px + var(--browse-row-depth, 0) * var(--browse-row-indent));
  border-radius: var(--app-radius-sm);
  background: transparent;
  color: var(--td-text-color-primary);
  font-size: var(--app-text-md);
  text-align: left;
  cursor: pointer;
  user-select: none;
  transition: background var(--app-motion-fast) ease, color var(--app-motion-fast) ease;

  &:hover {
    background: var(--td-bg-color-container-hover);
  }

  &:focus-visible {
    outline: none;
    outline: 2px solid var(--app-focus-border);
    outline-offset: 2px;
  }

  &.active {
    background: var(--app-selection-bg);

    .browse-tree-row__label,
    .browse-tree-row__icon {
      color: var(--td-brand-color);
    }
    .browse-tree-row__label { font-weight: 500; }
  }

  &.is-folder .browse-tree-row__label {
    font-weight: 500;
  }
}

.browse-tree-row__toggle,
.browse-tree-row__toggle-placeholder {
  flex: 0 0 auto;
  width: 16px;
  height: 16px;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  color: var(--td-text-color-placeholder);
  border-radius: var(--app-radius-xs);
}

.browse-tree-row__toggle {
  cursor: pointer;

  &:hover {
    color: var(--td-text-color-primary);
    background: var(--td-bg-color-secondarycontainer);
  }

  .t-icon {
    font-size: var(--app-text-base);
  }
}

.browse-tree-row__icon {
  flex: 0 0 auto;
  font-size: var(--app-text-xl);
  color: var(--td-text-color-secondary);

  &.is-doc {
    font-size: var(--app-text-lg);
  }
}

.browse-tree-row__label {
  flex: 1;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.browse-tree-row__status {
  flex: 0 0 auto;
  font-size: var(--app-text-base);

  &.is-processing {
    color: var(--td-brand-color);
  }

  &.is-failed {
    color: var(--td-error-color);
  }
}

.browse-tree-row__stale-dot {
  flex: 0 0 auto;
  width: 6px;
  height: 6px;
  border-radius: var(--app-radius-pill);
  background: var(--td-warning-color);
}
</style>
