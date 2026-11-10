<template>
  <div ref="rootRef" class="rich-editor" :inert="disabled">
    <div class="rich-editor__toolbar" @mousedown="preserveSelection">
      <t-tooltip :content="t('knowledgeBase.browse.editor.undo')" placement="bottom">
        <button type="button" class="rich-editor__btn" :disabled="!editor?.can().undo()" @click="run('undo')">
          <t-icon name="rollback" size="16px" />
        </button>
      </t-tooltip>
      <t-tooltip :content="t('knowledgeBase.browse.editor.redo')" placement="bottom">
        <button type="button" class="rich-editor__btn" :disabled="!editor?.can().redo()" @click="run('redo')">
          <t-icon name="rollfront" size="16px" />
        </button>
      </t-tooltip>
      <span class="rich-editor__divider" />

      <select class="rich-editor__select" :value="currentBlock" :aria-label="t('writingTools.blockType')" @change="changeBlock">
        <option value="paragraph">{{ t('writingTools.paragraph') }}</option>
        <option v-for="level in [1, 2, 3, 4]" :key="level" :value="`heading${level}`">H{{ level }} · {{ t(`writingTools.heading${level}`) }}</option>
      </select>
      <span class="rich-editor__divider" />

      <t-tooltip :content="t('knowledgeBase.browse.editor.bold')" placement="bottom">
        <button type="button" class="rich-editor__btn" :class="{ 'is-active': isActive('bold') }"
          @click="chain('toggleBold')">
          <t-icon name="textformat-bold" size="16px" />
        </button>
      </t-tooltip>
      <t-tooltip :content="t('knowledgeBase.browse.editor.italic')" placement="bottom">
        <button type="button" class="rich-editor__btn" :class="{ 'is-active': isActive('italic') }"
          @click="chain('toggleItalic')">
          <t-icon name="textformat-italic" size="16px" />
        </button>
      </t-tooltip>
      <t-tooltip :content="t('knowledgeBase.browse.editor.strikethrough')" placement="bottom">
        <button type="button" class="rich-editor__btn" :class="{ 'is-active': isActive('strike') }"
          @click="chain('toggleStrike')">
          <t-icon name="textformat-strikethrough" size="16px" />
        </button>
      </t-tooltip>
      <t-popup trigger="click" placement="bottom">
        <button type="button" class="rich-editor__btn" :class="{ 'is-active': isActive('highlight') }" :aria-label="t('richWriting.highlight')" :title="t('richWriting.highlight')"><t-icon name="highlight" size="16px" /></button>
        <template #content><div class="rich-editor__palette" @mousedown.prevent>
          <button v-for="color in highlightColors" :key="color" type="button" :style="{ background: color }" :aria-label="`${t('richWriting.highlight')} ${color}`" @click="setColor('highlight', color)" />
          <button type="button" class="rich-editor__palette-clear" @click="setColor('highlight', null)">{{ t('richWriting.clearColor') }}</button>
        </div></template>
      </t-popup>
      <t-popup trigger="click" placement="bottom">
        <button type="button" class="rich-editor__btn" :class="{ 'is-active': isActive('textColor') }" :aria-label="t('richWriting.textColor')" :title="t('richWriting.textColor')"><t-icon name="textformat-color" size="16px" /></button>
        <template #content><div class="rich-editor__palette" @mousedown.prevent>
          <button v-for="color in textColors" :key="color" type="button" :style="{ background: color }" :aria-label="`${t('richWriting.textColor')} ${color}`" @click="setColor('textColor', color)" />
          <button type="button" class="rich-editor__palette-clear" @click="setColor('textColor', null)">{{ t('richWriting.clearColor') }}</button>
        </div></template>
      </t-popup>
      <t-tooltip :content="t('richWriting.callout')" placement="bottom">
        <button type="button" class="rich-editor__btn" :class="{ 'is-active': isActive('callout') }" :aria-label="t('richWriting.callout')" @click="toggleCallout"><t-icon name="info-circle" size="16px" /></button>
      </t-tooltip>
      <t-tooltip :content="t('writingTools.inlineCode')" placement="bottom">
        <button type="button" class="rich-editor__btn" :class="{ 'is-active': isActive('code') }" @click="chain('toggleCode')"><t-icon name="code" size="16px" /></button>
      </t-tooltip>
      <t-tooltip :content="t('writingTools.clearFormatting')" placement="bottom">
        <button type="button" class="rich-editor__btn" @click="clearFormatting"><t-icon name="clear-formatting" size="16px" /></button>
      </t-tooltip>
      <t-tooltip :content="t('knowledgeBase.browse.editor.quote')" placement="bottom">
        <button type="button" class="rich-editor__btn" :class="{ 'is-active': isActive('blockquote') }"
          @click="chain('toggleBlockquote')">
          <t-icon name="quote" size="16px" />
        </button>
      </t-tooltip>
      <span class="rich-editor__divider" />

      <t-tooltip :content="t('knowledgeBase.browse.editor.bulletList')" placement="bottom">
        <button type="button" class="rich-editor__btn" :class="{ 'is-active': isActive('bulletList') }"
          @click="chain('toggleBulletList')">
          <t-icon name="bulletpoint" size="16px" />
        </button>
      </t-tooltip>
      <t-tooltip :content="t('knowledgeBase.browse.editor.orderedList')" placement="bottom">
        <button type="button" class="rich-editor__btn" :class="{ 'is-active': isActive('orderedList') }"
          @click="chain('toggleOrderedList')">
          <t-icon name="list-numbered" size="16px" />
        </button>
      </t-tooltip>
      <t-tooltip :content="t('knowledgeBase.browse.editor.taskList')" placement="bottom">
        <button type="button" class="rich-editor__btn" :class="{ 'is-active': isActive('taskList') }"
          @click="chain('toggleTaskList')">
          <t-icon name="task-checked" size="16px" />
        </button>
      </t-tooltip>
      <t-tooltip :content="t('knowledgeBase.browse.editor.codeBlock')" placement="bottom">
        <button type="button" class="rich-editor__btn" :class="{ 'is-active': isActive('codeBlock') }"
          @click="chain('toggleCodeBlock')">
          <t-icon name="code" size="16px" />
        </button>
      </t-tooltip>
      <t-tooltip :content="t('knowledgeBase.browse.editor.inlineMath')" placement="bottom">
        <button type="button" class="rich-editor__btn rich-editor__btn--text"
          :class="{ 'is-active': isActive('inlineMath') }" @click="insertInlineMath">
          $x
        </button>
      </t-tooltip>
      <t-tooltip :content="t('knowledgeBase.browse.editor.blockMath')" placement="bottom">
        <button type="button" class="rich-editor__btn rich-editor__btn--text"
          :class="{ 'is-active': isActive('blockMath') }" @click="insertBlockMath">
          ∑
        </button>
      </t-tooltip>
      <t-tooltip :content="t('knowledgeBase.browse.editor.diagram')" placement="bottom">
        <button type="button" class="rich-editor__btn" @click="insertMermaid">
          <t-icon name="chart" size="16px" />
        </button>
      </t-tooltip>
      <span class="rich-editor__divider" />

      <t-tooltip :content="t('knowledgeBase.browse.editor.table')" placement="bottom">
        <button type="button" class="rich-editor__btn" :class="{ 'is-active': isActive('table') }"
          @click="insertTable">
          <t-icon name="table" size="16px" />
        </button>
      </t-tooltip>
      <t-tooltip :content="t('knowledgeBase.browse.editor.horizontalRule')" placement="bottom">
        <button type="button" class="rich-editor__btn" @click="chain('setHorizontalRule')">
          <t-icon name="minus" size="16px" />
        </button>
      </t-tooltip>

      <t-popup trigger="click" placement="bottom" v-model="linkPopupVisible">
        <t-tooltip :content="t('knowledgeBase.browse.editor.link')" placement="bottom">
          <button type="button" class="rich-editor__btn" :class="{ 'is-active': isActive('link') }">
            <t-icon name="link" size="16px" />
          </button>
        </t-tooltip>
        <template #content>
          <div class="rich-editor__link-pop" @keydown.enter.prevent="confirmLink">
            <t-input v-model="linkUrl" size="small" clearable
              :placeholder="t('knowledgeBase.browse.editor.linkPlaceholder')" />
            <t-button size="small" theme="primary" @click="confirmLink">
              {{ t('knowledgeBase.browse.editor.linkConfirm') }}
            </t-button>
          </div>
        </template>
      </t-popup>

      <t-tooltip :content="t('documentDiscovery.insertLink')" placement="bottom">
        <button type="button" class="rich-editor__btn" :aria-label="t('documentDiscovery.insertLink')" @click="openDocumentPicker"><t-icon name="file-add" size="16px" /></button>
      </t-tooltip>
      <t-tooltip :content="t('knowledgeBase.browse.editor.image')" placement="bottom">
        <button type="button" class="rich-editor__btn" :disabled="imageUploading" @click="pickImage">
          <t-icon :name="imageUploading ? 'loading' : 'image'" size="16px" />
        </button>
      </t-tooltip>
      <span class="rich-editor__divider" />
      <t-tooltip :content="t('writingTools.findReplace')" placement="bottom">
        <button type="button" class="rich-editor__btn" :class="{ 'is-active': searchOpen }" :aria-label="t('writingTools.findReplace')" @click="openSearch"><t-icon name="search" size="16px" /></button>
      </t-tooltip>
    </div>

    <div v-if="isActive('table')" class="rich-editor__context" @mousedown="preserveSelection">
      <span>{{ t('writingTools.tableTools') }}</span>
      <button v-for="action in tableActions" :key="action.key" type="button" :class="{ 'is-danger': action.key === 'deleteTable' }"
        :disabled="!canTableCommand(action.key)" @click="tableCommand(action.key)">{{ action.label }}</button>
    </div>
    <p v-if="isActive('table')" class="rich-editor__table-hint">{{ t('richWriting.tablePasteHint') }}</p>
    <div v-if="isActive('callout')" class="rich-editor__context" @mousedown="preserveSelection">
      <span>{{ t('richWriting.callout') }}</span>
      <button v-for="kind in calloutKinds" :key="kind" type="button" @click="editor?.chain().focus().updateAttributes('callout', { kind }).run()">{{ t(`richWriting.${kind}`) }}</button>
      <button type="button" @click="editor?.chain().focus().lift('callout').run()">{{ t('richWriting.unwrapCallout') }}</button>
    </div>
    <div v-if="isActive('codeBlock')" class="rich-editor__context">
      <span>{{ t('writingTools.codeLanguage') }}</span>
      <select class="rich-editor__select" :value="codeLanguage" :aria-label="t('writingTools.codeLanguage')" @change="changeCodeLanguage">
        <option v-for="language in codeLanguages" :key="language" :value="language">{{ language || t('writingTools.plainText') }}</option>
      </select>
    </div>
    <div v-if="isActive('image')" class="rich-editor__context" @mousedown="preserveSelection">
      <span>{{ t('writingTools.imageTools') }}</span>
      <button v-for="align in imageAlignments" :key="align" type="button" :class="{ 'is-active': selectedImageAlignment === align }"
        :aria-pressed="selectedImageAlignment === align" @click="updateImageLayout({ align })">{{ t(`richWriting.imageAlign${align === 'left' ? 'Left' : align === 'center' ? 'Center' : 'Right'}`) }}</button>
      <label class="rich-editor__image-width">{{ t('richWriting.imageWidth') }}
        <input type="number" min="1" max="4096" step="1" :value="selectedImageWidth ?? ''" :placeholder="t('richWriting.imageAutoSize')"
          :aria-label="t('richWriting.imageWidth')" @change="changeImageWidth" @keydown.enter.prevent="($event.target as HTMLInputElement).blur()" />px
      </label>
      <button type="button" @click="updateImageLayout({ width: null, height: null })">{{ t('richWriting.imageOriginalSize') }}</button>
      <button type="button" @click="editImage">{{ t('writingTools.imageDescription') }}</button>
      <button type="button" class="is-danger" @click="editor?.chain().focus().deleteSelection().run()">{{ t('writingTools.removeImage') }}</button>
    </div>
    <div v-if="searchOpen" class="rich-editor__search" @keydown.esc.stop="closeSearch">
      <input ref="searchInput" v-model="searchText" :placeholder="t('writingTools.find')" :aria-label="t('writingTools.find')"
        @keydown.enter.prevent="navigateMatch($event.shiftKey ? -1 : 1)" />
      <span>{{ matchCount ? activeMatch + 1 : 0 }} / {{ matchCount }}</span>
      <button type="button" :disabled="!matchCount" :aria-label="t('writingTools.previousMatch')" @click="navigateMatch(-1)">↑</button>
      <button type="button" :disabled="!matchCount" :aria-label="t('writingTools.nextMatch')" @click="navigateMatch(1)">↓</button>
      <input v-model="replacement" :placeholder="t('writingTools.replaceWith')" :aria-label="t('writingTools.replaceWith')" />
      <button type="button" :disabled="!matchCount" @click="replaceMatch">{{ t('writingTools.replace') }}</button>
      <button type="button" :disabled="!matchCount" @click="replaceAll">{{ t('writingTools.replaceAll') }}</button>
      <button type="button" :aria-label="t('common.close')" @click="closeSearch">×</button>
    </div>

    <editor-content :editor="editor" class="rich-editor__content" />
    <footer class="rich-editor__status">
      <span>{{ imageUploading ? t('writingTools.uploadingImage') : t('writingTools.slashHint') }}</span>
      <span>{{ t('writingTools.characterCount', { count: characterCount }) }}</span>
    </footer>
    <Teleport to="body">
      <div v-if="slashOpen && !disabled" class="writing-menu writing-menu--slash" :style="slashStyle" role="listbox" :aria-label="t('writingTools.insertBlock')" @mousedown.prevent>
        <div class="writing-menu__title">{{ t('writingTools.insertBlock') }} <small>↑ ↓ · Enter · Esc</small></div>
        <button v-for="(item, index) in filteredCommands" :key="item.id" type="button" role="option" :aria-selected="index === slashIndex"
          :class="{ active: index === slashIndex }" @mouseenter="slashIndex = index" @click="executeSlash(item.id)">
          <t-icon :name="item.icon" size="17px" /><span>{{ item.label }}</span><small>{{ item.shortcut }}</small>
        </button>
        <p v-if="!filteredCommands.length">{{ t('writingTools.noCommands') }}</p>
      </div>
      <div v-if="bubbleVisible && !disabled" class="writing-menu writing-menu--bubble" :style="bubbleStyle" @mousedown.prevent>
        <button v-for="item in bubbleActions" :key="item.command" type="button" :title="item.label" :aria-label="item.label"
          :class="{ active: isActive(item.mark) }" @click="chain(item.command)"><t-icon :name="item.icon" size="16px" /></button>
        <button type="button" :title="t('knowledgeBase.browse.editor.link')" :aria-label="t('knowledgeBase.browse.editor.link')" @click="linkPopupVisible = true"><t-icon name="link" size="16px" /></button>
      </div>
    </Teleport>
    <input ref="imageInputRef" type="file" accept="image/png,image/jpeg,image/gif,image/webp,image/bmp,image/svg+xml,image/avif"
      style="display: none" multiple @change="onImagePicked" />

    <t-dialog v-if="imageEdit.visible" :visible="true" :header="t('writingTools.imageDescription')" :footer="false" width="480px" @close="imageEdit.visible = false">
      <t-input v-model="imageEdit.alt" :placeholder="t('writingTools.imageDescriptionHint')" @keydown.enter.prevent="confirmImageEdit" />
      <div class="rich-editor__math-actions"><t-button variant="outline" @click="imageEdit.visible = false">{{ t('common.cancel') }}</t-button><t-button theme="primary" @click="confirmImageEdit">{{ t('common.confirm') }}</t-button></div>
    </t-dialog>

    <!-- 点击已渲染的公式 → 编辑 LaTeX 源码。用 v-if 控制挂载而不是
         v-model 的动画关闭：连续编辑两个公式时（上一个还在关闭动画中），
         tdesign 的动画状态机会把 .t-dialog__wrap 的 visibility 卡在
         visible，透明层残留拦截住编辑器的保存按钮。v-if 关闭即卸载 DOM，
         没有可中断的动画。 -->
    <DocumentSearch v-model:visible="documentPickerOpen" :kb-id="kbId" picking @select="insertDocumentLink" />
    <t-dialog
      v-if="mathEdit.visible"
      :visible="true"
      :header="t('knowledgeBase.browse.editor.mathEditTitle')"
      :footer="false"
      width="520px"
      @confirm="confirmMathEdit"
      @close="mathEdit.visible = false"
    >
      <t-textarea
        v-model="mathEdit.latex"
        :autosize="{ minRows: 3, maxRows: 8 }"
        :placeholder="t('knowledgeBase.browse.editor.mathEditPlaceholder')"
        class="rich-editor__math-input"
      />
      <div class="rich-editor__math-actions">
        <t-button size="small" @click="mathEdit.visible = false">
          {{ t('common.cancel') }}
        </t-button>
        <t-button size="small" theme="primary" @click="confirmMathEdit">
          {{ t('common.confirm') }}
        </t-button>
      </div>
    </t-dialog>
  </div>
