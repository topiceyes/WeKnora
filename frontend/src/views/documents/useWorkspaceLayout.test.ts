import assert from 'node:assert/strict'
import test from 'node:test'
import { readFileSync } from 'node:fs'
import * as vue from 'vue'
import { fixture, flush } from '../../test-utils/vueFixture.ts'

test('layout preferences isolate identities and survive storage failures', async t => {
  const data = new Map<string, string>()
  const f = fixture(t, { storage: { getItem: k => data.get(k) ?? null, setItem: (k, v) => { data.set(k, v) } } as Storage })
  const hooks = f.evaluate(readFileSync(new URL('./useWorkspaceLayout.ts', import.meta.url), 'utf8'), { vue })
  const scope = vue.ref<any>({ user: 'a', tenant: 1, kb: 'book' })
  const { vm } = f.mount({ setup: () => ({ layout: hooks.useWorkspaceLayout(() => scope.value) }) })
  const l = vm.layout
  l.keydown({ key: 'ArrowRight', preventDefault() {} }); l.setOutline(false)
  assert.equal(l.sidebarWidth.value, 288)
  for (const next of [{ user: 'b', tenant: 1, kb: 'book' }, { user: 'a', tenant: 2, kb: 'book' }, { user: 'a', tenant: 1, kb: 'other' }]) {
    scope.value = next; await flush()
    assert.equal(l.sidebarWidth.value, 272); assert.equal(l.outline.value, null)
  }
  scope.value = { user: 'a', tenant: 1, kb: 'book' }; await flush()
  assert.equal(l.sidebarWidth.value, 288); assert.equal(l.outline.value, false)
  f.storage.setItem = () => { throw new Error('quota') }
  assert.doesNotThrow(() => l.reset())
})

test('drag width clamps to the viewport and cancellation restores the previous value', t => {
  const f = fixture(t)
  const hooks = f.evaluate(readFileSync(new URL('./useWorkspaceLayout.ts', import.meta.url), 'utf8'), { vue })
  const { vm } = f.mount({ setup: () => ({ layout: hooks.useWorkspaceLayout(() => null) }) })
  const l = vm.layout
  const pointer = (x: number, id = 7) => ({ button: 0, pointerId: id, clientX: x, preventDefault() {}, currentTarget: { setPointerCapture() {} } })
  l.start(pointer(272)); l.move(pointer(900)); assert.equal(l.sidebarWidth.value, 480)
  l.finish(pointer(900), true); assert.equal(l.sidebarWidth.value, 272)
  l.start(pointer(272)); l.move(pointer(100)); l.finish(pointer(100)); assert.equal(l.sidebarWidth.value, 208)
  f.window.innerWidth = 700; f.window.dispatchEvent(new Event('resize'))
  l.start(pointer(208)); l.move(pointer(800)); l.finish(pointer(800)); assert.equal(l.sidebarWidth.value, 280)
  l.keydown({ key: 'Home', preventDefault() {} }); assert.equal(l.sidebarWidth.value, 208)
})
