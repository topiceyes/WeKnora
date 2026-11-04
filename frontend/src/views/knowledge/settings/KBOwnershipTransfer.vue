<template>
  <div class="section-content kb-owner-transfer">
    <div class="section-header">
      <h3 class="section-title">{{ t('knowledgeEditor.transfer.title') }}</h3>
      <p class="section-desc">{{ t('knowledgeEditor.transfer.description') }}</p>
    </div>

    <div class="transfer-current">
      <span class="transfer-current-label">{{ t('knowledgeEditor.transfer.currentOwner') }}</span>
      <div class="transfer-current-card">
        <SpaceAvatar
          :name="currentOwnerName || currentOwnerId || workspaceOwnedFallback"
          size="small"
        />
        <span class="transfer-current-name">
          {{ currentOwnerId ? (currentOwnerName || currentOwnerId) : t('knowledgeEditor.transfer.workspaceOwned') }}
        </span>
      </div>
    </div>

    <!-- 归宿人本人：把已被历史转让留在别人空间的库迁回自己空间 -->
    <div v-if="showSelfMove" class="transfer-self-move" :class="{ 'is-selected': isSelfMove }">
      <div class="transfer-self-move-text">
        <p class="transfer-self-move-title">{{ t('knowledgeEditor.transfer.moveToMyWorkspace') }}</p>
        <p class="transfer-self-move-desc">
          {{ t('knowledgeEditor.transfer.moveToMyWorkspaceDesc', { workspace: myWorkspaceLabel }) }}
        </p>
      </div>
      <t-button
        size="small"
        variant="outline"
        :disabled="isSelfMove"
        @click="selectSelfMove"
      >
        {{ isSelfMove
          ? t('knowledgeEditor.transfer.selected')
          : t('knowledgeEditor.transfer.moveToMyWorkspaceAction') }}
      </t-button>
    </div>

    <div class="transfer-picker">
      <label class="transfer-picker-label">{{ t('knowledgeEditor.transfer.selectNewOwner') }}</label>
      <t-input
        v-model="searchQuery"
        :placeholder="t('knowledgeEditor.transfer.searchPlaceholder')"
        clearable
      >
        <template #prefix-icon><t-icon name="search" /></template>
      </t-input>

      <div v-if="loadingCandidates" class="transfer-state">
        <t-loading size="small" />
        <span>{{ t('knowledgeEditor.transfer.loading') }}</span>
      </div>
      <div v-else-if="loadError" class="transfer-state">
        <t-alert theme="error" :message="loadError">
          <template #operation>
            <t-button size="small" @click="loadCandidates">{{ t('knowledgeEditor.transfer.retry') }}</t-button>
          </template>
        </t-alert>
      </div>
      <div v-else-if="candidates.length === 0" class="transfer-state">
        <t-empty :description="searchQuery.trim()
          ? t('knowledgeEditor.transfer.emptySearch')
          : t('knowledgeEditor.transfer.empty')" />
      </div>
      <div v-else class="transfer-candidate-list">
        <label
          v-for="member in candidates"
          :key="member.user_id"
          class="transfer-candidate"
          :class="{ 'is-selected': selectedUserId === member.user_id }"
        >
          <input
            v-model="selectedUserId"
            type="radio"
            name="transfer-target"
            :value="member.user_id"
            class="transfer-candidate-radio"
          />
          <SpaceAvatar :name="transferCandidateLabel(member)" size="small" />
          <span class="transfer-candidate-name">{{ transferCandidateLabel(member) }}</span>
          <span class="transfer-candidate-role">
            {{ t(`tenantMember.role.${member.role}`) }}
            <template v-if="candidateTenantName(member)">
              · {{ candidateTenantName(member) }}
            </template>
            <template v-else-if="viewerWillPromote(member)">
              · {{ t('knowledgeEditor.transfer.viewerWillPromote') }}
            </template>
          </span>
        </label>
      </div>
    </div>

    <!-- 跨空间预检：目标空间、数据规模、阻断项 -->
    <div v-if="selectedUserId && planLoading" class="transfer-state">
      <t-loading size="small" />
      <span>{{ t('knowledgeEditor.transfer.planLoading') }}</span>
    </div>
    <t-alert
      v-else-if="selectedUserId && planError"
      theme="error"
      :message="planError"
    />
    <template v-else-if="plan && plan.moves_workspace">
      <div class="transfer-plan">
        <div class="transfer-plan-head">
          <span class="transfer-plan-title">{{ t('knowledgeEditor.transfer.planTitle') }}</span>
          <span class="transfer-plan-workspace">
            {{ t('knowledgeEditor.transfer.targetWorkspace') }}
            <strong>{{ plan.target_tenant_name || `#${plan.target_tenant_id}` }}</strong>
          </span>
        </div>
        <div class="transfer-plan-stats">
          <div class="transfer-plan-stat">
            <span class="transfer-plan-stat-value">{{ plan.stats.knowledge_count }}</span>
            <span class="transfer-plan-stat-label">{{ t('knowledgeEditor.transfer.statKnowledge') }}</span>
          </div>
          <div class="transfer-plan-stat">
            <span class="transfer-plan-stat-value">{{ plan.stats.chunk_count }}</span>
            <span class="transfer-plan-stat-label">{{ t('knowledgeEditor.transfer.statChunks') }}</span>
          </div>
          <div class="transfer-plan-stat">
            <span class="transfer-plan-stat-value">{{ plan.stats.wiki_page_count }}</span>
            <span class="transfer-plan-stat-label">{{ t('knowledgeEditor.transfer.statWikiPages') }}</span>
          </div>
          <div class="transfer-plan-stat">
            <span class="transfer-plan-stat-value">{{ plan.stats.file_count }}</span>
            <span class="transfer-plan-stat-label">{{ t('knowledgeEditor.transfer.statFiles') }}</span>
          </div>
          <div class="transfer-plan-stat">
            <span class="transfer-plan-stat-value">{{ fileSize.value }} {{ fileSize.unit }}</span>
            <span class="transfer-plan-stat-label">{{ t('knowledgeEditor.transfer.statSize') }}</span>
          </div>
        </div>
        <p v-if="plan.shares_kept > 0" class="transfer-plan-shares">
          {{ t('knowledgeEditor.transfer.sharesKept', { count: plan.shares_kept }) }}
        </p>
      </div>

      <t-alert v-if="plan.blockers.length" theme="error">
        <template #title>{{ t('knowledgeEditor.transfer.blockersTitle') }}</template>
        <ul class="transfer-blocker-list">
          <li v-for="blocker in plan.blockers" :key="blocker.code">{{ blocker.message }}</li>
        </ul>
      </t-alert>

      <t-alert v-if="plan.warnings && plan.warnings.length" theme="warning">
        <template #title>{{ t('knowledgeEditor.transfer.warningsTitle') }}</template>
        <ul class="transfer-blocker-list">
          <li v-for="warning in plan.warnings" :key="warning.code">{{ warning.message }}</li>
        </ul>
      </t-alert>
    </template>

    <div class="transfer-actions">
      <p class="transfer-warning">
        <t-icon name="error-circle" />
        {{ movesWorkspace
          ? t('knowledgeEditor.transfer.irreversibleWorkspaceHint')
          : t('knowledgeEditor.transfer.irreversibleHint') }}
      </p>
      <t-button
        theme="danger"
        variant="outline"
        :disabled="!canSubmit"
        :loading="submitting"
        @click="confirmTransfer"
      >
        {{ movesWorkspace
          ? t('knowledgeEditor.transfer.submitWorkspaceMove')
          : t('knowledgeEditor.transfer.submit') }}
      </t-button>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, watch, onBeforeUnmount } from 'vue'