</template>

<script setup lang="ts">
import { computed, defineAsyncComponent, nextTick, reactive, ref, watch, onMounted, onBeforeUnmount } from 'vue'
import { useI18n } from 'vue-i18n'
import { MessagePlugin } from 'tdesign-vue-next'
import { useEditor, EditorContent } from '@tiptap/vue-3'
import { NodeSelection, TextSelection, type SelectionBookmark } from '@tiptap/pm/state'
import { closeHistory } from '@tiptap/pm/history'
import { StarterKit } from '@tiptap/starter-kit'
import { TaskList } from '@tiptap/extension-list'
import { TableRow, TableCell, TableHeader } from '@tiptap/extension-table'
import { Markdown } from 'tiptap-markdown'
import 'katex/dist/katex.min.css'
import { uploadContentImage, type DocumentSearchItem } from '@/api/knowledge-base'
import { documentLink } from '@/views/documents/documentLinks'
import { MermaidCodeBlock } from './MermaidCodeBlock'
import { buildBlockMathExtension, buildInlineMathExtension } from './editorMathExtensions'
import { createEditorToolkit, editorToolkitKey, slashQuery } from './editorToolkit'
import { createEditorImage } from './editorImage'
import { createEditorOutline, jumpToEditorHeading } from './editorOutline'
import type { OutlineItem } from './types'
import { imageAlignment, imageDimension, type ImageAlignment } from './imageLayout'
import { Callout, Highlight, RichTable, RichTaskItem, TextColor } from './richDocumentExtensions'
import { RichClipboard } from './editorClipboard'
import { documentMarkdown, sameDocument, type StructuredDocument } from './structuredDocument'
import './richDocument.less'
import { hydrateProtectedFileImages } from '@/utils/security'

