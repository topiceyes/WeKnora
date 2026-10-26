// editorMathInput.test.ts — 打字路径 input rule 正则的边界（守卫与
// editorMathMarkdown.ts 的 markdown 规则对齐：货币不误判、空格定界拒绝）。
// input rule 与扩展的接线由 UI 冒烟验证（jsdom 里无法真实模拟 textInput）。
import test from 'node:test'
import assert from 'node:assert/strict'
import { BLOCK_MATH_INPUT_REGEX, INLINE_MATH_INPUT_REGEX } from './editorMathExtensions'

test('inline: $E=mc^2$ converts', () => {
  const m = '质能方程 $E=mc^2$'.match(INLINE_MATH_INPUT_REGEX)
  assert.ok(m)
  assert.equal(m[1], 'E=mc^2')
})

test('inline: currency "$5 和 $6" does not convert', () => {
  assert.equal('价格 $5 和 $6'.match(INLINE_MATH_INPUT_REGEX), null)
})

test('inline: space after opening $ rejected', () => {
  assert.equal('a $ x$'.match(INLINE_MATH_INPUT_REGEX), null)
})

test('inline: space before closing $ rejected', () => {
  assert.equal('a $x $'.match(INLINE_MATH_INPUT_REGEX), null)
})

test('inline: escaped \$ rejected', () => {
  assert.equal('a \\$x$'.match(INLINE_MATH_INPUT_REGEX), null)
})

test('inline: digit before opening $ rejected', () => {
  assert.equal('cost 5$ x$'.match(INLINE_MATH_INPUT_REGEX), null)
})

test('inline: $$x$$ left to block rule', () => {
  assert.equal('$$x$$'.match(INLINE_MATH_INPUT_REGEX), null)
})

test('inline: latex ending with ^ still fine (JS ^ escape not needed in match)', () => {
  const m = '$a^2$'.match(INLINE_MATH_INPUT_REGEX)
  assert.ok(m)
  assert.equal(m[1], 'a^2')
})

test('block: same-line $$x^2$$ converts', () => {
  const m = '公式 $$x^2$$'.match(BLOCK_MATH_INPUT_REGEX)
  assert.ok(m)
  assert.equal(m[1], 'x^2')
})

test('block: multi-line $$ does not match input rule (markdown path handles)', () => {
  assert.equal('$$\na+b\n$$'.match(BLOCK_MATH_INPUT_REGEX), null)
})
