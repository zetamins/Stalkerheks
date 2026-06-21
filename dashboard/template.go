package dashboard

const dashboardHTML = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>Stalkerhek — Dashboard</title>
<style>
:root{--bg:#0f1119;--sidebar:#151723;--card:#1a1d2e;--border:#252840;--text:#c8cdd8;--muted:#6b7280;--accent:#6366f1;--green:#22c55e;--red:#ef4444;--yellow:#eab308;--blue:#3b82f6}
*{margin:0;padding:0;box-sizing:border-box}
body{font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,sans-serif;background:var(--bg);color:var(--text);display:flex;min-height:100vh}
#sidebar{width:260px;background:var(--sidebar);border-right:1px solid var(--border);display:flex;flex-direction:column;position:fixed;top:0;left:0;bottom:0;z-index:100;transition:transform .25s}
#sidebar .logo{padding:24px 20px;font-size:18px;font-weight:700;letter-spacing:-.3px;border-bottom:1px solid var(--border);display:flex;align-items:center;gap:10px}
#sidebar .logo .dot{width:10px;height:10px;border-radius:50%;background:var(--accent);animation:pulse 2s infinite}
@keyframes pulse{0%,100%{opacity:1}50%{opacity:.4}}
#sidebar nav{flex:1;overflow-y:auto;padding:12px}
#sidebar nav a{display:flex;align-items:center;gap:12px;padding:10px 14px;border-radius:10px;color:var(--muted);text-decoration:none;font-size:14px;transition:all .15s;margin-bottom:2px}
#sidebar nav a:hover,#sidebar nav a.active{background:rgba(99,102,241,.12);color:#fff}
#sidebar nav a svg{width:20px;height:20px;flex-shrink:0}
#main{margin-left:260px;flex:1;padding:24px;min-width:0}
#header{display:flex;align-items:center;justify-content:space-between;margin-bottom:28px;flex-wrap:wrap;gap:12px}
#header h1{font-size:24px;font-weight:700;letter-spacing:-.5px}
.btn{padding:10px 18px;border-radius:10px;border:none;cursor:pointer;font-size:14px;font-weight:600;transition:all .15s;display:inline-flex;align-items:center;gap:8px}
.btn-accent{background:var(--accent);color:#fff}.btn-accent:hover{opacity:.9}
.btn-outline{background:transparent;border:1px solid var(--border);color:var(--text)}.btn-outline:hover{background:var(--card)}
.btn-red{background:var(--red);color:#fff}.btn-red:hover{opacity:.9}
.btn-sm{padding:6px 12px;font-size:12px;border-radius:8px}
.grid{display:grid;grid-template-columns:repeat(auto-fill,minmax(340px,1fr));gap:18px}
.card{background:var(--card);border:1px solid var(--border);border-radius:14px;padding:20px;transition:border-color .15s}
.card:hover{border-color:var(--accent)}
.card .card-header{display:flex;justify-content:space-between;align-items:flex-start;margin-bottom:14px}
.card .card-title{font-size:16px;font-weight:600}
.card .card-subtitle{font-size:13px;color:var(--muted);margin-top:2px}
.status{display:inline-flex;align-items:center;gap:6px;padding:4px 10px;border-radius:20px;font-size:12px;font-weight:600}
.status-running{background:rgba(34,197,94,.12);color:var(--green)}
.status-stopped{background:rgba(107,114,128,.12);color:var(--muted)}
.status-error{background:rgba(239,68,68,.12);color:var(--red)}
.dot-indicator{width:7px;height:7px;border-radius:50%;display:inline-block}
.dot-green{background:var(--green);box-shadow:0 0 6px var(--green)}
.dot-gray{background:var(--muted)}
.port-tag{display:inline-block;padding:2px 8px;border-radius:6px;background:rgba(59,130,246,.12);color:var(--blue);font-size:11px;font-family:monospace;margin:2px 4px 2px 0}
.card-actions{display:flex;gap:8px;margin-top:14px;flex-wrap:wrap}
.modal-overlay{position:fixed;inset:0;background:rgba(0,0,0,.6);z-index:200;display:flex;align-items:center;justify-content:center;padding:20px}
.modal{background:var(--card);border:1px solid var(--border);border-radius:16px;width:100%;max-width:600px;max-height:85vh;overflow-y:auto}
.modal-header{padding:20px 24px;border-bottom:1px solid var(--border);font-size:18px;font-weight:700}
.modal-body{padding:24px}
.modal-footer{padding:16px 24px;border-top:1px solid var(--border);display:flex;justify-content:flex-end;gap:10px}
.form-row{display:grid;grid-template-columns:1fr 1fr;gap:14px}
.form-group{margin-bottom:14px}
.form-group label{display:block;font-size:12px;font-weight:600;color:var(--muted);margin-bottom:5px;text-transform:uppercase;letter-spacing:.5px}
.form-group input{width:100%;padding:9px 12px;border-radius:8px;border:1px solid var(--border);background:var(--bg);color:var(--text);font-size:13px;font-family:inherit}
.form-group input:focus{outline:none;border-color:var(--accent)}
.form-group .hint{font-size:11px;color:var(--muted);margin-top:3px}
.log-viewer{background:var(--bg);border:1px solid var(--border);border-radius:10px;padding:14px;font-family:monospace;font-size:12px;max-height:300px;overflow-y:auto;white-space:pre-wrap;word-break:break-all;color:var(--muted)}
#menu-toggle{display:none;background:none;border:none;color:var(--text);cursor:pointer;padding:8px}
@media(max-width:768px){
  #sidebar{transform:translateX(-100%)}#sidebar.open{transform:translateX(0)}#main{margin-left:0}#menu-toggle{display:block}.grid{grid-template-columns:1fr}.form-row{grid-template-columns:1fr}
}
.toast{position:fixed;bottom:24px;right:24px;z-index:300;padding:12px 20px;border-radius:10px;font-size:14px;font-weight:600;animation:slideUp .3s ease}
.toast-success{background:var(--green);color:#000}
.toast-error{background:var(--red);color:#fff}
@keyframes slideUp{from{transform:translateY(20px);opacity:0}to{transform:translateY(0);opacity:1}}
</style>
</head>
<body>

<button id="menu-toggle" onclick="document.getElementById('sidebar').classList.toggle('open')">
  <svg width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><line x1="3" y1="6" x2="21" y2="6"/><line x1="3" y1="12" x2="21" y2="12"/><line x1="3" y1="18" x2="21" y2="18"/></svg>
</button>

<div id="sidebar">
  <div class="logo"><div class="dot"></div>Stalkerhek</div>
  <nav>
    <a href="#" class="active" onclick="showProfiles()">
      <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><rect x="3" y="3" width="7" height="7" rx="1"/><rect x="14" y="3" width="7" height="7" rx="1"/><rect x="3" y="14" width="7" height="7" rx="1"/><rect x="14" y="14" width="7" height="7" rx="1"/></svg>
      Profiles
    </a>
    <a href="#" onclick="addProfile()">
      <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="12" cy="12" r="10"/><line x1="12" y1="8" x2="12" y2="16"/><line x1="8" y1="12" x2="16" y2="12"/></svg>
      New Profile
    </a>
  </nav>
</div>

<div id="main">
  <div id="header"><h1>Profiles</h1><button class="btn btn-accent" onclick="addProfile()">+ New Profile</button></div>
  <div id="content" class="grid"></div>
</div>

<div id="modal-container"></div>
<div id="toast-container"></div>

<script>
const API='/api/profiles';

async function loadProfiles(){
  const res=await fetch(API);
  const profiles=await res.json();
  const grid=document.getElementById('content');
  if(!profiles||profiles.length===0){
    grid.innerHTML='<div class="card" style="text-align:center;padding:48px"><p style="color:var(--muted);font-size:15px">No profiles yet. Click "+ New Profile" to create one.</p></div>';
    return;
  }
  grid.innerHTML=profiles.map(p=>'<div class="card">'+
    '<div class="card-header"><div><div class="card-title">'+esc(p.name)+'</div><div class="card-subtitle"><span class="port-tag">proxy '+esc(p.services?.proxy_bind||'—')+'</span><span class="port-tag">hls '+esc(p.services?.hls_bind||'—')+'</span></div></div>'+
    '<span class="status status-'+(p.status||'stopped')+'"><span class="dot-indicator '+(p.status==='running'?'dot-green':'dot-gray')+'"></span>'+(p.status||'stopped')+'</span></div>'+
    '<div style="font-size:12px;color:var(--muted)">'+esc(p.portal?.model||'')+' | '+esc(p.portal?.url||'')+'</div>'+
    (p.status==='running'?'<div style="font-size:12px;color:var(--muted);margin-top:4px">PID: '+p.pid+'</div>':'')+
    '<div class="card-actions">'+
    (p.status==='running'
      ?'<button class="btn btn-sm btn-outline" onclick="stopProfile(\''+esc(p.name)+'\')">Stop</button>'
      :'<button class="btn btn-sm btn-accent" onclick="startProfile(\''+esc(p.name)+'\')">Start</button>')+
    '<button class="btn btn-sm btn-outline" onclick="viewLogs(\''+esc(p.name)+'\')">Logs</button>'+
    '<button class="btn btn-sm btn-outline" onclick="editProfile(\''+esc(p.name)+'\')">Edit</button>'+
    '<button class="btn btn-sm btn-red" onclick="deleteProfile(\''+esc(p.name)+'\')">Delete</button>'+
    '</div></div>').join('');
}

function addProfile(){
  showModal('New Profile',formHTML({}),async()=>{
    const cfg=readForm();
    if(!cfg.name||!cfg.portal?.url||!cfg.portal?.mac){toast('Name, URL and MAC are required','error');return;}
    const res=await fetch(API,{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify(cfg)});
    if(res.ok){toast('Profile created');loadProfiles()}else toast('Failed','error');
  });
}

async function editProfile(name){
  const res=await fetch(API);const profiles=await res.json();
  const p=profiles.find(p=>p.name===name);
  if(!p)return;
  showModal('Edit: '+name,formHTML(p),async()=>{
    const cfg=readForm();cfg.name=name;
    const res=await fetch(API+'/'+name,{method:'PUT',headers:{'Content-Type':'application/json'},body:JSON.stringify(cfg)});
    if(res.ok){toast('Profile saved');loadProfiles()}else toast('Failed','error');
  });
}

async function deleteProfile(name){
  if(!confirm('Delete "'+name+'"?'))return;
  const res=await fetch(API+'/'+name,{method:'DELETE'});
  if(res.ok){toast('Deleted');loadProfiles()}else toast('Failed','error');
}

async function startProfile(name){
  const res=await fetch(API+'/start',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({name,binary:''})});
  if(res.ok){toast('Starting...');setTimeout(loadProfiles,2500)}else toast('Failed to start','error');
}

async function stopProfile(name){
  const res=await fetch(API+'/stop',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({name})});
  if(res.ok){toast('Stopped');setTimeout(loadProfiles,1000)}else toast('Failed','error');
}

async function viewLogs(name){
  const res=await fetch(API+'/logs?name='+encodeURIComponent(name));
  const text=await res.text();
  showModal('Logs: '+name,'<div class="log-viewer">'+esc(text||'No logs yet')+'</div>');
}

function formHTML(cfg){
  const p=cfg.portal||{};
  const s=cfg.services||{};
  return'<div class="form-row"><div class="form-group"><label>Profile Name *</label><input id="f-name" value="'+esc(cfg.name||'')+'" placeholder="my-profile"></div><div class="form-group"><label>Model</label><input id="f-model" value="'+esc(p.model||'MAG254')+'" placeholder="MAG254"></div></div>'+
    '<div class="form-row"><div class="form-group"><label>Serial Number *</label><input id="f-sn" value="'+esc(p.serial_number||'')+'" placeholder="0000000000000"></div><div class="form-group"><label>MAC Address *</label><input id="f-mac" value="'+esc(p.mac||'')+'" placeholder="00:00:00:00:00:00"></div></div>'+
    '<div class="form-group"><label>Device ID *</label><input id="f-device-id" value="'+esc(p.device_id||'')+'" placeholder="64-char hex"><div class="hint">device_id2 and signature are auto-generated if left empty</div></div>'+
    '<div class="form-row"><div class="form-group"><label>Device ID2 (auto)</label><input id="f-device-id2" value="'+esc(p.device_id2||'')+'" placeholder="Auto-generated from device_id"></div><div class="form-group"><label>Signature (auto)</label><input id="f-signature" value="'+esc(p.signature||'')+'" placeholder="Auto-generated from device_id"></div></div>'+
    '<div class="form-row"><div class="form-group"><label>Portal URL *</label><input id="f-url" value="'+esc(p.url||'')+'" placeholder="http://host:port/c/"><div class="hint">portal.php auto-appended</div></div><div class="form-group"><label>Timezone</label><input id="f-tz" value="'+esc(p.time_zone||'Europe/Vilnius')+'" placeholder="Europe/Vilnius"></div></div>'+
    '<div class="form-group"><label>Token</label><input id="f-token" value="'+esc(p.token||'')+'" placeholder="Auto-generated if empty"></div>'+
    '<div class="form-row"><div class="form-group"><label>Proxy Bind</label><input id="f-proxy-bind" value="'+esc(s.proxy_bind||'0.0.0.0:8888')+'" placeholder="0.0.0.0:8888"></div><div class="form-group"><label>HLS Bind</label><input id="f-hls-bind" value="'+esc(s.hls_bind||'0.0.0.0:9999')+'" placeholder="0.0.0.0:9999"></div></div>';
}

function readForm(){
  return{
    name:el('f-name'),portal:{model:el('f-model')||'MAG254',serial_number:el('f-sn'),device_id:el('f-device-id'),
    device_id2:el('f-device-id2'),signature:el('f-signature'),mac:el('f-mac'),url:el('f-url'),
    time_zone:el('f-tz')||'Europe/Vilnius',token:el('f-token')},
    services:{proxy_bind:el('f-proxy-bind')||'0.0.0.0:8888',hls_bind:el('f-hls-bind')||'0.0.0.0:9999'}
  };
}
function el(id){return document.getElementById(id)?.value||''}

function showModal(title,body,onSave){
  document.getElementById('modal-container').innerHTML=
    '<div class="modal-overlay" onclick="if(event.target===this)closeModal()"><div class="modal">'+
    '<div class="modal-header">'+title+'</div><div class="modal-body">'+body+'</div>'+
    '<div class="modal-footer"><button class="btn btn-outline" onclick="closeModal()">Cancel</button>'+
    '<button class="btn btn-accent" id="modal-save">Save</button></div></div></div>';
  document.getElementById('modal-save').onclick=async()=>{await onSave();closeModal();};
}
function closeModal(){document.getElementById('modal-container').innerHTML='';}
function toast(msg,type){
  type=type||'success';
  const el=document.createElement('div');el.className='toast toast-'+type;el.textContent=msg;
  document.getElementById('toast-container').appendChild(el);
  setTimeout(()=>el.remove(),2500);
}
function esc(s){return(s||'').replace(/&/g,'&amp;').replace(/</g,'&lt;').replace(/>/g,'&gt;').replace(/"/g,'&quot;')}
loadProfiles();setInterval(loadProfiles,10000);
</script>
</body>
</html>`
