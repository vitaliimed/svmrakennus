(() => {
  'use strict';
  const configNode = document.getElementById('site-config');
  if (!configNode) return;
  const cfg = JSON.parse(configNode.textContent), tags = cfg.tracking;
  const key = 'svm-consent-v1', lifetime = 180 * 86400000;
  const denied = () => ({analytics:false, marketing:false});
  const read = () => {try {const v = JSON.parse(localStorage.getItem(key));return v && Date.now() - v.time < lifetime ? v : null;} catch {return null;}};
  let consent = read() || denied();
  const loaded = new Set(), sent = new Set();
  window.dataLayer = window.dataLayer || [];
  window.gtag = window.gtag || function(){window.dataLayer.push(arguments);};
  window.gtag('consent','default',{analytics_storage:'denied',ad_storage:'denied',ad_user_data:'denied',ad_personalization:'denied'});
  function script(name, src){if (loaded.has(name)) return; loaded.add(name); const el=document.createElement('script');el.src=src;el.async=true;document.head.append(el);}
  function cleanURL(){const raw=new URLSearchParams(location.search),query=new URLSearchParams();const keys=['utm_source','utm_medium','utm_campaign','utm_content','utm_term'];if(consent.marketing)keys.push('gclid','gbraid','wbraid','gad_source');keys.forEach(k=>{const v=raw.get(k);if(v)query.set(k,v.slice(0,200));});return location.origin+location.pathname+(query.size?'?'+query.toString():'');}
  function init(){
    window.gtag('consent','update',{analytics_storage:consent.analytics?'granted':'denied',ad_storage:consent.marketing?'granted':'denied',ad_user_data:consent.marketing?'granted':'denied',ad_personalization:consent.marketing?'granted':'denied'});
    if(tags.mode==='gtm') {
      // Conservative gate: custom GTM tags can access both categories.
      if(tags.gtm && consent.analytics && consent.marketing && !loaded.has('gtm')){window.dataLayer.push({'gtm.start':Date.now(),event:'gtm.js'});script('gtm','https://www.googletagmanager.com/gtm.js?id='+encodeURIComponent(tags.gtm));}
    } else {
      const googleID=(consent.analytics && tags.ga4) || (consent.marketing && tags.ads);
      if(googleID && !loaded.has('gtag')){script('gtag','https://www.googletagmanager.com/gtag/js?id='+encodeURIComponent(googleID));window.gtag('js',new Date());}
      if(consent.analytics && tags.ga4 && !loaded.has('ga4-config')){loaded.add('ga4-config');window.gtag('config',tags.ga4,{page_location:cleanURL(),page_title:document.title,send_page_view:true,allow_google_signals:consent.marketing,allow_ad_personalization_signals:consent.marketing});}
      if(consent.marketing && tags.ads && !loaded.has('ads-config')){loaded.add('ads-config');window.gtag('config',tags.ads,{page_location:cleanURL(),allow_enhanced_conversions:false});}
    }
    if(!consent.marketing) return;
    if(tags.meta && !loaded.has('meta')){
      const fb=window.fbq=function(){fb.callMethod?fb.callMethod.apply(fb,arguments):fb.queue.push(arguments);};fb.push=fb;fb.loaded=true;fb.version='2.0';fb.queue=[];window._fbq=fb;
      script('meta','https://connect.facebook.net/en_US/fbevents.js');fb('set','autoConfig',false,tags.meta);fb('init',tags.meta);fb('consent','grant');fb('track','PageView');
    }
    if(tags.linkedIn && !loaded.has('linkedin')){
      window._linkedin_partner_id=tags.linkedIn;window._linkedin_data_partner_ids=[tags.linkedIn];window.lintrk=window.lintrk||function(a,b){window.lintrk.q=window.lintrk.q||[];window.lintrk.q.push([a,b]);};script('linkedin','https://snap.licdn.com/li.lms-analytics/insight.min.js');
    }
    if(tags.tikTok && !loaded.has('tiktok')){
      window.TiktokAnalyticsObject='ttq';const ttq=window.ttq=window.ttq||[];
      ttq.methods=['page','track','identify','instances','debug','on','off','once','ready','alias','group','enableCookie','disableCookie','holdConsent','revokeConsent','grantConsent'];
      ttq.setAndDefer=(t,e)=>{t[e]=function(){t.push([e].concat(Array.from(arguments)));};};ttq.methods.forEach(m=>ttq.setAndDefer(ttq,m));
      ttq._i=ttq._i||{};ttq._i[tags.tikTok]=[];ttq._i[tags.tikTok]._u='https://analytics.tiktok.com/i18n/pixel/events.js';ttq._t=ttq._t||{};ttq._t[tags.tikTok]=Date.now();ttq._o=ttq._o||{};ttq._o[tags.tikTok]={};
      ttq.instance=(id)=>{const q=ttq._i[id]||[];ttq.methods.forEach(m=>ttq.setAndDefer(q,m));return q;};
      script('tiktok','https://analytics.tiktok.com/i18n/pixel/events.js?sdkid='+encodeURIComponent(tags.tikTok)+'&lib=ttq');ttq.grantConsent();ttq.page();
    }
  }
  function emit(name, params={}){
    const rule=tags.events[name];if(!rule || !rule.enabled) return;
    if(params.event_id){const dedup=name+':'+params.event_id;if(sent.has(dedup))return;sent.add(dedup);}
    // Explicit allowlist: no names, emails, phone numbers or enquiry text.
    const safe={page_path:location.pathname,language:cfg.lang};['event_id','content_id','content_type','percent_scrolled'].forEach(k=>{if(params[k]!==undefined)safe[k]=params[k];});
    if(consent.analytics || consent.marketing){window.dataLayer.push({event:name,...safe});}
    if(tags.mode==='direct'){
      if(consent.analytics && tags.ga4 && rule.ga){window.gtag('event',rule.ga,{...safe,send_to:tags.ga4});}
      if(consent.marketing && tags.ads && rule.adsLabel){window.gtag('event','conversion',{send_to:tags.ads+'/'+rule.adsLabel,transaction_id:params.event_id||undefined});}
    }
    if(consent.marketing && tags.meta && rule.meta && window.fbq){const standard=new Set(['Lead','Contact','ViewContent','CompleteRegistration','Subscribe','Schedule']);window.fbq(standard.has(rule.meta)?'track':'trackCustom',rule.meta,safe,params.event_id?{eventID:params.event_id}:{});}
    if(consent.marketing && tags.linkedIn && /^\d+$/.test(rule.linkedIn) && window.lintrk){window.lintrk('track',{conversion_id:Number(rule.linkedIn)});}
    if(consent.marketing && tags.tikTok && rule.tikTok && window.ttq){window.ttq.track(rule.tikTok,safe,params.event_id?{event_id:params.event_id}:{});}
  }
  function choose(next){
    const previous=consent;consent={analytics:!!next.analytics,marketing:!!next.marketing,time:Date.now()};try{localStorage.setItem(key,JSON.stringify(consent));}catch{}
    if((previous.analytics && !consent.analytics)||(previous.marketing && !consent.marketing)){
      if(window.fbq)window.fbq('consent','revoke');if(window.ttq?.revokeConsent)window.ttq.revokeConsent();window.gtag('consent','update',{analytics_storage:consent.analytics?'granted':'denied',ad_storage:consent.marketing?'granted':'denied',ad_user_data:consent.marketing?'granted':'denied',ad_personalization:consent.marketing?'granted':'denied'});
      // Unload vendor libraries; existing third-party cookies are provider-managed.
      location.reload();return;
    }
    init();
  }
  init();window.SVMTracking={emit,choose,get:()=>({...consent}),hasChoice:()=>!!read()};
})();
