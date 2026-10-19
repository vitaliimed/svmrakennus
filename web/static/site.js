(() => {
  'use strict';
  const cfg=JSON.parse(document.getElementById('site-config').textContent),tracking=window.SVMTracking;
  const menu=document.querySelector('.menu-toggle'),nav=document.getElementById('main-nav');
  menu?.addEventListener('click',()=>{const open=nav.classList.toggle('open');menu.setAttribute('aria-expanded',String(open));});
  nav?.querySelectorAll('a').forEach(a=>a.addEventListener('click',()=>{nav.classList.remove('open');menu?.setAttribute('aria-expanded','false');}));
  document.addEventListener('keydown',e=>{if(e.key==='Escape'){nav?.classList.remove('open');menu?.setAttribute('aria-expanded','false');}});
  const consentDialog=document.getElementById('consent-dialog');
  function showConsent(modal){const c=tracking.get();document.getElementById('consent-analytics').checked=c.analytics;document.getElementById('consent-marketing').checked=c.marketing;if(!consentDialog.open){modal?consentDialog.showModal():consentDialog.show();}}
  document.querySelectorAll('[data-consent-open]').forEach(b=>b.addEventListener('click',()=>showConsent(true)));
  consentDialog.querySelectorAll('[data-consent]').forEach(b=>b.addEventListener('click',()=>{let next;const mode=b.dataset.consent;if(mode==='all')next={analytics:true,marketing:true};else if(mode==='none')next={analytics:false,marketing:false};else next={analytics:document.getElementById('consent-analytics').checked,marketing:document.getElementById('consent-marketing').checked};consentDialog.close();tracking.choose(next);}));
  if(!tracking.hasChoice())showConsent(false);
  document.querySelectorAll('[data-event]').forEach(a=>a.addEventListener('click',()=>tracking.emit(a.dataset.event)));
  const path=location.pathname.split('/').filter(Boolean);if(path[1]==='services')tracking.emit('service_view',{content_id:path[2],content_type:'service'});if(path[1]==='projects')tracking.emit('project_view',{content_id:path[2],content_type:'project'});
  let thresholds=new Set();let queued=false;window.addEventListener('scroll',()=>{if(queued)return;queued=true;requestAnimationFrame(()=>{queued=false;const height=document.documentElement.scrollHeight-innerHeight;if(height<=0)return;const percent=scrollY/height*100;[25,50,75,90].forEach(n=>{if(percent>=n&&!thresholds.has(n)){thresholds.add(n);tracking.emit('scroll_depth',{percent_scrolled:n});}});});},{passive:true});
  const form=document.getElementById('lead-form');if(form){
    let submissionToken=null;let started=false;form.addEventListener('focusin',()=>{if(!started){started=true;tracking.emit('form_start');}});
    form.addEventListener('submit',async e=>{
      e.preventDefault();if(!form.reportValidity())return;const result=document.getElementById('form-result'),button=form.querySelector('[type="submit"]');result.className='';result.textContent='';
      const files=form.querySelector('[type="file"]')?.files||[];if(files.length>3||Array.from(files).some(f=>f.size>8*1024*1024)){result.className='error';result.textContent=cfg.labels.filesError;return;}
      button.disabled=true;button.textContent=cfg.labels.sending;
      try{
        if(!submissionToken){const tokenResponse=await fetch('/api/form-token',{cache:'no-store'});if(!tokenResponse.ok)throw new Error(cfg.labels.error);submissionToken=await tokenResponse.json();}form.elements.token.value=submissionToken.token;
        const data=new FormData(form);const query=new URLSearchParams(location.search);['utm_source','utm_medium','utm_campaign','utm_content','utm_term'].forEach(k=>data.set(k,(query.get(k)||'').slice(0,150)));
        const response=await fetch('/api/lead',{method:'POST',body:data,headers:{Accept:'application/json'}});const body=await response.json();if(!response.ok){let message=cfg.labels.error;if(response.status===429)message=cfg.labels.rateError;else if(/expired|token/i.test(body.error||'')){submissionToken=null;message=cfg.labels.tokenError;}else if(/file|attachment|image|extension/i.test(body.error||''))message=cfg.labels.filesError;else if(response.status===400)message=cfg.labels.validationError;throw new Error(message);}
        tracking.emit('generate_lead',{event_id:body.id});submissionToken=null;form.reset();started=false;result.className='success';result.textContent=cfg.labels.success;
      }catch(error){result.className='error';result.textContent=[cfg.labels.error,cfg.labels.filesError,cfg.labels.rateError,cfg.labels.validationError,cfg.labels.tokenError].includes(error.message)?error.message:cfg.labels.error;}finally{button.disabled=false;button.textContent=cfg.labels.submit+' ↗';}
    });
  }
  const gallery=document.getElementById('gallery-dialog');document.querySelectorAll('[data-image]').forEach(b=>b.addEventListener('click',()=>{gallery.querySelector('img').src=b.dataset.image;gallery.querySelector('img').alt=b.querySelector('img').alt;gallery.showModal();}));gallery.querySelector('.gallery-close').addEventListener('click',()=>gallery.close());gallery.addEventListener('click',e=>{if(e.target===gallery)gallery.close();});
})();
