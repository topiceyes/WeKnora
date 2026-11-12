<template>
  <div class="document-workspace" :class="{ 'is-focused': focused, 'is-resizing': layout.resizing.value }" :style="layout.style.value">
    <aside class="document-workspace__sidebar">
      <div class="document-workspace__identity">
        <RouterLink class="document-workspace__brand" :to="{ name: 'documentWorkspace' }">
          <t-icon name="book-open" size="20px" />
          <strong>{{ t('documentWorkspace.title') }}</strong>
        </RouterLink>
        <span class="document-workspace__space" :title="auth.currentTenantName">{{ auth.currentTenantName }}</span>
      </div>
      <template v-if="kbId">
        <div class="document-workspace__tools">
          <div class="document-workspace__primary-tools">
            <t-button class="document-workspace__search-button" variant="outline" :disabled="loading || navigationBusy"
              :title="`${t('documentDiscovery.search')} · Ctrl / ⌘ K`" @click="discoveryOpen = true">
              <template #icon><t-icon name="search" size="16px" /></template>{{ t('documentDiscovery.search') }}
            </t-button>
            <t-button v-if="canEdit" theme="primary" variant="outline" :disabled="creating || loading || saving || navigationBusy"
              @click="newDocument">
              <template #icon><t-icon name="add" /></template>{{ t('documentWorkspace.newDocument') }}
            </t-button>
          </div>
          <div class="document-workspace__tree-tools">
            <select v-model="treeFilter" :aria-label="t('documentNavigation.filter')" :title="t('documentNavigation.filter')"><option value="all">{{ t('documentNavigation.all') }}</option><option value="favorites">{{ t('documentNavigation.favorites') }}</option><option value="recent">{{ t('documentNavigation.recent') }}</option></select>
            <select v-model="sortBy" :aria-label="t('documentNavigation.sort')" :title="t('documentNavigation.sort')"><option value="manual">{{ t('documentNavigation.manual') }}</option><option value="name">{{ t('documentNavigation.byName') }}</option><option value="updated">{{ t('documentNavigation.byUpdated') }}</option></select>
            <t-dropdown trigger="click" :options="treeTools" @click="onTreeTool">
              <button type="button" class="document-workspace__tree-menu" :disabled="loading || navigationBusy"
                :aria-label="t('documentNavigation.actions')" :title="t('documentNavigation.actions')"><t-icon name="more" size="18px" /></button>
            </t-dropdown>
          </div>
          <div v-if="selectedIds.length" class="document-workspace__tree-tools document-workspace__selection-tools">
            <span>{{ selectedIds.length }} {{ t('documentNavigation.selected') }}</span>
            <t-button size="small" :disabled="navigationBusy" @click="navigation?.open({ action: 'move', ids: selectedIds })">{{ t('documentNavigation.move') }}</t-button>
            <button type="button" @click="selectedIds = []">{{ t('knowledgeBase.browse.cancel') }}</button>
          </div>
        </div>
        <BrowseDocTree ref="treeRef" :scope="kbId" :tree="filteredTree" :selected-doc-id="selectedDoc?.id" :loading="loading" compact-actions
          :manage="canOrganize && treeFilter === 'all'" :selected-folder="currentFolder" :selected-ids="selectedIds" :sort-by="sortBy" :busy="navigationBusy"
          :show-processing-status="false" @select="selectDocument" @prefetch="prefetchDocument" @refresh="loadBook"
          @folder-select="selectFolder" @toggle-selection="toggleSelection" @action="onTreeAction" @drop="(source, target, position) => navigation?.drop(source, target, position)" />
      </template>
      <nav v-else class="document-workspace__navigation" :aria-label="t('documentWorkspace.title')">
        <button v-for="scope in scopes" :key="scope.key" type="button"
          :class="{ active: homeScope === scope.key }" @click="homeScope = scope.key">
          <t-icon :name="scope.icon" />{{ scope.label }}
        </button>
      </nav>
      <footer class="document-workspace__footer">
        <RouterLink v-if="kbId" :to="{ name: 'documentWorkspace' }">
          <t-icon name="chevron-left" />{{ t('documentWorkspace.allBooks') }}
        </RouterLink>
        <RouterLink :to="managementLocation">
          <t-icon name="setting" />{{ t('documentWorkspace.management') }}
        </RouterLink>
      </footer>
    </aside>
    <div v-if="!focused" class="document-workspace__resize" role="separator" tabindex="0" aria-orientation="vertical"
      :aria-label="t('documentWorkspace.resizeNavigation')" :title="t('documentWorkspace.resizeNavigation')"
      :aria-valuenow="layout.sidebarWidth.value" :aria-valuemin="208" :aria-valuemax="layout.maxWidth.value"
      @pointerdown="layout.start" @pointermove="layout.move" @pointerup="layout.finish($event)"
      @pointercancel="layout.finish($event, true)" @lostpointercapture="layout.finish($event, true)"
      @keydown="layout.keydown" @dblclick="layout.reset" />

    <main class="document-workspace__main">
      <header class="document-workspace__topbar">
        <div class="document-workspace__breadcrumb">
          <button v-if="kbId" type="button" class="document-workspace__icon-button"
            :aria-label="t(focused ? 'documentWorkspace.showNavigation' : 'documentWorkspace.focus')"
            :title="t(focused ? 'documentWorkspace.showNavigation' : 'documentWorkspace.focus')"
            @click="focused = !focused">
            <t-icon name="view-column" size="18px" />
          </button>
          <RouterLink :to="{ name: 'documentWorkspace' }">{{ t('documentWorkspace.allBooks') }}</RouterLink>
          <template v-if="kbId"><span>/</span><button type="button" @click="selectFolder('')">{{ book?.name || t('documentWorkspace.pickBook') }}</button></template>
          <template v-for="crumb in folderCrumbs" :key="crumb.path"><span>/</span><button type="button" @click="selectFolder(crumb.path)">{{ crumb.name }}</button></template>
          <button v-if="selectedDoc && !creating" type="button" :aria-label="t('documentNavigation.favorite')" :title="t('documentNavigation.localPreferences')" @click="visits.toggle(selectedDoc.id)"><t-icon :name="visits.favorites.value.includes(selectedDoc.id) ? 'star-filled' : 'star'" /></button>
        </div>
        <div ref="readerToolbar" class="document-workspace__reader-toolbar" />
        <span v-if="book && !canEdit" class="document-workspace__readonly">{{ t('documentWorkspace.readonly') }}</span>
        <RouterLink class="document-workspace__platform" to="/platform/knowledge-bases">
          {{ t('documentWorkspace.platform') }}<t-icon name="jump" size="14px" />
        </RouterLink>
      </header>

      <section v-if="!kbId" class="document-workspace__home">
        <div class="document-workspace__home-heading">
          <div><h1>{{ t('documentWorkspace.homeTitle') }}</h1><p>{{ t('documentWorkspace.homeDescription') }}</p></div>
          <t-input v-model="bookQuery" clearable :placeholder="t('documentWorkspace.searchBooks')">
            <template #prefix-icon><t-icon name="search" /></template>
          </t-input>
        </div>
        <div v-if="listLoading" class="document-workspace__state"><t-loading /></div>
        <div v-else-if="listError && !books.length" class="document-workspace__state" role="alert">
          <p>{{ listError }}</p><t-button variant="outline" @click="loadBooks">{{ t('knowledgeBase.browse.retry') }}</t-button>
        </div>
        <div v-else-if="!filteredBooks.length" class="document-workspace__state">
          <t-icon name="book-open" size="40px" /><p>{{ t('documentWorkspace.emptyBooks') }}</p>
          <RouterLink to="/platform/knowledge-bases">{{ t('documentWorkspace.management') }}</RouterLink>
        </div>
        <div v-else>
          <p v-if="listError" role="alert">{{ listError }} <button type="button" @click="loadBooks">{{ t('knowledgeBase.browse.retry') }}</button></p>
          <div class="document-workspace__books">
            <RouterLink v-for="item in filteredBooks" :key="item.id" class="document-book"
              :to="{ name: 'documentWorkspace', params: { kbId: item.id } }">
              <div class="document-book__cover"><t-icon name="book-open" size="24px" /></div>
              <h2>{{ item.name }}</h2>
              <p>{{ item.description || t('documentWorkspace.noDescription') }}</p>
              <div class="document-book__meta">
                <span>{{ t('documentWorkspace.documentCount', { count: item.knowledge_count || 0 }) }}</span>
                <span>{{ item.org_name || auth.currentTenantName }}</span>
              </div>
            </RouterLink>
          </div>
        </div>
      </section>

      <div v-else-if="loading && !book" class="document-workspace__state"><t-loading /></div>
      <div v-else-if="bookError" class="document-workspace__state" role="alert">
        <p>{{ bookError }}</p><t-button variant="outline" @click="loadBook">{{ t('knowledgeBase.browse.retry') }}</t-button>
      </div>
      <section v-else-if="creating && canEdit" class="document-workspace__document" @keydown="onSaveKeydown">
        <div class="document-workspace__compose">
          <DraftRecovery :drafts="newDraft.candidates.value" :disabled="saving || editorBusy"
            @restore="restoreNewDraft" @discard="newDraft.discard" />
          <div class="document-workspace__compose-heading">
            <strong>{{ t('documentWorkspace.newDocument') }}</strong>
            <div><t-button variant="outline" :disabled="saving || editorBusy" @click="cancelNew">{{ t('knowledgeBase.browse.cancel') }}</t-button>
              <t-button theme="primary" :loading="saving" :disabled="editorBusy || editorInvalid || !newTitle.trim() || !newContent.trim()"
                @click="saveNew">{{ t('knowledgeBase.browse.save') }}</t-button></div>
          </div>
          <t-input v-model="newTitle" :maxlength="200" :disabled="saving" :placeholder="t('documentWorkspace.titlePlaceholder')"
            :aria-label="t('documentWorkspace.titlePlaceholder')" />
          <p class="document-workspace__draft-status" role="status" aria-live="polite">{{ newDraftStatus }}</p>
          <t-select v-model="newFolder" :options="newFolderOptions" :disabled="saving" :aria-label="t('documentNavigation.destination')" />
          <RichEditor ref="newEditorRef" v-model="newContent" :document="newDocumentBody" :kb-id="kbId" :disabled="saving || editorInvalid"
            :placeholder="t('documentWorkspace.contentPlaceholder')" @busy-change="editorBusy = $event"
            @content-change="onNewContent" @content-error="editorInvalid = true"
            @outline-change="newOutlineItems = $event" @outline-active="newOutlineActive = $event" />
        </div>
        <DocOutline :collapsed="newOutlineCollapsed" :items="newOutlineItems" :active-index="newOutlineActive"
          @update:collapsed="layout.setOutline" @jump="newEditorRef?.scrollToHeading($event)" @move-section="newEditorRef?.moveSection($event)" />
      </section>
      <section v-else class="document-workspace__document">
        <template v-if="folderView">
          <div class="document-workspace__folder-view">
            <h1>{{ currentFolder || book?.name }}</h1>
            <p v-if="!folderItems.length">{{ t('documentNavigation.emptyFolder') }}</p>
            <button v-for="item in folderItems" :key="itemKey(item)" type="button" @click="item.kind === 'folder' ? selectFolder(item.path) : selectDocument(item.id)"><t-icon :name="item.kind === 'folder' ? 'folder' : 'file'" />{{ item.kind === 'folder' ? item.name : item.title }}</button>
            <t-button v-if="canEdit" theme="primary" @click="newDocument">{{ t('documentWorkspace.newDocument') }}</t-button>
          </div>
        </template>
        <template v-else-if="selectedDoc">
          <DocReader ref="reader" :doc="selectedDoc" :kb-id="kbId" :can-edit="canEdit" workspace :toolbar-target="readerToolbar"
            :can-download="canDownload" @doc-updated="refreshTree"
            @open-linked-document="openLinkedDocument" @outline-change="outlineItems = $event" @outline-active="outlineActive = $event" />
          <DocOutline :collapsed="outlineCollapsed" :items="outlineItems" :active-index="outlineActive" @update:collapsed="layout.setOutline"
            @jump="reader?.scrollToHeading($event)" @move-section="reader?.moveSection($event)" />
        </template>
        <div v-else class="document-workspace__state">
          <t-icon name="file-paste" size="40px" />
          <p>{{ t(route.query.doc ? 'documentWorkspace.missingDocument' : 'knowledgeBase.browse.emptyKb') }}</p>
          <t-button v-if="canEdit" theme="primary" @click="newDocument">{{ t('documentWorkspace.newDocument') }}</t-button>
        </div>
      </section>
    </main>
    <DocumentSearch v-model:visible="discoveryOpen" :kb-id="kbId" @select="selectDocument($event.id)" />
    <DocumentNavigationActions ref="navigation" :kb-id="kbId" :tree="tree" :can-edit="canOrganize" :before-change="beforeNavigationChange" :refresh="refreshTree" @busy="navigationBusy = $event" @changed="onNavigationChanged" />
  </div>
