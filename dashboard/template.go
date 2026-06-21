package dashboard

const dashboardHTML = `<!doctype html>
<html>
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<link rel="stylesheet" href="https://cdnjs.cloudflare.com/ajax/libs/font-awesome/6.5.1/css/all.min.css" referrerpolicy="no-referrer">
<title>Stalkerhek Dashboard</title>
<style>
:root{--bg:#0a0f0a;--panel:#0d1410;--panel2:#111815;--border:#1f2e23;--text:#e0e6e0;--muted:#9aaa9a;--brand:#2d7a4e;--brand-hover:#3a8f5e;--ok:#3fb970;--warn:#d4a94a;--bad:#e85d4d}
*{box-sizing:border-box}
body{margin:0;font-family:system-ui,-apple-system,Segoe UI,Roboto,Ubuntu,Helvetica,Arial,sans-serif;background:linear-gradient(180deg,#0d1410 0%,#0a0f0a 100%);color:var(--text);min-height:100dvh}
a{color:var(--brand);text-decoration:none}a:hover{color:var(--brand-hover);text-decoration:underline}
.wrap{max-width:1200px;margin:0 auto;padding-top:calc(clamp(22px,4vw,32px) + env(safe-area-inset-top));padding-left:calc(clamp(16px,3vw,28px) + env(safe-area-inset-left));padding-right:calc(clamp(16px,3vw,28px) + env(safe-area-inset-right));padding-bottom:calc(100px + env(safe-area-inset-bottom));min-height:100dvh;display:flex;flex-direction:column;gap:16px}
.topbar{display:flex;align-items:center;justify-content:space-between;gap:12px;flex-wrap:wrap;padding:14px 20px;background:var(--panel);border:1px solid var(--border);border-radius:14px}
.topbar-left{display:flex;align-items:center;gap:14px}
.topbar .logo{font-size:20px;font-weight:700;letter-spacing:-.3px;display:flex;align-items:center;gap:10px}
.topbar .logo .dot{width:9px;height:9px;border-radius:50%;background:var(--ok);animation:pulse 2s infinite;box-shadow:0 0 8px var(--ok)}
@keyframes pulse{0%,100%{opacity:1}50%{opacity:.35}}
.tabs{display:flex;gap:4px;flex-wrap:wrap}
.tab{padding:9px 18px;border-radius:8px;border:none;cursor:pointer;font-size:13px;font-weight:600;background:transparent;color:var(--muted);transition:all .15s;display:flex;align-items:center;gap:7px}
.tab:hover{background:var(--panel2);color:var(--text)}
.tab.active{background:var(--brand);color:#fff}.tab.active:hover{background:var(--brand-hover)}
.btn{padding:10px 18px;border-radius:9px;border:none;cursor:pointer;font-size:13px;font-weight:600;transition:all .15s;display:inline-flex;align-items:center;gap:7px}
.btn-brand{background:var(--brand);color:#fff}.btn-brand:hover{opacity:.9}
.btn-outline{background:transparent;border:1px solid var(--border);color:var(--text)}.btn-outline:hover{background:var(--panel2)}
.btn-red{background:var(--bad);color:#fff}.btn-red:hover{opacity:.9}
.btn-sm{padding:6px 12px;font-size:12px;border-radius:7px}
.grid{display:grid;grid-template-columns:repeat(auto-fill,minmax(340px,1fr));gap:16px}
.card{background:var(--panel);border:1px solid var(--border);border-radius:14px;padding:20px;transition:border-color .15s}
.card:hover{border-color:var(--brand)}
.card-header{display:flex;justify-content:space-between;align-items:flex-start;margin-bottom:14px}
.card-title{font-size:16px;font-weight:600;display:flex;align-items:center;gap:8px}
.card-subtitle{font-size:13px;color:var(--muted);margin-top:3px}
.status{display:inline-flex;align-items:center;gap:6px;padding:4px 12px;border-radius:20px;font-size:12px;font-weight:600}
.status-running{background:rgba(63,185,112,.12);color:var(--ok)}
.status-stopped{background:rgba(154,170,154,.12);color:var(--muted)}
.dot-indicator{width:7px;height:7px;border-radius:50%;display:inline-block}
.dot-green{background:var(--ok);box-shadow:0 0 6px var(--ok)}
.dot-gray{background:var(--muted)}
.port-tag{display:inline-block;padding:2px 8px;border-radius:5px;background:rgba(45,122,78,.15);color:var(--brand);font-size:11px;font-family:monospace;margin:2px 4px 2px 0}
.card-actions{display:flex;gap:7px;margin-top:14px;flex-wrap:wrap}
.card-info{font-size:12px;color:var(--muted);margin-top:4px;display:flex;align-items:center;gap:6px}
.modal-overlay{position:fixed;inset:0;background:rgba(0,0,0,.65);z-index:200;display:flex;align-items:center;justify-content:center;padding:20px}
.modal{background:var(--panel);border:1px solid var(--border);border-radius:16px;width:100%;max-width:600px;max-height:85vh;overflow-y:auto}
.modal-header{padding:20px 24px;border-bottom:1px solid var(--border);font-size:18px;font-weight:700}
.modal-body{padding:24px}
.modal-footer{padding:16px 24px;border-top:1px solid var(--border);display:flex;justify-content:flex-end;gap:10px}
.form-row{display:grid;grid-template-columns:1fr 1fr;gap:14px}
.form-group{margin-bottom:14px}
.form-group label{display:block;font-size:12px;font-weight:600;color:var(--muted);margin-bottom:5px;text-transform:uppercase;letter-spacing:.4px}
.form-group input{width:100%;padding:9px 12px;border-radius:8px;border:1px solid var(--border);background:var(--bg);color:var(--text);font-size:13px;font-family:inherit}
.form-group input:focus{outline:none;border-color:var(--brand)}
.form-group .hint{font-size:11px;color:var(--muted);margin-top:3px}
.log-viewer{background:var(--bg);border:1px solid var(--border);border-radius:10px;padding:14px;font-family:monospace;font-size:12px;max-height:350px;overflow-y:auto;white-space:pre-wrap;word-break:break-all;color:var(--muted)}
.toast{position:fixed;bottom:24px;right:24px;z-index:300;padding:12px 20px;border-radius:10px;font-size:14px;font-weight:600;animation:slideUp .3s ease}
.toast-success{background:var(--ok);color:#000}
.toast-error{background:var(--bad);color:#fff}
.connect-grid{display:flex;flex-direction:column;align-items:center;gap:24px}
.connect-card{padding:32px 20px}
@keyframes slideUp{from{transform:translateY(20px);opacity:0}to{transform:translateY(0);opacity:1}}
@media(max-width:768px){.grid{grid-template-columns:1fr}.form-row{grid-template-columns:1fr}.topbar{flex-direction:column;align-items:flex-start}}
@media(min-width:1920px){.wrap{max-width:1600px;gap:28px}.topbar{padding:22px 32px;border-radius:18px}.topbar .logo{font-size:28px}.tab{padding:14px 26px;font-size:17px;border-radius:12px}.btn{padding:14px 26px;font-size:17px;border-radius:12px}.card{padding:28px;border-radius:18px}.card-title{font-size:22px}.card-subtitle{font-size:17px}.card-info{font-size:16px}.status{padding:6px 16px;font-size:16px}.port-tag{font-size:14px}.btn-sm{padding:10px 18px;font-size:16px}.form-group label{font-size:16px}.form-group input{padding:14px 18px;font-size:17px;border-radius:12px}.log-viewer{font-size:15px;max-height:500px}.connect-grid{flex-direction:row!important;justify-content:center!important;gap:60px!important}.connect-card{padding:40px!important}.connect-qr{width:320px!important;height:320px!important}.connect-url{padding:16px!important;font-size:18px!important}.connect-url code{font-size:20px!important}}
</style>
</head>
<body>
<div class="wrap">
  <div class="topbar">
    <div class="topbar-left">
      <div class="logo"><div class="dot"></div>Stalkerhek</div>
    </div>
    <div class="tabs">
      <button class="tab active" onclick="showTab('connect')"><i class="fa-solid fa-qrcode"></i>Connect</button>
      <button class="tab" onclick="showTab('profiles')"><i class="fa-solid fa-server"></i>Profiles</button>
      <button class="tab" onclick="showTab('logs')"><i class="fa-solid fa-file-lines"></i>Logs</button>
      <button class="tab" onclick="addProfile()"><i class="fa-solid fa-plus"></i>New</button>
    </div>
  </div>
  <div id="content" class="grid"></div>
</div>
<div id="modal-container"></div>
<div id="toast-container"></div>
<script>
const API='/api/profiles';let currentTab='connect';
function showTab(tab){currentTab=tab;document.querySelectorAll('.tab').forEach(t=>t.classList.remove('active'));event.target.classList.add('active');if(tab==='connect')showConnect();else if(tab==='profiles')loadProfiles();else showLogSelector()}
async function showConnect(){
  const res=await fetch(API);const profiles=await res.json();
  const active=profiles.find(p=>p.status==='running')||profiles[0];
  const host=location.hostname;
  const proxyPort=active?.services?.proxy_bind?.split(':')[1]||'8888';
  const hlsPort=active?.services?.hls_bind?.split(':')[1]||'9999';
  const dashPort=location.port||'8080';
  const proxyURL='http://'+host+':'+proxyPort+'/c/';
  const hlsURL='http://'+host+':'+hlsPort+'/iptv/';
  const dashURL='http://'+host+':'+dashPort+'/';
  const qrURL='https://api.qrserver.com/v1/create-qr-code/?size=200x200&data='+encodeURIComponent(dashURL);
  document.getElementById('content').className='';
  document.getElementById('content').innerHTML=
    '<div class="card connect-card" style="text-align:center;grid-column:1/-1;padding:32px 20px">'+
    '<div style="display:flex;align-items:center;justify-content:center;gap:10px;margin-bottom:20px"><div class="dot-indicator dot-green" style="width:10px;height:10px"></div><span style="font-size:16px;font-weight:700">'+(active?'Active: '+esc(active.name):'No profiles')+'</span></div>'+
    '<div class="connect-grid">'+
    '<div style="text-align:center"><p style="color:var(--muted);font-size:13px;margin-bottom:10px">Scan with phone</p>'+
    '<img src="'+qrURL+'" class="connect-qr" alt="QR Code" style="border-radius:12px;background:#fff;padding:8px;width:200px;height:200px" onerror="this.src=\'data:image/svg+xml,<svg xmlns=%22http://www.w3.org/2000/svg%22 width=%22200%22 height=%22200%22><rect width=%22200%22 height=%22200%22 fill=%22%23fff%22/><text x=%2250%25%22 y=%2250%25%22 text-anchor=%22middle%22 dy=%22.3em%22 font-size=%2214%22 fill=%22%23000%22>QR unavailable</text></svg>\'"></div>'+
    '<div style="display:grid;gap:8px">'+
    '<div class="card-info connect-url" style="justify-content:flex-start;padding:10px;background:var(--panel2);border-radius:8px"><i class="fa-solid fa-tv"></i><span>STB Portal: <code style="color:var(--brand)">'+esc(proxyURL)+'</code></span></div>'+
    '<div class="card-info connect-url" style="justify-content:flex-start;padding:10px;background:var(--panel2);border-radius:8px"><i class="fa-solid fa-play"></i><span>HLS Streams: <code style="color:var(--brand)">'+esc(hlsURL)+'&lt;channel&gt;</code></span></div>'+
    '<div class="card-info connect-url" style="justify-content:flex-start;padding:10px;background:var(--panel2);border-radius:8px"><i class="fa-solid fa-gauge"></i><span>Dashboard: <code style="color:var(--brand)">'+esc(dashURL)+'</code></span></div>'+
    '</div></div>'+
    (active?'<div style="margin-top:16px;font-size:12px;color:var(--muted)">Model: '+esc(active.portal?.model||'—')+' | MAC: '+esc(active.portal?.mac||'—')+'</div>':'')+
    '</div>';
}
showConnect();
async function loadProfiles(){
  const res=await fetch(API);const profiles=await res.json();
  const grid=document.getElementById('content');
  if(!profiles||profiles.length===0){grid.innerHTML='<div class="card" style="text-align:center;padding:48px"><p style="color:var(--muted);font-size:15px">No profiles yet. Click "New Profile" to create one.</p></div>';return}
  grid.innerHTML=profiles.map(p=>'<div class="card">'+
    '<div class="card-header"><div><div class="card-title">'+esc(p.name)+' <span class="status status-'+(p.status||'stopped')+'"><span class="dot-indicator '+(p.status==='running'?'dot-green':'dot-gray')+'"></span>'+(p.status||'stopped')+'</span></div><div class="card-subtitle"><span class="port-tag">proxy '+esc(p.services?.proxy_bind||'—')+'</span><span class="port-tag">hls '+esc(p.services?.hls_bind||'—')+'</span></div></div></div>'+
    '<div class="card-info"><i class="fa-solid fa-tv"></i>'+esc(p.portal?.model||'')+' <span style="margin-left:8px"><i class="fa-solid fa-link"></i>'+esc(p.portal?.url||'')+'</span></div>'+
    (p.status==='running'?'<div class="card-info" style="margin-top:2px">PID: '+p.pid+'</div>':'')+
    '<div class="card-actions">'+
    (p.status==='running'
      ?'<button class="btn btn-sm btn-outline" onclick="stopProfile(\''+esc(p.name)+'\')"><i class="fa-solid fa-stop"></i>Stop</button>'
      :'<button class="btn btn-sm btn-brand" onclick="startProfile(\''+esc(p.name)+'\')"><i class="fa-solid fa-play"></i>Start</button>')+
    '<button class="btn btn-sm btn-outline" onclick="viewLogs(\''+esc(p.name)+'\')"><i class="fa-solid fa-file-lines"></i>Logs</button>'+
    '<button class="btn btn-sm btn-outline" onclick="editProfile(\''+esc(p.name)+'\')"><i class="fa-solid fa-pen-to-square"></i>Edit</button>'+
    '<button class="btn btn-sm btn-red" onclick="deleteProfile(\''+esc(p.name)+'\')"><i class="fa-solid fa-trash"></i>Delete</button>'+
    '</div></div>').join('')}
function addProfile(){
  showModal('New Profile',formHTML({}),async()=>{
    const cfg=readForm();if(!cfg.name||!cfg.portal?.url||!cfg.portal?.mac){toast('Name, URL and MAC are required','error');return}
    const res=await fetch(API,{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify(cfg)});
    if(res.ok){toast('Profile created');loadProfiles()}else toast('Failed','error')})}
async function editProfile(name){
  const res=await fetch(API);const profiles=await res.json();const p=profiles.find(p=>p.name===name);if(!p)return
  showModal('Edit: '+name,formHTML(p),async()=>{
    const cfg=readForm();cfg.name=name;
    const res=await fetch(API+'/'+name,{method:'PUT',headers:{'Content-Type':'application/json'},body:JSON.stringify(cfg)});
    if(res.ok){toast('Saved');loadProfiles()}else toast('Failed','error')})}
async function deleteProfile(name){if(!confirm('Delete "'+name+'"?'))return;const res=await fetch(API+'/'+name,{method:'DELETE'});if(res.ok){toast('Deleted');loadProfiles()}else toast('Failed','error')}
async function startProfile(name){const res=await fetch(API+'/start',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({name,binary:''})});if(res.ok){toast('Starting...');setTimeout(loadProfiles,2500)}else toast('Failed','error')}
async function stopProfile(name){const res=await fetch(API+'/stop',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({name})});if(res.ok){toast('Stopped');setTimeout(loadProfiles,1000)}else toast('Failed','error')}
async function viewLogs(name){const res=await fetch(API+'/logs?name='+encodeURIComponent(name));const text=await res.text();showModal('Logs: '+name,'<div class="log-viewer">'+esc(text||'No logs yet')+'</div>')}
function showLogSelector(){
  fetch(API).then(r=>r.json()).then(profiles=>{document.getElementById('content').innerHTML=profiles.map(p=>
    '<div class="card"><div class="card-title">'+esc(p.name)+'</div><button class="btn btn-sm btn-outline" style="margin-top:8px" onclick="viewLogs(\''+esc(p.name)+'\')"><i class="fa-solid fa-file-lines"></i> View Logs</button></div>').join('')})}
function formHTML(cfg){const p=cfg.portal||{};const s=cfg.services||{}
  return'<div class="form-row"><div class="form-group"><label>Profile Name *</label><input id="f-name" value="'+esc(cfg.name||'')+'" placeholder="my-profile"></div><div class="form-group"><label>Model</label><input id="f-model" value="'+esc(p.model||'MAG254')+'" placeholder="MAG254"></div></div>'+
    '<div class="form-row"><div class="form-group"><label>Serial Number *</label><input id="f-sn" value="'+esc(p.serial_number||'')+'" placeholder="0000000000000"></div><div class="form-group"><label>MAC Address *</label><input id="f-mac" value="'+esc(p.mac||'')+'" placeholder="00:00:00:00:00:00"></div></div>'+
    '<div class="form-group"><label>Device ID *</label><input id="f-device-id" value="'+esc(p.device_id||'')+'" placeholder="64-char hex"><div class="hint">device_id2 and signature auto-generated if empty</div></div>'+
    '<div class="form-row"><div class="form-group"><label>Device ID2 (auto)</label><input id="f-device-id2" value="'+esc(p.device_id2||'')+'" placeholder="Auto-generated"></div><div class="form-group"><label>Signature (auto)</label><input id="f-signature" value="'+esc(p.signature||'')+'" placeholder="Auto-generated"></div></div>'+
    '<div class="form-row"><div class="form-group"><label>Portal URL *</label><input id="f-url" value="'+esc(p.url||'')+'" placeholder="http://host:port/c/"><div class="hint">portal.php auto-appended</div></div><div class="form-group"><label>Timezone</label><input id="f-tz" value="'+esc(p.time_zone||'Europe/Vilnius')+'" placeholder="Europe/Vilnius"></div></div>'+
    '<div class="form-group"><label>Token</label><input id="f-token" value="'+esc(p.token||'')+'" placeholder="Auto-generated if empty"></div>'+
    '<div class="form-row"><div class="form-group"><label>Proxy Bind</label><input id="f-proxy-bind" value="'+esc(s.proxy_bind||'0.0.0.0:8888')+'" placeholder="0.0.0.0:8888"></div><div class="form-group"><label>HLS Bind</label><input id="f-hls-bind" value="'+esc(s.hls_bind||'0.0.0.0:9999')+'" placeholder="0.0.0.0:9999"></div></div>'}
function readForm(){return{name:el('f-name'),portal:{model:el('f-model')||'MAG254',serial_number:el('f-sn'),device_id:el('f-device-id'),device_id2:el('f-device-id2'),signature:el('f-signature'),mac:el('f-mac'),url:el('f-url'),time_zone:el('f-tz')||'Europe/Vilnius',token:el('f-token')},services:{proxy_bind:el('f-proxy-bind')||'0.0.0.0:8888',hls_bind:el('f-hls-bind')||'0.0.0.0:9999'}}}
function el(id){return document.getElementById(id)?.value||''}
function showModal(title,body,onSave){document.getElementById('modal-container').innerHTML='<div class="modal-overlay" onclick="if(event.target===this)closeModal()"><div class="modal"><div class="modal-header">'+title+'</div><div class="modal-body">'+body+'</div><div class="modal-footer"><button class="btn btn-outline" onclick="closeModal()">Cancel</button><button class="btn btn-brand" id="modal-save">Save</button></div></div></div>';document.getElementById('modal-save').onclick=async()=>{await onSave();closeModal()}}
function closeModal(){document.getElementById('modal-container').innerHTML=''}
function toast(msg,type){type=type||'success';const el=document.createElement('div');el.className='toast toast-'+type;el.textContent=msg;document.getElementById('toast-container').appendChild(el);setTimeout(()=>el.remove(),2500)}
function esc(s){return(s||'').replace(/&/g,'&amp;').replace(/</g,'&lt;').replace(/>/g,'&gt;').replace(/"/g,'&quot;')}
loadProfiles();setInterval(()=>{if(currentTab==='profiles')loadProfiles()},10000)
</script>
</body>
</html>`