const DocumentSearch = defineAsyncComponent(() => import('@/views/documents/DocumentSearch.vue'))
const documentPickerOpen = ref(false)
let documentBookmark: SelectionBookmark | null = null
function openDocumentPicker() {
  const e = editor.value
  if (!e || props.disabled) return
  documentBookmark = e.state.selection.getBookmark()
  documentPickerOpen.value = true
}
function insertDocumentLink(item: DocumentSearchItem) {
  const e = editor.value
  if (!e || e.isDestroyed || props.disabled || !documentBookmark) return
  try {
    const selection = documentBookmark.resolve(e.state.doc)
    const href = documentLink(item.id)
    documentBookmark = null
    if (selection.empty) {
      e.chain().focus().insertContentAt(selection.from, [
        { type: 'text', text: item.title || item.id, marks: [{ type: 'link', attrs: { href } }] },
        { type: 'text', text: ' ' },
      ]).run()
    } else {
      e.chain().focus().setTextSelection({ from: selection.from, to: selection.to }).setLink({ href }).run()
    }
  } catch { MessagePlugin.error(t('documentDiscovery.insertFailed')) }
}
watch(documentPickerOpen, visible => { if (!visible) documentBookmark = null })

const props = defineProps<{
  modelValue: string
  document?: StructuredDocument | null
  kbId: string
  placeholder?: string
  disabled?: boolean
}>()

const emit = defineEmits<{
  'outline-change': [items: OutlineItem[]]
  'outline-active': [index: number]
  'update:modelValue': [value: string]
  'busy-change': [busy: boolean]
  'content-change': [value: { content: string; document: StructuredDocument }]
  'content-error': []
}>()

const { t } = useI18n()

// ---------- 公式 / 图表 ----------

// 点击已渲染公式 → 弹窗改 LaTeX 源码（pos 固定，update 就地替换 attrs）。
const mathEdit = reactive({
  visible: false,
  kind: 'inline' as 'inline' | 'block',
  latex: '',
  pos: 0,
})

const openMathEditor = (kind: 'inline' | 'block', latex: string, pos: number) => {
  mathEdit.kind = kind
  mathEdit.latex = latex
  mathEdit.pos = pos
  mathEdit.visible = true
}

