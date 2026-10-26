// editorMathMarkdown 规则单测：用真实 markdown-it 实例（与 tiptap-markdown
// 内部同款）挂上插件，验证公式定界符识别与货币守卫。
import { test } from 'node:test'
import assert from 'node:assert/strict'
import MarkdownIt from 'markdown-it'

import { mathMarkdownPlugin, matchInlineMath } from './editorMathMarkdown'

const render = (src: string): string => {
  const md = new MarkdownIt({ html: false })
  mathMarkdownPlugin(md)
  return md.render(src)
}

test('块级 $$ 跨行形态渲染为 block-math 节点', () => {
  const html = render('前文\n\n$$\nE=mc^2\n$$\n\n后文\n')
  // 注意：JS 正则里 ^ 任何位置都是锚点，必须转义。
  assert.match(html, /<div data-type="block-math" data-latex="E=mc\^2"><\/div>/)
  assert.ok(!html.includes('$$'), '定界符不应残留在输出里')
})

test('块级 $$ 同行闭合（$$E=mc^2$$）同样识别', () => {
  const html = render('$$E=mc^2$$\n')
  assert.match(html, /data-latex="E=mc\^2"/)
})

test('块级 \\[…\\] 归一为 block-math', () => {
  const html = render('\\[\\sum_{i=1}^n i\\]\n')
  assert.match(html, /<div data-type="block-math" data-latex="\\sum_\{i=1\}\^n i"><\/div>/)
})

test('行内 $…$ 渲染为 inline-math', () => {
  const html = render('质能方程 $E=mc^2$ 出现在文中\n')
  assert.match(html, /<span data-type="inline-math" data-latex="E=mc\^2"><\/span>/)
})

test('行内 \\(…\\) 归一为 inline-math', () => {
  const html = render('面积 \\(\\pi r^2\\) 不变\n')
  assert.match(html, /data-type="inline-math" data-latex="\\pi r\^2"/)
})

test('货币 $5 和 $6 不被误判为公式', () => {
  const html = render('价格 $5 和 $6 之间\n')
  assert.ok(!html.includes('data-type="inline-math"'), html)
})

test('开 $ 后跟空格不成公式', () => {
  const html = render('这个 $ 不是公式的 $x$ 起点吗\n')
  // 第一个 $ 后是空格 → 跳过；真正的 $x$ 仍应被识别。
  assert.match(html, /data-latex="x"/)
})

test('matchInlineMath：闭定界符前的空格跳过继续找', () => {
  // "a $b $c$" → 开 $ 在 b 前，遇 "$c" 的 $（前是空格）跳过，闭在末尾 $。
  const m = matchInlineMath('a $b $c$', 2)
  assert.ok(m)
  assert.equal(m!.latex, 'b $c')
  assert.equal(m!.end, 8)
})

test('latex 中的引号/尖括号被属性转义', () => {
  const html = render('$a"b<c$\n')
  assert.match(html, /data-latex="a&quot;b&lt;c"/)
})

test('插件重复注册幂等（parse.setup 每次 setContent 都会跑）', () => {
  const md = new MarkdownIt({ html: false })
  mathMarkdownPlugin(md)
  mathMarkdownPlugin(md)
  assert.doesNotThrow(() => md.render('$$x$$\n'))
  const html = md.render('$x$\n')
  assert.match(html, /data-type="inline-math"/)
})

test('同行 $$ 后还有正文时不吞成块（交给行内/普通文本）', () => {
  const html = render('一句话 $$x$$ 后面还有字\n')
  assert.ok(!html.includes('data-type="block-math"'), html)
})
