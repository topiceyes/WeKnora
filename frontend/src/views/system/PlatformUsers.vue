<template>
  <div class="platform-users">
    <header class="section-header">
      <h2>{{ t('platformUsers.title') }}</h2>
      <p class="section-description">{{ t('platformUsers.description') }}</p>
    </header>

    <t-tabs v-model="activeTab" class="platform-users__tabs">
      <!-- 组织架构 -->
      <t-tab-panel value="org" :label="t('platformUsers.tabs.org')">
        <div class="platform-users__panel">
          <div v-if="orgLoading" class="platform-users__state">
            <t-loading size="small" />
            <span>{{ t('platformUsers.loading') }}</span>
          </div>
          <div v-else-if="orgError" class="platform-users__state">
            <t-alert theme="error" :message="orgError">
              <template #operation>
                <t-button size="small" @click="loadOrg">{{ t('platformUsers.retry') }}</t-button>
              </template>
            </t-alert>
          </div>
          <template v-else>
            <div v-if="orgSyncedAt" class="platform-users__org-syncbar">
              <span class="platform-users__muted">{{ t('platformUsers.lastSyncAt') }}: {{ formatDateTime(orgSyncedAt) }}</span>
            </div>
            <div v-if="departments.length === 0" class="platform-users__state">
              <t-empty :description="t('platformUsers.neverSynced')">
                <template #action>
                  <t-button theme="primary" size="small" :loading="syncTriggering" @click="syncFromOrgTab">
                    {{ t('platformUsers.syncNow') }}
                  </t-button>
                </template>
              </t-empty>
            </div>
            <div v-else class="platform-users__org-layout">
            <div class="data-table-shell narrow-scrollbar platform-users__tree">
              <div
                v-for="node in orgTree"
                :key="node.dept_id"
                class="platform-users__tree-node"
                :style="{ paddingLeft: `${node.depth * 18 + 8}px` }"
                :class="{ 'platform-users__tree-node--active': selectedDept === node.dept_id }"
                @click="selectedDept = node.dept_id"
              >
                <t-icon name="folder" class="platform-users__tree-icon" />
                <span class="platform-users__tree-name">{{ node.name }}</span>
                <span class="platform-users__tree-count">{{ deptMemberCount(node.dept_id) }}</span>
              </div>
            </div>
            <div class="data-table-shell platform-users__members">
              <div class="platform-users__members-title">
                {{ selectedDeptName }} · {{ t('platformUsers.members') }} ({{ selectedDeptMembers.length }})
              </div>
              <t-table
                v-if="selectedDeptMembers.length > 0"
                row-key="id"
                size="small"
                :data="selectedDeptMembers"
                :columns="memberColumns"
              />
              <t-empty v-else :description="t('platformUsers.noMembers')" />
            </div>
            </div>
          </template>
        </div>
      </t-tab-panel>

      <!-- 用户清单 -->
      <t-tab-panel value="users" :label="t('platformUsers.tabs.users')">
        <div class="platform-users__panel">
          <div v-if="usersLoading" class="platform-users__state">
            <t-loading size="small" />
            <span>{{ t('platformUsers.loading') }}</span>
          </div>
          <div v-else class="data-table-shell">
            <t-table row-key="id" :data="users" :columns="userColumns">
              <template #status="{ row }">
                <t-tag v-if="row.is_active" theme="success" variant="light">{{ t('platformUsers.active') }}</t-tag>
                <t-tag v-else theme="danger" variant="light">{{ t('platformUsers.disabled') }}</t-tag>
              </template>
              <template #isAdmin="{ row }">
                <t-tag v-if="row.is_system_admin" theme="primary" variant="light">{{ t('platformUsers.sysAdmin') }}</t-tag>
                <span v-else class="platform-users__muted">—</span>
              </template>
              <template #createdAt="{ row }">{{ formatDate(row.created_at) }}</template>
              <template #actions="{ row }">
                <t-popconfirm
                  :content="row.is_active ? t('platformUsers.disableConfirm') : t('platformUsers.enableConfirm')"
                  :confirm-btn="{ content: row.is_active ? t('platformUsers.disable') : t('platformUsers.enable'), theme: row.is_active ? 'danger' : 'primary' }"
                  :cancel-btn="{ content: t('common.cancel') }"
                  @confirm="toggleUserStatus(row)"
                >
                  <t-button
                    size="small"
                    variant="outline"
                    :theme="row.is_active ? 'danger' : 'primary'"
                    :disabled="row.id === currentUserId"
                  >
                    {{ row.is_active ? t('platformUsers.disable') : t('platformUsers.enable') }}
                  </t-button>
                </t-popconfirm>
              </template>
            </t-table>
          </div>
        </div>
      </t-tab-panel>

      <!-- 钉钉绑定 -->
      <t-tab-panel value="identities" :label="t('platformUsers.tabs.identities')">
        <div class="platform-users__panel">
          <div class="platform-users__toolbar">
            <t-input
              v-model="identityKeyword"
              clearable
              :placeholder="t('platformUsers.searchPlaceholder')"
              style="max-width: 260px"
              @enter="loadIdentities"
              @clear="loadIdentities"
            />
            <t-button variant="outline" @click="loadIdentities">
              <template #icon><t-icon name="search" /></template>
              {{ t('platformUsers.search') }}
            </t-button>
          </div>
          <div v-if="identitiesLoading" class="platform-users__state">
            <t-loading size="small" />
            <span>{{ t('platformUsers.loading') }}</span>
          </div>
          <div v-else-if="identities.length === 0" class="platform-users__state">
            <t-empty :description="t('platformUsers.noIdentities')" />
          </div>
          <div v-else class="data-table-shell">
            <t-table row-key="id" :data="identities" :columns="identityColumns">
              <template #depts="{ row }">
                <span v-if="deptNamesById.size > 0">
                  {{ (row.dept_id_list || []).map((id: number) => deptNamesById.get(id) || `#${id}`).join(' / ') || '—' }}
                </span>
                <span v-else>{{ (row.dept_id_list || []).join(' / ') || '—' }}</span>
              </template>
              <template #lastLogin="{ row }">{{ formatDate(row.last_login_at) }}</template>
              <template #lastSync="{ row }">{{ formatDate(row.last_synced_at) }}</template>
            </t-table>
          </div>
        </div>
      </t-tab-panel>

      <!-- 同步记录 -->
      <t-tab-panel value="sync" :label="t('platformUsers.tabs.sync')">
        <div class="platform-users__panel">
          <div class="platform-users__toolbar">
            <t-button theme="primary" :loading="syncTriggering" @click="triggerSync">
              <template #icon><t-icon name="refresh" /></template>
              {{ t('platformUsers.syncNow') }}
            </t-button>
          </div>
          <div v-if="runsLoading" class="platform-users__state">
            <t-loading size="small" />
            <span>{{ t('platformUsers.loading') }}</span>
          </div>
          <div v-else-if="runs.length === 0" class="platform-users__state">
            <t-empty :description="t('platformUsers.noRuns')" />
          </div>
          <div v-else class="data-table-shell">
            <t-table row-key="id" :data="runs" :columns="runColumns">
              <template #trigger="{ row }">
                <t-tag theme="default" variant="light">{{ row.trigger_type }}</t-tag>
              </template>
              <template #status="{ row }">
                <t-tag :theme="row.status === 'success' ? 'success' : 'danger'" variant="light">{{ row.status }}</t-tag>
              </template>
              <template #startedAt="{ row }">{{ formatDateTime(row.started_at) }}</template>
              <template #duration="{ row }">
                {{ row.finished_at ? `${Math.max(1, Math.round((new Date(row.finished_at).getTime() - new Date(row.started_at).getTime()) / 1000))}s` : '—' }}
              </template>
              <template #errorMessage="{ row }">
                <t-popup v-if="row.error_message" :content="row.error_message">
                  <span class="platform-users__error">{{ t('platformUsers.hasError') }}</span>
                </t-popup>
                <span v-else class="platform-users__muted">—</span>
              </template>
            </t-table>
          </div>
        </div>
      </t-tab-panel>
    </t-tabs>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { MessagePlugin } from 'tdesign-vue-next'
