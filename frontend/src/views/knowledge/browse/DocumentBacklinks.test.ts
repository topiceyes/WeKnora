import assert from 'node:assert/strict'
import test from 'node:test'
import { fixture, flush } from '../../../test-utils/vueFixture.ts'

test('empty backlinks stay compact, explicit expansion works and stale document responses are ignored', async t => {
  const f = fixture(t)
  let settle!: (value: any) => void
  const response = (items: any[]) => ({ success: true, data: { items, next_offset: items.length, has_more: false } })
  const request = t.mock.fn(async (id: string) => id === 'slow' ? new Promise(resolve => { settle = resolve }) : response([]))
  const page = f.component(new URL('./DocumentBacklinks.vue', import.meta.url), {
    'vue-i18n': { useI18n: () => ({ t: (key: string) => key }) },
    '@/api/knowledge-base': { documentBacklinks: request },
    '@/views/documents/documentLinks': { documentLink: (id: string) => '/documents/' + id },
  }, { knowledgeId: 'empty' })
  await flush(); assert.equal(page.vm.expanded, false)
  page.vm.open = true; assert.equal(page.vm.expanded, true)
  page.vm.$.props.knowledgeId = 'slow'; await flush()
  page.vm.$.props.knowledgeId = 'other'; await flush()
  settle(response([{ id: 'stale' }])); await flush()
  assert.equal(page.vm.items.length, 0); assert.equal(page.vm.expanded, false)
  request.mock.mockImplementation(async () => response([{ id: 'source' }]))
  await page.vm.load(false); assert.equal(page.vm.expanded, true)
  page.vm.open = false; await page.vm.load(false); assert.equal(page.vm.expanded, false)
})
