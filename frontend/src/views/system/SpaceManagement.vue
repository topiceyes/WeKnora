<template>
  <div class="space-management">
    <header class="section-header">
      <h2>{{ t('spaceManagement.title') }}</h2>
      <p class="section-description">{{ t('spaceManagement.description') }}</p>
    </header>

    <div v-if="spacesLoading" class="space-management__state">
      <t-loading size="small" />
      <span>{{ t('spaceManagement.loading') }}</span>
    </div>
    <div v-else-if="spacesError" class="space-management__state">
      <t-alert theme="error" :message="spacesError">
        <template #operation>
          <t-button size="small" @click="loadSpaces">{{ t('spaceManagement.retry') }}</t-button>
        </template>
      </t-alert>
    </div>
    <t-empty v-else-if="spaces.length === 0" :description="t('spaceManagement.empty')" />

    <div v-else class="space-management__layout">
      <!-- 左：全部空间 -->
      <div class="data-table-shell narrow-scrollbar space-management__list">
        <div
          v-for="space in spaces"
          :key="space.id"
          class="space-management__item"
          :class="{ 'space-management__item--active': space.id === selectedSpaceId }"
          @click="selectSpace(space)"
        >
          <t-icon name="city" class="space-management__item-icon" />
          <span class="space-management__item-name" :title="space.name">{{ space.name }}</span>
          <span class="space-management__item-id">#{{ space.id }}</span>
        </div>
      </div>

      <!-- 右：选中空间的知识库（只读） -->
      <div class="data-table-shell space-management__detail">
        <template v-if="selectedSpace">
          <div class="space-management__detail-head">
            <span class="space-management__detail-title">
              {{ selectedSpace.name }} ·
              {{ t('spaceManagement.kbCount', { count: kbs.length }) }}
            </span>
            <t-button
              v-if="isEnterableSpace(selectedSpace)"
              size="small"
              theme="primary"
              @click="enterSpace(selectedSpace)"
            >
              <template #icon><t-icon name="login" /></template>
              {{ t('spaceManagement.enter') }}
            </t-button>
          </div>
          <div v-if="kbsLoading" class="space-management__state">
            <t-loading size="small" />
            <span>{{ t('spaceManagement.loading') }}</span>
          </div>
          <div v-else-if="kbsError" class="space-management__state">
            <t-alert theme="error" :message="kbsError" />
          </div>
          <t-empty v-else-if="kbs.length === 0" :description="t('spaceManagement.noKbs')" />
          <t-table v-else row-key="id" size="small" :data="kbs" :columns="kbColumns">
            <template #createdAt="{ row }">{{ formatDate(row.created_at) }}</template>
            <template #creator="{ row }">{{ row.creator_name || row.creator_id || '—' }}</template>
          </t-table>
        </template>
        <t-empty v-else :description="t('spaceManagement.pickHint')" />
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { listAllTenants, type TenantInfo } from '@/api/tenant'
import { listSpaceKnowledgeBases } from '@/api/system'
import type { KnowledgeBaseInfo } from '@/api/auth'
import { useAuthStore } from '@/stores/auth'
import { switchToTenant } from '@/utils/tenantSwitch'
import {
  isEnterableSpace,
  kbListFrom,
  toSpaceRows,
  type SpaceRow,
} from './spaceManagementState'

// SpaceManagement — platform system admin's read-only inventory of every
// workspace and the knowledge bases inside them. Settings section
// `space-management`, gated to isSystemAdmin by settingsAccess.ts and to
// RequireSystemAdmin on the server.
//
// Deliberately read-only: there is no edit/delete here. A system admin who
// needs to change something uses 「进入空间」, which switches the active
// workspace and hands control to the ordinary in-workspace UI (where the
// server grants them a temporary Admin role). Keeping writes out of this
// view means the authorization story never has to answer "an admin acting on
// a workspace they are not a member of".

const { t } = useI18n()
const authStore = useAuthStore()

const spaces = ref<SpaceRow[]>([])
const spacesLoading = ref(false)
const spacesError = ref('')
const selectedSpaceId = ref<number | null>(null)

const kbs = ref<KnowledgeBaseInfo[]>([])
const kbsLoading = ref(false)
const kbsError = ref('')

// Request-shape guard: utils/request rejects with a plain object, not an
// Error, so String(e) prints "[object Object]". Same ladder as PlatformUsers.
const errText = (e: unknown): string => {
  const err = e as { message?: unknown; error?: { message?: unknown } | string }
  if (typeof err?.message === 'string' && err.message) return err.message
  const nested = typeof err?.error === 'object' ? err.error?.message : err?.error
  if (typeof nested === 'string' && nested) return nested
  try {
    return JSON.stringify(e)
  } catch {
    return String(e)
  }
}

