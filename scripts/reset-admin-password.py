#!/usr/bin/env python3
"""Owner-only recovery: update auth with Firestore optimistic concurrency."""
import argparse,datetime,getpass,hashlib,json,re,secrets,subprocess,sys,urllib.error,urllib.parse,urllib.request

def main():
    parser=argparse.ArgumentParser(description='Reset the site admin password without deleting content.')
    parser.add_argument('--project',required=True)
    parser.add_argument('--database',required=True)
    parser.add_argument('--namespace',required=True)
    parser.add_argument('--setup-password',action='store_true',help='Explicitly set the initial setup password 12345 without prompting.')
    args=parser.parse_args()
    if not re.fullmatch(r'[a-z][a-z0-9-]{4,28}[a-z0-9]',args.project):
        raise ValueError('Invalid project ID')
    for value in (args.database,args.namespace):
        if not re.fullmatch(r'[A-Za-z0-9_()-]{1,80}',value):
            raise ValueError('Invalid database or namespace')
    if args.setup_password:
        password='12345'
    else:
        password=getpass.getpass('New admin password (at least 12 characters): ')
        confirm=getpass.getpass('Repeat password: ')
        if password!=confirm or not 12<=len(password)<=256 or len(password.encode())>256:
            raise ValueError('Passwords must match, contain at least 12 characters and fit within 256 UTF-8 bytes')
    result=subprocess.run(['gcloud','auth','print-access-token','--project',args.project],check=True,capture_output=True,text=True)
    token=result.stdout.strip()
    if not token:
        raise ValueError('No gcloud access token')
    quote=urllib.parse.quote
    endpoint='https://firestore.googleapis.com/v1/projects/'+quote(args.project,safe='')+'/databases/'+quote(args.database,safe='')+'/documents/'+quote(args.namespace,safe='')+'/main/state/settings'
    headers={'Authorization':'Bearer '+token,'Content-Type':'application/json'}
    with urllib.request.urlopen(urllib.request.Request(endpoint,headers=headers),timeout=30) as response:
        document=json.load(response)
    state=json.loads(document['fields']['payload']['stringValue'])
    if state.get('schema')!=1:
        raise ValueError('Unsupported site schema')
    salt=secrets.token_bytes(24)
    derived=hashlib.pbkdf2_hmac('sha256',password.encode(),salt,600000).hex()
    state['auth']={'hash':'pbkdf2-sha256$600000$'+salt.hex()+'$'+derived,'version':state['auth']['version']+1,'changed':not args.setup_password}
    state['revision']+=1
    state['updated']=datetime.datetime.now(datetime.timezone.utc).isoformat(timespec='seconds').replace('+00:00','Z')
    payload=json.dumps({'fields':{'payload':{'stringValue':json.dumps(state,ensure_ascii=False,separators=(',',':'))}}},ensure_ascii=False).encode()
    query=urllib.parse.urlencode({'currentDocument.updateTime':document['updateTime']})
    request=urllib.request.Request(endpoint+'?'+query,data=payload,headers=headers,method='PATCH')
    with urllib.request.urlopen(request,timeout=30) as response:
        if response.status!=200:
            raise ValueError('Password update did not succeed')
    print('Password updated. Old sessions have been invalidated. Content and leads are unchanged.')

if __name__=='__main__':
    try:
        main()
    except urllib.error.HTTPError as error:
        print('Firestore request failed with HTTP '+str(error.code)+'. Check permissions or retry after a concurrent update.',file=sys.stderr)
        sys.exit(1)
    except (ValueError,KeyError,subprocess.CalledProcessError,urllib.error.URLError) as error:
        print('Recovery failed: '+str(error),file=sys.stderr)
        sys.exit(1)
