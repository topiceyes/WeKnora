import assert from 'node:assert/strict'
import test from 'node:test'
import { locateCommentQuote } from './commentAnchors.ts'

const anchor = { quote: '所选文字', prefix: '前文：', suffix: '。后文', content_version: 2 }
test('quote locator requires the captured document version and exact context', () => {
  assert.equal(locateCommentQuote('前文：所选文字。后文', anchor, 2), 3)
  assert.equal(locateCommentQuote('前文：所选文字。后文', anchor, 3), null)
  assert.equal(locateCommentQuote('前文改了所选文字。后文', anchor, 2), null)
  assert.equal(locateCommentQuote('前文：所选文字。新的后文', anchor, 2), null)
})
test('duplicate quotes never jump to an arbitrary occurrence', () => {
  assert.equal(locateCommentQuote('前文：所选文字。后文\n前文：所选文字。后文', anchor, 2), null)
  assert.equal(locateCommentQuote('重复 重复', { quote: '重复', prefix: '', suffix: '', content_version: 0 }, 0), null)
  assert.equal(locateCommentQuote('😀前文：所选文字。后文', anchor, 2), 5)
  assert.equal(locateCommentQuote('正文', { ...anchor, quote: '' }, 2), null)
})