const formatDate = (v?: string) => (v ? new Date(v).toLocaleString() : '—')

const selectedSpace = computed(
  () => spaces.value.find((s) => s.id === selectedSpaceId.value) ?? null,
)

const kbColumns = computed(() => [
  { colKey: 'name', title: t('spaceManagement.columns.name') },
  { colKey: 'type', title: t('spaceManagement.columns.type') },
  { colKey: 'creator', title: t('spaceManagement.columns.creator') },
  { colKey: 'createdAt', title: t('spaceManagement.columns.createdAt') },
])

const loadSpaces = async () => {
  spacesLoading.value = true
  spacesError.value = ''
  try {
    const resp = await listAllTenants()
    const rows = toSpaceRows(resp.data?.items as TenantInfo[] | undefined)
    spaces.value = rows
    // Keep the current selection across a refresh when it still exists;
    // otherwise start on the first row so the detail pane isn't empty on
    // arrival (the layout is master-detail, so an unselected pane looks
    // like a failure rather than a prompt).
    if (!rows.some((r) => r.id === selectedSpaceId.value)) {
      selectedSpaceId.value = rows.length > 0 ? rows[0].id : null
      void loadKbsFor(selectedSpaceId.value)
    }
  } catch (e) {
    spacesError.value = errText(e)
    spaces.value = []
    selectedSpaceId.value = null
  } finally {
    spacesLoading.value = false
  }
}

const loadKbsFor = async (spaceId: number | null) => {
  if (spaceId === null) {
    kbs.value = []
    return
  }
  kbsLoading.value = true
  kbsError.value = ''
  try {
    const resp = await listSpaceKnowledgeBases(spaceId)
    // Ignore a late response for a space the user has since navigated away
    // from — otherwise the detail pane briefly shows the wrong workspace's
    // knowledge bases.
    if (selectedSpaceId.value !== spaceId) return
    kbs.value = kbListFrom(resp.data)
  } catch (e) {
    if (selectedSpaceId.value !== spaceId) return
    kbsError.value = errText(e)
    kbs.value = []
  } finally {
    if (selectedSpaceId.value === spaceId) kbsLoading.value = false
  }
}

const selectSpace = (space: SpaceRow) => {
  if (selectedSpaceId.value === space.id) return
  selectedSpaceId.value = space.id
  kbs.value = []
  void loadKbsFor(space.id)
}

const enterSpace = (space: SpaceRow) => {
  if (!isEnterableSpace(space)) return
  // Same switch path the sidebar TenantSelector uses, including the
  // post-reload toast and the hard navigation that resets every store.
  // isHomeTenant matters: entering one's own workspace must clear the
  // persisted "last active" preference rather than point at it.
  const isHomeTenant = String(authStore.tenant?.id ?? '') === String(space.id)
  authStore.setSelectedTenant(space.id, space.name)
  switchToTenant({
    tenantId: space.id,
    name: space.name,
    isHomeTenant,
  })
}

onMounted(loadSpaces)
</script>

<style lang="less" scoped>
@import (reference) '@/components/css/settings-section.less';

.space-management {
  display: flex;
  flex-direction: column;
  gap: 12px;
  min-height: 0;
}

.section-header {
  .settings-section-header();
}

.space-management__state {
  display: flex;
  align-items: center;
  gap: 8px;
  min-height: 120px;
  color: var(--td-text-color-secondary);
}

.space-management__layout {
  display: grid;
  grid-template-columns: minmax(220px, 300px) minmax(0, 1fr);
  gap: 16px;
  align-items: start;
}

.space-management__list {
  max-height: 480px;
  overflow-y: auto;
  padding: 4px;
}

.space-management__item {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 8px 10px;
  border-radius: var(--td-radius-default);
  cursor: pointer;

  &:hover {
    background: var(--td-bg-color-container-hover);
  }

  &--active {
    background: var(--td-brand-color-light);
    color: var(--td-brand-color);
  }
}

.space-management__item-icon {
  flex: none;
}

.space-management__item-name {
  flex: 1;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.space-management__item-id {
  flex: none;
  color: var(--td-text-color-placeholder);
  font-size: var(--app-text-sm);
}

.space-management__detail {
  min-height: 200px;
  padding: 12px;
}

.space-management__detail-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  margin-bottom: 12px;
}

.space-management__detail-title {
  font-weight: 600;
  color: var(--td-text-color-primary);
}
</style>
