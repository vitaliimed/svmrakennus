#!/usr/bin/env python3
"""Compare production defaults and assets with the selected, immutable concept."""
import base64
import json
from pathlib import Path
import re

root = Path(__file__).resolve().parent.parent
reference = (root / 'docs/Contrast-selected.html').read_text()
preview = json.loads(re.search(
    r'<script id="preview-data" type="application/json">(.*?)</script>',
    reference, re.S)[1])
state = json.loads((root / 'web/default.json').read_text())
for lang in ('fi', 'en'):
    for key, value in preview['text'][lang].items():
        assert state['text'][lang][key] == value, f'Contrast copy differs: {lang}/{key}'
for url, value in preview['assets'].items():
    assert (root / 'web' / url.lstrip('/')).read_bytes() == base64.b64decode(
        value.split(',', 1)[1]), f'Contrast image differs: {url}'
for key, value in dict(accent='#fbbb21', paper='#f8faff', ink='#090b19',
                       surface='#eceff2', headingFont='sans', logo='').items():
    assert state['settings'][key] == value, f'Contrast setting differs: {key}'

# Keep every selected CSS rule. Only the about surface becomes an editor variable,
# and the root gains the theme controls used by the server-rendered pages.
selected = re.search(r'<style[^>]*>(.*?)</style>', reference, re.S)[1]
selected = selected[:selected.index('.preview-modal{')].strip()
css = (root / 'web/static/site.css').read_text()
for rule in selected.split('}'):
    if not rule.strip():
        continue
    rule = rule.replace('background:#eceff2', 'background:var(--surface)')
    if rule.startswith(':root{'):
        rule = rule.replace('--max:1440px', '--max:1440px;--surface:#eceff2;--heading:Arial,Helvetica,sans-serif')
    assert rule + '}' in css, f'Contrast style differs: {rule[:100]}'
assert css == (root / 'web/static/site-new.css').read_text(), 'Legacy stylesheet differs'
print('PASS: Contrast FI/EN copy, five image assets, theme defaults and selected CSS')