const confirmMathEdit = () => {
  const e = editor.value
  if (!e || !mathEdit.latex.trim()) {
    mathEdit.visible = false
    return
  }
  if (mathEdit.kind === 'block') {
    e.chain().focus().updateBlockMath({ pos: mathEdit.pos, latex: mathEdit.latex }).run()
  } else {
    e.chain().focus().updateInlineMath({ pos: mathEdit.pos, latex: mathEdit.latex }).run()
  }
  mathEdit.visible = false
}

// 工具栏 active 状态依赖编辑器选区；tiptap 本身非响应式，用 nonce 触发重渲染。
const stateNonce = ref(0)

const rootRef = ref<HTMLElement | null>(null)
const searchInput = ref<HTMLInputElement | null>(null)
const searchOpen = ref(false)
const searchText = ref('')
const replacement = ref('')
const slashIndex = ref(0)
const slashDismissed = ref(false)
const slashStyle = ref<Record<string, string>>({})
const bubbleStyle = ref<Record<string, string>>({})
const bubbleVisible = ref(false)
let menuFrame = 0
let disposed = false
const currentBlock = computed(() => {
  void stateNonce.value
  const level = editor.value?.getAttributes('heading').level
  return level ? `heading${level}` : 'paragraph'
})
const characterCount = computed(() => { void stateNonce.value; return Array.from(editor.value?.getText().replace(/\s/g, '') || '').length })
const slash = computed(() => { void stateNonce.value; return editor.value ? slashQuery(editor.value.state) : null })
const commandItems = computed(() => [
  { id: 'paragraph', icon: 'text', label: t('writingTools.paragraph'), keywords: 'text zhengwen 正文', shortcut: '' },
  ...[1, 2, 3, 4].map(level => ({ id: `heading${level}`, icon: 'textformat', label: t(`writingTools.heading${level}`), keywords: `h${level} heading biaoti 标题`, shortcut: `H${level}` })),
  { id: 'bulletList', icon: 'bulletpoint', label: t('knowledgeBase.browse.editor.bulletList'), keywords: 'bullet list w uxuliebiao 无序列表', shortcut: '- ' },
  { id: 'orderedList', icon: 'order-ascending', label: t('knowledgeBase.browse.editor.orderedList'), keywords: 'ordered list youxu 有序列表', shortcut: '1. ' },
  { id: 'taskList', icon: 'task-checked', label: t('knowledgeBase.browse.editor.taskList'), keywords: 'todo task daiban 待办', shortcut: '[] ' },
  { id: 'blockquote', icon: 'quote', label: t('knowledgeBase.browse.editor.quote'), keywords: 'quote yinyong 引用', shortcut: '> ' },
  { id: 'codeBlock', icon: 'code', label: t('knowledgeBase.browse.editor.codeBlock'), keywords: 'code daima 代码', shortcut: '```' },
  { id: 'callout', icon: 'info-circle', label: t('richWriting.callout'), keywords: 'callout info tip warning 提示块 警告 提示', shortcut: '' },
  { id: 'table', icon: 'table', label: t('knowledgeBase.browse.editor.table'), keywords: 'table biaoge 表格', shortcut: '' },
  { id: 'documentLink', icon: 'file-add', label: t('documentDiscovery.insertLink'), keywords: 'document link wendang 文档 链接 引用', shortcut: '' },
  { id: 'image', icon: 'image', label: t('knowledgeBase.browse.editor.image'), keywords: 'image tupian 图片', shortcut: '' },
  { id: 'horizontalRule', icon: 'minus', label: t('knowledgeBase.browse.editor.horizontalRule'), keywords: 'divider rule fengexian 分割线', shortcut: '---' },
  { id: 'blockMath', icon: 'function', label: t('knowledgeBase.browse.editor.blockMath'), keywords: 'math formula gongshi 公式', shortcut: '$$' },
  { id: 'mermaid', icon: 'chart', label: t('knowledgeBase.browse.editor.diagram'), keywords: 'mermaid diagram liuchengtu 流程图', shortcut: '' },
])
const filteredCommands = computed(() => {
  const query = slash.value?.query.toLocaleLowerCase() || ''
  return commandItems.value.filter(item => `${item.label} ${item.keywords}`.toLocaleLowerCase().includes(query))
})
const slashOpen = computed(() => !!slash.value && !slashDismissed.value && !!editor.value?.isFocused)
const bubbleActions = computed(() => [
  { command: 'toggleBold', mark: 'bold', icon: 'textformat-bold', label: t('knowledgeBase.browse.editor.bold') },
  { command: 'toggleItalic', mark: 'italic', icon: 'textformat-italic', label: t('knowledgeBase.browse.editor.italic') },
  { command: 'toggleStrike', mark: 'strike', icon: 'textformat-strikethrough', label: t('knowledgeBase.browse.editor.strikethrough') },
  { command: 'toggleCode', mark: 'code', icon: 'code', label: t('writingTools.inlineCode') },
] as const)
const tableActions = computed(() => [
  { key: 'addRowBefore', label: t('writingTools.rowAbove') },
  { key: 'addRowAfter', label: t('writingTools.rowBelow') },
  { key: 'addColumnBefore', label: t('writingTools.columnLeft') },
  { key: 'addColumnAfter', label: t('writingTools.columnRight') },
  { key: 'mergeCells', label: t('richWriting.mergeCells') },
  { key: 'splitCell', label: t('richWriting.splitCell') },
  { key: 'toggleHeaderRow', label: t('richWriting.headerRow') },
  { key: 'deleteRow', label: t('writingTools.deleteRow') },
  { key: 'deleteColumn', label: t('writingTools.deleteColumn') },
  { key: 'deleteTable', label: t('writingTools.deleteTable') },
] as const)
const highlightColors = ['#fff3a3', '#c8f7c5', '#bae0ff', '#ffd6e7', '#e4d7ff']
const textColors = ['#d4380d', '#d48806', '#389e0d', '#1677ff', '#722ed1', '#595959']
const calloutKinds = ['info', 'tip', 'warning', 'danger']
function setColor(mark: 'highlight' | 'textColor', color: string | null) {
  const e = editor.value
  if (color) e?.chain().focus().setMark(mark, { color }).run()
  else e?.chain().focus().unsetMark(mark).run()
}
function toggleCallout() {
  const e = editor.value
  if (!e) return
  if (e.isActive('callout')) e.chain().focus().lift('callout').run()
  else if (e.can().wrapIn('callout')) e.chain().focus().wrapIn('callout', { kind: 'info' }).run()
}
const codeLanguages = ['', 'plaintext', 'javascript', 'typescript', 'python', 'go', 'java', 'bash', 'sql', 'json', 'yaml', 'html', 'css', 'rust', 'cpp', 'mermaid']
const codeLanguage = computed(() => { void stateNonce.value; return editor.value?.getAttributes('codeBlock').language || '' })
const imageEdit = reactive({ visible: false, alt: '', pos: 0 })
const imageAlignments: ImageAlignment[] = ['left', 'center', 'right']
const selectedImageWidth = computed(() => { void stateNonce.value; return imageDimension(editor.value?.getAttributes('image').width) })
const selectedImageAlignment = computed(() => { void stateNonce.value; return imageAlignment(editor.value?.getAttributes('image').align) })
const matchCount = computed(() => { void stateNonce.value; return editor.value ? editorToolkitKey.getState(editor.value.state)?.matches.length || 0 : 0 })
const activeMatch = computed(() => { void stateNonce.value; return editor.value ? editorToolkitKey.getState(editor.value.state)?.active || 0 : 0 })

