<template>
  <aside ref="rootRef" class="browse-doc-tree" :class="{ 'is-selecting': selectedIds.length > 0, 'has-compact-actions': compactActions }">
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
      <button v-if="manage" type="button" class="browse-tree-root" :class="{ active: selectedFolder === '' }"
        @click="emit('folder-select', '')" @dragover.prevent @drop.prevent="dropAt($event, null, 'inside')">{{ t('documentNavigation.root') }}</button>
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
            'is-selected-folder': row.kind === 'folder' && row.path === selectedFolder,
            'is-drop-target': dropTarget === rowKey(row),
            'is-checked': row.kind === 'doc' && selectedIds.includes(row.id),
          }"
          :style="{ '--browse-row-depth': row.depth }"
          :title="row.kind === 'folder' ? row.path : row.title"
          role="button"
          tabindex="0"
          :data-node-key="rowKey(row)" :data-folder-path="row.kind === 'folder' ? row.path : undefined"
          :draggable="manage && !busy && sortBy === 'manual' && !query.trim()"
          @dragstart="dragStart($event, row)" @dragend="dropTarget = ''"
          @dragover="dragOver($event, row)" @dragleave="dropTarget = ''" @drop.prevent="dropAt($event, row)"
          @click="onRowClick(row)"
          @pointerenter="row.kind === 'doc' && emit('prefetch', row.id)"
          @focus="row.kind === 'doc' && emit('prefetch', row.id)"
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
          <input v-if="manage && row.kind === 'doc'" type="checkbox" class="browse-tree-row__check" :checked="selectedIds.includes(row.id)" :disabled="busy"
            :aria-label="`${t('documentNavigation.select')} ${row.title}`" @click.stop @change="emit('toggle-selection', row.id)" />
          <t-dropdown v-if="manage" trigger="click" :options="menuOptions(row)" @click="(data: any) => emit('action', data.value, asItem(row))">
            <button type="button" class="browse-tree-row__menu" :disabled="busy" :aria-label="`${t('documentNavigation.actions')} ${row.kind === 'folder' ? row.name : row.title}`" @click.stop @keydown.stop><t-icon name="more" /></button>
          </t-dropdown>
          <t-icon
            v-if="showProcessingStatus && row.kind === 'doc' && isDocProcessing(row.doc)"
            name="loading"
            class="browse-tree-row__status is-processing"
          />
          <t-tooltip v-else-if="showProcessingStatus && row.kind === 'doc' && row.doc.parse_status === 'failed'"
            :content="t('knowledgeBase.browse.parseFailed')" placement="top">
            <t-icon name="error-circle" class="browse-tree-row__status is-failed" />
          </t-tooltip>
          <t-tooltip v-if="showProcessingStatus && row.kind === 'doc' && row.doc.index_stale"
            :content="t('knowledgeBase.browse.staleDot')" placement="top">
            <span class="browse-tree-row__stale-dot" aria-hidden="true" />
          </t-tooltip>
        </div>
      </template>
    </div>
  </aside>
</template>

