<template>
  <div v-if="drafts.length" class="draft-recovery" role="status">
    <div class="draft-recovery__heading"><t-icon name="history" /><strong>{{ t('documentSaving.recoveryTitle') }}</strong><span>{{ t('documentSaving.localOnly') }}</span></div>
    <div v-for="draft in drafts" :key="draft.key" class="draft-recovery__item">
      <span class="draft-recovery__description">
        <strong>{{ draft.title || t('knowledgeBase.untitledDocument') }}</strong>
        <time>{{ new Date(draft.savedAt).toLocaleString() }}</time>
        <span v-if="baseVersion !== undefined && draft.baseVersion !== baseVersion" class="draft-recovery__warning">{{ t('documentSaving.oldDraft') }}</span>
        <details><summary>{{ t('documentSaving.previewDraft') }}</summary><pre>{{ draft.content }}</pre></details>
      </span>
      <t-button size="small" variant="outline" :disabled="disabled" @click="emit('discard', draft)">{{ t('documentSaving.discardDraft') }}</t-button>
      <t-button size="small" theme="primary" :disabled="disabled" @click="emit('restore', draft)">{{ t('documentSaving.restoreDraft') }}</t-button>
    </div>
  </div>
</template>
<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import type { StoredDraft } from './documentDrafts'
defineProps<{ drafts: StoredDraft[]; baseVersion?: number; disabled?: boolean }>()
const emit = defineEmits<{ restore: [draft: StoredDraft]; discard: [draft: StoredDraft] }>()
const { t } = useI18n()
</script>
<style scoped lang="less">
.draft-recovery { margin: 12px 20px; padding: 12px 16px; border: 1px solid var(--td-warning-color);
  border-radius: var(--app-radius-sm); background: var(--td-warning-color-light); font-size: var(--app-text-sm);
  max-height: 280px; overflow: auto; flex-shrink: 0; }
.draft-recovery__heading { display: flex; align-items: center; gap: 8px; margin-bottom: 8px;
  > span { color: var(--td-text-color-secondary); } }
.draft-recovery__item { display: flex; gap: 8px; align-items: flex-start; padding: 8px 0; }
.draft-recovery__description { flex: 1; min-width: 0; display: grid; gap: 6px;
  time { color: var(--td-text-color-secondary); }
  pre { max-height: 180px; overflow: auto; white-space: pre-wrap; overflow-wrap: anywhere; margin: 8px 0; }
  summary { cursor: pointer; } }
.draft-recovery__warning { color: var(--td-warning-color); }
</style>
