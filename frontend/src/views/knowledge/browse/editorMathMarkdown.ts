// editorMathMarkdown.ts — 给 tiptap-markdown 内部的 markdown-it 实例加装
// 公式定界符识别（P4 公式所见即所得）。
//
// 职责边界：这里只「认出」$$…$$ / $…$ / \[…\] / \(…\) 并输出
// @tiptap/extension-mathematics 的 parseHTML 能匹配的标签
// （div[data-type="block-math"] / span[data-type="inline-math"]，latex 走
// data-latex 属性），真正的 KaTeX 渲染由扩展的 NodeView 完成。与阅读器的
// preprocessMathDelimiters（markdownPipeline.ts）语义对齐：读写同源，两边的
// `\(...\)` / `\[…\]` 都归一为 `$…$` / `$$…$$`。
//
// 已知限制（有意为之，注释在规则处）：
// - 行内 `$$…$$` 不支持（块级 `$$` 必须独占行/行组），避免与行内货币混淆；
// - `$` 后紧跟数字视为货币（"$5 和 $6" 不成公式）；
// - latex 内含 `$` 无法回环（序列化端同样约定）。
import type MarkdownIt from 'markdown-it'

function escapeAttr(text: string): string {
  return text
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;')
}

// 在 text[start] 处尝试匹配一个行内公式。返回 latex 与结束位置（闭定界符后）。
// 导出仅供单测使用。
export function matchInlineMath(text: string, start: number): { latex: string; end: number } | null {
  // \（反斜号）形式：\(…\)
  if (text.startsWith('\\(', start)) {
    const close = text.indexOf('\\)', start + 2)
    if (close > start + 2 && !text.slice(start + 2, close).includes('\n')) {
      return { latex: text.slice(start + 2, close).trim(), end: close + 2 }
    }
    return null
  }
  if (text[start] !== '$') return null
  const first = text[start + 1]
  // 开 $ 后不能是空白/换行/又一个 $（$$ 是块级定界，行内不处理）。
  if (!first || first === ' ' || first === '\t' || first === '\n' || first === '$') return null
  // 货币守卫：内容以数字开头（"$5 …"）不当公式。
  if (first >= '0' && first <= '9') return null
  for (let i = start + 2; i < text.length; i++) {
    const ch = text[i]
    if (ch === '\n') return null // 闭定界符必须与开定界符同行
    if (ch !== '$') continue
    // 闭 $ 前不能是空白，后不能还是 $（那会是 $$）。
    const prev = text[i - 1]
    if (prev === ' ' || prev === '\t') continue
    if (text[i + 1] === '$') {
      i++ // 跳过 $$ 的第二个字符
      continue
    }
    return { latex: text.slice(start + 1, i), end: i + 1 }
  }
  return null
}

function mathInlineRule(state: any, silent: boolean): boolean {
  const m = matchInlineMath(state.src, state.pos)
  if (!m || m.end > state.posMax) return false
  if (!silent) {
    const token = state.push('math_inline', 'math', 0)
    token.content = m.latex
  }
  state.pos = m.end
  return true
}

// 块级规则：`$$` / `\[` 开头。支持两种形态——
// 1) 同行闭合：`$$E=mc^2$$` 整行（后面只允许空白）；
// 2) 跨行：开定界行、若干内容行、闭定界行（`$$` 或 `\]` 独占一行）。
function mathBlockRule(state: any, startLine: number, endLine: number, silent: boolean): boolean {
  const pos = state.bMarks[startLine] + state.tShift[startLine]
  const lineText = state.src.slice(pos, state.eMarks[startLine])
  const isOpen = lineText.startsWith('$$') || lineText.startsWith('\\[')
  if (!isOpen) return false
  const closeDelim = lineText.startsWith('$$') ? '$$' : '\\]'
  const firstRest = lineText.slice(2)

  const pushToken = (latex: string, endLineNo: number) => {
    if (!silent) {
      const token = state.push('math_block', 'math', 0)
      token.block = true
      token.content = latex
    }
    state.line = endLineNo
  }

  // 同行闭合：`$$x$$`。
  const sameLineClose = firstRest.indexOf(closeDelim)
  if (sameLineClose >= 0) {
    // 闭定界符之后还有正文 → 是行内场景，交给 inline 规则/普通文本。
    if (firstRest.slice(sameLineClose + 2).trim() !== '') return false
    pushToken(firstRest.slice(0, sameLineClose).trim(), startLine + 1)
    return true
  }
  // 同行只有开定界符 → 跨行形态，找独占一行的闭定界符。
  for (let line = startLine + 1; line < endLine; line++) {
    const t = state.src
      .slice(state.bMarks[line] + state.tShift[line], state.eMarks[line])
      .trim()
    if (t !== closeDelim) continue
    const lines: string[] = []
    for (let l = startLine + 1; l < line; l++) {
      lines.push(state.src.slice(state.bMarks[l] + state.tShift[l], state.eMarks[l]))
    }
    pushToken(lines.join('\n').trim(), line + 1)
    return true
  }
  return false
}

// parse.setup 每次 setContent 都会跑，插件注册必须幂等。
export function mathMarkdownPlugin(md: MarkdownIt): void {
  const flag = '__weknoraMathMarkdown'
  if ((md as any)[flag]) return
  ;(md as any)[flag] = true
  md.block.ruler.before('paragraph', 'math_block', mathBlockRule, { alt: [] })
  // 必须排在 escape 之前：否则 \( 的反斜杠会被 escape 规则先吃掉。
  md.inline.ruler.before('escape', 'math_inline', mathInlineRule)
  md.renderer.rules.math_block = (tokens: any[], idx: number) =>
    `<div data-type="block-math" data-latex="${escapeAttr(tokens[idx].content)}"></div>`
  md.renderer.rules.math_inline = (tokens: any[], idx: number) =>
    `<span data-type="inline-math" data-latex="${escapeAttr(tokens[idx].content)}"></span>`
}
