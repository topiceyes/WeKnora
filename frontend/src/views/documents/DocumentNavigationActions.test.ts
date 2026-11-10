import assert from 'node:assert/strict'
import test, { type TestContext } from 'node:test'
import { fixture, flush } from '../../test-utils/vueFixture.ts'
import { reactive } from 'vue'
import * as model from './navigationModel.ts'
async function setup(t: TestContext) {
 const f=fixture(t), tree=reactive<any>({navigation_revision:3,folders:[{path:'A',name:'A'}],documents:[{id:'d',title:'Doc',folder_path:''}]})
 const before=t.mock.fn(async()=>true),refresh=t.mock.fn(async()=>{}),update=t.mock.fn(async(_id:string,_payload:any):Promise<any>=>({success:true}))
 const props=reactive({kbId:'kb',tree,canEdit:true,beforeChange:before,refresh})
 const page=f.component(new URL('./DocumentNavigationActions.vue',import.meta.url),{
  'vue-i18n':{useI18n:()=>({t:(key:string)=>key})},'tdesign-vue-next':{MessagePlugin:{success(){},error(){},info(){}}},
  '@/api/knowledge-base':{updateKnowledgeNavigation:update},'./navigationModel':model,
 },props)
 await flush();return {...page,before,refresh,update,props}
}
test('folder creation and bulk move use current revision, respect unsaved rejection and keep failures editable',async t=>{
 const f=await setup(t)
 f.vm.open({action:'create',parent:'A'});f.vm.name='Child';f.before.mock.mockImplementationOnce(async()=>false)
 await f.vm.submit();assert.equal(f.update.mock.callCount(),0);assert.equal(f.vm.dialog.action,'create')
 await f.vm.submit();assert.deepEqual(JSON.parse(JSON.stringify(f.update.mock.calls[0].arguments)),['kb',{action:'create_folder',to:'A/Child',base_revision:3}]);assert.equal(f.vm.dialog,null)
 f.vm.open({action:'move',ids:['d']});f.vm.destination='A';f.update.mock.mockImplementationOnce(async()=>{throw {status:409}})
 await f.vm.submit();assert.equal(f.vm.dialog.action,'move');assert.equal(f.vm.busy,false);assert.equal(f.refresh.mock.callCount(),2)
 f.vm.$.props.canEdit=false;await f.vm.submit();assert.equal(f.update.mock.callCount(),2)
})
test('drop ignores self targets and uses atomic reorder for direct siblings',async t=>{
 const f=await setup(t),doc={kind:'doc' as const,id:'d',title:'Doc',folder_path:''},folder={kind:'folder' as const,path:'A',name:'A'}
 await f.vm.drop(doc,doc,'after');assert.equal(f.update.mock.callCount(),0)
 await f.vm.drop(doc,folder,'before');assert.equal(f.update.mock.callCount(),1)
 assert.deepEqual(JSON.parse(JSON.stringify(f.update.mock.calls[0].arguments[1].items)),['d:d','f:A'])
 await f.vm.drop(doc,folder,'inside');assert.equal(f.update.mock.calls[1].arguments[1].action,'move_documents')
})
