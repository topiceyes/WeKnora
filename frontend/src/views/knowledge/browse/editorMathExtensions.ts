// editorMathExtensions.ts — 公式扩展的组装（P4）。独立成模块是为了让
// richEditorRoundtrip.test.ts 能挂上与 RichEditor 完全一致的扩展配置。
import { BlockMath, InlineMath } from '@tiptap/extension-mathematics'
import { InputRule } from '@tiptap/core'
import { mathMarkdownPlugin } from './editorMathMarkdown'

export type MathEditOpener = (kind: 'inline' | 'block', latex: string, pos: number) => void

// 打字路径的即时转换（markdown parse.setup 只覆盖 setContent/粘贴）。
// 守卫与 editorMathMarkdown.ts 一致：开 $ 前不是数字/反斜杠/$，开 $ 后、
// 闭 $ 前无空格（货币「$5 和 $6」因此不触发），闭 $ 后不跟数字。
export const INLINE_MATH_INPUT_REGEX = /(?<![\d\\$])\$(?!\s)([^$\n]*[^$\s])\$(?!\d)$/
// 同行 $$x$$（跨行块公式一般经工具栏按钮或 markdown 粘贴进来）
export const BLOCK_MATH_INPUT_REGEX = /\$\$([^$\n]+?)\$\$$/

// 不用 tiptap 的 nodeInputRule：其 match[1] 分支只把捕获组替换为节点、
// 把开定界符留在文本里并在末尾补插字符（语义面向 mention/#tag 类），
// 定界符必须整体消失的场景会产出「$ + 节点 + $$」的脏文档。原生
// InputRule 的 range 恰好覆盖整个 match[0]（含刚输入的收尾字符），
// 直接 replaceWith 即可。
function mathNodeInputRule(regex: RegExp, type: any) {
  return new InputRule({
    find: regex,
    handler: ({ state, range, match }) => {
      state.tr.replaceWith(range.from, range.to, type.create({ latex: match[1] }))
    },
  })
}

// 官方数学扩展的 parseMarkdown/renderMarkdown 字段是给 tiptap pro 的 markdown
// 支持用的，tiptap-markdown 只认 extension.storage.markdown——所以必须自己挂
// 序列化/解析钩子：blockMath ↔ $$\n…\n$$，inlineMath ↔ $…$。解析用本地
// markdown-it 插件（editorMathMarkdown.ts，注册幂等，两个扩展都调它没关系）。
export function buildBlockMathExtension(openMathEditor: MathEditOpener) {
  return BlockMath.extend({
    addStorage() {
      return {
        markdown: {
          serialize(state: any, node: any) {
            state.write(`$$\n${node.attrs.latex || ''}\n$$`)
            state.closeBlock(node)
          },
          parse: {
            setup(md: any) {
              mathMarkdownPlugin(md)
            },
          },
        },
      }
    },
    addInputRules() {
      return [mathNodeInputRule(BLOCK_MATH_INPUT_REGEX, this.type)]
    },
  }).configure({
    katexOptions: { throwOnError: false },
    onClick: (node: any, pos: number) => openMathEditor('block', node.attrs.latex || '', pos),
  })
}

export function buildInlineMathExtension(openMathEditor: MathEditOpener) {
  return InlineMath.extend({
    addStorage() {
      return {
        markdown: {
          serialize(state: any, node: any) {
            state.write(`$${node.attrs.latex || ''}$`)
          },
          parse: {
            setup(md: any) {
              mathMarkdownPlugin(md)
            },
          },
        },
      }
    },
    addInputRules() {
      return [mathNodeInputRule(INLINE_MATH_INPUT_REGEX, this.type)]
    },
  }).configure({
    katexOptions: { throwOnError: false },
    onClick: (node: any, pos: number) => openMathEditor('inline', node.attrs.latex || '', pos),
  })
}