import {
  listPlatformUsers,
  listDingTalkIdentities,
  listDingTalkDepartments,
  listDingTalkSyncRuns,
  triggerDingTalkSync,
  type PlatformUserRow,
  type DingTalkIdentityRow,
  type DingTalkSyncRun,
} from '@/api/system'
import { put } from '@/utils/request'
import { useAuthStore } from '@/stores/auth'

const { t } = useI18n()
const authStore = useAuthStore()
const currentUserId = computed(() => authStore.user?.id || '')

const activeTab = ref('org')

// ---------------------------------------------------------------------------
// 通用
// ---------------------------------------------------------------------------
// utils/request 的 reject 是普通对象（{message, $httpStatus, ...}）而不是
// Error 实例，String(e) 会显示成 "[object Object]"——统一从 message 取，
// 再逐层兜底。
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
const formatDate = (v?: string) => (v ? new Date(v).toLocaleDateString() : '—')
const formatDateTime = (v?: string) => (v ? new Date(v).toLocaleString() : '—')

// ---------------------------------------------------------------------------
// 组织架构
// ---------------------------------------------------------------------------
interface DeptNode {
  dept_id: number
  name: string
  parent_id: number
  depth: number
}

const orgLoading = ref(false)
const orgError = ref('')
const orgSyncedAt = ref('')
const departments = ref<{ dept_id: number; name: string; parent_id: number }[]>([])
const identities = ref<DingTalkIdentityRow[]>([])
const identitiesLoading = ref(false)
const identityKeyword = ref('')
const selectedDept = ref<number>(1)