import { useI18n } from 'vue-i18n'
import { MessagePlugin, DialogPlugin } from 'tdesign-vue-next'
import {
  getKnowledgeBaseWorkspaceTransferPlan,
  listKnowledgeBaseTransferCandidates,
  transferKnowledgeBaseOwner,
  transferKnowledgeBaseWorkspace,
} from '@/api/knowledge-base'
import type {
  KBTransferCandidate,
  KBWorkspaceTransferPlan,
} from '@/api/knowledge-base'
import SpaceAvatar from '@/components/SpaceAvatar.vue'
import { useAuthStore } from '@/stores/auth'
import {
  filterTransferCandidates,
  formatTransferFileSize,
  transferCandidateLabel,
  transferHasBlockers,
} from './ownershipTransfer'

const { t } = useI18n()
const authStore = useAuthStore()

interface Props {
  kbId: string
  tenantId: number
  currentOwnerId?: string
  currentOwnerName?: string
}

const props = withDefaults(defineProps<Props>(), {
  currentOwnerId: '',
  currentOwnerName: '',
})

const emit = defineEmits<{
  (e: 'transferred', payload: {
    user_id: string
    display_name: string
    moved_workspace?: boolean
    to_tenant_id?: number
  }): void
}>()

const workspaceOwnedFallback = '—'
const searchQuery = ref('')
const loadingCandidates = ref(false)
const loadError = ref('')
const members = ref<KBTransferCandidate[]>([])
const selectedUserId = ref('')
const submitting = ref(false)
const plan = ref<KBWorkspaceTransferPlan | null>(null)
const planLoading = ref(false)
const planError = ref('')

