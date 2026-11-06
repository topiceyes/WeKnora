import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { createRequire } from 'node:module'
import { runInNewContext } from 'node:vm'
import test from 'node:test'
import { compileScript, parse } from '@vue/compiler-sfc'
import ts from 'typescript'
import * as vue from 'vue'

const require = createRequire(import.meta.url)
const { descriptor } = parse(readFileSync(new URL('./IMChannelPanel.vue', import.meta.url), 'utf8'))
const script = compileScript(descriptor, { id: 'im-credentials-test' }).content
  .replace('__expose();', '')
  .replace('return __returned__', '__expose(__returned__); return __returned__')
const compiled = ts.transpileModule(script, {
  compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 },
}).outputText
const renderer = vue.createRenderer<any, any>({
  createElement: () => ({}), createText: () => ({}), createComment: () => ({}),
  insert() {}, remove() {}, setElementText() {}, setText() {}, patchProp() {},
  parentNode: () => null, nextSibling: () => null,
})

async function fixture() {
  const channel = {
    id: 'ding-channel', agent_id: 'wiki-agent', platform: 'dingtalk',
    name: 'DingTalk', mode: 'websocket', output_mode: 'stream', enabled: true,
    credentials_configured: true,
  }
  const updates: any[] = []
  const errors: string[] = []
  const resources = {
    agents: [{ id: 'wiki-agent', name: 'Wiki', config: {} }], rawKnowledgeBases: [],
    async ensureAgents() {}, async ensureKnowledgeBases() {},
  }
  const api = {
    async listAllIMChannels() { return { data: [channel] } },
    async listIMChannels() { return { data: [channel] } },
    async updateIMChannel(id: string, payload: any) {
      updates.push({ id, payload: JSON.parse(JSON.stringify(payload)) })
    },
  }
  const exports: any = {}
  runInNewContext(compiled, {
    exports, setTimeout, clearTimeout,
    require(name: string) {
      if (name === 'vue') return vue
      if (name === 'vue-i18n') return { useI18n: () => ({ t: (key: string) => key }) }
      if (name === 'tdesign-vue-next') return {
        MessagePlugin: { success() {}, warning() {}, error(msg: string) { errors.push(msg) } },
      }
      if (name === '@/api/agent') return api
      if (name === '@/stores/chatResources') return { useChatResourcesStore: () => resources }
      if (name === '@/stores/auth') return { useAuthStore: () => ({ hasRole: () => true }) }
      if (name === '@/utils/optionalString') return { normalizeOptionalString: (s: string) => s || undefined }
      return { default: {} }
    },
  })
  const component = exports.default
  component.render = () => null
  const instance = vue.ref<any>()
  const app = renderer.createApp({ render: () => vue.h(component, { ref: instance }) })
  app.mount({})
  await new Promise(resolve => setImmediate(resolve))
  return {
    channel, updates, vm: instance.value,
    close() { app.unmount(); assert.deepEqual(errors, []) },
  }
}

test('editing an IM summary and changing the agent does not erase hidden credentials', async () => {
  const f = await fixture()
  try {
    await f.vm.editChannel(f.channel)
    f.vm.formData.target_agent_id = 'replacement-wiki-agent'
    f.vm.formData.name = 'Renamed bot'
    await f.vm.handleSave()
    assert.equal(f.updates.length, 1)
    const { id, payload } = f.updates[0]
    assert.equal(id, f.channel.id)
    assert.equal(payload.agent_id, 'replacement-wiki-agent')
    assert.equal(payload.name, 'Renamed bot')
    assert.equal(Object.hasOwn(payload, 'credentials'), false)
  } finally { f.close() }
})

test('explicitly replacing bot credentials still submits the replacement', async () => {
  const f = await fixture()
  try {
    await f.vm.editChannel(f.channel)
    f.vm.formData.credentials = { client_id: 'bot', client_secret: 'replacement-secret' }
    await f.vm.handleSave()
    assert.equal(f.updates.length, 1)
    assert.deepEqual(f.updates[0].payload.credentials, {
      client_id: 'bot', client_secret: 'replacement-secret',
    })
  } finally { f.close() }
})
