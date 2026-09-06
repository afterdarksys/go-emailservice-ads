#!/usr/bin/env python3
"""Isolated SMTP->IMAP, graceful shutdown, backup and restore qualification."""
import argparse
import datetime
import urllib.error
import imaplib
import json
import os
import pathlib
import smtplib
import socket
import ssl
import subprocess
import tempfile
import time
import urllib.request

def port():
    with socket.socket() as sock:
        sock.bind(('127.0.0.1', 0))
        return sock.getsockname()[1]

def run(binary, backup):
    with tempfile.TemporaryDirectory(prefix='mailhub-smoke-') as directory:
        root = pathlib.Path(directory)
        cert, key = root/'cert.pem', root/'key.pem'
        subprocess.run(['openssl','req','-x509','-newkey','rsa:2048','-nodes','-keyout',str(key),'-out',str(cert),'-days','1','-subj','/CN=localhost','-addext','subjectAltName=DNS:localhost,IP:127.0.0.1'], check=True, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        smtp_port, imap_port, api_port = port(), port(), port()
        policies = root/'policies.yaml'
        policies.write_text('policies: []\n')
        config = {
            'server': {'addr':f'127.0.0.1:{smtp_port}', 'domain':'mail.test', 'local_domains':['mail.test'], 'require_auth':True, 'require_tls':True, 'tls':{'cert':str(cert),'key':str(key)}, 'spf':{'enabled':False}, 'dmarc':{'enabled':False}, 'dane':{'enabled':False}},
            'imap': {'addr':f'127.0.0.1:{imap_port}', 'tls_mode':'starttls','tls':{'cert':str(cert),'key':str(key)}},
            'api': {'rest_addr':f'127.0.0.1:{api_port}', 'tls':{'cert':str(cert),'key':str(key)}, 'api_keys':[{'name':'qa-admin','key':'qa-isolated-admin','permissions':['mailboxes:read','mailboxes:write','queue:read','policies:read','policies:write']},{'name':'qa-reader','key':'qa-isolated-reader','permissions':['queue:read']}]},
            'platform': {'data_dir':str(root/'data'),'policy_path':str(policies)},
            'auth': {'default_users':[{'username':'probe@mail.test','email':'probe@mail.test','password':'isolated-test-password'}]},
            'logging': {'level':'warn','format':'json'},
        }
        path=root/'config.yaml'; path.write_text(json.dumps(config))
        with (root/'service.log').open('w+') as log:
            test_env=dict(os.environ, MAILHUB_DATA_DIR=str(root/'data'))
            subprocess.run([binary,'--check-config','--config',str(path)],check=True,env=test_env)
            process=subprocess.Popen([binary,'--config',str(path)], stdout=log, stderr=log, env=test_env)
            try:
                deadline=time.monotonic()+30
                while time.monotonic()<deadline:
                    if process.poll() is not None:
                        log.seek(0); raise RuntimeError(log.read())
                    try:
                        with urllib.request.urlopen(f'https://localhost:{api_port}/ready',timeout=1,context=ssl.create_default_context(cafile=str(cert))) as response:
                            if response.status==200: break
                    except OSError: time.sleep(.2)
                else: raise RuntimeError('service readiness timed out')
                tls=ssl.create_default_context(cafile=str(cert))
                with smtplib.SMTP('localhost',smtp_port,timeout=10) as client:
                    client.starttls(context=tls)
                    client.login('probe@mail.test','isolated-test-password')
                    failures=client.sendmail('probe@mail.test',['probe@mail.test'],'From: probe@mail.test\r\nTo: probe@mail.test\r\nSubject: mailhub-qualification\r\n\r\nEnd-to-end test\r\n')
                    assert not failures, failures
                deadline=time.monotonic()+20
                while time.monotonic()<deadline:
                    with imaplib.IMAP4('localhost',imap_port,timeout=5) as client:
                        client.starttls(ssl_context=tls)
                        client.login('probe@mail.test','isolated-test-password')
                        client.select('INBOX')
                        status, messages=client.search(None,'SUBJECT','mailhub-qualification')
                        if status=='OK' and messages[0]: break
                    time.sleep(.2)
                else: raise RuntimeError('message not delivered through IMAP')
                def api(method,route,body=None,token='qa-isolated-admin',expected=200):
                    headers={'Authorization':'Bearer '+token}
                    if body is not None: headers['Content-Type']='application/json'
                    request=urllib.request.Request(f'https://localhost:{api_port}/api/v1/'+route,data=None if body is None else json.dumps(body).encode(),headers=headers,method=method)
                    try: response=urllib.request.urlopen(request,context=tls,timeout=10)
                    except urllib.error.HTTPError as error: response=error
                    with response:
                        payload=response.read()
                        assert response.status==expected,(route,response.status,payload)
                        return json.loads(payload) if payload and response.headers.get_content_type()=='application/json' else payload
                api('GET','queue/stats',token='',expected=401)
                api('POST','mailboxes',{'username':'qa@mail.test','password':'qa-new-password'},token='qa-isolated-reader',expected=403)
                api('POST','mailboxes',{'username':'qa@mail.test','password':'qa-new-password'},expected=201)
                api('PUT','mailboxes/qa%40mail.test',{'password':'x'*73},expected=400)
                api('PUT','mailboxes/qa%40mail.test',{'enabled':False})
                with smtplib.SMTP('localhost',smtp_port,timeout=10) as client:
                    client.starttls(context=tls); client.login('probe@mail.test','isolated-test-password')
                    assert client.mail('probe@mail.test')[0]==250
                    assert client.rcpt('qa@mail.test')[0]==550
                api('PUT','mailboxes/qa%40mail.test',{'enabled':True})
                api('POST','policies',{'name':'qa-rule','type':'starlark','enabled':False,'scope':{'type':'global'},'script':'reject("QA policy test")'},expected=201)
                tested=api('POST','policies/qa-rule/test',{'From':'qa@mail.test','To':['probe@mail.test'],'Subject':'synthetic','Body':'sample'})
                assert tested['action']['Type']=='reject',tested
                api('DELETE','policies/qa-rule',expected=204)
                with imaplib.IMAP4('localhost',imap_port,timeout=10) as client:
                    client.starttls(ssl_context=tls); client.login('qa@mail.test','qa-new-password')
                    def ok(result):
                        assert result[0]=='OK',result
                        return result[1]
                    ok(client.create('UAT/Work'));ok(client.create('UAT/Archive'))
                    ok(client.subscribe('UAT/Work'))
                    date=datetime.datetime(2020,1,2,12,0,tzinfo=datetime.timezone.utc)
                    raw=b'From: sender@mail.test\r\nTo: qa@mail.test\r\nDate: Mon, 02 Jan 2006 15:04:05 -0700\r\nSubject: UAT persistent folder\r\nContent-Type: text/plain\r\n\r\nSearchable body\r\n'
                    ok(client.append('UAT/Work','(\\Flagged customer)',date,raw))
                    assert ok(client.select('UAT/Work',readonly=True)) == [b'1'], 'SELECT omitted message count'
                    assert client.response('UIDVALIDITY')[1][0], 'SELECT omitted UIDVALIDITY'
                    ok(client.uid('fetch','*','(BODY[])'))
                    assert b'\\Seen' not in repr(ok(client.uid('fetch','*','(FLAGS)'))).encode(), 'EXAMINE changed flags'
                    ok(client.select('UAT/Work'))
                    assert ok(client.uid('search',None,'BODY','searchable'))[0], 'BODY search missed'
                    assert not ok(client.search(None,'TO','unknown@mail.test'))[0], 'header search ignored'
                    assert ok(client.search(None,'SENTBEFORE','01-Jan-2007'))[0], 'header/internal dates conflated'
                    fetched=ok(client.uid('fetch','*','(FLAGS INTERNALDATE BODY.PEEK[])'))
                    assert b'2020' in repr(fetched).encode() and b'customer' in repr(fetched).encode(),fetched
                    assert b'\\Seen' not in repr(fetched).encode(),fetched
                    ok(client.uid('fetch','*','(BODY[])'))
                    assert b'\\Seen' in repr(ok(client.uid('fetch','*','(FLAGS)'))).encode()
                    ok(client.uid('copy','*','UAT/Archive'))
                    ok(client.close())
                    ok(client.rename('UAT','Review'))
                    assert any(b'Review/Work' in row for row in ok(client.lsub())), 'subscription lost after rename'
                    ok(client.select('Review/Archive'))
                    assert b'\\Deleted' in repr(ok(client.uid('store','*','+FLAGS','(\\Deleted)'))).encode(), 'STORE omitted flags'
                    assert ok(client.expunge())==[b'1'], 'missing EXPUNGE notification'
                    ok(client.close());ok(client.delete('Review/Archive'))
                    ok(client.select('Review/Work'))
                    assert ok(client.uid('search',None,'ALL'))[0], 'COPY expunge removed source'
                    ok(client.close());ok(client.create('Bulk'))
                    for _ in range(260): ok(client.append('Bulk',None,None,raw))
                    assert ok(client.select('Bulk')) == [b'260']
                    ok(client.fetch('1:*','(BODY[])'))
                    assert len(ok(client.search(None,'SEEN'))[0].split()) == 260, 'bulk FETCH lost flags'
                    ok(client.close());ok(client.delete('Bulk'))


            finally:
                process.terminate()
                try: process.wait(timeout=15)
                except subprocess.TimeoutExpired:
                    process.kill(); process.wait(); raise RuntimeError('graceful shutdown timed out')
            if process.returncode!=0:
                log.seek(0); raise RuntimeError(log.read())
        archive=root/'snapshot.tar.gz'
        subprocess.run([backup,'create','--data-dir',str(root/'data'),'--archive',str(archive)],check=True)
        subprocess.run([backup,'verify','--archive',str(archive)],check=True)
        subprocess.run([backup,'restore','--archive',str(archive),'--data-dir',str(root/'restored')],check=True)
        assert (root/'restored'/'mailbox.db').exists()
        # Start the restored service and verify usable data, not just archive files.
        with (root/'restored.log').open('w+') as log:
            process=subprocess.Popen([binary,'--config',str(path)],stdout=log,stderr=log,env=dict(os.environ,MAILHUB_DATA_DIR=str(root/'restored')))
            try:
                deadline=time.monotonic()+30
                while time.monotonic()<deadline:
                    if process.poll() is not None:
                        log.seek(0);raise RuntimeError(log.read())
                    try:
                        with urllib.request.urlopen(f'https://localhost:{api_port}/ready',context=tls,timeout=1) as response:
                            if response.status==200:break
                    except OSError:time.sleep(.2)
                else:raise RuntimeError('restored service readiness timed out')
                with imaplib.IMAP4('localhost',imap_port,timeout=10) as client:
                    client.starttls(ssl_context=tls);client.login('qa@mail.test','qa-new-password')
                    assert client.select('Review/Work')[0]=='OK'
                    status,result=client.uid('fetch','*','(FLAGS INTERNALDATE BODY.PEEK[])')
                    assert status=='OK' and b'2020' in repr(result).encode() and b'Searchable body' in repr(result).encode(),result
                    assert not any(b'Review/Archive' in row for row in client.list()[1]),'deleted folder resurrected'
            finally:
                process.terminate()
                try:process.wait(timeout=15)
                except subprocess.TimeoutExpired:
                    process.kill();process.wait();raise RuntimeError('restored service shutdown timed out')
            assert process.returncode==0,process.returncode
        print('PASS: SMTP/IMAP delivery, REST authorization/accounts/policies, folder mutations, flags/search, shutdown and live restored-service verification')

if __name__=='__main__':
    parser=argparse.ArgumentParser()
    parser.add_argument('--binary',required=True)
    parser.add_argument('--backup',required=True)
    args=parser.parse_args()
    run(str(pathlib.Path(args.binary).resolve()),str(pathlib.Path(args.backup).resolve()))
