// MermaidCodeBlock.ts — 给代码块加 NodeView：language=mermaid 时在代码上方
// 内联渲染图（P4 图表所见即所得）。不加新节点类型，markdown 侧仍是
// ```mermaid fence，round-trip 天然由 tiptap-markdown 的 codeBlock spec 保证。
//
// 该模块被 richEditorRoundtrip.test.ts 直接 import，保持零副作用依赖：
// 错误文案经扩展选项注入（RichEditor 里用 i18n 传进来），mermaidShared
// 动态加载（它静态引入 hljs 的 CSS，node 环境载不了）。
import CodeBlock from '@tiptap/extension-code-block'
import type { NodeViewRendererProps } from '@tiptap/core'
import type { NodeView as PMNodeView, ViewMutationRecord } from '@tiptap/pm/view'
import type { Node as ProseMirrorNode } from '@tiptap/pm/model'

// 渲染防抖：代码每敲一个字符都会触发 update，mermaid 渲染较重，必须防抖。
const RENDER_DEBOUNCE_MS = 300

export interface MermaidCodeBlockOptions {
  /** 图表语法解析失败时预览区展示的提示文案（由 RichEditor 用 i18n 注入）。 */
  parseErrorText: string
}

export const MermaidCodeBlock = CodeBlock.extend<MermaidCodeBlockOptions>({
  addOptions() {
    return {
      ...this.parent?.(),
      parseErrorText: 'mermaid parse error',
    }
  },

  addNodeView() {
    const parseErrorText = () => this.options.parseErrorText
    return (props: NodeViewRendererProps): PMNodeView => {
      let node: ProseMirrorNode = props.node
      // wrapper：预览区在上、原生可编辑 pre>code 在下（code 是 contentDOM）。
      const dom = document.createElement('div')
      dom.setAttribute('data-code-block-wrap', '')
      const preview = document.createElement('div')
      preview.className = 'rich-editor__mermaid-preview'
      const pre = document.createElement('pre')
      const code = document.createElement('code')
      pre.appendChild(code)
      dom.append(preview, pre)

      let timer: ReturnType<typeof setTimeout> | null = null
      let renderSeq = 0

      const render = async () => {
        const seq = ++renderSeq
        const text = node.textContent
        if (!text.trim()) {
          preview.innerHTML = ''
          preview.classList.remove('rich-editor__mermaid-preview--error')
          return
        }
        const { renderMermaidToSvg } = await import('../../../utils/mermaidShared')
        const svg = await renderMermaidToSvg(text, 'rich-editor')
        if (seq !== renderSeq) return // 过期渲染（期间又改了代码）
        if (svg) {
          // securityLevel:'strict' 下 mermaid 输出的自家 SVG，与阅读器同信任级。
          preview.innerHTML = svg
          preview.classList.remove('rich-editor__mermaid-preview--error')
        } else {
          preview.innerHTML = ''
          preview.textContent = parseErrorText()
          preview.classList.add('rich-editor__mermaid-preview--error')
        }
      }

      const scheduleRender = () => {
        if (timer) clearTimeout(timer)
        timer = setTimeout(() => {
          timer = null
          void render()
        }, RENDER_DEBOUNCE_MS)
      }

      const applyLanguage = () => {
        const lang = (node.attrs.language as string) || ''
        code.className = lang ? `language-${lang}` : ''
        if (lang === 'mermaid') {
          preview.style.display = ''
          scheduleRender()
        } else {
          preview.style.display = 'none'
          preview.innerHTML = ''
        }
      }

      applyLanguage()

      return {
        dom,
        contentDOM: code,
        update(updatedNode: ProseMirrorNode) {
          if (updatedNode.type !== node.type) return false
          const wasMermaid = node.attrs.language === 'mermaid'
          node = updatedNode
          // language 进出 mermaid → 结构变化，返回 false 让 tiptap 重建视图。
          if (wasMermaid !== (updatedNode.attrs.language === 'mermaid')) return false
          applyLanguage()
          return true
        },
        // 预览区的 SVG 是我们自己 innerHTML 进去的，不是文档内容；不忽略的话
        // ProseMirror 会把它当文档突变处理。
        ignoreMutation(mutation: ViewMutationRecord) {
          return (
            preview.contains(mutation.target) ||
            (mutation.type === 'attributes' && mutation.target === code)
          )
        },
        destroy() {
          if (timer) clearTimeout(timer)
        },
      }
    }
  },
})