<script setup lang="ts">
import { computed, nextTick, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import type {
  KnowledgeFolderNode,
  KnowledgeTree,
  KnowledgeTreeDocument,
} from '@/api/knowledge-base'
import { folderAncestorPaths, ROOT_FOLDER_PATH } from '../folderTree'
import { browseDocIcon } from './docIcon'
import { directItems, itemKey, orderItems, type NavigationItem, type NavigationSort } from '../../documents/navigationModel'

type TreeRow =
  | { kind: 'folder'; path: string; name: string; depth: number; hasChildren: boolean }
  | { kind: 'doc'; id: string; title: string; depth: number; doc: KnowledgeTreeDocument }

const props = withDefaults(defineProps<{
  tree: KnowledgeTree | null
  selectedDocId?: string
  loading?: boolean
  query?: string
  showProcessingStatus?: boolean
  manage?: boolean
  selectedFolder?: string
  selectedIds?: string[]
  sortBy?: NavigationSort
  busy?: boolean
  scope?: string
  compactActions?: boolean
}>(), {
  selectedDocId: '',
  loading: false,
  query: '',
  showProcessingStatus: true,
  manage: false, selectedFolder: '', selectedIds: () => [], sortBy: 'manual', busy: false, scope: '',
})

const emit = defineEmits<{
  select: [docId: string]
  prefetch: [docId: string]
  refresh: []
  'folder-select': [path: string]
  'toggle-selection': [id: string]
  action: [key: string, item: NavigationItem]
  drop: [source: NavigationItem, target: NavigationItem | null, position: 'inside' | 'before' | 'after']
}>()

const { t } = useI18n()
const dropTarget = ref('')
function asItem(row: TreeRow): NavigationItem { return row.kind === 'folder' ? row : { kind: 'doc', id: row.id, title: row.title, folder_path: row.doc.folder_path || '' } }
const rowKey = (row: TreeRow) => itemKey(asItem(row))
function menuOptions(row: TreeRow) {
  const keys = row.kind === 'folder' ? ['create', 'newDocument', 'rename', 'move', 'delete'] : ['move']
  if (props.sortBy === 'manual') keys.push('up', 'down')
  return keys.map(value => ({ value, content: t(`documentNavigation.${value}`) }))
}
function dragStart(event: DragEvent, row: TreeRow) {
  if (!props.manage || props.busy || props.query.trim() || !event.dataTransfer) { event.preventDefault(); return }
  event.dataTransfer.setData('application/x-weknora-navigation', JSON.stringify({ scope: props.scope, key: rowKey(row) }))
  event.dataTransfer.effectAllowed = 'move'
}
function dragOver(event: DragEvent, row: TreeRow) {
  if (!props.manage || props.busy || props.query.trim() || !event.dataTransfer?.types.includes('application/x-weknora-navigation')) return
  event.preventDefault(); event.dataTransfer.dropEffect = 'move'; dropTarget.value = rowKey(row)
}
function dropAt(event: DragEvent, row: TreeRow | null, forced?: 'inside') {
  dropTarget.value = ''
  if (!props.manage || props.busy || props.query.trim() || props.sortBy !== 'manual') return
  let key = ''
  try { const data = JSON.parse(event.dataTransfer?.getData('application/x-weknora-navigation') || '{}'); if (data.scope !== props.scope) return; key = data.key } catch { return }
  if (!key) return
  // Resolve against the current tree; never trust cross-book drag payloads.
  const all: NavigationItem[] = []
  const visit = (parent: string) => { for (const item of directItems(props.tree, parent)) { all.push(item); if (item.kind === 'folder') visit(item.path) } }
  visit('')
  const source = all.find(item => itemKey(item) === key)
  if (!source) return
  let position: 'inside' | 'before' | 'after' = forced || 'inside'
  if (row && !forced) {
    const rect = (event.currentTarget as HTMLElement).getBoundingClientRect()
    const ratio = (event.clientY - rect.top) / rect.height
    position = row.kind === 'folder' && ratio > .25 && ratio < .75 ? 'inside' : ratio < .5 ? 'before' : 'after'
  }
  emit('drop', source, row ? asItem(row) : null, position)
}

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

const isDocProcessing = (doc: KnowledgeTreeDocument) =>
  doc.parse_status === 'pending' || doc.parse_status === 'processing'

const rows = computed<TreeRow[]>(() => {
  const out: TreeRow[] = []
  const query = props.query.trim().toLocaleLowerCase()
  const matches = (doc: KnowledgeTreeDocument) => !query ||
    `${doc.title} ${doc.file_name} ${doc.folder_path}`.toLocaleLowerCase().includes(query)
  const matchingDocs = (props.tree?.documents ?? []).filter(matches)
  const orderedWalk = (parent: string, depth: number) => {
    let items = directItems(props.tree, parent)
    // Sorting is read-only; manual order is restored when switched back.
    if (props.sortBy !== 'manual') items = orderItems(items, props.tree, parent, props.sortBy)
    for (const item of items) {
      if (item.kind === 'doc') { const doc = props.tree!.documents.find(d => d.id === item.id)!; if (matches(doc)) out.push({ kind: 'doc', id: doc.id, title: item.title, depth, doc }); continue }
      if (query && !matchingDocs.some(doc => doc.folder_path === item.path || doc.folder_path.startsWith(item.path + '/')) && !item.path.toLocaleLowerCase().includes(query)) continue
      const hasChildren = directItems(props.tree, item.path).length > 0
      out.push({ ...item, depth, hasChildren })
      if (hasChildren && (query || expanded.value.has(item.path))) orderedWalk(item.path, depth + 1)
    }
  }
  orderedWalk('', 0)
  return out
})

const onRowClick = (row: TreeRow) => {
  if (row.kind === 'folder') {
    toggle(row.path)
    emit('folder-select', row.path)
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

function expandAll() { const next = new Set<string>(['']); const visit = (folders: KnowledgeFolderNode[]) => folders.forEach(f => { next.add(f.path); visit(f.children || []) }); visit(props.tree?.folders || []); expanded.value = next }
function collapseAll() { expanded.value = new Set(['']) }
function revealFolder(path: string) { const next = new Set(expanded.value); folderAncestorPaths(path).forEach(p => next.add(p)); expanded.value = next }
function locate() {
  const doc = props.tree?.documents.find(d => d.id === props.selectedDocId)
  if (doc) { const next = new Set(expanded.value); folderAncestorPaths(doc.folder_path).forEach(p => next.add(p)); expanded.value = next }
  nextTick(() => rootRef.value?.querySelector('.browse-tree-row.active')?.scrollIntoView({ block: 'nearest' }))
}
const rootRef = ref<HTMLElement | null>(null)
defineExpose({ expandAll, collapseAll, locate, revealFolder })
</script>

<style scoped lang="less">
.browse-tree-root { width: 100%; text-align: left; border: 0; background: transparent; color: var(--td-text-color-secondary); padding: 8px; cursor: pointer; }
.browse-tree-root.active, .is-selected-folder { background: var(--td-bg-color-container-hover); }
.browse-tree-row.is-drop-target { outline: 2px solid var(--td-brand-color); }
.browse-tree-row__menu { border: 0; padding: 4px; background: transparent; color: var(--td-text-color-secondary); cursor: pointer; }
.browse-tree-row__check { flex: none; accent-color: var(--td-brand-color); }
// Keep titles wide until an action is requested; keyboard and touch remain usable.
@media (hover: hover) and (pointer: fine) {
  .browse-doc-tree.has-compact-actions:not(.is-selecting) .browse-tree-row:not(:hover):not(:focus-within):not(.is-checked) {
    .browse-tree-row__check, .browse-tree-row__menu { position: absolute; opacity: 0; pointer-events: none; }
  }
}
.browse-tree-row { position: relative; }
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