const orgTree = computed<DeptNode[]>(() => {
  // 根部门（dept_id=1，钉钉默认的公司节点）作为树顶，其余按 parent_id 深度优先展开。
  const byParent = new Map<number, number[]>()
  for (const d of departments.value) {
    const list = byParent.get(d.parent_id) ?? []
    list.push(d.dept_id)
    byParent.set(d.parent_id, list)
  }
  const names = new Map(departments.value.map((d) => [d.dept_id, d.name]))
  const out: DeptNode[] = [{ dept_id: 1, name: t('platformUsers.companyRoot'), parent_id: 0, depth: 0 }]
  const walk = (parent: number, depth: number) => {
    for (const id of byParent.get(parent) ?? []) {
      out.push({ dept_id: id, name: names.get(id) || `#${id}`, parent_id: parent, depth })
      walk(id, depth + 1)
    }
  }
  walk(1, 1)
  return out
})

const deptNamesById = computed(() => new Map(departments.value.map((d) => [d.dept_id, d.name])))

const deptMemberCount = (deptId: number) =>
  identities.value.filter((it) => (it.dept_id_list || []).includes(deptId)).length

const selectedDeptName = computed(
  () => orgTree.value.find((n) => n.dept_id === selectedDept.value)?.name || '—',
)
const selectedDeptMembers = computed(() =>
  identities.value.filter((it) => (it.dept_id_list || []).includes(selectedDept.value)),
)

const memberColumns = computed(() => [
  { colKey: 'nick', title: t('platformUsers.nick') },
  { colKey: 'title', title: t('platformUsers.jobTitle') },
  { colKey: 'job_number', title: t('platformUsers.jobNumber') },
  { colKey: 'email', title: t('platformUsers.email') },
  { colKey: 'username', title: t('platformUsers.localAccount') },
])

