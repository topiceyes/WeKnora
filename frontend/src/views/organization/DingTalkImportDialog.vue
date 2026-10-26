<template>
  <t-dialog
    :visible="visible"
    :header="t('organization.dingtalkImport.dialogTitle')"
    :footer="false"
    width="960px"
    placement="top"
    destroy-on-close
    @close="handleClose"
  >
    <!-- 挑选阶段：左部门树 + 右成员多选表 -->
    <div v-if="step === 'pick'" class="dt-import">
      <div v-if="!loadingDepts && departments.length === 0" class="dt-import__empty">
        <t-empty :description="t('organization.dingtalkImport.emptySnapshot')">
          <template #action>
            <span v-if="syncedAt" class="dt-import__synced">
              {{ t('organization.dingtalkImport.syncedAt', { time: formatSyncedAt }) }}
            </span>
          </template>
        </t-empty>
      </div>
      <div v-else class="dt-import__layout">
        <div class="dt-import__tree narrow-scrollbar">
          <div
            v-for="node in deptTree"
            :key="node.dept_id"
            class="dt-import__tree-node"
            :style="{ paddingLeft: `${node.depth * 18 + 8}px` }"
            :class="{ 'dt-import__tree-node--active': selectedDept === node.dept_id }"
            @click="handleDeptChange(node.dept_id)"
          >
            <t-icon name="folder" class="dt-import__tree-icon" />
            <span class="dt-import__tree-name">{{ node.name }}</span>
          </div>
        </div>
        <div class="dt-import__members">
          <div class="dt-import__toolbar">
            <t-input
              v-model="keyword"
              :placeholder="t('organization.dingtalkImport.searchPlaceholder')"
              clearable
              size="small"
              @change="handleKeywordChange"
              @clear="handleKeywordChange"
            >
              <template #prefix-icon><t-icon name="search" /></template>
            </t-input>
            <span v-if="syncedAt" class="dt-import__synced">
              {{ t('organization.dingtalkImport.syncedAt', { time: formatSyncedAt }) }}
            </span>
          </div>
          <t-table
            row-key="ding_user_id"
            size="small"
            height="380px"
            :data="contacts"
            :columns="contactColumns"
            :loading="loadingContacts"
            :selected-row-keys="selectedRowKeys"
            @select-change="handleSelectChange"
          >
            <template #state="{ row }">
              <t-tag
                v-if="row.state === 'already_member'"
                theme="success"
                variant="light"
                size="small"
              >{{ t('organization.dingtalkImport.stateAlreadyMember') }}</t-tag>
              <t-tag
                v-else-if="row.state === 'registered'"
                variant="light"
                size="small"
              >{{ t('organization.dingtalkImport.stateRegistered') }}</t-tag>
              <t-tag v-else theme="warning" variant="light" size="small">
                {{ t('organization.dingtalkImport.stateUnregistered') }}
              </t-tag>
            </template>
          </t-table>
        </div>
      </div>
      <div class="dt-import__footer">
        <t-select
          v-model="role"
          :options="roleOptions"
          size="small"
          class="dt-import__role"
          :label="t('organization.dingtalkImport.selectRole')"
        />
        <span class="dt-import__selected">
          {{ t('organization.dingtalkImport.selectedCount', { count: selectedRowKeys.length }) }}
        </span>
        <t-button variant="outline" :disabled="importing" @click="handleClose">
          {{ t('common.cancel') }}
        </t-button>
        <t-button
          theme="primary"
          :loading="importing"
          :disabled="selectedRowKeys.length === 0"
          @click="handleImport"
        >
          {{ t('organization.dingtalkImport.confirmBtn') }}
        </t-button>
      </div>
    </div>

    <!-- 结果阶段：汇总 + 逐项失败明细 -->
    <div v-else class="dt-import__result">
      <div class="dt-import__result-summary">
        <t-tag theme="success" variant="light">
          {{ t('organization.dingtalkImport.resultAdded', { count: resultSummary.added }) }}
        </t-tag>
        <t-tag variant="light">
          {{ t('organization.dingtalkImport.resultAlreadyMember', { count: resultSummary.already_member }) }}
        </t-tag>
        <t-tag v-if="resultSummary.failed > 0" theme="danger" variant="light">
          {{ t('organization.dingtalkImport.resultFailed', { count: resultSummary.failed }) }}
        </t-tag>
      </div>
      <div v-if="failedItems.length > 0" class="dt-import__result-list narrow-scrollbar">
        <div v-for="item in failedItems" :key="item.ding_user_id" class="dt-import__result-row">
          <span class="dt-import__result-name">{{ item.name || item.ding_user_id }}</span>
          <span class="dt-import__result-error">{{ item.error }}</span>
        </div>
      </div>
      <div class="dt-import__footer">
        <t-button theme="primary" @click="handleDone">{{ t('organization.dingtalkImport.doneBtn') }}</t-button>
      </div>
    </div>
  </t-dialog>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { MessagePlugin } from 'tdesign-vue-next'