const currentUserId = computed(() => authStore.user?.id || '')

const candidates = computed(() =>
  filterTransferCandidates(members.value, props.currentOwnerId),
)

const isSelfMove = computed(
  () => !!selectedUserId.value && selectedUserId.value === currentUserId.value,
)

// 归宿人本人在别的空间看到的库（v1.8.1/v1.8.2 历史转让的遗留形态）：
// 提供「迁入我的空间」修复入口。目标空间固定为 users.tenant_id。
const showSelfMove = computed(() => {
  if (!props.kbId || !props.currentOwnerId) return false
  if (props.currentOwnerId !== currentUserId.value) return false
  if (!authStore.hasRole('contributor')) return false
  const homeTenantId = Number(authStore.user?.tenant_id || 0)
  return homeTenantId !== 0 && homeTenantId !== Number(props.tenantId || 0)
})

const myWorkspaceLabel = computed(() => {
  const home = String(authStore.user?.tenant_id || '')
  const membership = authStore.memberships.find((m) => String(m.tenant_id) === home)
  return membership?.tenant_name || (home ? `#${home}` : '')
})

const movesWorkspace = computed(
  () => !!plan.value && plan.value.moves_workspace,
)

const fileSize = computed(() => formatTransferFileSize(plan.value?.stats.file_bytes))

const canSubmit = computed(() => {
  if (!selectedUserId.value || submitting.value) return false
  if (planLoading.value || planError.value) return false
  // 阻断项非空时后端一定拒绝；计划缺失同样不放行，避免无预检迁移。
  return !transferHasBlockers(plan.value)
})

// Same-workspace Viewer targets are promoted by the backend during the
// transfer. Cross-space candidates must already have Contributor rights in
// their own workspace, so no promotion hint applies there.
function viewerWillPromote(member: KBTransferCandidate): boolean {
  return member.tenant_id === props.tenantId && member.role === 'viewer'
}

function candidateTenantName(member: KBTransferCandidate): string {
  if (member.tenant_id === props.tenantId) return ''
  return member.tenant_name || ''
}

function selectSelfMove() {
  if (!currentUserId.value) return
  selectedUserId.value = currentUserId.value
}

// Requests run in parallel while the user types; only the newest response may
// publish results, otherwise a slow earlier search can overwrite a newer one.
let loadSeq = 0

async function loadCandidates() {
  if (!props.kbId) return
  const seq = ++loadSeq
  loadingCandidates.value = true
  loadError.value = ''
  try {
    const resp = await listKnowledgeBaseTransferCandidates(props.kbId, {
      q: searchQuery.value.trim() || undefined,
      limit: 50,
    })
    if (seq !== loadSeq) return
    members.value = resp.success && resp.data ? resp.data : []
    if (!isSelfMove.value && !candidates.value.some((m) => m.user_id === selectedUserId.value)) {
      selectedUserId.value = ''
    }
  } catch (e) {
    if (seq !== loadSeq) return
    members.value = []
    const err = e as { message?: string } | null
    loadError.value = err?.message || t('knowledgeEditor.transfer.loadFailed')
  } finally {
    if (seq === loadSeq) loadingCandidates.value = false
  }
}

// The plan is the single source of truth for "same workspace" vs "whole-KB
// move": the candidate row carries the shared-space membership tenant, while
// the backend moves the KB into the target's HOME workspace.
let planSeq = 0

async function loadPlan() {
  const target = selectedUserId.value
  if (!props.kbId || !target) {
    // Bump the sequence so an in-flight plan can never republish itself.
    planSeq += 1
    plan.value = null
    planError.value = ''
    planLoading.value = false
    return
  }
  const seq = ++planSeq
  planLoading.value = true
  planError.value = ''
  plan.value = null
  try {
    const resp = await getKnowledgeBaseWorkspaceTransferPlan(props.kbId, target)
    if (seq !== planSeq) return
    if (resp.success && resp.data) {
      plan.value = resp.data
    } else {
      planError.value = resp.message || t('knowledgeEditor.transfer.planFailed')
    }
  } catch (e) {
    if (seq !== planSeq) return
    const err = e as { message?: string } | null
    planError.value = err?.message?.trim() || t('knowledgeEditor.transfer.planFailed')
  } finally {
    if (seq === planSeq) planLoading.value = false
  }
}