const loadOrg = async () => {
  orgLoading.value = true
  orgError.value = ''
  try {
    // 部门树读本地表（每日同步全量替换，不再实时调钉钉）；
    // 成员清单取本地绑定（含职位/工号快照）。
    const [deptResp, identResp] = await Promise.all([
      listDingTalkDepartments(),
      listDingTalkIdentities(1, 100),
    ])
    departments.value = deptResp.data?.departments ?? []
    orgSyncedAt.value = deptResp.data?.synced_at ?? ''
    identities.value = identResp.data?.identities ?? []
  } catch (e: unknown) {
    orgError.value = errText(e)
  } finally {
    orgLoading.value = false
  }
}

// 组织架构空态（从未同步过）里的「立即同步」：跑完手动同步后重载本 tab。
const syncFromOrgTab = async () => {
  syncTriggering.value = true
  try {
    const resp = await triggerDingTalkSync()
    if (resp.success) {
      MessagePlugin.success(t('platformUsers.syncDone', { total: resp.data?.total_remote ?? 0, updated: resp.data?.updated_count ?? 0 }))
      await loadOrg()
    } else {
      MessagePlugin.error(t('platformUsers.syncFailed'))
    }
  } catch (e: unknown) {
    MessagePlugin.error(errText(e))
  } finally {
    syncTriggering.value = false
  }
}

// ---------------------------------------------------------------------------
// 用户清单
// ---------------------------------------------------------------------------
const usersLoading = ref(false)
const users = ref<PlatformUserRow[]>([])

const userColumns = computed(() => [
  { colKey: 'username', title: t('platformUsers.username') },
  { colKey: 'email', title: t('platformUsers.email') },
  { colKey: 'status', title: t('platformUsers.status'), width: 100 },
  { colKey: 'isAdmin', title: t('platformUsers.sysAdmin'), width: 110 },
  { colKey: 'createdAt', title: t('platformUsers.createdAt'), width: 120 },
  { colKey: 'actions', title: t('platformUsers.actions'), width: 110 },
])

const loadUsers = async () => {
  usersLoading.value = true
  try {
    const resp = await listPlatformUsers(1, 100)
    users.value = resp.data?.list ?? []
  } catch (e: unknown) {
    MessagePlugin.error(errText(e))
  } finally {
    usersLoading.value = false
  }
}

const toggleUserStatus = async (row: PlatformUserRow) => {
  try {
    const resp = await put(`/api/v1/system/users/${row.id}/status`, { is_active: !row.is_active }) as unknown as {
      success: boolean
      error?: string
    }
    if (!resp.success) {
      MessagePlugin.error(resp.error || t('platformUsers.actionFailed'))
      return
    }
    row.is_active = !row.is_active
    MessagePlugin.success(row.is_active ? t('platformUsers.enabled') : t('platformUsers.disabledMsg'))
  } catch (e: unknown) {
    MessagePlugin.error(errText(e))
  }
}

// ---------------------------------------------------------------------------
// 钉钉绑定
// ---------------------------------------------------------------------------
const loadIdentities = async () => {
  identitiesLoading.value = true
  try {
    const resp = await listDingTalkIdentities(1, 100, identityKeyword.value.trim())
    identities.value = resp.data?.identities ?? []
  } catch (e: unknown) {
    MessagePlugin.error(errText(e))
  } finally {
    identitiesLoading.value = false
  }
}

const identityColumns = computed(() => [
  { colKey: 'nick', title: t('platformUsers.nick') },
  { colKey: 'title', title: t('platformUsers.jobTitle') },
  { colKey: 'job_number', title: t('platformUsers.jobNumber') },
  { colKey: 'email', title: t('platformUsers.email') },
  { colKey: 'depts', title: t('platformUsers.departments') },
  { colKey: 'username', title: t('platformUsers.localAccount') },
  { colKey: 'lastLogin', title: t('platformUsers.lastLogin'), width: 110 },
  { colKey: 'lastSync', title: t('platformUsers.lastSync'), width: 110 },
])