function preserveSelection(event: MouseEvent) {
  if ((event.target as HTMLElement).closest('button')) event.preventDefault()
}
function changeBlock(event: Event) {
  const value = (event.target as HTMLSelectElement).value
  if (value === 'paragraph') editor.value?.chain().focus().setParagraph().run()
  else editor.value?.chain().focus().setHeading({ level: Number(value.slice(-1)) as 1 | 2 | 3 | 4 }).run()
}
function changeCodeLanguage(event: Event) {
  editor.value?.chain().focus().updateAttributes('codeBlock', { language: (event.target as HTMLSelectElement).value || null }).run()
}
function clearFormatting() { editor.value?.chain().focus().unsetAllMarks().clearNodes().run() }
function canTableCommand(command: typeof tableActions.value[number]['key']) {
  void stateNonce.value
  return editor.value?.can()[command]() ?? false
}
function tableCommand(command: typeof tableActions.value[number]['key']) {
  const e = editor.value
  if (!e) return
  e.chain().focus()[command]().run()

}
function editImage() {
  const e = editor.value
  if (!e || !(e.state.selection instanceof NodeSelection) || e.state.selection.node.type.name !== 'image') return
  imageEdit.pos = e.state.selection.from
  imageEdit.alt = e.state.selection.node.attrs.alt || ''
  imageEdit.visible = true
}
function updateImageLayout(patch: { width?: number | null; height?: null; align?: ImageAlignment }) {
  const e = editor.value
  if (!e?.isEditable || !(e.state.selection instanceof NodeSelection) || e.state.selection.node.type.name !== 'image') return
  const { from, node } = e.state.selection
  e.view.dispatch(closeHistory(e.state.tr.setNodeMarkup(from, undefined, { ...node.attrs, ...patch })))
  e.view.dispatch(closeHistory(e.state.tr))
  e.commands.focus()
}
function changeImageWidth(event: Event) {
  const input = event.target as HTMLInputElement
  const width = input.value === '' ? null : imageDimension(Number(input.value))
  if (input.value !== '' && width == null) {
    MessagePlugin.warning(t('richWriting.imageWidthHint')); input.value = String(selectedImageWidth.value ?? ''); return
  }
  updateImageLayout({ width, height: null })
}
function confirmImageEdit() {
  const e = editor.value
  const node = e?.state.doc.nodeAt(imageEdit.pos)
  if (e && node?.type.name === 'image') e.view.dispatch(e.state.tr.setNodeMarkup(imageEdit.pos, undefined, { ...node.attrs, alt: imageEdit.alt }))
  imageEdit.visible = false
}
function executeSlash(id: string) {
  const e = editor.value
  const range = slash.value
  if (!e || !range) return
  e.chain().focus().deleteRange({ from: range.from, to: range.to }).run()
  slashDismissed.value = true
  if (id.startsWith('heading')) e.chain().focus().setHeading({ level: Number(id.slice(-1)) as 1 | 2 | 3 | 4 }).run()
  else if (id === 'paragraph') e.chain().focus().setParagraph().run()
  else if (id === 'bulletList') chain('toggleBulletList')
  else if (id === 'orderedList') chain('toggleOrderedList')
  else if (id === 'taskList') chain('toggleTaskList')
  else if (id === 'blockquote') chain('toggleBlockquote')
  else if (id === 'codeBlock') chain('toggleCodeBlock')
  else if (id === 'horizontalRule') chain('setHorizontalRule')
  else if (id === 'callout') toggleCallout()
  else if (id === 'table') insertTable()
  else if (id === 'image') pickImage()
  else if (id === 'documentLink') openDocumentPicker()
  else if (id === 'blockMath') insertBlockMath()
  else if (id === 'mermaid') insertMermaid()
}
function handleEditorKeydown(event: KeyboardEvent): boolean {
  if ((event.ctrlKey || event.metaKey) && event.key.toLowerCase() === 'f') {
    event.preventDefault(); void openSearch(); return true
  }
  if (!slashOpen.value) return false
  if (event.key === 'Escape') { slashDismissed.value = true; return true }
  const count = filteredCommands.value.length
  if ((event.key === 'ArrowDown' || event.key === 'ArrowUp') && count) {
    slashIndex.value = (slashIndex.value + (event.key === 'ArrowDown' ? 1 : -1) + count) % count
    nextTick(() => document.querySelector('.writing-menu--slash [aria-selected="true"]')?.scrollIntoView({ block: 'nearest' }))
    return true
  }
  if (event.key === 'Enter' && count) { executeSlash(filteredCommands.value[slashIndex.value % count]!.id); return true }
  return false
}
function scheduleMenus() {
  if (disposed || menuFrame) return
  menuFrame = requestAnimationFrame(() => {
    menuFrame = 0
    const e = editor.value
    if (!e || e.isDestroyed || !e.view.dom.isConnected) return
    const { from, to, empty } = e.state.selection
    const coords = e.view.coordsAtPos(from)
    const bounds = e.view.dom.closest('.rich-editor__content')?.getBoundingClientRect()
    const visible = !bounds || (coords.bottom >= bounds.top && coords.top <= bounds.bottom)
    slashStyle.value = { left: `${Math.max(8, Math.min(coords.left, window.innerWidth - 296))}px`,
      top: `${Math.max(8, Math.min(coords.bottom + 6, window.innerHeight - 334))}px` }
    bubbleVisible.value = !empty && e.isFocused && visible && !isActive('codeBlock') && !isActive('image') &&
      e.state.selection instanceof TextSelection && !!e.state.doc.textBetween(from, to).trim()
    bubbleStyle.value = { left: `${Math.max(8, Math.min(coords.left, window.innerWidth - 220))}px`, top: `${Math.max(8, coords.top - 44)}px` }
    if (slashOpen.value && !visible) slashDismissed.value = true
  })
}
async function openSearch() {
  searchOpen.value = true
  const e = editor.value
  if (e && !e.state.selection.empty) searchText.value = e.state.doc.textBetween(e.state.selection.from, e.state.selection.to, ' ')
  await nextTick()
  searchInput.value?.focus()
  searchInput.value?.select()
}
function closeSearch() {
  searchOpen.value = false
  searchText.value = ''
  editor.value?.commands.focus()
}
watch(searchText, query => {
  const e = editor.value
  if (e && !e.isDestroyed) e.view.dispatch(e.state.tr.setMeta(editorToolkitKey, { query, active: 0 }).setMeta('addToHistory', false))
})
function navigateMatch(direction: number) {
  const e = editor.value
  if (!e || !matchCount.value) return
  const active = (activeMatch.value + direction + matchCount.value) % matchCount.value
  const match = editorToolkitKey.getState(e.state)!.matches[active]!
  e.view.dispatch(e.state.tr.setMeta(editorToolkitKey, { active }).setSelection(TextSelection.create(e.state.doc, match.from, match.to)).scrollIntoView().setMeta('addToHistory', false))
}
function replaceMatch() {
  const e = editor.value
  if (!e) return
  const match = editorToolkitKey.getState(e.state)?.matches[activeMatch.value]
  if (match) e.view.dispatch(e.state.tr.insertText(replacement.value, match.from, match.to))
}
function replaceAll() {
  const e = editor.value
  if (!e) return
  const matches = editorToolkitKey.getState(e.state)?.matches || []
  const tr = e.state.tr
  for (const match of [...matches].reverse()) tr.insertText(replacement.value, match.from, match.to)
  if (tr.docChanged) e.view.dispatch(tr)
}
onMounted(() => {
  window.addEventListener('resize', scheduleMenus)
  window.addEventListener('scroll', scheduleMenus, true)
})
onBeforeUnmount(() => {
  disposed = true
  if (menuFrame) cancelAnimationFrame(menuFrame)
  window.removeEventListener('resize', scheduleMenus)
  window.removeEventListener('scroll', scheduleMenus, true)
})

