'use strict';
let credential='';
const el=id=>document.getElementById(id);
async function request(path,method='GET',body){
 if(!credential){el('status').textContent='Connect with an API credential first.';return;}
 el('status').textContent='Loading…';
 try{
  const response=await fetch('/api/v1/'+path,{method,credentials:'omit',cache:'no-store',redirect:'error',headers:{Authorization:'Bearer '+credential,'Content-Type':'application/json'},body:body===undefined?undefined:JSON.stringify(body),signal:AbortSignal.timeout(30000)});
  const text=await response.text();let display=text;try{display=JSON.stringify(JSON.parse(text),null,2);}catch{}
  el('result').textContent=display;el('status').textContent=response.ok?'Request completed.':'Request failed (HTTP '+response.status+'). Check your permissions and inputs.';
 }catch{el('status').textContent='Request failed. Check the connection and service status.';}
}
el('login').addEventListener('submit',e=>{e.preventDefault();credential=el('token').value;el('token').value='';request('queue/stats');});
el('logout').addEventListener('click',()=>{credential='';el('token').value='';el('password').value='';el('result').textContent='';el('status').textContent='Disconnected';});
document.querySelectorAll('[data-path]').forEach(b=>b.addEventListener('click',()=>request(b.dataset.path)));
el('account').addEventListener('submit',e=>{e.preventDefault();const method=el('action').value;const username=el('username').value;const body=method==='POST'?{username,email:el('email').value}:{enabled:el('enabled').checked};if(method==='PUT'&&el('email').value)body.email=el('email').value;if(el('password').value)body.password=el('password').value;if(method==='DELETE'&&!window.confirm('Delete identity '+username+'? Mail remains stored.'))return;request('mailboxes'+(method==='POST'?'':'/'+encodeURIComponent(username)),method,method==='DELETE'?undefined:body);el('password').value='';});