function selectedMember(): KBTransferCandidate | undefined {
  return candidates.value.find((m) => m.user_id === selectedUserId.value)
}

function selectedLabel(): string {
  const target = selectedMember()
  if (target) return transferCandidateLabel(target)
  if (isSelfMove.value) {
    return authStore.user?.username || authStore.user?.email || currentUserId.value
  }
  return ''
}

function confirmTransfer() {
  if (!canSubmit.value) return
  const target = selectedMember()
  const isSelf = isSelfMove.value
  if (!target && !isSelf) return
  // Capture the selection now: the dialog is async and the radio may change
  // before the user confirms.
  const targetUserId = selectedUserId.value
  const name = selectedLabel()
  const moves = movesWorkspace.value
  const workspace = plan.value?.target_tenant_name || `#${plan.value?.target_tenant_id ?? ''}`
  const dialog = DialogPlugin.confirm({
    header: moves
      ? t('knowledgeEditor.transfer.confirmMoveTitle')
      : t('knowledgeEditor.transfer.confirmTitle'),
    body: moves
      ? t('knowledgeEditor.transfer.confirmMoveBody', { name, workspace })
      : t('knowledgeEditor.transfer.confirmBody', { name }),
    confirmBtn: {
      content: moves
        ? t('knowledgeEditor.transfer.confirmMoveAction')
        : t('knowledgeEditor.transfer.confirmAction'),
      theme: 'danger',
    },
    cancelBtn: t('common.cancel'),
    onConfirm: () => {
      dialog.destroy()
      doTransfer(targetUserId, name, isSelf)
    },
    onCancel: () => {
      dialog.destroy()
    },
  })
}

async function doTransfer(targetUserId: string, name: string, isSelf: boolean) {
  submitting.value = true
  try {
    if (movesWorkspace.value) {
      const resp = await transferKnowledgeBaseWorkspace(props.kbId, targetUserId)
      MessagePlugin.success(t('knowledgeEditor.transfer.moveSuccess', { name }))
      emit('transferred', {
        user_id: targetUserId,
        display_name: name,
        moved_workspace: true,
        to_tenant_id: Number((resp as any)?.data?.to_tenant_id || plan.value?.target_tenant_id || 0),
      })
      // A moved KB may no longer be readable from this workspace; the parent
      // handles navigation, so do not refetch candidates for a KB we lost.
      selectedUserId.value = ''
      plan.value = null
      // The self-move leaves the panel mounted (the owner keeps access) but
      // the eligible candidate pool is now the new workspace's members.
      if (isSelf) await loadCandidates()
      return
    }
    await transferKnowledgeBaseOwner(props.kbId, targetUserId)
    MessagePlugin.success(t('knowledgeEditor.transfer.success', { name }))
    emit('transferred', {
      user_id: targetUserId,
      display_name: name,
      moved_workspace: false,
    })
    selectedUserId.value = ''
    await loadCandidates()
  } catch (e) {
    // request.ts rejects with the parsed response body (not an Error), carrying
    // the HTTP status as `status` and as the non-enumerable `$httpStatus`.
    const err = e as {
      status?: number
      $httpStatus?: number
      message?: string
      error?: { details?: { blockers?: Array<{ code?: string; message?: string }> } }
    } | null
    const status = err?.$httpStatus ?? err?.status
    const blockers = err?.error?.details?.blockers
    if (status === 409) {
      if (Array.isArray(blockers) && blockers.length > 0) {
        plan.value = plan.value
          ? { ...plan.value, blockers: blockers as KBWorkspaceTransferPlan['blockers'] }
          : plan.value
        MessagePlugin.error(t('knowledgeEditor.transfer.blockedHint'))
      } else {
        MessagePlugin.warning(t('knowledgeEditor.transfer.conflict'))
      }
    } else {
      MessagePlugin.error(err?.message?.trim() || t('knowledgeEditor.transfer.failed'))
    }
    // The KB may have changed between plan and execute; refresh the plan so
    // the button reflects the current blockers.
    if (!isSelf || status === 409) await loadPlan()
  } finally {
    submitting.value = false
  }
}

watch(() => props.kbId, loadCandidates, { immediate: true })

// Re-evaluate the plan whenever the target changes. Self-move is included:
// the backend returns moves_workspace=true for it, with its own blockers.
watch(selectedUserId, loadPlan)

// Remote search with a short debounce; plain @change on t-input only fires
// on blur/enter, which makes typing feel dead.
let searchTimer: ReturnType<typeof setTimeout> | undefined
watch(searchQuery, () => {
  clearTimeout(searchTimer)
  searchTimer = setTimeout(loadCandidates, 250)
})

