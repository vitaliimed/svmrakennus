/* Editor logic QA with a minimal DOM mock; this is not browser or visual QA. */
const fs=require('node:fs'),vm=require('node:vm'),assert=require('node:assert/strict');
const defaults=JSON.parse(fs.readFileSync('web/default.json','utf8'));
const elements=new Map(),listeners=new Map(),requests=[];
function element(selector){if(!elements.has(selector))elements.set(selector,{innerHTML:'',textContent:'',className:'',hidden:false,disabled:false,open:false,showModal(){this.open=true},close(){this.open=false},focus(){},setSelectionRange(){}});return elements.get(selector);}
const state=structuredClone(defaults),context={console,Intl,Date,JSON,FormData,URLSearchParams,Number,String,Array,Set,Map,Error,Promise,structuredClone,
 document:{querySelector:element,querySelectorAll:()=>[],addEventListener:(type,fn)=>{if(!listeners.has(type))listeners.set(type,[]);listeners.get(type).push(fn)}},
 window:{scrollTo(){},addEventListener(){}},navigator:{clipboard:{writeText:async()=>{}}},setTimeout:()=>0,clearTimeout(){},confirm:()=>true,
 fetch:async(url,options={})=>{requests.push({url,options});const path=url.replace('/api/admin/','');let data;if(path==='session')data={csrf:'test-csrf',weakPassword:true};else if(path==='state')data=structuredClone(state);else if(path==='leads'||path==='media')data=[];else data={ok:true};return {ok:true,status:200,json:async()=>data};}
};context.window=context;context.scrollTo=()=>{};context.addEventListener=()=>{};
vm.createContext(context);vm.runInContext(fs.readFileSync('web/static/admin-help.js','utf8'),context);vm.runInContext(fs.readFileSync('web/static/admin.js','utf8'),context);
async function settle(){await new Promise(resolve=>setImmediate(resolve));}
async function click(data={},id=''){const button={dataset:data,id,disabled:false,hasAttribute:()=>false};for(const fn of listeners.get('click')||[])await fn({target:{closest:()=>button}});await settle();return element('#admin-content').innerHTML;}
const unescape=v=>v.replace(/&quot;/g,'"').replace(/&#39;/g,"'").replace(/&amp;/g,'&').replace(/&lt;/g,'<').replace(/&gt;/g,'>');
function checkPaths(html){for(const match of html.matchAll(/data-path="([^"]+)"/g)){const path=JSON.parse(unescape(match[1]));assert(Array.isArray(path)&&path.length>=2);let value=state;for(const part of path.slice(0,-1)){assert(value!=null,`Invalid editor path: ${path}`);value=value[part];}assert(value!=null,`Missing parent in editor path: ${path}`);}}
(async()=>{
 await settle();assert.equal(element('#app').hidden,false,'Authenticated editor should open');
 assert.equal((element('#admin-nav').innerHTML.match(/data-tab=/g)||[]).length,15);
 const tabs=['overview','general','text','sections','services','projects','media','faqs','documents','reviews','seo','tracking','leads','glossary','system'];
 for(const tab of tabs){const html=await click({tab});assert(html.length>50,`Empty tab ${tab}`);assert(!/[ыэёЫЭЁъЪ]/.test(html),`Russian text in ${tab}`);assert(!html.includes('undefined'),'Unresolved value in '+tab);checkPaths(html);}
 let html=await click({tab:'text'});assert(html.includes('Переклад українською'));assert(html.includes('Uusi elämä'));assert(!html.includes('English — англійська</label>'),'Default mode must focus Finnish/Ukrainian');
 html=await click({editItem:'services:5'});assert(html.includes('Palokatkotyöt')&&html.includes('Протипожежна'));assert(html.includes('english-fold'));checkPaths(html);
 html=await click({tab:'general'});assert(html.includes('Yrityksen nimi — назва компанії'));assert(html.includes('Скандинавське оформлення'));
 await click({tab:'text'});
 for(const fn of listeners.get('input')||[])fn({target:{dataset:{path:JSON.stringify(['text','uk','hero.title'])},type:'textarea',value:'Редакторська примітка'}});
 assert.equal(element('#save-status').textContent,'Є незбережені зміни');
 await click({},'save-settings');const save=requests.find(r=>r.url==='/api/admin/state'&&r.options.method==='PUT');assert(save);const payload=JSON.parse(save.options.body);assert.equal(payload.text.uk['hero.title'],'Редакторська примітка');assert.equal(payload.text.fi['hero.title'],defaults.text.fi['hero.title']);assert.equal(save.options.headers['X-CSRF-Token'],'test-csrf');
 // Exercise a failed server save through the same localized error path.
 context.fetch=async()=>({ok:false,status:400,json:async()=>({error:'accent must be a hex colour'})});
 await click({},'save-settings');assert.equal(element('#toast').textContent,'Акцентний колір має бути у форматі #RRGGBB.');
 assert(context.SVMAdminHelp.glossary.some(([fi,uk])=>fi==='Palokatkotyöt'&&uk.includes('Протипожежна')));
 console.log('PASS: 15 editor tabs, field paths, Finnish/Ukrainian pairs, save isolation, CSRF and localized errors');
})().catch(error=>{console.error(error);process.exitCode=1});
