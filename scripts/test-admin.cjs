/* Editor logic QA with a minimal DOM mock; this is not browser or visual QA. */
const fs=require('node:fs'),vm=require('node:vm'),assert=require('node:assert/strict');
const defaults=JSON.parse(fs.readFileSync('web/default.json','utf8'));
const elements=new Map(),listeners=new Map(),requests=[];
function element(selector){if(!elements.has(selector))elements.set(selector,{innerHTML:'',textContent:'',className:'',hidden:false,disabled:false,open:false,showModal(){this.open=true},close(){this.open=false},focus(){},setSelectionRange(){}});return elements.get(selector);}
const state={...structuredClone(defaults),pageHTML:{}},context={console,Intl,Date,JSON,FormData,URLSearchParams,Number,String,Array,Set,Map,Error,Promise,structuredClone,
 document:{querySelector:element,querySelectorAll:()=>[],addEventListener:(type,fn)=>{if(!listeners.has(type))listeners.set(type,[]);listeners.get(type).push(fn)}},
 window:{scrollTo(){},addEventListener(){}},navigator:{clipboard:{writeText:async()=>{}}},setTimeout:()=>0,clearTimeout(){},confirm:()=>true,
 fetch:async(url,options={})=>{requests.push({url,options});const path=url.replace('/api/admin/','');let data;if(path==='session')data={csrf:'test-csrf',weakPassword:true,role:'owner',email:'medynikv@gmail.com'};else if(path==='state')data=structuredClone(state);else if(path==='leads'||path==='media'||path==='users')data=[];else data={ok:true};return {ok:true,status:200,json:async()=>data};}
};context.window=context;context.scrollTo=()=>{};context.addEventListener=()=>{};
vm.createContext(context);vm.runInContext(fs.readFileSync('web/static/admin.js','utf8'),context);
async function settle(){await new Promise(resolve=>setImmediate(resolve));}
async function click(data={},id=''){const button={dataset:data,id,disabled:false,hasAttribute:()=>false};for(const fn of listeners.get('click')||[])await fn({target:{closest:()=>button}});await settle();return element('#admin-content').innerHTML;}
const unescape=v=>v.replace(/&quot;/g,'"').replace(/&#39;/g,"'").replace(/&amp;/g,'&').replace(/&lt;/g,'<').replace(/&gt;/g,'>');
function checkPaths(html){for(const match of html.matchAll(/data-path="([^"]+)"/g)){const path=JSON.parse(unescape(match[1]));assert(Array.isArray(path)&&path.length>=2);let value=state;for(const part of path.slice(0,-1)){assert(value!=null,`Invalid editor path: ${path}`);value=value[part];}assert(value!=null,`Missing parent in editor path: ${path}`);}}
(async()=>{
 await settle();assert.equal(element('#app').hidden,false,'Authenticated editor should open');
 assert.equal((element('#admin-nav').innerHTML.match(/data-tab=/g)||[]).length,16);
 const tabs=['overview','general','text','sections','services','projects','media','faqs','documents','reviews','seo','tracking','leads','layout','users','system'];
 for(const tab of tabs){const html=await click({tab});assert(html.length>50,`Empty tab ${tab}`);assert(!/[А-Яа-яІіЇїЄє]/.test(html),`Unexpected Cyrillic UI in ${tab}`);assert(!html.includes('undefined'),'Unresolved value in '+tab);checkPaths(html);}
 assert(!element('#admin-nav').innerHTML.includes('glossary'));
 assert(element('#admin-nav').innerHTML.includes('data-tab="users"'));
 assert((await click({tab:'users'})).includes('Invite a user'));
 assert((await click({tab:'system'})).includes('System email · fixed'));
 assert(element('#admin-content').innerHTML.includes('service@svmrakennus.fi'));
 let html=await click({tab:'text'});assert(html.includes('Finnish and English') && html.includes('Uusi elämä'));
 html=await click({editItem:'services:5'});assert(html.includes('Palokatkotyöt') && html.includes('English'));checkPaths(html);
 html=await click({tab:'media'});assert.equal((html.match(/data-delete-media=/g)||[]).length,6);
 html=await click({tab:'layout'});assert(html.includes('Shared header') && html.includes('Preview draft'));checkPaths(html);
 await click({tab:'text'});
 for(const fn of listeners.get('input')||[])fn({target:{dataset:{path:JSON.stringify(['text','en','hero.title'])},type:'textarea',value:'Updated headline'}});
 assert.equal(element('#save-status').textContent,'Unsaved changes');
 await click({},'save-settings');const save=requests.find(r=>r.url==='/api/admin/state'&&r.options.method==='PUT');assert(save);const payload=JSON.parse(save.options.body);assert.equal(payload.text.en['hero.title'],'Updated headline');assert.equal(payload.text.fi['hero.title'],defaults.text.fi['hero.title']);assert.equal(save.options.headers['X-CSRF-Token'],'test-csrf');
 // Exercise a failed server save through the same localized error path.
 context.fetch=async()=>({ok:false,status:400,json:async()=>({error:'accent must be a hex colour'})});
 await click({},'save-settings');assert.equal(element('#toast').textContent,'accent must be a hex colour');
 console.log('PASS: English editor, HTML area selection, media removal controls, field paths, CSRF and save isolation');
})().catch(error=>{console.error(error);process.exitCode=1});