onBeforeUnmount(() => clearTimeout(searchTimer))
</script>

<style scoped lang="less">
.kb-owner-transfer {
  display: flex;
  flex-direction: column;
  gap: var(--app-space-6);
}

.transfer-current-label,
.transfer-picker-label {
  display: block;
  font-size: var(--app-text-sm);
  color: var(--td-text-color-secondary);
  margin-bottom: var(--app-space-2);
}

.transfer-current-card {
  display: inline-flex;
  align-items: center;
  gap: var(--app-space-3);
  padding: var(--app-space-3) var(--app-space-4);
  border: 1px solid var(--td-component-stroke);
  border-radius: var(--app-radius-md);
  background: var(--td-bg-color-container);
}

.transfer-current-name {
  font-size: var(--app-text-base);
  color: var(--td-text-color-primary);
}

.transfer-self-move {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: var(--app-space-4);
  padding: var(--app-space-3) var(--app-space-4);
  border: 1px dashed var(--td-component-stroke);
  border-radius: var(--app-radius-md);
  background: var(--td-bg-color-container);

  &.is-selected {
    border-color: var(--td-brand-color);
    background: var(--td-brand-color-light);
  }
}

.transfer-self-move-text {
  display: flex;
  flex-direction: column;
  gap: var(--app-space-1);
}

.transfer-self-move-title {
  margin: 0;
  font-size: var(--app-text-base);
  color: var(--td-text-color-primary);
}

.transfer-self-move-desc {
  margin: 0;
  font-size: var(--app-text-xs);
  color: var(--td-text-color-secondary);
}

.transfer-picker {
  display: flex;
  flex-direction: column;
  gap: var(--app-space-2);
}

.transfer-state {
  display: flex;
  align-items: center;
  gap: var(--app-space-3);
  padding: var(--app-space-4) 0;
}

.transfer-candidate-list {
  display: flex;
  flex-direction: column;
  border: 1px solid var(--td-component-stroke);
  border-radius: var(--app-radius-md);
  max-height: 320px;
  overflow-y: auto;
}

.transfer-candidate {
  display: flex;
  align-items: center;
  gap: var(--app-space-3);
  padding: var(--app-space-3) var(--app-space-4);
  cursor: pointer;
  border-bottom: 1px solid var(--td-component-stroke);

  &:last-child {
    border-bottom: none;
  }

  &:hover {
    background: var(--td-bg-color-container-hover);
  }

  &.is-selected {
    background: var(--td-brand-color-light);
  }
}

.transfer-candidate-radio {
  flex-shrink: 0;
}

.transfer-candidate-name {
  font-size: var(--app-text-base);
  color: var(--td-text-color-primary);
  flex: 1;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.transfer-candidate-role {
  font-size: var(--app-text-xs);
  color: var(--td-text-color-secondary);
  flex-shrink: 0;
}

.transfer-plan {
  display: flex;
  flex-direction: column;
  gap: var(--app-space-3);
  padding: var(--app-space-4);
  border: 1px solid var(--td-component-stroke);
  border-radius: var(--app-radius-md);
  background: var(--td-bg-color-container);
}

.transfer-plan-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: var(--app-space-4);
  flex-wrap: wrap;
}

.transfer-plan-title {
  font-size: var(--app-text-base);
  color: var(--td-text-color-primary);
}

.transfer-plan-workspace {
  font-size: var(--app-text-sm);
  color: var(--td-text-color-secondary);
}

.transfer-plan-stats {
  display: grid;
  grid-template-columns: repeat(5, minmax(0, 1fr));
  gap: var(--app-space-3);
}

.transfer-plan-stat {
  display: flex;
  flex-direction: column;
  gap: 2px;
}

.transfer-plan-stat-value {
  font-size: var(--app-text-base);
  color: var(--td-text-color-primary);
  font-variant-numeric: tabular-nums;
}

.transfer-plan-stat-label {
  font-size: var(--app-text-xs);
  color: var(--td-text-color-secondary);
}

.transfer-plan-shares {
  margin: 0;
  font-size: var(--app-text-xs);
  color: var(--td-text-color-secondary);
}

.transfer-blocker-list {
  margin: var(--app-space-1) 0 0;
  padding-left: var(--app-space-5);
  font-size: var(--app-text-sm);
}

.transfer-actions {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: var(--app-space-4);
  flex-wrap: wrap;
}

.transfer-warning {
  display: inline-flex;
  align-items: center;
  gap: var(--app-space-2);
  font-size: var(--app-text-xs);
  color: var(--td-warning-color);
  margin: 0;
}
</style>
