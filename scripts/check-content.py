#!/usr/bin/env python3
"""Static release checks; runtime behaviours are covered by Go / JS tests."""
from pathlib import Path
import json,re

root=Path(__file__).resolve().parent.parent
data=json.loads((root/'web/default.json').read_text())
assert set(data['text']['en'])==set(data['text']['fi'])==set(data['text']['uk']), 'Language keys differ'
assert all(data['text'][lang][key] for lang in ('en','fi','uk') for key in data['text'][lang]), 'Empty default translations'
assert data['settings']['mainLanguage']=='fi'
assert data['settings']['indexing'] is False and data['settings']['demo'] is True
assert not data['reviews'] and not data['documents'], 'Do not ship invented reviews or certifications'
for collection in ('services','projects'):
    assert len({x['slug'] for x in data[collection]})==len(data[collection])
    for item in data[collection]:
        assert re.fullmatch(r'[a-z0-9][a-z0-9-]*',item['slug'])
        for lang in ('en','fi'):
            assert item['title'][lang] and item['summary'][lang] and item['body'][lang]
        for url in [item['image'],*item['gallery']]:
            if url.startswith('/static/'):
                assert (root/'web'/url.lstrip('/')).exists(), url
for name in ('hero','interior','terrace','craft'):
    assert (root/f'web/static/images/{name}.webp').stat().st_size<500_000
html=(root/'web/templates/page.html').read_text()
for token in ('hreflang="en"','hreflang="fi"','rel="canonical"','application/ld+json','og:image','twitter:card','multipart/form-data','name="consent"','loading="lazy"'):
    assert token in html, token
for css in ('site','admin'):
    source=(root/f'web/static/{css}.css').read_text()
    assert '@media' in source and 'max-width:540px' in source if css=='site' else '@media' in source
print(f'PASS: {len(data["text"]["en"])} text fields in each language, default images, SEO markup and launch safeguards')

assert data['settings']['company']=='SVM Rakennus'
assert '<html lang="uk">' in (root/'web/templates/admin.html').read_text()
assert not re.search('[ыэёЫЭЁъЪ]',(root/'web/static/admin.js').read_text())
assert not re.search('NORDRA|Nordra|nordra',data['seo']['fi']['title'])
print('PASS: SVM branding, Finnish primary language, Ukrainian editor translations')
