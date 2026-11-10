<template>
  <t-drawer :visible="visible" :header="t('documentComments.title')" size="min(440px, 100vw)" :footer="false"
    :close-btn="true" :close-on-esc-keydown="true" :close-on-overlay-click="false" @close="emit('update:visible', false)">
    <section class="document-comments">
      <div class="document-comments__toolbar">
        <t-radio-group v-model="status" size="small" variant="outline" :disabled="loading || busy">
          <t-radio-button value="all">{{ t('documentComments.all') }}</t-radio-button>
          <t-radio-button value="open">{{ t('documentComments.open') }}</t-radio-button>
          <t-radio-button value="resolved">{{ t('documentComments.resolved') }}</t-radio-button>
        </t-radio-group>
        <t-button size="small" variant="text" :disabled="loading || busy" @click="load(false)">{{ t('documentComments.refresh') }}</t-button>
      </div>
      <p v-if="error" role="alert" class="document-comments__error">{{ error }}</p>
      <t-loading v-if="loading && !items.length" size="small" />
      <p v-else-if="!loading && !items.length && !error" class="document-comments__hint">{{ t('documentComments.empty') }}</p>
      <article v-for="thread in items" :key="thread.id" class="document-comments__thread" :data-comment-id="thread.id">
        <div class="document-comments__meta">
          <strong>{{ thread.author_name || t('documentComments.unknownUser') }}</strong>
          <time :datetime="thread.created_at">{{ formatStringDate(thread.created_at) }}</time>
          <t-tag v-if="thread.resolved" size="small" theme="success">{{ t('documentComments.resolved') }}</t-tag>
        </div>
        <button v-if="thread.quote" type="button" class="document-comments__quote" @click="emit('locate', thread)">
          <span>{{ thread.quote }}</span>
          <small>{{ t(thread.content_version === contentVersion ? 'documentComments.locate' : 'documentComments.staleQuote') }}</small>
        </button>
        <p class="document-comments__body">{{ thread.deleted ? t('documentComments.deleted') : thread.body }}</p>
        <div v-if="canEdit" class="document-comments__actions">
          <t-button v-if="!thread.deleted" size="small" variant="text" :disabled="busy || loading" @click="mutate(thread, 'resolve')">
            {{ t(thread.resolved ? 'documentComments.reopen' : 'documentComments.resolve') }}
          </t-button>
          <t-button v-if="!thread.deleted && thread.author_id === userId" size="small" variant="text" theme="danger" :disabled="busy || loading" @click="confirmDelete(thread)">{{ t('documentComments.delete') }}</t-button>
          <t-button v-if="!thread.deleted && !thread.resolved" size="small" variant="text" :disabled="busy || loading" @click="chooseReply(thread)">{{ t('documentComments.reply') }}</t-button>
        </div>
        <div v-for="reply in thread.replies || []" :key="reply.id" class="document-comments__reply">
          <div class="document-comments__meta"><strong>{{ reply.author_name || t('documentComments.unknownUser') }}</strong><time :datetime="reply.created_at">{{ formatStringDate(reply.created_at) }}</time></div>
          <p class="document-comments__body">{{ reply.deleted ? t('documentComments.deleted') : reply.body }}</p>
          <t-button v-if="canEdit && !reply.deleted && reply.author_id === userId" size="small" variant="text" theme="danger" :disabled="busy || loading" @click="confirmDelete(reply)">{{ t('documentComments.delete') }}</t-button>
        </div>
      </article>
      <t-button v-if="hasMore" block variant="outline" :loading="loading" :disabled="busy" @click="load(true)">{{ t('documentComments.more') }}</t-button>
      <div v-if="canEdit" class="document-comments__composer">
        <div v-if="replyTo" class="document-comments__context">
          <span>{{ t('documentComments.replying', { name: replyTo.author_name || t('documentComments.unknownUser') }) }}</span>
          <t-button size="small" variant="text" :disabled="busy" @click="replyTo = null">{{ t('documentComments.cancelReply') }}</t-button>
        </div>
        <div v-if="draftAnchor" class="document-comments__context">
          <blockquote>{{ draftAnchor.quote }}</blockquote>
          <t-button size="small" variant="text" :disabled="busy" @click="draftAnchor = null">{{ t('documentComments.removeQuote') }}</t-button>
        </div>
        <t-textarea ref="composerRef" v-model="body" :maxlength="5000" :autosize="{ minRows: 3, maxRows: 8 }" :disabled="busy"
          :placeholder="t('documentComments.placeholder')" :aria-label="t('documentComments.placeholder')" @keydown.ctrl.enter.prevent="submit" @keydown.meta.enter.prevent="submit" />
        <t-button theme="primary" :loading="busy" :disabled="!body.trim() || loading" @click="submit">{{ t('documentComments.send') }}</t-button>
        <p class="document-comments__hint">{{ t('documentComments.draftHint') }}</p>
      </div>
      <p v-else class="document-comments__hint">{{ t('documentComments.readOnly') }}</p>
    </section>
  </t-drawer>
</template>
<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { DialogPlugin } from 'tdesign-vue-next'
import { listDocumentComments, createDocumentComment, mutateDocumentComment, type DocumentComment, type DocumentCommentAnchor } from '@/api/knowledge-base'
import { formatStringDate } from '@/utils/index'

