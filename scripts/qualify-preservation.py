#!/usr/bin/env python3
"""Object Lock qualification against an isolated local MinIO container."""
import datetime
import json
import os
import pathlib
import subprocess
import sys
import tempfile
import time
import urllib.request

def run(binary):
    image=subprocess.check_output(['docker','image','inspect','minio/minio:latest','--format','{{.Id}}'],text=True).strip()
    container=subprocess.check_output(['docker','run','--rm','-d','-p','127.0.0.1::9000','-e','MINIO_ROOT_USER=qualification','-e','MINIO_ROOT_PASSWORD=isolated-qualification-password',image,'server','/data'],text=True).strip()
    try:
        address=subprocess.check_output(['docker','port',container,'9000/tcp'],text=True).strip()
        endpoint='http://'+address
        for _ in range(100):
            try:
                with urllib.request.urlopen(endpoint+'/minio/health/ready',timeout=1) as response:
                    if response.status==200: break
            except OSError: time.sleep(.2)
        else: raise RuntimeError('MinIO readiness timed out')
        env=dict(os.environ,AWS_ACCESS_KEY_ID='qualification',AWS_SECRET_ACCESS_KEY='isolated-qualification-password',AWS_DEFAULT_REGION='us-east-1',AWS_PAGER='',AWS_EC2_METADATA_DISABLED='true')
        aws=['aws','--endpoint-url',endpoint,'--region','us-east-1','s3api']
        subprocess.run(aws+['create-bucket','--bucket','mailhub-qualification','--object-lock-enabled-for-bucket'],env=env,check=True,stdout=subprocess.DEVNULL)
        with tempfile.TemporaryDirectory(prefix='mailhub-object-lock-') as directory:
            root=pathlib.Path(directory)
            source=root/'checkpoint.json'; source.write_text('{"synthetic":"audit checkpoint"}\n')
            config={'source_file':str(source),'bucket':'mailhub-qualification','prefix':'audit-checkpoints','region':'us-east-1','endpoint':endpoint,'retain_until':(datetime.datetime.now(datetime.timezone.utc)+datetime.timedelta(days=1)).isoformat(),'legal_hold':True,'allow_local_http':True}
            path=root/'preserve.yaml'; path.write_text(json.dumps(config))
            receipt=json.loads(subprocess.check_output([binary,'--config',str(path)],env=env,text=True))
            deletion=subprocess.run(aws+['delete-object','--bucket',receipt['bucket'],'--key',receipt['key'],'--version-id',receipt['version_id']],env=env,capture_output=True,text=True)
            if deletion.returncode==0 or not ('AccessDenied' in deletion.stderr or 'Object is WORM protected' in deletion.stderr): raise RuntimeError('locked object version was not protected: '+deletion.stderr)
            restored=root/'restored.json'
            subprocess.run(aws+['get-object','--bucket',receipt['bucket'],'--key',receipt['key'],'--version-id',receipt['version_id'],str(restored)],env=env,check=True,stdout=subprocess.DEVNULL)
            assert restored.read_bytes()==source.read_bytes()
            print('PASS: immutable upload, verified retention, deletion denied, byte-exact recovery; image='+image)
    finally:
        subprocess.run(['docker','stop',container],check=True,stdout=subprocess.DEVNULL)

if __name__=='__main__': run(str(pathlib.Path(sys.argv[1]).resolve()))
