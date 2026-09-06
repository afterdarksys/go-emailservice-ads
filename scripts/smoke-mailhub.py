#!/usr/bin/env python3
"""Isolated SMTP->IMAP, graceful shutdown, backup and restore qualification."""
import argparse
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
            'api': {'rest_addr':f'127.0.0.1:{api_port}'},
            'platform': {'data_dir':str(root/'data'),'policy_path':str(policies)},
            'auth': {'default_users':[{'username':'probe@mail.test','email':'probe@mail.test','password':'isolated-test-password'}]},
            'logging': {'level':'warn','format':'json'},
        }
        path=root/'config.yaml'; path.write_text(json.dumps(config))
        with (root/'service.log').open('w+') as log:
            test_env=dict(os.environ, MAILHUB_DATA_DIR=str(root/'data'))
            process=subprocess.Popen([binary,'--config',str(path)], stdout=log, stderr=log, env=test_env)
            try:
                deadline=time.monotonic()+30
                while time.monotonic()<deadline:
                    if process.poll() is not None:
                        log.seek(0); raise RuntimeError(log.read())
                    try:
                        with urllib.request.urlopen(f'http://127.0.0.1:{api_port}/ready',timeout=1) as response:
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
        print('PASS: authenticated SMTP, IMAP delivery, shutdown, backup and restore')

if __name__=='__main__':
    parser=argparse.ArgumentParser()
    parser.add_argument('--binary',required=True)
    parser.add_argument('--backup',required=True)
    args=parser.parse_args()
    run(str(pathlib.Path(args.binary).resolve()),str(pathlib.Path(args.backup).resolve()))