import {
  importOrgDingTalkMembers,
  listOrgDingTalkContacts,
  listOrgDingTalkDepartments,
  type OrgDingTalkContact,
  type OrgDingTalkDepartment,
  type OrgDingTalkImportResultItem,
} from '@/api/organization'
import { buildDeptTree, DINGTALK_ROOT_DEPT_ID } from '@/utils/dingtalkDept'

const props = defineProps<{
  visible: boolean
  orgId: string
}>()

const emit = defineEmits<{
  (e: 'update:visible', value: boolean): void
  (e: 'done'): void
}>()

const { t } = useI18n()

type Step = 'pick' | 'result'
const step = ref<Step>('pick')

const departments = ref<OrgDingTalkDepartment[]>([])
const contacts = ref<OrgDingTalkContact[]>([])
const syncedAt = ref('')
const selectedDept = ref<number>(DINGTALK_ROOT_DEPT_ID)
const keyword = ref('')
const loadingDepts = ref(false)
const loadingContacts = ref(false)
const importing = ref(false)
const selectedRowKeys = ref<string[]>([])
const role = ref<'viewer' | 'editor' | 'admin'>('viewer')

const resultItems = ref<OrgDingTalkImportResultItem[]>([])
const resultSummary = ref({ added: 0, already_member: 0, failed: 0 })

const deptTree = computed(() => buildDeptTree(departments.value, t('organization.dingtalkImport.companyRoot')))

const roleOptions = computed(() => [
  { label: t('organization.role.viewer'), value: 'viewer' },
  { label: t('organization.role.editor'), value: 'editor' },
  { label: t('organization.role.admin'), value: 'admin' },
])

// 已加入的行禁选（checkProps 是 tdesign 行选择列的逐项禁用口子）。
const contactColumns = computed(() => [
  {
    colKey: 'row-select',
    type: 'multiple' as const,
    width: 40,
    checkProps: ({ row }: { row: OrgDingTalkContact }) => ({ disabled: row.state === 'already_member' }),
  },
  { colKey: 'name', title: t('organization.dingtalkImport.colName'), ellipsis: true },
  { colKey: 'job_number', title: t('organization.dingtalkImport.colJobNumber'), width: 90, ellipsis: true },
  { colKey: 'title', title: t('organization.dingtalkImport.colTitle'), width: 110, ellipsis: true },
  { colKey: 'email', title: t('organization.dingtalkImport.colEmail'), ellipsis: true },
  { colKey: 'state', title: t('organization.dingtalkImport.colState'), width: 100 },
])

const failedItems = computed(() => resultItems.value.filter((it) => it.status === 'error'))

const formatSyncedAt = computed(() => {
  if (!syncedAt.value) return ''
  const d = new Date(syncedAt.value)
  return Number.isNaN(d.getTime()) ? syncedAt.value : d.toLocaleString()
})

const loadDepartments = async () => {
  loadingDepts.value = true
  try {
    const resp = await listOrgDingTalkDepartments(props.orgId)
    if (resp.success && resp.data) {
      departments.value = resp.data.departments ?? []
      syncedAt.value = resp.data.synced_at ?? ''
    } else {
      MessagePlugin.error(resp.message || t('organization.dingtalkImport.loadFailed'))
    }
  } finally {
    loadingDepts.value = false
  }
}

const loadContacts = async () => {
  loadingContacts.value = true
  try {
    const resp = await listOrgDingTalkContacts(props.orgId, {
      deptId: selectedDept.value,
      keyword: keyword.value.trim(),
    })
    if (resp.success && resp.data) {
      contacts.value = resp.data.contacts ?? []
      if (resp.data.synced_at) syncedAt.value = resp.data.synced_at
    } else {
      MessagePlugin.error(resp.message || t('organization.dingtalkImport.loadFailed'))
    }
  } finally {
    loadingContacts.value = false
  }
}