const editor = useEditor({
  editable: !props.disabled,
  content: props.document?.content || props.modelValue,
  enableContentCheck: true,
  onContentError: () => { emit('content-error'); MessagePlugin.error(t('richWriting.unsupportedDocument')) },
  extensions: [
    createEditorOutline(items => emit('outline-change', items), index => emit('outline-active', index)),
    RichClipboard.configure({
      imageLabel: t('knowledgeBase.browse.editor.image'),
      onUnsupportedImage: () => MessagePlugin.warning(t('richWriting.pasteImageNeedsUpload')),
      onTablePasteRejected: problem => MessagePlugin.warning(t({
        selection: 'richWriting.tablePasteSelection', merged: 'richWriting.tablePasteMerged', limit: 'richWriting.tablePasteLimit',
      }[problem])),
    }),
    createEditorToolkit({
      placeholder: () => props.placeholder || t('writingTools.placeholder'),
      uploading: () => t('writingTools.uploadingImage'),
      onFiles: (files, pos) => { void uploadImages(files, pos) },
      onKeyDown: handleEditorKeydown,
    }),
    StarterKit.configure({
      heading: { levels: [1, 2, 3, 4] },
      link: { openOnClick: false },
      codeBlock: false, // 用带 mermaid NodeView 的版本替换
    }),
    MermaidCodeBlock.configure({
      parseErrorText: t('knowledgeBase.browse.editor.mermaidParseError'),
    }),
    buildBlockMathExtension(openMathEditor),
    buildInlineMathExtension(openMathEditor),
    TaskList,
    RichTaskItem.configure({ nested: true }),
    createEditorImage(root => { void hydrateProtectedFileImages(root, { mode: 'knowledgeBase', kbId: props.kbId }) }, { resize: t('richWriting.imageResize') }),
    Highlight, TextColor, Callout,
    RichTable.configure({ resizable: true, cellMinWidth: 60 }),
    TableRow,
    TableCell,
    TableHeader,
    Markdown.configure({
      html: false,
      transformPastedText: true,
      breaks: false,
    }),
  ],
  onBlur: () => { stateNonce.value++; scheduleMenus() },
  onFocus: () => { stateNonce.value++; scheduleMenus() },
  onUpdate: ({ editor: e }) => {
    const document: StructuredDocument = { schema_version: 1, content: e.getJSON() as StructuredDocument['content'] }
    const content = documentMarkdown(document)
    emit('content-change', { content, document })
    emit('update:modelValue', content)
  },
  onTransaction: ({ transaction }) => {
    if (documentBookmark && transaction.docChanged) documentBookmark = documentBookmark.map(transaction.mapping)
    stateNonce.value++
    scheduleMenus()
  },
})

// 外部重置内容（如取消编辑后重新进入）时同步回编辑器。
// watch evaluates its source during setup; register only after editor exists.
watch(() => slash.value?.query, () => { slashIndex.value = 0; slashDismissed.value = false })
watch(() => props.disabled, (disabled) => editor.value?.setEditable(!disabled))

watch([() => props.modelValue, () => props.document], ([value, document]) => {
  const e = editor.value
  if (!e) return
  const current: StructuredDocument = { schema_version: 1, content: e.getJSON() as StructuredDocument['content'] }
  if (document) {
    if (!sameDocument(document, current)) e.commands.setContent(document.content, { emitUpdate: false, errorOnInvalidContent: true })
  } else if (value !== documentMarkdown(current)) {
    e.commands.setContent(value || '', { emitUpdate: false })
  }
})

onBeforeUnmount(() => {
  editor.value?.destroy()
})

const isActive = (name: string, attrs?: Record<string, unknown>): boolean => {
  void stateNonce.value
  return editor.value?.isActive(name, attrs) ?? false
}

const chain = (command: 'toggleBold' | 'toggleItalic' | 'toggleStrike' | 'toggleBlockquote'
  | 'toggleBulletList' | 'toggleOrderedList' | 'toggleTaskList' | 'toggleCodeBlock' | 'setHorizontalRule' | 'toggleCode') => {
  editor.value?.chain().focus()[command]().run()
}

const run = (command: 'undo' | 'redo') => {
  editor.value?.chain().focus()[command]().run()
}

const insertTable = () => {
  editor.value?.chain().focus().insertTable({ rows: 3, cols: 3, withHeaderRow: true }).run()
}

// ---------- 公式 / 图表插入 ----------

// 选中文本时把它当 LaTeX 预填；空选区给占位公式，插入后点击可改。
const selectionLatex = (): string => {
  const e = editor.value
  if (!e) return ''
  const { from, to, empty } = e.state.selection
  if (empty) return ''
  return e.state.doc.textBetween(from, to, '\n').trim()
}

// 官方 insertInlineMath/insertBlockMath 插入 atom 节点后会把选区留在节点上
// （NodeSelection）——随后的 insertContent（如插 mermaid 块）会把这个节点
// 整体替换掉。插入后把光标移到节点后面，继续打字/插入都落在公式之后。
const moveSelectionPastNode = ({ tr, state }: any) => {
  if (state.selection instanceof NodeSelection) {
    tr.setSelection(TextSelection.create(state.doc, state.selection.to))
  }
  return true
}

const insertInlineMath = () => {
  const latex = selectionLatex() || 'E=mc^2'
  editor.value?.chain().focus().deleteSelection().insertInlineMath({ latex })
    .command(moveSelectionPastNode).run()
}

const insertBlockMath = () => {
  const latex = selectionLatex() || 'E=mc^2'
  editor.value?.chain().focus().deleteSelection().insertBlockMath({ latex })
    .command(moveSelectionPastNode).run()
}

const insertMermaid = () => {
  editor.value?.chain().focus().insertContent({
    type: 'codeBlock',
    attrs: { language: 'mermaid' },
    content: [{ type: 'text', text: 'flowchart TD\n  A --> B' }],
  }).run()
}

// ---------- 链接 ----------

const linkPopupVisible = ref(false)
const linkUrl = ref('')

watch(linkPopupVisible, (visible) => {
  if (visible) {
    linkUrl.value = (editor.value?.getAttributes('link').href as string) || ''
  }
})

const confirmLink = () => {
  const url = linkUrl.value.trim()
  const e = editor.value
  if (e) {
    if (!url) {
      e.chain().focus().extendMarkRange('link').unsetLink().run()
    } else {
      e.chain().focus().extendMarkRange('link').setLink({ href: url }).run()
    }
  }
  linkPopupVisible.value = false
}

