import assert from 'node:assert/strict'
import test from 'node:test'
import { readFileSync } from 'node:fs'
import * as vue from 'vue'
import { fixture, flush } from '../../test-utils/vueFixture.ts'
import { memoryStorage } from './documentDrafts.test.ts'
test('favorites and recent visits persist only in the matching user, tenant and book namespace', async t => {
 const storage=memoryStorage(), f=fixture(t,{storage}), scope=vue.ref<any>({user:'a',tenant:1,kb:'kb'})
 const hooks=f.evaluate(readFileSync(new URL('./useDocumentVisits.ts',import.meta.url),'utf8'),{vue})
 const page=f.mount({setup(){const state=hooks.useDocumentVisits(()=>scope.value);return {state}}})
 const visits=page.vm.state
 visits.visit('d1');visits.visit('d2');visits.visit('d1');visits.toggle('d1')
 assert.deepEqual(Array.from(visits.recent.value),['d1','d2'])
 assert.deepEqual(Array.from(visits.favorites.value),['d1'])
 for (const next of [{user:'b',tenant:1,kb:'kb'},{user:'a',tenant:2,kb:'kb'},{user:'a',tenant:1,kb:'other'}]) {
  scope.value=next;await flush();assert.equal(visits.favorites.value.length,0)
 }
 scope.value={user:'a',tenant:1,kb:'kb'};await flush();assert.deepEqual(Array.from(visits.favorites.value),['d1'])
 visits.toggle('d1');assert.equal(visits.favorites.value.length,0)
 storage.setItem=()=>{throw new Error('quota')};assert.doesNotThrow(()=>visits.visit('d3'))
})
