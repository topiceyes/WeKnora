import assert from 'node:assert/strict'
import test from 'node:test'
import { fixture, flush } from '../../test-utils/vueFixture.ts'
const id = '12345678-1234-4234-8234-123456789abc'
for (const mode of ['moved', 'forbidden', 'invalid']) test(`stable link resolver: ${mode}`, async t => {
  const f = fixture(t)
  const replace = t.mock.fn(async (_value: any) => {})
  const get = t.mock.fn(async () => {
    if (mode === 'forbidden') throw new Error('private error details')
    return { success: true, data: { knowledge_base_id: 'moved-book' } }
  })
  const page = f.component(new URL('./DocumentLinkResolver.vue', import.meta.url), {
    'vue-router': { useRoute: () => ({ params: { documentId: mode === 'invalid' ? '../escape' : id } }), useRouter: () => ({ replace }) },
    'vue-i18n': { useI18n: () => ({ t: (key: string) => key }) },
    '@/api/knowledge-base': { getKnowledgeDetails: get },
  })
  await flush()
  if (mode === 'moved') {
    assert.deepEqual(structuredClone(replace.mock.calls[0].arguments[0]), { name: 'documentWorkspace', params: { kbId: 'moved-book' }, query: { doc: id } })
    assert.equal(page.vm.error, '')
  } else {
    assert.equal(replace.mock.callCount(), 0)
    assert.equal(page.vm.error, 'documentDiscovery.unavailable')
    if (mode === 'invalid') assert.equal(get.mock.callCount(), 0)
  }
})
