import assert from 'node:assert/strict'
import test from 'node:test'
import { fixture } from '../../../test-utils/vueFixture.ts'
import * as navigation from '../../documents/navigationModel.ts'
import * as folders from '../folderTree.ts'

test('directory tree exposes empty folders, shared order and safe drop payloads', t => {
 const f=fixture(t),events:any[]=[]
 const tree:any={navigation_revision:1,total_document_count:1,root_document_count:1,sibling_orders:{'':['d:doc','f:Empty']},folders:[{path:'Empty',name:'Empty',children:[]}],documents:[{id:'doc',title:'Title',folder_path:'',file_name:'Title'}]}
 const page=f.component(new URL('./BrowseDocTree.vue',import.meta.url),{
  'vue-i18n':{useI18n:()=>({t:(key:string)=>key})},'../folderTree':folders,'./docIcon':{browseDocIcon:()=>''},'../../documents/navigationModel':navigation,
 },{tree,scope:'kb',manage:true,onDrop:(...args:any[])=>events.push(args)})
 assert.deepEqual(Array.from(page.vm.rows,(r:any)=>r.kind),['doc','folder'])
 const event=(scope:string)=>({dataTransfer:{getData:()=>JSON.stringify({scope,key:'d:doc'})}})
 page.vm.dropAt(event('foreign'),null,'inside');assert.equal(events.length,0)
 page.vm.dropAt(event('kb'),null,'inside');assert.equal(events.length,1)
 page.vm.$.props.query='Title';page.vm.dropAt(event('kb'),null,'inside');assert.equal(events.length,1)
 page.vm.$.props.query='';page.vm.$.props.manage=false;page.vm.dropAt(event('kb'),null,'inside');assert.equal(events.length,1)
})