// Upload placeholders track positions through intervening edits and undo.
const imageInputRef = ref<HTMLInputElement | null>(null)
const pendingUploads = ref(0)
const imageUploading = computed(() => pendingUploads.value > 0)
watch(imageUploading, busy => emit('busy-change', busy))
let uploadSequence = 0
let pickerPosition: number | null = null
const pickImage = () => {
  pickerPosition = editor.value?.state.selection.from ?? null
  imageInputRef.value?.click()
}
const onImagePicked = (event: Event) => {
  const input = event.target as HTMLInputElement
  const files = Array.from(input.files ?? [])
  input.value = ''
  void uploadImages(files, pickerPosition ?? editor.value?.state.selection.from ?? 1)
}
async function uploadImages(files: File[], position: number) {
  const e = editor.value
  if (!e || e.isDestroyed || props.disabled) return
  const valid = files.filter(file => /^image\/(png|jpeg|gif|webp|bmp|svg\+xml|avif)$/.test(file.type) && file.size > 0 && file.size <= 10 * 1024 * 1024)
  if (valid.length !== files.length) MessagePlugin.warning(t('writingTools.imageFileHint'))
  if (!valid.length) return
  // Register all anchors before starting network I/O, preserving the input order.
  const jobs = valid.map(file => ({ file, id: `upload-${++uploadSequence}` }))
  for (const job of jobs) e.view.dispatch(e.state.tr.setMeta(editorToolkitKey, { addUpload: { id: job.id, pos: position } }).setMeta('addToHistory', false))
  pendingUploads.value += jobs.length
  for (const { file, id } of jobs) {
    try {
      const result: any = await uploadContentImage(props.kbId, file)
      if (!result?.success || !result.data?.url) throw new Error()
      if (e.isDestroyed) continue
      const pos = editorToolkitKey.getState(e.state)?.uploads.get(id)
      if (pos === undefined) continue
      const node = e.schema.nodes.image!.create({ src: result.data.url, alt: file.name })
      const tr = e.state.tr.replaceWith(pos, pos, node).setMeta(editorToolkitKey, { removeUpload: id })
      e.view.dispatch(tr)
    } catch {
      if (!e.isDestroyed) MessagePlugin.error(t('knowledgeBase.browse.editor.imageUploadFailed'))
    } finally {
      if (!e.isDestroyed) e.view.dispatch(e.state.tr.setMeta(editorToolkitKey, { removeUpload: id }).setMeta('addToHistory', false))
      pendingUploads.value--
    }
  }
}
function snapshot() {
  const e = editor.value
  if (!e || e.isDestroyed) return null
  const document: StructuredDocument = { schema_version: 1, content: e.getJSON() as StructuredDocument['content'] }
  return { document, content: documentMarkdown(document) }
}
function scrollToHeading(index: number) { jumpToEditorHeading(editor.value, index) }
defineExpose({ snapshot, scrollToHeading })
</script>

<style lang="less">
.writing-menu {
  position: fixed; z-index: 2500; background: var(--td-bg-color-container); color: var(--td-text-color-primary);
  border: 1px solid var(--td-component-stroke); border-radius: var(--app-radius-md); box-shadow: var(--td-shadow-2); padding: 5px;
  button { font: inherit; border: 0; background: transparent; color: inherit; cursor: pointer; border-radius: var(--app-radius-xs);
    &:hover, &.active { color: var(--td-brand-color); background: var(--td-brand-color-light); }
  }
  small { color: var(--td-text-color-placeholder); font-size: var(--app-text-xs); }
}
.writing-menu--slash { width: 280px; max-height: 320px; overflow-y: auto;
  button { display: flex; gap: 10px; align-items: center; width: 100%; padding: 9px 10px; font-size: var(--app-text-md); text-align: left;
    span { flex: 1; }
  }
  p { padding: 12px; font-size: var(--app-text-md); color: var(--td-text-color-secondary); }
}
.writing-menu__title { display: flex; justify-content: space-between; gap: 12px; padding: 8px 10px; font-size: var(--app-text-sm); color: var(--td-text-color-secondary); }
.writing-menu--bubble { display: flex; gap: 3px; button { display: grid; place-items: center; height: 30px; width: 32px; } }
</style>

<style scoped lang="less">
.rich-editor__palette { display: flex; gap: 6px; align-items: center; padding: 6px;
  button { height: 24px; width: 24px; border: 1px solid var(--td-component-stroke); border-radius: var(--app-radius-xs); cursor: pointer; }
  button.rich-editor__palette-clear { width: auto; background: transparent; color: var(--td-text-color-primary); font-size: var(--app-text-sm); }
}
.rich-editor__select { max-width: 180px; background: var(--td-bg-color-container); color: var(--td-text-color-primary);
  border: 1px solid var(--td-component-stroke); border-radius: var(--app-radius-xs); padding: 4px 8px; font: inherit; font-size: var(--app-text-sm); height: 30px;
}
.rich-editor__context, .rich-editor__search { display: flex; flex-wrap: wrap; align-items: center; gap: 5px; padding: 6px 12px;
  border-bottom: 1px solid var(--td-component-stroke); background: var(--td-bg-color-container); font-size: var(--app-text-sm);
  > span { color: var(--td-text-color-secondary); margin-right: 6px; }
  button { font: inherit; border: 0; background: transparent; color: var(--td-text-color-primary); border-radius: var(--app-radius-xs); padding: 5px 7px; cursor: pointer;
    &:hover:not(:disabled) { background: var(--td-brand-color-light); color: var(--td-brand-color); }
    &:disabled { opacity: .4; cursor: default; }
    &.is-danger { color: var(--td-error-color); }
  }
  input { width: 160px; font: inherit; padding: 5px 8px; border: 1px solid var(--td-component-stroke);
    border-radius: var(--app-radius-xs); color: var(--td-text-color-primary); background: var(--td-bg-color-container); }
}
.rich-editor__table-hint { margin: 0; padding: 4px 12px; color: var(--td-text-color-secondary); font-size: var(--app-text-xs); }
.rich-editor__image-width { display: inline-flex; align-items: center; gap: 4px;
  input { width: 68px; font: inherit; padding: 3px; color: var(--td-text-color-primary); background: var(--td-bg-color-container); border: 1px solid var(--td-component-stroke); border-radius: var(--app-radius-xs); }
}
.rich-editor__context button[aria-pressed='true'] { color: var(--td-brand-color); background: var(--td-brand-color-light); }
.rich-editor__status { display: flex; justify-content: space-between; gap: 12px; padding: 8px 14px;
  border-top: 1px solid var(--td-component-stroke); color: var(--td-text-color-placeholder); font-size: var(--app-text-xs);
}
.rich-editor__content :deep(.ProseMirror) {
  p.is-editor-empty::before { content: attr(data-placeholder); float: left; height: 0; color: var(--td-text-color-placeholder); pointer-events: none; }
  .editor-search-match { background: var(--td-warning-color-light); &.is-current { outline: 2px solid var(--td-warning-color); } }
  .editor-image-uploading { display: inline-block; padding: 12px; border: 1px dashed var(--td-component-stroke); color: var(--td-text-color-secondary); }
  .editor-image { display: flex; justify-content: flex-start; margin: 12px 0;
    &[data-align='center'] { justify-content: center; }
    &[data-align='right'] { justify-content: flex-end; .editor-image__resize { left: 0; right: auto; cursor: nesw-resize; } }
    &.is-resizing { user-select: none; }
  }
  .editor-image__frame { display: block; position: relative; max-width: 100%; line-height: 0;
    img { display: block; margin: 0; }
  }
  .editor-image__resize { position: absolute; bottom: 0; right: 0; width: 14px; height: 14px; padding: 0;
    background: var(--td-brand-color); border: 2px solid var(--td-bg-color-container); cursor: nwse-resize; touch-action: none;
    &:focus-visible { outline: 2px solid var(--app-focus-border); outline-offset: 2px; }
    &[hidden] { display: none; }
  }
  &[contenteditable='false'] .editor-image__resize { display: none; }
  .column-resize-handle { position: absolute; top: 0; right: -2px; bottom: 0; width: 4px; background: var(--td-brand-color); pointer-events: none; }
  &.resize-cursor { cursor: col-resize; }
  .tableWrapper { overflow-x: auto; }
  table td, table th { position: relative; min-width: 60px; }
  .selectedCell { position: relative; &::after { position: absolute; inset: 0; background: var(--td-brand-color-light); opacity: .5; pointer-events: none; content: ''; } }
}