const props = defineProps<{ visible: boolean; knowledgeId: string; contentVersion: number; canEdit: boolean; userId: string; anchor: DocumentCommentAnchor | null }>()
const emit = defineEmits<{ 'update:visible': [value: boolean]; locate: [anchor: DocumentCommentAnchor] }>()
const { t } = useI18n()
const items = ref<DocumentComment[]>([])
const status = ref('all')
const loading = ref(false)
const busy = ref(false)
const error = ref('')
const body = ref('')
const replyTo = ref<DocumentComment | null>(null)
const draftAnchor = ref<DocumentCommentAnchor | null>(null)
const composerRef = ref<{ focus?: () => void } | null>(null)
const hasMore = ref(false)
const offset = ref(0)
const hasDraft = computed(() => !!body.value.trim())
let generation = 0
let live = true
let pending: { fingerprint: string; id: string } | null = null
function discard() { body.value = ''; replyTo.value = null; draftAnchor.value = null; pending = null }
async function load(append = false) {
  if (!props.knowledgeId || (append && loading.value)) return
  const ticket = ++generation
  const id = props.knowledgeId
  loading.value = true
  error.value = ''
  try {
    const result: any = await listDocumentComments(id, append ? offset.value : 0, status.value)
    if (!live || ticket !== generation || props.knowledgeId !== id) return
    if (!result?.success || !result.data) throw new Error(result?.message || t('documentComments.loadFailed'))
    items.value = append ? [...new Map<string, DocumentComment>([...items.value, ...result.data.items].map((item: DocumentComment) => [item.id, item])).values()] : result.data.items
    offset.value = result.data.next_offset
    hasMore.value = result.data.has_more
  } catch (e: any) { if (live && ticket === generation) error.value = e?.message || t('documentComments.loadFailed') }
  finally { if (live && ticket === generation) loading.value = false }
}
async function chooseReply(thread: DocumentComment) {
  replyTo.value = thread
  draftAnchor.value = null
  await nextTick()
  composerRef.value?.focus?.()
}
async function submit() {
  if (!props.canEdit || busy.value || loading.value || !body.value.trim()) return
  const id = props.knowledgeId
  const payload = { body: body.value.trim(), parent_id: replyTo.value?.id || '', ...(draftAnchor.value || {}) }
  const fingerprint = JSON.stringify(payload)
  if (!pending || pending.fingerprint !== fingerprint) pending = { fingerprint, id: crypto.randomUUID() }
  busy.value = true
  error.value = ''
  try {
    const result: any = await createDocumentComment(id, { ...payload, client_id: pending.id })
    if (!live || props.knowledgeId !== id) return
    if (!result?.success) throw new Error(result?.message || t('documentComments.sendFailed'))
    discard()
    status.value = 'all'
    // Let the status watcher settle before refreshing once.
    await nextTick()
    if (!loading.value) await load(false)
  } catch (e: any) { if (live && props.knowledgeId === id) error.value = e?.message || t('documentComments.sendFailed') }
  finally { if (live) busy.value = false }
}
async function mutate(row: DocumentComment, action: 'resolve' | 'delete') {
  if (!props.canEdit || busy.value || loading.value) return
  busy.value = true
  error.value = ''
  try {
    const result: any = await mutateDocumentComment(props.knowledgeId, row.id, { action, base_revision: row.revision, resolved: !row.resolved })
    if (!live) return
    if (!result?.success) throw new Error(result?.message || t('documentComments.sendFailed'))
    if (replyTo.value?.id === row.id) replyTo.value = null
    await load(false)
  } catch (e: any) { if (live) error.value = e?.message || t('documentComments.sendFailed') }
  finally { if (live) busy.value = false }
}
function confirmDelete(row: DocumentComment) {
  const dialog = DialogPlugin.confirm({ header: t('documentComments.delete'), body: t('documentComments.deleteConfirm'),
    onConfirm: () => { dialog.destroy(); if (live) void mutate(row, 'delete') }, onClose: () => dialog.destroy() })
}
watch(() => props.anchor, value => { if (value) { draftAnchor.value = value; replyTo.value = null } }, { immediate: true })
watch(status, () => { if (props.visible) void load(false) })
watch(() => props.visible, visible => { if (visible) void load(false) }, { immediate: true })
onBeforeUnmount(() => { live = false; generation++ })
defineExpose({ hasDraft, busy, discard })
</script>
<style scoped lang="less">
.document-comments { display: flex; flex-direction: column; gap: 18px; color: var(--td-text-color-primary); }
.document-comments__toolbar, .document-comments__meta, .document-comments__actions, .document-comments__context { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; }
.document-comments__toolbar { justify-content: space-between; }
.document-comments__meta { font-size: var(--app-text-xs); color: var(--td-text-color-secondary); }
.document-comments__meta strong { color: var(--td-text-color-primary); }
.document-comments__thread { padding-bottom: 18px; border-bottom: 1px solid var(--td-component-stroke); }
.document-comments__body { white-space: pre-wrap; overflow-wrap: anywhere; margin: 10px 0; line-height: 1.7; }
.document-comments__quote { display: flex; flex-direction: column; gap: 6px; text-align: left; width: 100%; padding: 10px; margin-top: 12px; border: 0; border-left: 3px solid var(--td-brand-color); border-radius: var(--app-radius-sm); background: var(--td-bg-color-secondarycontainer); color: var(--td-text-color-secondary); cursor: pointer; overflow-wrap: anywhere; white-space: pre-wrap; }
.document-comments__quote small { color: var(--td-brand-color); }
.document-comments__reply { margin: 12px 0 0 12px; padding-left: 12px; border-left: 2px solid var(--td-component-stroke); }
.document-comments__composer { display: flex; flex-direction: column; gap: 10px; padding: 14px; background: var(--td-bg-color-secondarycontainer); border-radius: var(--app-radius-md); }
.document-comments__context blockquote { margin: 0; max-height: 100px; overflow: auto; white-space: pre-wrap; overflow-wrap: anywhere; }
.document-comments__hint { color: var(--td-text-color-placeholder); font-size: var(--app-text-xs); margin: 0; }
.document-comments__error { color: var(--td-error-color); font-size: var(--app-text-sm); }
</style>
