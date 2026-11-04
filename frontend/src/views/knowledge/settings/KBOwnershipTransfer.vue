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

    <div class="transfer-actions">
      <p class="transfer-warning">
        <t-icon name="error-circle" />
        {{ t('knowledgeEditor.transfer.irreversibleHint') }}
      </p>
      <t-button
        theme="danger"
        variant="outline"
        :disabled="!selectedUserId"
        :loading="submitting"
        @click="confirmTransfer"
      >
        {{ t('knowledgeEditor.transfer.submit') }}
      </t-button>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, watch, onBeforeUnmount } from 'vue'
import { useI18n } from 'vue-i18n'
import { MessagePlugin, DialogPlugin } from 'tdesign-vue-next'
import {
  listKnowledgeBaseTransferCandidates,
  transferKnowledgeBaseOwner,
} from '@/api/knowledge-base'
import type { KBTransferCandidate } from '@/api/knowledge-base'
import SpaceAvatar from '@/components/SpaceAvatar.vue'
import { filterTransferCandidates, transferCandidateLabel } from './ownershipTransfer'

const { t } = useI18n()

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
  (e: 'transferred', payload: { user_id: string; display_name: string }): void
}>()

const workspaceOwnedFallback = '—'
const searchQuery = ref('')
const loadingCandidates = ref(false)
const loadError = ref('')
const members = ref<KBTransferCandidate[]>([])
const selectedUserId = ref('')
const submitting = ref(false)

const candidates = computed(() =>
  filterTransferCandidates(members.value, props.currentOwnerId),
)

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
    if (!candidates.value.some((m) => m.user_id === selectedUserId.value)) {
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

function selectedMember(): KBTransferCandidate | undefined {
  return candidates.value.find((m) => m.user_id === selectedUserId.value)
}

function confirmTransfer() {
  const target = selectedMember()
  if (!target) return
  const name = transferCandidateLabel(target)
  const dialog = DialogPlugin.confirm({
    header: t('knowledgeEditor.transfer.confirmTitle'),
    body: t('knowledgeEditor.transfer.confirmBody', { name }),
    confirmBtn: {
      content: t('knowledgeEditor.transfer.confirmAction'),
      theme: 'danger',
    },
    cancelBtn: t('common.cancel'),
    onConfirm: () => {
      dialog.destroy()
      doTransfer(target)
    },
    onCancel: () => {
      dialog.destroy()
    },
  })
}

async function doTransfer(target: KBTransferCandidate) {
  submitting.value = true
  try {
    await transferKnowledgeBaseOwner(props.kbId, target.user_id)
    MessagePlugin.success(t('knowledgeEditor.transfer.success', { name: transferCandidateLabel(target) }))
    emit('transferred', {
      user_id: target.user_id,
      display_name: transferCandidateLabel(target),
    })
    selectedUserId.value = ''
    await loadCandidates()
  } catch (e) {
    // request.ts rejects with the parsed response body (not an Error), carrying
    // the HTTP status as `status` and as the non-enumerable `$httpStatus`.
    const err = e as { status?: number; $httpStatus?: number; message?: string } | null
    const status = err?.$httpStatus ?? err?.status
    if (status === 409) {
      MessagePlugin.warning(t('knowledgeEditor.transfer.conflict'))
    } else {
      MessagePlugin.error(err?.message?.trim() || t('knowledgeEditor.transfer.failed'))
    }
  } finally {
    submitting.value = false
  }
}

watch(() => props.kbId, loadCandidates, { immediate: true })

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
