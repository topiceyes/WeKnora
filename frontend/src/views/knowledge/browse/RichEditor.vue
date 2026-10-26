<template>
  <div class="rich-editor">
    <div class="rich-editor__toolbar">
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

      <t-tooltip v-for="lv in [1, 2, 3]" :key="lv"
        :content="t(`knowledgeBase.browse.editor.heading${lv}`)" placement="bottom">
        <button type="button" class="rich-editor__btn rich-editor__btn--text"
          :class="{ 'is-active': isActive('heading', { level: lv }) }"
          @click="toggleHeading(lv)">
          H{{ lv }}
        </button>
      </t-tooltip>
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

      <t-tooltip :content="t('knowledgeBase.browse.editor.image')" placement="bottom">
        <button type="button" class="rich-editor__btn" :disabled="imageUploading" @click="pickImage">
          <t-icon :name="imageUploading ? 'loading' : 'image'" size="16px" />
        </button>
      </t-tooltip>
    </div>

    <editor-content :editor="editor" class="rich-editor__content" />
    <input ref="imageInputRef" type="file" accept="image/png,image/jpeg,image/gif,image/webp,image/bmp,image/svg+xml,image/avif"
      style="display: none" @change="onImagePicked" />

    <!-- 点击已渲染的公式 → 编辑 LaTeX 源码。用 v-if 控制挂载而不是
         v-model 的动画关闭：连续编辑两个公式时（上一个还在关闭动画中），
         tdesign 的动画状态机会把 .t-dialog__wrap 的 visibility 卡在
         visible，透明层残留拦截住编辑器的保存按钮。v-if 关闭即卸载 DOM，
         没有可中断的动画。 -->
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
import { reactive, ref, watch, onBeforeUnmount } from 'vue'
import { useI18n } from 'vue-i18n'
import { MessagePlugin } from 'tdesign-vue-next'
import { useEditor, EditorContent } from '@tiptap/vue-3'
import { NodeSelection, TextSelection } from '@tiptap/pm/state'
import { StarterKit } from '@tiptap/starter-kit'
import { TaskList, TaskItem } from '@tiptap/extension-list'
import { Image } from '@tiptap/extension-image'
import { Table, TableRow, TableCell, TableHeader } from '@tiptap/extension-table'
import { Markdown } from 'tiptap-markdown'
import 'katex/dist/katex.min.css'
import { uploadContentImage } from '@/api/knowledge-base'
import { MermaidCodeBlock } from './MermaidCodeBlock'
import { buildBlockMathExtension, buildInlineMathExtension } from './editorMathExtensions'

const props = defineProps<{
  modelValue: string
  kbId: string
  placeholder?: string
}>()

const emit = defineEmits<{
  'update:modelValue': [value: string]
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

const editor = useEditor({
  content: props.modelValue,
  extensions: [
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
    TaskItem.configure({ nested: true }),
    Image,
    Table.configure({ resizable: false }),
    TableRow,
    TableCell,
    TableHeader,
    Markdown.configure({
      html: false,
      transformPastedText: true,
      breaks: false,
    }),
  ],
  onUpdate: ({ editor: e }) => {
    emit('update:modelValue', (e.storage as any).markdown.getMarkdown())
  },
  onTransaction: () => {
    stateNonce.value++
  },
})

// 外部重置内容（如取消编辑后重新进入）时同步回编辑器。
watch(() => props.modelValue, (value) => {
  const e = editor.value
  if (!e) return
  const current = (e.storage as any).markdown.getMarkdown()
  if (value !== current) {
    e.commands.setContent(value || '')
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
  | 'toggleBulletList' | 'toggleOrderedList' | 'toggleTaskList' | 'toggleCodeBlock' | 'setHorizontalRule') => {
  editor.value?.chain().focus()[command]().run()
}

const run = (command: 'undo' | 'redo') => {
  editor.value?.chain().focus()[command]().run()
}

const toggleHeading = (level: number) => {
  editor.value?.chain().focus().toggleHeading({ level: level as 1 | 2 | 3 }).run()
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

// ---------- 图片上传 ----------

const imageInputRef = ref<HTMLInputElement | null>(null)
const imageUploading = ref(false)

const pickImage = () => {
  imageInputRef.value?.click()
}

const onImagePicked = async (event: Event) => {
  const input = event.target as HTMLInputElement
  const file = input.files?.[0]
  input.value = ''
  if (!file || imageUploading.value) return
  imageUploading.value = true
  try {
    const result: any = await uploadContentImage(props.kbId, file)
    const url = result?.data?.url
    if (!result?.success || !url) {
      throw new Error(result?.message || 'upload failed')
    }
    editor.value?.chain().focus().setImage({ src: url, alt: file.name }).run()
  } catch (error: any) {
    console.error('[RichEditor] image upload failed', error)
    MessagePlugin.error(error?.message || t('knowledgeBase.browse.editor.imageUploadFailed'))
  } finally {
    imageUploading.value = false
  }
}
</script>

<style scoped lang="less">
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
