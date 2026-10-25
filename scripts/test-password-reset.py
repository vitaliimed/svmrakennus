#!/usr/bin/env python3
"""Exercise owner recovery against an in-memory Firestore response, without cloud access."""
import contextlib
import copy
import hashlib
import importlib.util
import io
import json
import sys
from pathlib import Path
from types import SimpleNamespace
from unittest.mock import patch
from urllib.parse import parse_qs, urlsplit

script = Path(__file__).with_name('reset-admin-password.py')
sys.dont_write_bytecode = True
spec = importlib.util.spec_from_file_location('svm_password_recovery', script)
recovery = importlib.util.module_from_spec(spec)
spec.loader.exec_module(recovery)
state = {'schema': 1, 'revision': 9, 'updated': 'before',
         'auth': {'hash': 'old', 'version': 4, 'changed': True},
         'settings': {'company': 'SVM Rakennus'}, 'text': {'fi': {'hero.title': 'Keep this'}}}
document = {'fields': {'payload': {'stringValue': json.dumps(state)}},
            'updateTime': '2026-10-20T12:00:00Z'}
args = ['reset-admin-password.py', '--project', 'project-c6c1d639-9739-4837-9c8',
        '--database', 'svm-rakennus', '--namespace', 'svm_rakennus_site']

def run(extra=(), passwords=()):
    requests = []
    def respond(request, timeout):
        requests.append(request)
        response = io.BytesIO(json.dumps(document if request.get_method() == 'GET' else {}).encode())
        response.status = 200
        return response
    output = io.StringIO()
    with patch.object(recovery.sys, 'argv', args + list(extra)), \
         patch.object(recovery.getpass, 'getpass', side_effect=passwords) as prompt, \
         patch.object(recovery.subprocess, 'run', return_value=SimpleNamespace(stdout='test-access-token')), \
         patch.object(recovery.urllib.request, 'urlopen', side_effect=respond), \
         contextlib.redirect_stdout(output):
        recovery.main()
    assert len(requests) == 2
    assert requests[0].get_method() == 'GET' and requests[1].get_method() == 'PATCH'
    assert parse_qs(urlsplit(requests[1].full_url).query)['currentDocument.updateTime'] == [document['updateTime']]
    assert requests[0].full_url.endswith('/documents/svm_rakennus_site/main/state/settings')
    updated = json.loads(json.loads(requests[1].data)['fields']['payload']['stringValue'])
    expected = copy.deepcopy(state)
    expected.update(auth=updated['auth'], revision=10, updated=updated['updated'])
    assert updated == expected, 'Recovery must preserve existing site content'
    assert updated['auth']['version'] == 5, 'Old sessions must be invalidated'
    assert 'test-access-token' not in output.getvalue()
    return updated, prompt.call_count

def password_matches(auth, password):
    scheme, iterations, salt, actual = auth['hash'].split('$')
    assert scheme == 'pbkdf2-sha256' and int(iterations) == 600000
    assert len(bytes.fromhex(salt)) == 24
    assert hashlib.pbkdf2_hmac('sha256', password.encode(), bytes.fromhex(salt), int(iterations)).hex() == actual

updated, prompts = run(['--setup-password'])
password_matches(updated['auth'], '12345')
assert prompts == 0 and updated['auth']['changed'] is False
updated, prompts = run(passwords=['a-strong-recovery-password'] * 2)
password_matches(updated['auth'], 'a-strong-recovery-password')
assert prompts == 2 and updated['auth']['changed'] is True
try:
    run(passwords=['12345'] * 2)
except ValueError:
    pass
else:
    raise AssertionError('Short passwords require the explicit setup option')
print('PASS: setup password 12345, regular recovery, password hash, session revocation, concurrency guard and content preservation (no cloud requests)')
