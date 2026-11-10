<template>
  <t-dialog :visible="!!dialog" :header="dialogTitle" :confirm-btn="null" :cancel-btn="null" :close-on-overlay-click="!busy" :close-btn="!busy" @close="close">
    <div class="document-navigation-dialog">
      <p v-if="dialog?.action === 'delete'">{{ t('documentNavigation.deleteEmptyHint') }}</p>
      <p v-else-if="dialog?.action === 'move'">{{ t('documentNavigation.moveHint') }}</p>
      <t-input v-if="dialog && ['create', 'rename'].includes(dialog.action)" v-model="name" :disabled="busy" :placeholder="t('documentNavigation.folderName')" :aria-label="t('documentNavigation.folderName')" @enter="submit" />
      <t-select v-if="dialog?.action === 'move'" v-model="destination" :disabled="busy" :options="destinations" filterable :aria-label="t('documentNavigation.destination')" />
      <p v-if="error" class="document-navigation-dialog__error" role="alert">{{ error }}</p>
      <div class="document-navigation-dialog__buttons">
        <t-button variant="outline" :disabled="busy" @click="close">{{ t('knowledgeBase.browse.cancel') }}</t-button>
        <t-button :theme="dialog?.action === 'delete' ? 'danger' : 'primary'" :loading="busy" @click="submit">{{ t('knowledgeBase.browse.save') }}</t-button>
      </div>
    </div>
  </t-dialog>
</template>
<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { MessagePlugin } from 'tdesign-vue-next'
import { updateKnowledgeNavigation, type KnowledgeTree, type NavigationMutation } from '@/api/knowledge-base'
import { folderOptions, folderNameValid, itemKey, itemParent, parentPath, reorderedKeys, directItems, type NavigationItem } from './navigationModel'
const props = defineProps<{ kbId: string; tree: KnowledgeTree | null; canEdit: boolean; beforeChange: () => Promise<boolean>; refresh: () => Promise<void> }>()
const emit = defineEmits<{ busy: [value: boolean]; changed: [change: { from?: string; to?: string; action: string }] }>()
const { t } = useI18n()
type Dialog = { action: 'create' | 'rename' | 'move' | 'delete'; item?: NavigationItem; parent?: string; ids?: string[] }
const dialog = ref<Dialog | null>(null), name = ref(''), destination = ref(''), error = ref(''), busy = ref(false)
const dialogTitle = computed(() => t(`documentNavigation.${dialog.value?.action || 'create'}`))
const destinations = computed(() => [{ label: t('documentNavigation.root'), value: '' }, ...folderOptions(props.tree, dialog.value?.item?.kind === 'folder' ? dialog.value.item.path : undefined)])
function close() { if (!busy.value) { dialog.value = null; error.value = '' } }
function open(value: Dialog) {
  if (!props.canEdit || busy.value) return
  dialog.value = value; name.value = value.action === 'rename' && value.item?.kind === 'folder' ? value.item.name : ''
  destination.value = value.item ? itemParent(value.item) : ''; error.value = ''
}
async function run(payload: Omit<NavigationMutation, 'base_revision'>) {
  if (!props.canEdit || busy.value || !props.tree || props.tree.navigation_revision == null) return false
  busy.value = true; emit('busy', true)
  const kb = props.kbId
  try {
    if (!await props.beforeChange() || kb !== props.kbId) return false
    const result: any = await updateKnowledgeNavigation(kb, { ...payload, base_revision: props.tree.navigation_revision! })
    if (!result?.success) throw new Error(result?.message || t('documentNavigation.failed'))
    await props.refresh()
    busy.value = false; emit('busy', false)
    emit('changed', { action: payload.action, from: payload.from, to: payload.to })
    MessagePlugin.success(t('documentNavigation.success'))
    return true
  } catch (e: any) {
    error.value = e?.message || t('documentNavigation.failed')
    if (e?.status === 409 || e?.response?.status === 409) { await props.refresh(); error.value = t('documentNavigation.conflict') }
    MessagePlugin.error(error.value)
    return false
  } finally { busy.value = false; emit('busy', false) }
}
async function submit() {
  const d = dialog.value
  if (!d || busy.value) return
  let payload: Omit<NavigationMutation, 'base_revision'>
  if (d.action === 'create' || d.action === 'rename') {
    const parent = d.action === 'create' ? d.parent || '' : parentPath(d.item?.kind === 'folder' ? d.item.path : '')
    if (!folderNameValid(name.value, parent)) { error.value = t('documentNavigation.invalidName'); return }
    const to = [parent, name.value.trim()].filter(Boolean).join('/')
    payload = d.action === 'create' ? { action: 'create_folder', to } : { action: 'move_folder', from: d.item?.kind === 'folder' ? d.item.path : '', to }
  } else if (d.action === 'delete') payload = { action: 'delete_folder', from: d.item?.kind === 'folder' ? d.item.path : '' }
  else payload = d.item?.kind === 'folder'
    ? { action: 'move_folder', from: d.item.path, to: [destination.value, d.item.name].filter(Boolean).join('/') }
    : { action: 'move_documents', knowledge_ids: d.ids || (d.item?.kind === 'doc' ? [d.item.id] : []), to: destination.value }
  if (await run(payload)) dialog.value = null
}
async function action(key: string, item: NavigationItem) {
  if (key === 'up' || key === 'down') {
    const items = props.tree && reorderedKeys(props.tree, item, key === 'up' ? -1 : 1)
    if (items) await run({ action: 'reorder', from: itemParent(item), items })
  } else open({ action: key as Dialog['action'], item, parent: item.kind === 'folder' ? item.path : item.folder_path })
}
async function drop(source: NavigationItem, target: NavigationItem | null, position: 'inside' | 'before' | 'after') {
  if (!props.tree || !props.canEdit || busy.value) return
  if (target && itemKey(source) === itemKey(target)) return
  if (position === 'inside' && (!target || target.kind === 'folder')) {
    const to = target?.kind === 'folder' ? target.path : ''
    if (source.kind === 'folder') await run({ action: 'move_folder', from: source.path, to: [to, source.name].filter(Boolean).join('/') })
    else await run({ action: 'move_documents', knowledge_ids: [source.id], to })
  } else if (target && itemParent(source) === itemParent(target)) {
    const items = directItems(props.tree, itemParent(source)).map(itemKey).filter(k => k !== itemKey(source))
    const at = items.indexOf(itemKey(target)); items.splice(at + (position === 'after' ? 1 : 0), 0, itemKey(source))
    await run({ action: 'reorder', from: itemParent(source), items })
  } else MessagePlugin.info(t('documentNavigation.dragHint'))
}
defineExpose({ open, action, drop })
</script>
<style scoped lang="less">
.document-navigation-dialog { display: grid; gap: 16px; }
.document-navigation-dialog__error { color: var(--td-error-color); }
.document-navigation-dialog__buttons { display: flex; justify-content: flex-end; gap: 8px; }
</style>
