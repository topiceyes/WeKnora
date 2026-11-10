import assert from 'node:assert/strict'
import test from 'node:test'
import { documentLink, linkedDocumentID, highlightedParts } from './documentLinks.ts'
const id = '12345678-1234-4234-8234-123456789abc'
test('document links contain a stable ID and never an owning folder, book or origin', () => {
  const href = documentLink(id)
  assert.equal(linkedDocumentID(href), id)
  for (const bad of ['https://other.invalid' + href, '//other.invalid' + href, href + '?x', href + '#x', href + '/', href + '\n', href.replace('12345678', '%31' + '2345678')]) assert.equal(linkedDocumentID(bad), null)
  assert.throws(() => documentLink('../escape'))
})
test('search highlights literal text without producing HTML or interpreting regex patterns', () => {
  assert.deepEqual(highlightedParts('a.*b <script> 中a.*b', 'a.*b'), [
    { text: 'a.*b', match: true }, { text: ' <script> 中', match: false }, { text: 'a.*b', match: true },
  ])
  assert.deepEqual(highlightedParts('Abc ABC', 'abc'), [{ text: 'Abc', match: true }, { text: ' ', match: false }, { text: 'ABC', match: true }])
  assert.deepEqual(highlightedParts('中文', ''), [{ text: '中文', match: false }])
  assert.deepEqual(highlightedParts('hello', '['), [{ text: 'hello', match: false }])
})
