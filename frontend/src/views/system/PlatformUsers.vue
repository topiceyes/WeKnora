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

      <!-- 用户清单已砍：改用官方「系统设置 → 用户」面板（/system/admin/users），
           见 SystemSettings.vue 的 users tab。本组件只保留钉钉三块。 -->

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
  listDingTalkIdentities,
  listDingTalkDepartments,
  listDingTalkSyncRuns,
  triggerDingTalkSync,
  type DingTalkIdentityRow,
  type DingTalkSyncRun,
} from '@/api/system'
import { buildDeptTree, type DeptNode } from '@/utils/dingtalkDept'

const { t } = useI18n()

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

// 后端单页硬上限 100（parseListPagination），而本页三处清单都要完整数据：
// 组织架构的部门人数/成员表是按 identity 全量汇总的，只拉第一页会把
// 300+ 人企业的统计算错。循环翻页拉全量，短页即止；页数上限防御
// total/短页信号异常时的死循环。
const PAGE_SIZE = 100
const MAX_PAGES = 50
const fetchAllIdentities = async (keyword?: string): Promise<DingTalkIdentityRow[]> => {
  const out: DingTalkIdentityRow[] = []
  for (let page = 1; page <= MAX_PAGES; page++) {
    const resp = await listDingTalkIdentities(page, PAGE_SIZE, keyword)
    const rows = resp.data?.identities ?? []
    out.push(...rows)
    if (rows.length < PAGE_SIZE) break
  }
  return out
}
// ---------------------------------------------------------------------------
// 组织架构
// ---------------------------------------------------------------------------

const orgLoading = ref(false)
const orgError = ref('')
const orgSyncedAt = ref('')
const departments = ref<{ dept_id: number; name: string; parent_id: number }[]>([])
const identities = ref<DingTalkIdentityRow[]>([])
const identitiesLoading = ref(false)
const identityKeyword = ref('')
const selectedDept = ref<number>(1)

const orgTree = computed<DeptNode[]>(() => buildDeptTree(departments.value, t('platformUsers.companyRoot')))

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

// ensureDepartments 拉取部门表（幂等，已加载则跳过）。绑定 tab 也要用
// 部门名渲染 dept_id_list，先于组织 tab 打开时不能显示成 #1234。
const ensureDepartments = async () => {
  if (departments.value.length > 0) return
  try {
    const resp = await listDingTalkDepartments()
    departments.value = resp.data?.departments ?? []
    orgSyncedAt.value = resp.data?.synced_at ?? ''
  } catch {
    // 部门名只是增强显示，失败不阻断调用方
  }
}

const loadOrg = async () => {
  orgLoading.value = true
  orgError.value = ''
  try {
    // 部门树读本地表（每日同步全量替换，不再实时调钉钉）；
    // 成员清单取本地绑定全量（含职位/工号快照），人数统计才准。
    const [, allIdentities] = await Promise.all([ensureDepartments(), fetchAllIdentities()])
    identities.value = allIdentities
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
      departments.value = [] // 同步后部门树已更新，强制下次重新拉取
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
// 钉钉绑定
// ---------------------------------------------------------------------------
const loadIdentities = async () => {
  identitiesLoading.value = true
  try {
    // 部门名用于把 dept_id_list 渲染成可读名称；先于组织 tab 打开时
    // 也要加载（幂等）。绑定本身拉全量分页，清单不截断。
    await ensureDepartments()
    identities.value = await fetchAllIdentities(identityKeyword.value.trim())
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
      departments.value = [] // 同步后部门树已更新，组织 tab 下次进入重新拉取
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
  font-size: var(--app-text-sm);
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
  font-size: var(--app-text-sm);
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