.rich-editor {
  display: flex;
  flex-direction: column;
  min-height: 0;
  flex: 1;
  border: 1px solid var(--td-component-stroke);
  border-radius: var(--app-radius-md);
  background: var(--td-bg-color-container);
  overflow: hidden;
}

.rich-editor__toolbar {
  flex-shrink: 0;
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 2px;
  padding: 6px 8px;
  border-bottom: 1px solid var(--td-component-stroke);
  background: var(--td-bg-color-secondarycontainer);
}

.rich-editor__btn {
  width: 28px;
  height: 28px;
  padding: 0;
  border: 0;
  border-radius: var(--app-radius-sm);
  background: transparent;
  color: var(--td-text-color-secondary);
  display: inline-flex;
  align-items: center;
  justify-content: center;
  cursor: pointer;
  transition: background-color var(--app-motion-fast) ease, color var(--app-motion-fast) ease;

  &:hover:not(:disabled) {
    color: var(--td-brand-color);
    background: var(--td-bg-color-container-hover);
  }

  &.is-active {
    color: var(--td-brand-color);
    background: var(--td-brand-color-light);
  }

  &:disabled {
    cursor: not-allowed;
    opacity: 0.5;
  }

  &--text {
    font-size: var(--app-text-xs);
    font-weight: 600;
  }
}

.rich-editor__divider {
  width: 1px;
  height: 16px;
  margin: 0 6px;
  background: var(--td-component-stroke);
}

.rich-editor__link-pop {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 8px;
  width: 320px;
}

.rich-editor__math-input {
  :deep(textarea) {
    font-family: 'SF Mono', Consolas, Menlo, monospace;
  }
}

.rich-editor__math-actions {
  display: flex;
  justify-content: flex-end;
  gap: 8px;
  margin-top: 12px;
}

.rich-editor__content {
  flex: 1;
  min-height: 0;
  overflow-y: auto;
  padding: 16px 20px 40px;

  :deep(.ProseMirror) {
    outline: none;
    min-height: 320px;
    font-size: var(--app-text-md);
    line-height: 1.75;
    color: var(--td-text-color-primary);

    h1, h2, h3, h4 {
      margin: 1em 0 0.5em;
      font-weight: 600;
      line-height: 1.4;
    }

    h1 { font-size: var(--app-text-2xl); }
    h2 { font-size: var(--app-text-xl); }
    h3 { font-size: var(--app-text-lg); }
    h4 { font-size: var(--app-text-md); }

    p { margin: 0.5em 0; }

    blockquote {
      margin: 0.75em 0;
      padding: 4px 14px;
      border-left: 3px solid var(--td-brand-color-light);
      background: var(--td-bg-color-secondarycontainer);
      color: var(--td-text-color-secondary);
    }

    code {
      padding: 2px 5px;
      border-radius: var(--app-radius-xs);
      background: var(--td-bg-color-secondarycontainer);
      font-family: 'SF Mono', Consolas, Menlo, monospace;
      font-size: 0.9em;
    }

    pre {
      margin: 0.75em 0;
      padding: 12px 14px;
      border-radius: var(--app-radius-sm);
      background: var(--td-bg-color-secondarycontainer);
      overflow-x: auto;

      code {
        padding: 0;
        background: transparent;
      }
    }

    ul, ol {
      padding-left: 1.5em;
      margin: 0.5em 0;
    }

    ul[data-type='taskList'] {
      list-style: none;
      padding-left: 0.25em;

      li {
        display: flex;
        align-items: flex-start;
        gap: 6px;

        > label {
          margin-top: 4px;
        }
      }
    }

    table {
      border-collapse: collapse;
      margin: 0.75em 0;
      width: 100%;

      th, td {
        border: 1px solid var(--td-component-stroke);
        padding: 6px 10px;
        text-align: left;
      }

      th {
        background: var(--td-bg-color-secondarycontainer);
        font-weight: 600;
      }
    }

    img {
      max-width: 100%;
      border-radius: var(--app-radius-sm);

      &.ProseMirror-selectednode {
        outline: 2px solid var(--td-brand-color);
      }
    }

    hr {
      border: 0;
      border-top: 1px solid var(--td-component-stroke);
      margin: 1.25em 0;
    }

    // 公式节点（@tiptap/extension-mathematics NodeView 渲染 KaTeX）
    .tiptap-mathematics-render {
      position: relative;

      &.tiptap-mathematics-render--editable {
        cursor: pointer;

        &:hover {
          outline: 1px dashed var(--td-brand-color);
          border-radius: var(--app-radius-xs);
        }
      }

      &.ProseMirror-selectednode {
        outline: 2px solid var(--td-brand-color);
        border-radius: var(--app-radius-xs);
      }

      &.block-math-error {
        color: var(--td-error-color);
        font-family: 'SF Mono', Consolas, Menlo, monospace;
      }
    }

    div[data-type='block-math'] {
      margin: 0.75em 0;
      text-align: center;
      overflow-x: auto;
    }

    span[data-type='inline-math'] .katex {
      font-size: 1.05em;
    }

    // mermaid 代码块：预览在上、源码在下
    div[data-code-block-wrap] {
      display: flex;
      flex-direction: column;

      .rich-editor__mermaid-preview {
        display: flex;
        justify-content: center;
        padding: 10px;
        margin-bottom: 6px;
        border-radius: var(--app-radius-sm);
        background: var(--td-bg-color-secondarycontainer);
        overflow-x: auto;

        > svg {
          max-width: 100%;
          height: auto;
        }

        &--error {
          justify-content: flex-start;
          color: var(--td-error-color);
          font-size: var(--app-text-xs);
        }
      }
    }

    a {
      color: var(--td-brand-color);
      text-decoration: none;

      &:hover {
        text-decoration: underline;
      }
    }
  }
}
</style>