</template>

<script setup lang="ts">
import { computed, defineAsyncComponent, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { onBeforeRouteLeave, onBeforeRouteUpdate, useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { MessagePlugin } from 'tdesign-vue-next'
import { useAuthStore } from '@/stores/auth'
import { useOrganizationStore } from '@/stores/organization'
import { listKnowledgeBases, getKnowledgeBaseById, getKnowledgeTree, createManualKnowledge,
  type KnowledgeTree } from '@/api/knowledge-base'
import BrowseDocTree from '../knowledge/browse/BrowseDocTree.vue'
import DocReader from '../knowledge/browse/DocReader.vue'
import DocOutline from '../knowledge/browse/DocOutline.vue'
import type { OutlineItem } from '../knowledge/browse/types'
import { mergeAllScopeKnowledgeBases } from '../knowledge/kbListMerge'
import { canEditDocumentBook, type DocumentBook } from './workspaceModel'
import { confirmDiscardChanges } from './unsavedChanges'
import DraftRecovery from './DraftRecovery.vue'
import { useDocumentDraft } from './useDocumentDraft'
import type { StoredDraft } from './documentDrafts'
import { readStructuredDocument, type StructuredDocument } from '../knowledge/browse/structuredDocument'
import DocumentNavigationActions from './DocumentNavigationActions.vue'
import { directItems, folderOptions, itemKey, type NavigationItem, type NavigationSort } from './navigationModel'
import { folderBreadcrumbs } from '../knowledge/folderTree'
import { useDocumentVisits } from './useDocumentVisits'
import { useWorkspaceLayout } from './useWorkspaceLayout'

const DocumentSearch = defineAsyncComponent(() => import('./DocumentSearch.vue'))
const RichEditor = defineAsyncComponent(() => import('../knowledge/browse/RichEditor.vue'))
const { t } = useI18n()
const auth = useAuthStore()
const organizations = useOrganizationStore()
const route = useRoute()
const router = useRouter()
const kbId = computed(() => String(route.params.kbId || ''))
const book = ref<DocumentBook | null>(null)
const books = ref<DocumentBook[]>([])
const tree = ref<KnowledgeTree | null>(null)
const documentSchemaVersion = ref(0)
const loading = ref(false)
const listLoading = ref(false)
const bookError = ref('')
const listError = ref('')
const discoveryOpen = ref(false)
const bookQuery = ref('')
const homeScope = ref('all')
const focused = ref(false)
const creating = computed(() => route.query.new === '1')
const folderView = computed(() => typeof route.query.folder === 'string' && !route.query.doc && !creating.value)
const navigation = ref<InstanceType<typeof DocumentNavigationActions> | null>(null)
const treeRef = ref<InstanceType<typeof BrowseDocTree> | null>(null)
const navigationBusy = ref(false), selectedIds = ref<string[]>([])
const sortBy = ref<NavigationSort>('manual'), treeFilter = ref('all'), newFolder = ref('')
const visits = useDocumentVisits(() => auth.user?.id && auth.effectiveTenantId && kbId.value ? { user: auth.user.id, tenant: auth.effectiveTenantId, kb: kbId.value } : null)
const filteredTree = computed(() => !tree.value || treeFilter.value === 'all' ? tree.value : { ...tree.value, documents: tree.value.documents.filter(d => (treeFilter.value === 'favorites' ? visits.favorites.value : visits.recent.value).includes(d.id)) })
const newTitle = ref('')
const newContent = ref('')
const newDocumentBody = ref<StructuredDocument | null>(null)
const newEditorRef = ref<InstanceType<typeof RichEditor> | null>(null)
const editorInvalid = ref(false)
function onNewContent(value: { content: string; document: StructuredDocument }) {
  newContent.value = value.content
  newDocumentBody.value = value.document
}
const saving = ref(false)
const editorBusy = ref(false)
const reader = ref<InstanceType<typeof DocReader> | null>(null)
const outlineItems = ref<OutlineItem[]>([])
const outlineActive = ref(-1)
const newOutlineItems = ref<OutlineItem[]>([])
const newOutlineActive = ref(-1)
const readerToolbar = ref<HTMLElement | null>(null)
const layout = useWorkspaceLayout(() => auth.user?.id && auth.effectiveTenantId && kbId.value
  ? { user: auth.user.id, tenant: auth.effectiveTenantId, kb: kbId.value } : null)
const outlineCollapsed = computed(() => layout.outline.value ?? outlineItems.value.length <= 2)
const newOutlineCollapsed = computed(() => layout.outline.value ?? newOutlineItems.value.length <= 2)
const scopes = computed(() => [
  { key: 'all', icon: 'book-open', label: t('documentWorkspace.allBooks') },
  { key: 'mine', icon: 'user', label: t('documentWorkspace.myBooks') },
  { key: 'shared', icon: 'usergroup', label: t('documentWorkspace.sharedBooks') },
])
const filteredBooks = computed(() => books.value.filter(item => {
  if (homeScope.value === 'mine' && item.creator_id !== auth.user?.id) return false
  if (homeScope.value === 'shared' && item.isMine !== false) return false
  return `${item.name || ''} ${item.description || ''}`.toLocaleLowerCase().includes(bookQuery.value.trim().toLocaleLowerCase())
}))
const canEdit = computed(() => canEditDocumentBook(book.value, {
  id: auth.user?.id, tenantId: auth.effectiveTenantId,
  contributor: auth.hasRole('contributor'), admin: auth.hasRole('admin'),
}, organizations.getKBPermission(kbId.value)))
const canDownload = computed(() => auth.hasRole('contributor') && canEdit.value)
const canOrganize = computed(() => canEdit.value && tree.value?.navigation_revision != null)
const treeTools = computed(() => (canOrganize.value ? ['create', 'locate', 'expand', 'collapse'] : ['locate', 'expand', 'collapse'])
  .map(value => ({ value, content: t(`documentNavigation.${value}`) })))
function onTreeTool(option: { value?: string | number }) {
  if (loading.value || navigationBusy.value) return
  if (option.value === 'create' && canOrganize.value) navigation.value?.open({ action: 'create', parent: currentFolder.value })
  else if (option.value === 'locate') treeRef.value?.locate()
  else if (option.value === 'expand') treeRef.value?.expandAll()
  else if (option.value === 'collapse') treeRef.value?.collapseAll()
}
const selectedDoc = computed(() => {
  const docs = tree.value?.documents ?? []
  if (folderView.value) return null
  return typeof route.query.doc === 'string' ? docs.find(doc => doc.id === route.query.doc) ?? null : docs[0] ?? null
})
const currentFolder = computed(() => creating.value ? newFolder.value : selectedDoc.value?.folder_path || (typeof route.query.folder === 'string' ? route.query.folder : ''))
const folderCrumbs = computed(() => folderBreadcrumbs(currentFolder.value))
const folderItems = computed(() => directItems(tree.value, currentFolder.value))
const newFolderOptions = computed(() => [{ label: t('documentNavigation.root'), value: '' }, ...folderOptions(tree.value)])
const managementLocation = computed(() => kbId.value
  ? { name: 'knowledgeBaseDetail', params: { kbId: kbId.value } }
  : { name: 'knowledgeBaseList' })
const newDirty = computed(() => creating.value && !!(newTitle.value || newContent.value))
const newDraft = useDocumentDraft({
  scope: () => auth.user?.id && auth.effectiveTenantId && kbId.value ? {
    userId: auth.user.id, tenantId: String(auth.effectiveTenantId), kbId: kbId.value, documentId: 'new',
  } : null,
  value: () => ({ title: newTitle.value, content: newContent.value, baseVersion: 0, document: newDocumentBody.value, folderPath: newFolder.value }),
  active: () => creating.value && canEdit.value,
  dirty: () => newDirty.value,
})
const newDraftStatus = computed(() => t(newDraft.storageError.value ? 'documentSaving.localFailed'
  : newDraft.savedAt.value ? 'documentSaving.newDraftSaved' : 'documentSaving.newDraftHint'))
async function restoreNewDraft(draft: StoredDraft) {
  if (saving.value || editorBusy.value || !canEdit.value) return
  if (newDirty.value && !await confirmDiscardChanges(t)) return
  newTitle.value = draft.title
  newContent.value = draft.content
  newDocumentBody.value = draft.document || null
  newFolder.value = draft.folderPath || ''
  newDraft.adopt(draft)
  newDraft.flush()
}
let listGeneration = 0
let bookGeneration = 0
let treeGeneration = 0

async function loadBooks() {
  const generation = ++listGeneration
  listLoading.value = true
  listError.value = ''
  const results = await Promise.allSettled([listKnowledgeBases(), organizations.fetchSharedKnowledgeBases()])
  if (generation !== listGeneration) return
  const own = results[0]
  const shared = results[1]
  const data = own.status === 'fulfilled' ? (own.value as any)?.data : null
  books.value = mergeAllScopeKnowledgeBases(Array.isArray(data) ? data : [],
    shared.status === 'fulfilled' ? shared.value : [], auth.user?.id).filter(item => item.type !== 'faq') as DocumentBook[]
  if (own.status === 'rejected' || !Array.isArray(data) || shared.status === 'rejected' || organizations.error) {
    listError.value = t('documentWorkspace.loadBooksFailed')
  }
  listLoading.value = false
}

async function loadBook() {
  const generation = ++bookGeneration
  const target = kbId.value
  if (!target) return
  ++treeGeneration
  loading.value = true
  bookError.value = ''
  try {
    const [info, contents]: any[] = await Promise.all([getKnowledgeBaseById(target), getKnowledgeTree(target)])
    if (generation !== bookGeneration) return
    if (!info?.success || !info.data || !contents?.success || !contents.data) throw new Error()
    if (info.data.type === 'faq') {
      bookError.value = t('documentWorkspace.faqHint')
      return
    }
    book.value = info.data
    documentSchemaVersion.value = contents.document_schema_version || 0
    tree.value = contents.data
  } catch {
    if (generation === bookGeneration) bookError.value = t('documentWorkspace.loadBookFailed')
  } finally {
    if (generation === bookGeneration) loading.value = false
  }
}

async function refreshTree() {
  const generation = ++treeGeneration
  const target = kbId.value
  try {
    const result: any = await getKnowledgeTree(target)
    if (generation !== treeGeneration || target !== kbId.value) return
    if (!result?.success || !result.data) throw new Error()
    tree.value = result.data
  } catch {
    if (generation === treeGeneration) MessagePlugin.error(t('documentWorkspace.loadBookFailed'))
  }
}

function openLinkedDocument(id: string) {
  if (!navigationBusy.value) void router.push({ name: 'documentLink', params: { documentId: id } })
}
function selectDocument(id: string) {
  if (navigationBusy.value) return
  void router.push({ name: 'documentWorkspace', params: { kbId: kbId.value }, query: { doc: id } })
}
function prefetchDocument(id: string) {
  const doc = tree.value?.documents.find(doc => doc.id === id)
  if (doc && !navigationBusy.value && !creating.value) reader.value?.prefetchDocument(doc)
}
function selectFolder(path: string) {
  if (navigationBusy.value) return
  void router.push({ name: 'documentWorkspace', params: { kbId: kbId.value }, query: { folder: path } })
}
function toggleSelection(id: string) {
  if (!canOrganize.value || navigationBusy.value) return
  selectedIds.value = selectedIds.value.includes(id) ? selectedIds.value.filter(v => v !== id) : selectedIds.value.length < 200 ? [...selectedIds.value, id] : selectedIds.value
}
function onTreeAction(action: string, item: NavigationItem) {
  if (action === 'newDocument' && item.kind === 'folder') newDocument(item.path)
  else void navigation.value?.action(action, item)
}
async function beforeNavigationChange() {
  if (!await requestLeave()) return false
  if (creating.value) { newTitle.value = ''; newContent.value = ''; newDocumentBody.value = null; newDraft.clear() }
  return true
}
function onNavigationChanged(change: { action: string; from?: string; to?: string }) {
  selectedIds.value = []
  if (change.to) treeRef.value?.revealFolder(change.to)
  if (folderView.value && change.from && currentFolder.value && (currentFolder.value === change.from || currentFolder.value.startsWith(change.from + '/'))) {
    const path = change.action === 'delete_folder' ? '' : (change.to || '') + currentFolder.value.slice(change.from.length)
    selectFolder(path)
  }
}
function newDocument(folder?: unknown) {
  if (navigationBusy.value) return
  const target = typeof folder === 'string' ? folder : currentFolder.value
  void router.push({ name: 'documentWorkspace', params: { kbId: kbId.value }, query: { new: '1', ...(target ? { folder: target } : {}) } })
}
function cancelNew() {
  void router.push({ name: 'documentWorkspace', params: { kbId: kbId.value },
    query: selectedDoc.value ? { doc: selectedDoc.value.id } : {} })
}
async function saveNew() {
  if (saving.value || editorBusy.value || editorInvalid.value || !canEdit.value || !newTitle.value.trim() || !newContent.value.trim()) return
  const snapshot = newEditorRef.value?.snapshot()
  if (snapshot) onNewContent(snapshot)
  if (newDocumentBody.value && documentSchemaVersion.value !== 1) {
    newDraft.flush()
    MessagePlugin.error(t('richWriting.serverUpgradeRequired'))
    return
  }
  const title = newTitle.value.trim()
  const content = newContent.value
  newDraft.flush()
  saving.value = true
  try {
    const result: any = await createManualKnowledge(kbId.value, { title, content, status: 'draft',
      ...(newFolder.value ? { folder_path: newFolder.value } : {}),
      ...(newDocumentBody.value ? { document: newDocumentBody.value } : {}) })
    if (!result?.success || !result.data?.id) throw new Error()
    const metadata = typeof result.data.metadata === 'string' ? JSON.parse(result.data.metadata) : result.data.metadata
    if (newDocumentBody.value && !readStructuredDocument(metadata?.document)) throw new Error(t('richWriting.serverUpgradeRequired'))
    const doc = { ...result.data, title, folder_path: result.data.folder_path || '' }
    if (tree.value) tree.value = { ...tree.value, documents: [...tree.value.documents, doc],
      total_document_count: tree.value.total_document_count + 1 }
    newTitle.value = ''
    newContent.value = ''
    newDocumentBody.value = null
    newDraft.clear()
    // Unlock the navigation guard only after persistence succeeded.
    saving.value = false
    await router.replace({ name: 'documentWorkspace', params: { kbId: kbId.value }, query: { doc: doc.id } })
    MessagePlugin.success(t('knowledgeBase.browse.saveSuccess'))
    void refreshTree()
  } catch {
    MessagePlugin.error(t('knowledgeBase.browse.saveFailed'))
  } finally {
    saving.value = false
  }
}
function onSaveKeydown(event: KeyboardEvent) {
  if ((event.ctrlKey || event.metaKey) && event.key.toLowerCase() === 's') {
    event.preventDefault()
    void saveNew()
  }
}
async function requestLeave() {
  if (saving.value || editorBusy.value) return false
  if (newDirty.value && !await confirmDiscardChanges(t)) return false
  const leave = await reader.value?.requestLeave() ?? true
  if (leave && creating.value) newDraft.clear()
  return leave
}
onBeforeRouteLeave(() => !navigationBusy.value && requestLeave())
onBeforeRouteUpdate((to, from) => {
  if (navigationBusy.value) return false
  if (to.params.kbId === from.params.kbId && to.query.doc === from.query.doc && to.query.new === from.query.new && to.query.folder === from.query.folder) return true
  return requestLeave()
})
function beforeUnload(event: BeforeUnloadEvent) {
  newDraft.flush()
  if (!newDirty.value && !saving.value && !editorBusy.value) return
  event.preventDefault()
  event.returnValue = ''
}
watch(kbId, () => {
  ++bookGeneration
  ++treeGeneration
  book.value = null
  tree.value = null
  bookError.value = ''
  loading.value = false
  focused.value = false
  discoveryOpen.value = false
  selectedIds.value = []
  treeFilter.value = 'all'
  outlineItems.value = []
  outlineActive.value = -1
  if (kbId.value) void loadBook()
}, { immediate: true })
watch([kbId, creating], () => {
  newOutlineItems.value = []
  newOutlineActive.value = -1
  newTitle.value = ''
  newContent.value = ''
  newDocumentBody.value = null
  editorInvalid.value = false
  newFolder.value = typeof route.query.folder === 'string' ? route.query.folder : ''
}, { immediate: true })
watch(() => selectedDoc.value?.id, id => { if (id && !creating.value) visits.visit(id) })
function onWorkspaceKeydown(event: KeyboardEvent) {
  if (!(event.ctrlKey || event.metaKey)) return
  if (event.key.toLowerCase() === 'k' && kbId.value) {
    event.preventDefault(); discoveryOpen.value = true
  } else if (event.altKey && event.key.toLowerCase() === 'n' && canEdit.value && !creating.value) {
    event.preventDefault(); newDocument()
  }
}
onMounted(() => {
  void loadBooks()
  window.addEventListener('beforeunload', beforeUnload)
  window.addEventListener('keydown', onWorkspaceKeydown)
})
onBeforeUnmount(() => {
  newDraft.flush()
  ++listGeneration
  ++bookGeneration
  ++treeGeneration
  window.removeEventListener('beforeunload', beforeUnload)
  window.removeEventListener('keydown', onWorkspaceKeydown)
})
</script>

<style scoped lang="less">
.document-workspace__primary-tools { display: flex; gap: 6px;
  > :deep(.t-button) { flex: 1; min-width: 0; padding: 0 8px; }
  :deep(.t-button__text) { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
}
.document-workspace__tree-tools { display: flex; gap: 4px; align-items: center; min-width: 0;
  button, select { color: var(--td-text-color-secondary); background: transparent; border: 0; border-radius: var(--app-radius-sm); padding: 4px; cursor: pointer; min-width: 0; height: 28px;
    &:hover { background: var(--td-bg-color-container-hover); }
    &:focus-visible { outline: 2px solid var(--app-focus-border); outline-offset: 1px; }
  }
  select { flex: 1; width: 0; font: inherit; font-size: var(--app-text-sm); text-overflow: ellipsis; }
  .document-workspace__tree-menu { display: grid; place-items: center; width: 28px; flex-shrink: 0; }
}
.document-workspace__selection-tools { flex-wrap: wrap; font-size: var(--app-text-sm);
  > span { margin-right: auto; }
}
.document-workspace__breadcrumb button { color: inherit; border: 0; background: transparent; cursor: pointer; white-space: nowrap; }
.document-workspace__folder-view { padding: 32px; display: flex; flex-direction: column; align-items: flex-start; gap: 16px; overflow: auto; width: 100%;
  > button { display: flex; align-items: center; gap: 12px; padding: 12px; border: 0; background: var(--td-bg-color-page); color: inherit; cursor: pointer; width: 100%; text-align: left; }
}
.document-workspace {
  display: flex; width: 100%; height: 100%; min-height: 0; overflow: hidden;
  background: var(--td-bg-color-container); color: var(--td-text-color-primary);
  a { color: inherit; text-decoration: none; }
  button { font: inherit; }
}
.document-workspace__sidebar {
  width: var(--workspace-sidebar-width, 272px); flex: 0 0 var(--workspace-sidebar-width, 272px); display: flex; flex-direction: column; min-height: 0;
  background: var(--td-bg-color-page); border-right: 1px solid var(--td-component-stroke);
}
.document-workspace__identity { display: flex; align-items: center; gap: 8px; padding: 12px; min-height: 48px; box-sizing: border-box; flex-shrink: 0; }
.document-workspace__brand { display: flex; align-items: center; gap: 6px; min-width: 0; font-size: var(--app-text-lg);
  .t-icon { color: var(--td-brand-color); flex-shrink: 0; }
  strong { white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
}
.document-workspace__space { flex: 1; min-width: 0; max-width: 45%; text-align: right; margin-left: auto; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; color: var(--td-text-color-placeholder); font-size: var(--app-text-xs); }
.document-workspace__tools { display: flex; flex-direction: column; gap: 6px; padding: 0 12px 4px; flex-shrink: 0; }
.document-workspace__sidebar :deep(.browse-doc-tree) {
  flex: 1; width: 100%; min-height: 0; margin: 0; padding: 0 8px; border: 0;
}
.document-workspace__sidebar :deep(.browse-doc-tree__header) { height: 28px; padding: 0 4px; }
.document-workspace__sidebar :deep(.browse-doc-tree__body) { padding-top: 4px; }
.document-workspace__sidebar :deep(.browse-tree-row) { height: 30px; margin-bottom: 0; }
.document-workspace__navigation { display: grid; gap: 4px; padding: 0 12px;
  button { display: flex; align-items: center; gap: 10px; text-align: left; cursor: pointer;
    border: 0; border-radius: var(--app-radius-sm); padding: 12px; background: transparent; color: var(--td-text-color-secondary);
    &:hover { background: var(--td-bg-color-container-hover); }
    &.active { background: var(--td-brand-color-light); color: var(--td-brand-color); }
  }
}
.document-workspace__footer { margin-top: auto; padding: 6px 12px; display: flex; flex-wrap: wrap; align-items: center; justify-content: space-between; gap: 4px 8px; flex-shrink: 0;
  border-top: 1px solid var(--td-component-stroke); font-size: var(--app-text-sm); color: var(--td-text-color-secondary);
  a { display: flex; align-items: center; gap: 4px; min-height: 28px; &:hover { color: var(--td-brand-color); } }
}
.document-workspace__main { flex: 1; min-width: 0; min-height: 0; display: flex; flex-direction: column; }
.document-workspace__resize { width: 6px; margin: 0 -3px; flex-shrink: 0; position: relative; z-index: 1; cursor: col-resize; touch-action: none;
  &:hover, &:focus-visible { background: var(--td-brand-color-light); outline: 1px solid var(--app-focus-border); }
}
.document-workspace.is-resizing { cursor: col-resize; user-select: none; }
.document-workspace__reader-toolbar { display: flex; align-items: center; flex-shrink: 0; }
.document-workspace__topbar { display: flex; align-items: center; gap: 12px; min-height: 56px; padding: 0 24px;
  border-bottom: 1px solid var(--td-component-stroke); font-size: var(--app-text-md); flex-shrink: 0;
}
.document-workspace__breadcrumb { display: flex; align-items: center; gap: 8px; flex: 1; min-width: 0; overflow: hidden;
  a, span { color: var(--td-text-color-secondary); }
  button:not(.document-workspace__icon-button), a { min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .document-workspace__icon-button { flex-shrink: 0; }
  strong { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; font-weight: 500; }
}
.document-workspace__icon-button { border: 0; background: transparent; color: var(--td-text-color-secondary);
  cursor: pointer; width: 32px; height: 32px; display: grid; place-items: center; border-radius: var(--app-radius-xs);
  &:hover { background: var(--td-bg-color-container-hover); }
}
.document-workspace__platform { display: flex; align-items: center; gap: 6px; color: var(--td-text-color-secondary) !important; }
.document-workspace__readonly { color: var(--td-text-color-placeholder); font-size: var(--app-text-sm); }
.document-workspace__home { flex: 1; min-height: 0; overflow: auto; padding: 48px clamp(24px, 4vw, 72px); }
.document-workspace__home-heading { display: flex; justify-content: space-between; align-items: center; gap: 24px; margin-bottom: 32px;
  h1 { font-size: var(--app-text-4xl); margin: 0 0 12px; font-weight: 600; }
  p { color: var(--td-text-color-secondary); margin: 0; }
  .t-input__wrap { width: 260px; flex-shrink: 0; }
}
.document-workspace__books { display: grid; grid-template-columns: repeat(auto-fill, minmax(240px, 1fr)); gap: 20px; }
.document-book { display: flex; flex-direction: column; padding: 24px; min-height: 200px; border: 1px solid var(--td-component-stroke);
  border-radius: var(--app-radius-lg); transition: border-color var(--app-motion-fast), box-shadow var(--app-motion-fast);
  &:hover { border-color: var(--td-brand-color); box-shadow: var(--td-shadow-1); }
  h2 { font-size: var(--app-text-2xl); line-height: 1.5; margin: 16px 0 8px; overflow-wrap: anywhere; }
  p { margin: 0 0 24px; font-size: var(--app-text-md); line-height: 1.7; color: var(--td-text-color-secondary);
    display: -webkit-box; -webkit-line-clamp: 2; -webkit-box-orient: vertical; overflow: hidden; }
}
.document-book__cover { display: grid; place-items: center; width: 46px; height: 46px; border-radius: var(--app-radius-md);
  background: var(--td-brand-color-light); color: var(--td-brand-color);
}
.document-book__meta { display: flex; justify-content: space-between; gap: 12px; margin-top: auto;
  font-size: var(--app-text-sm); color: var(--td-text-color-placeholder); span { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
}
.document-workspace__state { flex: 1; display: flex; align-items: center; justify-content: center; flex-direction: column;
  gap: 16px; min-height: 240px; padding: 32px; color: var(--td-text-color-secondary); text-align: center;
}
.document-workspace__document { flex: 1; min-width: 0; min-height: 0; display: flex;
  > :deep(.doc-outline) { margin: 16px 16px 16px 0; }
}
.document-workspace__compose { flex: 1; min-width: 0; min-height: 0; display: flex; flex-direction: column; gap: 20px; padding: 24px 32px;
  > :deep(.rich-editor) { flex: 1; min-height: 0; }
}
.document-workspace__compose-heading { display: flex; align-items: center; justify-content: space-between;
  > div { display: flex; gap: 8px; }
}
.document-workspace__draft-status { font-size: var(--app-text-sm); color: var(--td-text-color-secondary); margin: -8px 0; }
.document-workspace.is-focused { .document-workspace__sidebar, :deep(.doc-outline) { display: none; } }
@media (max-width: 1200px) { .document-workspace__document > :deep(.doc-outline) { display: none; } }
@media (max-width: 760px) {
  .document-workspace__sidebar { width: var(--workspace-sidebar-width, 208px); flex-basis: var(--workspace-sidebar-width, 208px); }
  .document-workspace__identity { padding: 10px 8px; }
  .document-workspace__tools { padding-left: 8px; padding-right: 8px; }
  .document-workspace__topbar { padding: 0 12px; }
  .document-workspace__platform { font-size: var(--app-text-sm); }
  .document-workspace__home { padding: 24px 16px; }
  .document-workspace__home-heading { flex-direction: column; align-items: stretch; .t-input__wrap { width: 100%; } }
  .document-workspace__compose { padding: 16px; }
}
</style>