const handleDeptChange = (deptId: number) => {
  if (deptId === selectedDept.value) return
  selectedDept.value = deptId
  loadContacts()
}

let keywordTimer: ReturnType<typeof setTimeout> | undefined
const handleKeywordChange = () => {
  // 关键词搜索走后端过滤，防抖 300ms（与现有 tenant 搜索一致）。
  if (keywordTimer) clearTimeout(keywordTimer)
  keywordTimer = setTimeout(() => loadContacts(), 300)
}

const handleSelectChange = (keys: (string | number)[]) => {
  selectedRowKeys.value = keys.map(String)
}

const handleImport = async () => {
  if (selectedRowKeys.value.length === 0) return
  importing.value = true
  try {
    const resp = await importOrgDingTalkMembers(props.orgId, {
      ding_user_ids: selectedRowKeys.value,
      role: role.value,
    })
    if (resp.success && resp.data) {
      resultItems.value = resp.data.results ?? []
      resultSummary.value = resp.data.summary ?? { added: 0, already_member: 0, failed: 0 }
      step.value = 'result'
    } else {
      MessagePlugin.error(resp.message || t('organization.dingtalkImport.importFailed'))
    }
  } finally {
    importing.value = false
  }
}

const resetState = () => {
  step.value = 'pick'
  departments.value = []
  contacts.value = []
  syncedAt.value = ''
  selectedDept.value = DINGTALK_ROOT_DEPT_ID
  keyword.value = ''
  selectedRowKeys.value = []
  role.value = 'viewer'
  resultItems.value = []
  resultSummary.value = { added: 0, already_member: 0, failed: 0 }
}

const handleClose = () => {
  emit('update:visible', false)
}

const handleDone = () => {
  emit('update:visible', false)
  emit('done')
}

watch(
  () => props.visible,
  (visible) => {
    if (!visible) return
    resetState()
    loadDepartments()
    loadContacts()
  },
)
</script>

<style scoped lang="less">
.dt-import {
  &__layout {
    display: flex;
    gap: 12px;
    min-height: 420px;
  }

  &__tree {
    flex: 0 0 220px;
    max-height: 440px;
    overflow-y: auto;
    border: 1px solid var(--td-component-border);
    border-radius: var(--app-radius-md);
    padding: 4px 0;
  }

  &__tree-node {
    display: flex;
    align-items: center;
    gap: 6px;
    padding: 6px 8px;
    cursor: pointer;
    font-size: var(--app-text-sm);
    color: var(--td-text-color-primary);
    white-space: nowrap;
    overflow: hidden;

    &:hover {
      background: var(--td-bg-color-container-hover);
    }

    &--active {
      background: var(--td-bg-color-container-select);
      color: var(--td-brand-color);
    }
  }

  &__tree-icon {
    flex: 0 0 auto;
    color: var(--td-text-color-secondary);
  }

  &__tree-name {
    overflow: hidden;
    text-overflow: ellipsis;
  }

  &__members {
    flex: 1 1 auto;
    min-width: 0;
    display: flex;
    flex-direction: column;
    gap: 8px;
  }

  &__toolbar {
    display: flex;
    align-items: center;
    gap: 12px;
  }

  &__synced {
    flex: 0 0 auto;
    font-size: var(--app-text-xs);
    color: var(--td-text-color-secondary);
  }

  &__footer {
    display: flex;
    align-items: center;
    justify-content: flex-end;
    gap: 12px;
    padding-top: 12px;
  }

  &__role {
    width: 160px;
  }

  &__selected {
    margin-right: auto;
    font-size: var(--app-text-sm);
    color: var(--td-text-color-secondary);
  }

  &__empty {
    padding: 48px 0;
  }

  &__result-summary {
    display: flex;
    gap: 8px;
    padding: 8px 0 16px;
  }

  &__result-list {
    max-height: 320px;
    overflow-y: auto;
    border: 1px solid var(--td-component-border);
    border-radius: var(--app-radius-md);
    margin-bottom: 8px;
  }

  &__result-row {
    display: flex;
    justify-content: space-between;
    padding: 8px 12px;
    font-size: var(--app-text-sm);

    & + & {
      border-top: 1px solid var(--td-component-border);
    }
  }

  &__result-error {
    color: var(--td-error-color);
    font-family: 'SF Mono', Consolas, Menlo, monospace;
    font-size: var(--app-text-xs);
  }
}
</style>
