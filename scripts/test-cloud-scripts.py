#!/usr/bin/env python3
"""Exercise infrastructure scripts against an isolated fake CLI, without cloud calls."""
import json
import os
from pathlib import Path
import shutil
import subprocess
import tempfile

ROOT = Path(__file__).resolve().parent.parent
PROJECT = 'project-c6c1d639-9739-4837-9c8'
FAKE = r'''#!/usr/bin/env python3
import json,os,sys
from pathlib import Path
path=Path(os.environ['SVM_MOCK_STATE'])
s=json.loads(path.read_text())
a=sys.argv[1:];s.setdefault('calls',[]).append(a)
out=''
if a[:3]==['billing','projects','describe']:out='True'
elif a[:3]==['iam','service-accounts','describe']:out='True' if s.get('disabled') else 'False'
elif a[:3]==['firestore','databases','list']:
 if s.get('deny_database'):
  path.write_text(json.dumps(s));sys.exit(1)
 out=json.dumps([{'name':'projects/test/databases/svm-rakennus','type':'FIRESTORE_NATIVE','locationId':s.get('database_location','europe-north1')}] if s.get('database') else [])
elif a[:3]==['firestore','databases','create']:s['database']=True
elif a[:3]==['storage','buckets','list']:
 assert '--uri' in a and not any(x.startswith('--filter') for x in a)
 if s.get('deny_bucket_list'):
  path.write_text(json.dumps(s));sys.exit(1)
 out='gs://unrelated-bucket/\ngs://project-c6c1d639-9739-4837-9c8-svm-rakennus-media/' if s.get('bucket') else ''
elif a[:3]==['storage','buckets','describe']:out=s.get('bucket_location','EUROPE-NORTH1')
elif a[:3]==['storage','buckets','create']:s['bucket']=True
elif a[:2]==['secrets','describe']:
 if a[2] not in s.get('secrets',[]):
  path.write_text(json.dumps(s));sys.exit(1)
elif a[:2]==['secrets','create']:
 s.setdefault('secrets',[]).append(a[2]);s.setdefault('secret_lengths',{})[a[2]]=len(sys.stdin.read())
elif a[:3]==['secrets','versions','describe']:out='DISABLED' if s.get('disabled_secret') else 'ENABLED'
elif a[:3]==['run','services','describe']:out='https://svm-test.run.app'
path.write_text(json.dumps(s))
if out:print(out)
'''

with tempfile.TemporaryDirectory(prefix='svm-cloud-tests-') as directory:
    work = Path(directory)
    (work / 'scripts').mkdir()
    (work / 'cli').mkdir()
    for name in ('bootstrap-gcloud.sh', 'deploy-cloud-run.sh'):
        shutil.copyfile(ROOT / 'scripts' / name, work / 'scripts' / name)
    (work / 'scripts/verify.sh').write_text('#!/bin/bash\nset -euo pipefail\necho mock-verification\n')
    cli = work / 'cli/gcloud'
    cli.write_text(FAKE)
    cli.chmod(0o755)
    state_path = work / 'state.json'
    env = dict(os.environ, PATH=str(work / 'cli') + os.pathsep + os.environ['PATH'],
               SVM_MOCK_STATE=str(state_path))

    def save(value):
        state_path.write_text(json.dumps(value))

    def state():
        return json.loads(state_path.read_text())

    def run(script, *args, password='', ok=True):
        result = subprocess.run(['bash', 'scripts/' + script, *args], cwd=work, env=env,
                                input=password, text=True, capture_output=True, timeout=30)
        assert (result.returncode == 0) == ok, (script, result.stdout, result.stderr)
        return result

    save({})
    password = 'test-only-admin-password'
    output = run('bootstrap-gcloud.sh', PROJECT, password=password + '\n' + password + '\n')
    initial = state()
    assert password not in output.stdout + output.stderr
    assert initial['secret_lengths']['svm-rakennus-admin-password'] == len(password)
    assert initial['secret_lengths']['svm-rakennus-session'] == 96
    config = (work / '.deployment.env').read_text()
    assert 'SITE_RUNTIME=svm-runtime@' + PROJECT in config
    assert 'SITE_BUILDER=svm-build@' + PROJECT in config
    assert (work / '.deployment.env').stat().st_mode & 0o777 == 0o600
    assert not any(c[:2] == ['run', 'deploy'] or c[:3] == ['iam', 'service-accounts', 'create'] for c in initial['calls'])
    for call in initial['calls']:
        assert PROJECT in call or ('--project' in call and call[call.index('--project') + 1] == PROJECT), call
    before = len(initial['calls'])
    run('bootstrap-gcloud.sh', PROJECT)
    assert not any('create' in c or c[:3] == ['secrets', 'versions', 'add'] for c in state()['calls'][before:])

    run('deploy-cloud-run.sh', ok=False)
    run('deploy-cloud-run.sh', '--private')
    private = next(c for c in reversed(state()['calls']) if c[:2] == ['run', 'deploy'])
    assert '--invoker-iam-check' in private and '--no-allow-unauthenticated' in private
    assert '--no-invoker-iam-check' not in private
    assert private[private.index('--build-service-account') + 1] == f'projects/{PROJECT}/serviceAccounts/svm-build@{PROJECT}.iam.gserviceaccount.com'
    run('deploy-cloud-run.sh', '--public')
    public = next(c for c in reversed(state()['calls']) if c[:2] == ['run', 'deploy'])
    assert '--no-invoker-iam-check' in public

    for failure in ({'disabled': True}, {'deny_database': True},
                    {'database': True, 'database_location': 'us-central1'},
                    {'database': True, 'deny_bucket_list': True}):
        save(failure)
        run('bootstrap-gcloud.sh', PROJECT, ok=False)
        assert not any('create' in c for c in state()['calls'])
    save(dict(initial, disabled_secret=True, calls=[]))
    run('bootstrap-gcloud.sh', PROJECT, ok=False)
    assert not any('create' in c for c in state()['calls'])
    save({})
    (work / '.deployment.env').unlink()
    run('bootstrap-gcloud.sh', PROJECT, password='\n', ok=False)
    assert 'svm-rakennus-admin-password' not in state().get('secrets', [])
    assert not (work / '.deployment.env').exists()
print('PASS: explicit project, existing accounts, repeat setup, private/public deploy, disabled account/API/location failures and password handling (mock CLI)')