// ---------------------------------------------------------------------------
// 同步记录
// ---------------------------------------------------------------------------
const runsLoading = ref(false)
const syncTriggering = ref(false)
const runs = ref<DingTalkSyncRun[]>([])

const runColumns = computed(() => [
  { colKey: 'trigger', title: t('platformUsers.trigger'), width: 90 },
  { colKey: 'status', title: t('platformUsers.status'), width: 100 },
  { colKey: 'startedAt', title: t('platformUsers.startedAt') },
  { colKey: 'duration', title: t('platformUsers.duration'), width: 80 },
  { colKey: 'total_remote', title: t('platformUsers.totalRemote'), width: 90 },
  { colKey: 'updated_count', title: t('platformUsers.updated'), width: 80 },
  { colKey: 'deactivated_count', title: t('platformUsers.deactivated'), width: 90 },
  { colKey: 'errorMessage', title: t('platformUsers.errorMessage') },
])

const loadRuns = async () => {
  runsLoading.value = true
  try {
    const resp = await listDingTalkSyncRuns(1, 20)
    runs.value = resp.data?.runs ?? []
  } catch (e: unknown) {
    MessagePlugin.error(errText(e))
  } finally {
    runsLoading.value = false
  }
}

const triggerSync = async () => {
  syncTriggering.value = true
  try {
    const resp = await triggerDingTalkSync()
    if (resp.success && resp.data) {
      MessagePlugin.success(t('platformUsers.syncDone', { total: resp.data.total_remote, updated: resp.data.updated_count }))
    } else {
      MessagePlugin.error(t('platformUsers.syncFailed'))
    }
    await loadRuns()
  } catch (e: unknown) {
    MessagePlugin.error(errText(e))
  } finally {
    syncTriggering.value = false
  }
}

// tab 懒加载：首次切入时拉数据
const loadedTabs = new Set<string>(['org'])
watch(activeTab, (tab) => {
  if (loadedTabs.has(tab)) return
  loadedTabs.add(tab)
  if (tab === 'users') loadUsers()
  if (tab === 'identities') loadIdentities()
  if (tab === 'sync') loadRuns()
})

onMounted(loadOrg)
</script>

<style lang="less" scoped>
@import (reference) '@/components/css/settings-section.less';

.platform-users {
  display: flex;
  flex-direction: column;
  gap: 12px;
  min-height: 0;
}

.platform-users__tabs {
  :deep(.t-tabs__content) {
    padding-top: 8px;
  }
}

.platform-users__panel {
  display: flex;
  flex-direction: column;
  gap: 12px;
  min-height: 200px;
}

.platform-users__state {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 8px;
  padding: 32px 0;
  color: var(--td-text-color-placeholder);
}

.platform-users__org-syncbar {
  display: flex;
  justify-content: flex-end;
  font-size: 12px;
}

.platform-users__org-layout {
  display: grid;
  grid-template-columns: minmax(220px, 320px) 1fr;
  gap: 12px;
  min-height: 0;
}

.platform-users__tree {
  display: flex;
  flex-direction: column;
  max-height: 480px;
  overflow: auto;
  padding: 6px 0;
}

.platform-users__tree-node {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 6px 10px;
  cursor: pointer;
  border-radius: var(--td-radius-default);
  &:hover {
    background: var(--td-bg-color-container-hover);
  }
}

.platform-users__tree-node--active {
  background: var(--td-brand-color-light);
}

.platform-users__tree-name {
  flex: 1;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.platform-users__tree-count {
  font-size: 12px;
  color: var(--td-text-color-placeholder);
}

.platform-users__members {
  padding: 12px;
}

.platform-users__members-title {
  margin-bottom: 8px;
  font-weight: 600;
}

.platform-users__toolbar {
  display: flex;
  align-items: center;
  gap: 8px;
}

.platform-users__muted {
  color: var(--td-text-color-placeholder);
}

.platform-users__error {
  color: var(--td-error-color);
  cursor: help;
  text-decoration: underline;
}
</style>
