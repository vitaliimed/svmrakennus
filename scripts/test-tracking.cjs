const fs=require('node:fs');
const vm=require('node:vm');
const assert=require('node:assert/strict');
const source=fs.readFileSync('web/static/tracking.js','utf8');
const base=JSON.parse(fs.readFileSync('web/default.json','utf8')).tracking;
function boot(consent=null,mode='direct'){
  const tracking=structuredClone(base);Object.assign(tracking,{mode,ga4:'G-TEST123',ads:'AW-12345',gtm:'GTM-TEST123',meta:'12345',linkedIn:'45678',tikTok:'TESTPIXEL'});
  tracking.events.generate_lead.adsLabel='LeadLabel';tracking.events.generate_lead.linkedIn='9876';
  const scripts=[],values=new Map();if(consent)values.set('svm-consent-v1',JSON.stringify({...consent,time:Date.now()}));
  const context={URLSearchParams,document:{getElementById:()=>({textContent:JSON.stringify({lang:'en',tracking})}),createElement:()=>({}),head:{append:el=>scripts.push(el.src)}},localStorage:{getItem:k=>values.get(k)||null,setItem:(k,v)=>values.set(k,v)},location:{origin:'https://example.fi',pathname:'/en/',search:'?email=secret@example.fi',reload:()=>context.reloads++},reloads:0};context.window=context;vm.createContext(context);vm.runInContext(source,context);return {context,scripts,tracking};
}
const {context:c,scripts}=boot();assert.equal(scripts.length,0,'No vendor requests before consent');
c.SVMTracking.emit('generate_lead',{event_id:'first',email:'secret@example.fi'});assert.equal(c.dataLayer.filter(x=>x.event==='generate_lead').length,0,'No tracking on denied consent');
c.SVMTracking.choose({analytics:true,marketing:false});assert.equal(scripts.length,1);assert.match(scripts[0],/gtag/);
c.SVMTracking.emit('generate_lead',{event_id:'second',email:'secret@example.fi',phone:'+358123'});
const events=c.dataLayer.filter(x=>x.event==='generate_lead');assert.equal(events.length,1);assert.equal(events[0].email,undefined);assert.equal(events[0].phone,undefined);
assert.equal(JSON.stringify(c.dataLayer).includes('secret@example.fi'),false,'PII must not appear in app event payloads');
c.SVMTracking.choose({analytics:true,marketing:true});assert.equal(scripts.length,4,'GA + Meta + LinkedIn + TikTok');
c.SVMTracking.emit('generate_lead',{event_id:'third'});const before=c.dataLayer.length;c.SVMTracking.emit('generate_lead',{event_id:'third'});assert.equal(c.dataLayer.length,before,'Lead ID deduplicates conversions');
assert(c.fbq.queue.some(x=>x[0]==='track'&&x[1]==='Lead'));assert(c.lintrk.q.some(x=>x[1].conversion_id===9876));assert(c.ttq.some(x=>x[0]==='track'&&x[1]==='Lead'));
c.SVMTracking.choose({analytics:true,marketing:true});assert.equal(scripts.length,4,'Consent save must not duplicate script loads');
c.SVMTracking.choose({analytics:false,marketing:false});assert.equal(c.reloads,1,'Withdrawal unloads vendor libraries');
const gtm=boot(null,'gtm');gtm.context.SVMTracking.choose({analytics:true,marketing:false});assert.equal(gtm.scripts.length,0,'GTM remains off until both categories are accepted');gtm.context.SVMTracking.choose({analytics:true,marketing:true});assert(gtm.scripts.some(s=>s.includes('gtm.js')));assert(!gtm.scripts.some(s=>s.includes('/gtag/js')),'GTM and direct Google tags are mutually exclusive');
console.log('PASS: consent gates, event allowlist, lead deduplication, five platform queues, GTM separation and withdrawal');
