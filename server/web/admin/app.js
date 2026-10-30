/* ATV Loader admin. Plain JS, no build step. Talks to /api/v1/admin/*. */
(() => {
  const $ = (s, el = document) => el.querySelector(s);
  const $$ = (s, el = document) => [...el.querySelectorAll(s)];
  const API = '/api/v1';

  const state = { apps: [], categories: [], cat: null, q: '', showHidden: false, sel: null, devices: [], pending: [] };

  // ---------- helpers
  async function api(path, opts = {}) {
    const r = await fetch(API + path, { headers: { 'Content-Type': 'application/json', ...(opts.headers || {}) }, ...opts });
    if (r.status === 204) return null;
    const text = await r.text();
    let body = null;
    try { body = text ? JSON.parse(text) : null; } catch { body = { error: text }; }
    if (!r.ok) throw new Error((body && body.error) || `HTTP ${r.status}`);
    return body;
  }
  const esc = s => String(s ?? '').replace(/[&<>"']/g, c => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]));
  const size = b => b < 1024 ? `${b} B` : b < 1048576 ? `${(b / 1024).toFixed(0)} KB` : b < 1073741824 ? `${(b / 1048576).toFixed(1)} MB` : `${(b / 1073741824).toFixed(2)} GB`;
  const ago = iso => {
    const d = (Date.now() - new Date(iso)) / 1000;
    if (d < 60) return 'just now';
    if (d < 3600) return `${Math.floor(d / 60)} min ago`;
    if (d < 86400) return `${Math.floor(d / 3600)} h ago`;
    return `${Math.floor(d / 86400)} d ago`;
  };
  let toastTimer;
  function toast(msg, err = false) {
    const t = $('#toast');
    t.textContent = msg; t.className = 'toast' + (err ? ' err' : ''); t.hidden = false;
    clearTimeout(toastTimer); toastTimer = setTimeout(() => t.hidden = true, err ? 6000 : 2500);
  }
  function iconEl(app, cls = 'icon') {
    if (app.iconUrl) return `<img class="${cls}" src="${esc(app.iconUrl)}" alt="">`;
    return `<span class="${cls} ph">${esc((app.name || '?')[0].toUpperCase())}</span>`;
  }

  // ---------- tabs
  function showTab(name) {
    $$('.tabs a').forEach(a => a.classList.toggle('active', a.dataset.tab === name));
    $('#page-apps').hidden = name !== 'apps';
    $('#page-devices').hidden = name !== 'devices';
    $('#page-tokens').hidden = name !== 'tokens';
    if (name === 'devices') { loadDevices(); $('#pair-code').focus(); }
    if (name === 'tokens') loadTokens();
  }
  window.addEventListener('hashchange', route);
  function route() {
    showTab(location.hash === '#devices' ? 'devices' : location.hash === '#tokens' ? 'tokens' : 'apps');
    const m = location.hash.match(/^#app\/(.+)$/);
    if (m && m[1] !== state.sel) select(decodeURIComponent(m[1]));
  }

  // ---------- apps
  async function loadApps() {
    try {
      const r = await api('/admin/apps');
      state.apps = r.apps; state.categories = r.categories;
      renderCats(); renderList();
      $('#cat-options').innerHTML = state.categories.map(c => `<option value="${esc(c)}">`).join('');
      if (state.sel) { const a = state.apps.find(x => x.package === state.sel); a ? renderEditor(a) : closeEditor(); }
    } catch (e) { toast(`Could not load apps: ${e.message}`, true); }
  }

  function renderCats() {
    const counts = {};
    for (const a of state.apps) counts[a.category || ''] = (counts[a.category || ''] || 0) + 1;
    const cats = [...new Set([...state.categories, ...Object.keys(counts).filter(Boolean)])];
    const li = (label, key, n) => `<li data-cat="${esc(key ?? '')}" class="${state.cat === key ? 'active' : ''}"><span>${esc(label)}</span><span class="n">${n}</span></li>`;
    $('#cat-list').innerHTML = li('All apps', null, state.apps.length)
      + cats.map(c => li(c, c, counts[c] || 0)).join('')
      + (counts[''] ? li('Uncategorized', '', counts['']) : '');
    $$('#cat-list li').forEach(el => el.onclick = () => {
      state.cat = el.dataset.cat === '' && el.textContent.startsWith('All') ? null : el.dataset.cat;
      if (el.textContent.startsWith('All apps')) state.cat = null;
      renderCats(); renderList();
    });
  }

  function visible() {
    const q = state.q.toLowerCase();
    return state.apps.filter(a =>
      (state.showHidden || !a.hidden) &&
      (state.cat === null || (a.category || '') === state.cat) &&
      (!q || a.name.toLowerCase().includes(q) || a.package.toLowerCase().includes(q)));
  }

  function renderList() {
    const rows = visible();
    $('#count').textContent = `${rows.length} of ${state.apps.length} apps`;
    $('#empty').hidden = state.apps.length > 0;
    $('#rows').innerHTML = rows.map(a => {
      const l = a.latest;
      const shown = (a.hidden ? '<span class="tag">Hidden</span>' : (a.featured ? '<span class="tag feat">Featured</span>' : '<span class="tag">Listed</span>'))
        + (a.protected ? '<span class="tag self">Loader itself</span>' : '');
      return `<tr data-pkg="${esc(a.package)}" class="${a.hidden ? 'hid' : ''}${state.sel === a.package ? ' sel' : ''}">
        <td>${iconEl(a)}</td>
        <td><div class="name">${esc(a.name)}</div><div class="mono dim">${esc(a.package)}</div></td>
        <td>${esc(a.category) || '<span class="dim">—</span>'}</td>
        <td>${l ? `${esc(l.versionName)} <span class="dim">(${l.versionCode})</span>` : ''}${a.versions.length > 1 ? ` <span class="dim">+${a.versions.length - 1}</span>` : ''}</td>
        <td>${l ? size(l.size) : ''}</td>
        <td>${shown}</td></tr>`;
    }).join('');
    $$('#rows tr').forEach(tr => tr.onclick = () => select(tr.dataset.pkg));
  }

  function select(pkg) {
    state.sel = pkg;
    if (location.hash !== '#app/' + pkg) history.replaceState(null, '', '#app/' + encodeURIComponent(pkg));
    const a = state.apps.find(x => x.package === pkg);
    if (a) renderEditor(a);
    renderList();
  }

  function closeEditor() { state.sel = null; $('#editor').hidden = true; history.replaceState(null, '', '#apps'); renderList(); }

  function renderEditor(a) {
    const ed = $('#editor');
    ed.hidden = false;
    $('#ed-icon').outerHTML = a.iconUrl ? `<img id="ed-icon" class="icon lg" src="${esc(a.iconUrl)}" alt="">` : `<span id="ed-icon" class="icon lg ph">${esc(a.name[0].toUpperCase())}</span>`;
    $('#ed-name').value = a.name;
    $('#ed-pkg').textContent = a.package;
    $('#ed-cat').value = a.category || '';
    $('#ed-desc').value = a.description || '';
    $('#ed-featured').checked = !!a.featured;
    $('#ed-hidden').checked = !!a.hidden;
    $('#ed-hidden').disabled = !!a.protected;
    $('#ed-delete').hidden = !!a.protected;
    $('#ed-self').hidden = !a.protected;
    const latestSigner = a.versions[0]?.signer?.sha256;
    $('#ed-versions').innerHTML = a.versions.map(v => `<li data-code="${v.versionCode}">
      <div class="v"><b>${esc(v.versionName)} <span class="dim">(${v.versionCode})</span></b><span class="meta">${size(v.size)} · min API ${v.minSdk} · added ${ago(v.addedAt)}</span></div>
      ${v.signer ? `<div class="signer${latestSigner && v.signer.sha256 !== latestSigner ? ' warn' : ''}" title="Certificate SHA-256 ${esc(v.signer.sha256)}">Signed by ${esc(v.signer.name || 'an unnamed certificate')} · <code>${esc(v.signer.sha256.slice(0, 16))}…</code>${latestSigner && v.signer.sha256 !== latestSigner ? ' · different key from the latest version' : ''}</div>` : ''}
      <textarea rows="2" placeholder="What changed in this version? Shown on the TV.">${esc(v.changelog)}</textarea>
      <div class="vact"><button class="save-log">Save note</button><a class="btn" href="${esc(v.downloadUrl)}">Download</a>${a.protected && a.versions.length === 1 ? '' : '<button class="ghost red del-ver">Delete this version</button>'}</div>
    </li>`).join('');
    $$('#ed-versions li').forEach(li => {
      const code = li.dataset.code;
      $('.save-log', li).onclick = async () => {
        try { await api(`/admin/apps/${a.package}/versions/${code}`, { method: 'PATCH', body: JSON.stringify({ changelog: $('textarea', li).value }) }); toast('Saved'); loadApps(); }
        catch (e) { toast(e.message, true); }
      };
      const del = $('.del-ver', li); if (del) del.onclick = async () => {
        if (!confirm(`Delete version ${code} of ${a.name}?${a.versions.length === 1 ? ' This is the only version, so the app will be removed too.' : ''}`)) return;
        try { await api(`/admin/apps/${a.package}/versions/${code}`, { method: 'DELETE' }); toast('Version deleted'); loadApps(); }
        catch (e) { toast(e.message, true); }
      };
    });
  }

  $('#ed-close').onclick = closeEditor;
  $('#ed-save').onclick = async () => {
    const pkg = state.sel; if (!pkg) return;
    try {
      await api(`/admin/apps/${pkg}`, { method: 'PATCH', body: JSON.stringify({
        name: $('#ed-name').value, category: $('#ed-cat').value, description: $('#ed-desc').value,
        featured: $('#ed-featured').checked, hidden: $('#ed-hidden').checked }) });
      toast('Saved'); loadApps();
    } catch (e) { toast(e.message, true); }
  };
  $('#ed-delete').onclick = async () => {
    const a = state.apps.find(x => x.package === state.sel); if (!a) return;
    if (!confirm(`Remove ${a.name} and all ${a.versions.length} APK file(s)? TVs that already installed it keep it.`)) return;
    try { await api(`/admin/apps/${a.package}`, { method: 'DELETE' }); toast('App removed'); closeEditor(); loadApps(); }
    catch (e) { toast(e.message, true); }
  };
  $('#ed-iconfile').onchange = async e => {
    const f = e.target.files[0]; if (!f || !state.sel) return;
    const fd = new FormData(); fd.append('icon', f);
    try {
      const r = await fetch(`${API}/admin/apps/${state.sel}/icon`, { method: 'POST', body: fd });
      if (!r.ok) throw new Error((await r.json()).error);
      toast('Icon replaced'); loadApps();
    } catch (err) { toast(err.message, true); }
    e.target.value = '';
  };

  // search/filter
  $('#search').oninput = e => { state.q = e.target.value; renderList(); };
  $('#show-hidden').onchange = e => { state.showHidden = e.target.checked; renderList(); };

  // categories dialog
  $('#edit-cats').onclick = () => {
    $('#cats-text').value = state.categories.join('\n');
    $('#cats-dialog').showModal();
  };
  $('#cats-dialog').onclose = async () => {
    if ($('#cats-dialog').returnValue !== 'save') return;
    const cats = $('#cats-text').value.split('\n').map(s => s.trim()).filter(Boolean);
    try { await api('/admin/categories', { method: 'PUT', body: JSON.stringify({ categories: cats }) }); toast('Categories saved'); loadApps(); }
    catch (e) { toast(e.message, true); }
  };

  // ---------- upload
  const drop = $('#drop');
  drop.onclick = e => { if (e.target.tagName !== 'LABEL') $('#file').click(); };
  drop.onkeydown = e => { if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); $('#file').click(); } };
  ['dragenter', 'dragover'].forEach(ev => drop.addEventListener(ev, e => { e.preventDefault(); drop.classList.add('over'); }));
  ['dragleave', 'drop'].forEach(ev => drop.addEventListener(ev, e => { e.preventDefault(); drop.classList.remove('over'); }));
  drop.addEventListener('drop', e => uploadAll([...e.dataTransfer.files]));
  $('#file').onchange = e => { uploadAll([...e.target.files]); e.target.value = ''; };
  document.addEventListener('dragover', e => e.preventDefault());
  document.addEventListener('drop', e => e.preventDefault());

  function uploadAll(files) {
    files.filter(f => /\.apk$/i.test(f.name) || f.type === 'application/vnd.android.package-archive' || !f.type).forEach(upload);
  }

  function upload(file, allowSignerChange = false) {
    const li = document.createElement('li');
    li.innerHTML = `<span>${esc(file.name)} <span class="dim">${size(file.size)}</span></span><div class="bar"><i></i></div><span class="st dim">Uploading…</span>`;
    $('#uploads').prepend(li);
    const bar = $('.bar i', li), st = $('.st', li);
    const fd = new FormData();
    if (allowSignerChange) fd.append('allowSignerChange', '1'); // must precede the file: the server streams the form
    fd.append('apk', file, file.name);
    const xhr = new XMLHttpRequest();
    xhr.open('POST', `${API}/admin/apps`);
    xhr.upload.onprogress = e => { if (e.lengthComputable) bar.style.width = `${(e.loaded / e.total) * 100}%`; };
    xhr.upload.onload = () => { st.textContent = 'Reading the APK…'; };
    xhr.onload = () => {
      bar.style.width = '100%';
      let body = {}; try { body = JSON.parse(xhr.responseText); } catch {}
      if (xhr.status >= 200 && xhr.status < 300) {
        const r = body.imported[0];
        st.className = 'st ok';
        st.textContent = `${r.created ? 'Added' : r.replaced ? 'Replaced version' : 'Added version'} ${r.app.name} ${r.app.versions.find(v => v.versionCode === r.versionCode)?.versionName || ''}`;
        loadApps().then(() => select(r.app.package));
        setTimeout(() => li.remove(), 8000);
      } else if (xhr.status === 409 && body.code === 'signer_mismatch') {
        li.remove();
        if (confirm(`${body.error}\n\nTVs that have the current version installed cannot update to this one; they would have to uninstall it first. Add it anyway?`)) upload(file, true);
      } else {
        st.className = 'st err'; st.textContent = body.error || `Upload failed (HTTP ${xhr.status})`;
      }
    };
    xhr.onerror = () => { st.className = 'st err'; st.textContent = 'Upload failed: network error'; };
    xhr.send(fd);
  }

  // ---------- devices
  let devTimer;
  async function loadDevices() {
    try {
      const r = await api('/admin/devices');
      state.devices = r.devices; state.pending = r.pending;
      renderDevices();
    } catch (e) { toast(`Could not load devices: ${e.message}`, true); }
    clearTimeout(devTimer);
    if (!$('#page-devices').hidden) devTimer = setTimeout(loadDevices, 4000);
  }
  // Keep the badge fresh even on the Apps tab.
  setInterval(async () => { if ($('#page-devices').hidden) { try { const r = await api('/admin/devices'); state.pending = r.pending; badge(); } catch {} } }, 15000);

  function badge() {
    const b = $('#pending-badge'); b.hidden = state.pending.length === 0; b.textContent = state.pending.length;
  }

  function renderDevices() {
    badge();
    $('#pending').innerHTML = state.pending.length ? state.pending.map(p => `<li data-code="${esc(p.userCode)}">
      <span class="pc">${esc(p.userCode)}</span>
      <span>${esc(p.model || 'Unknown device')}<small>Android API ${p.sdk} · asked ${ago(p.createdAt)}</small></span>
      <span class="pa"><button class="primary ok">Approve</button><button class="ghost no">Deny</button></span></li>`).join('')
      : '<li class="dim" style="display:block;border:0;padding:4px 0">No TV is asking right now.</li>';
    $$('#pending li[data-code]').forEach(li => {
      $('.ok', li).onclick = () => approve(li.dataset.code, '');
      $('.no', li).onclick = async () => { try { await api('/admin/devices/deny', { method: 'POST', body: JSON.stringify({ userCode: li.dataset.code }) }); loadDevices(); } catch (e) { toast(e.message, true); } };
    });
    $('#devices-empty').hidden = state.devices.length > 0;
    $('#devices').innerHTML = state.devices.map(d => `<tr data-id="${esc(d.id)}">
      <td><input class="rename" value="${esc(d.name)}"></td>
      <td>${esc(d.model) || '<span class="dim">—</span>'}</td>
      <td>${ago(d.lastSeen)}</td>
      <td>${ago(d.createdAt)}${d.approvedBy && d.approvedBy !== 'anonymous' ? ` <span class="dim">by ${esc(d.approvedBy)}</span>` : ''}</td>
      <td style="text-align:right"><button class="ghost red revoke">Unlink</button></td></tr>`).join('');
    $$('#devices tr').forEach(tr => {
      const id = tr.dataset.id;
      const inp = $('.rename', tr);
      inp.onchange = async () => { try { await api(`/admin/devices/${id}`, { method: 'PATCH', body: JSON.stringify({ name: inp.value }) }); toast('Renamed'); } catch (e) { toast(e.message, true); } };
      $('.revoke', tr).onclick = async () => {
        if (!confirm(`Unlink "${inp.value}"? It will have to pair again before it can browse the store.`)) return;
        try { await api(`/admin/devices/${id}`, { method: 'DELETE' }); toast('Unlinked'); loadDevices(); } catch (e) { toast(e.message, true); }
      };
    });
  }

  async function approve(code, name) {
    const msg = $('#pair-msg');
    try {
      const d = await api('/admin/devices/approve', { method: 'POST', body: JSON.stringify({ userCode: code, name }) });
      msg.className = 'msg ok'; msg.textContent = `Linked "${d.name}". The TV will load the store in a few seconds.`;
      $('#pair-code').value = ''; $('#pair-name').value = '';
      loadDevices();
    } catch (e) { msg.className = 'msg err'; msg.textContent = e.message; }
  }
  $('#pair-form').onsubmit = e => { e.preventDefault(); approve($('#pair-code').value, $('#pair-name').value); };
  $('#pair-code').oninput = e => {
    let v = e.target.value.toUpperCase().replace(/[^A-Z0-9]/g, '').slice(0, 6);
    e.target.value = v.length > 3 ? `${v.slice(0, 3)}-${v.slice(3)}` : v;
  };

  // ---------- API tokens
  const scopeLabel = { read: 'Read', publish: 'Publish', manage: 'Manage', devices: 'Devices' };
  async function loadTokens() {
    try {
      const r = await api('/admin/tokens');
      state.tokens = r.tokens;
      renderTokens();
    } catch (e) { toast(`Could not load tokens: ${e.message}`, true); }
  }
  function renderTokens() {
    const toks = state.tokens || [];
    $('#tokens-empty').hidden = toks.length > 0;
    $('#tokens').innerHTML = toks.map(t => `<tr data-id="${esc(t.id)}" class="${t.expired ? 'hid' : ''}">
      <td><div class="name">${esc(t.name)}</div><div class="dim">${ago(t.createdAt)}${t.createdBy && t.createdBy !== 'anonymous' ? ` by ${esc(t.createdBy)}` : ''}</div></td>
      <td class="mono">${esc(t.prefix)}…</td>
      <td>${t.scopes.map(sc => `<span class="tag">${esc(scopeLabel[sc] || sc)}</span>`).join('')}</td>
      <td class="mono">${t.packages.length ? t.packages.map(esc).join('<br>') : '<span class="dim">any</span>'}</td>
      <td>${t.lastUsed ? ago(t.lastUsed) : '<span class="dim">never</span>'}</td>
      <td>${t.expiresAt ? (t.expired ? '<span class="tag red">Expired</span>' : new Date(t.expiresAt).toLocaleDateString()) : '<span class="dim">never</span>'}</td>
      <td style="text-align:right"><button class="ghost red revoke">Revoke</button></td></tr>`).join('');
    $$('#tokens tr').forEach(tr => {
      $('.revoke', tr).onclick = async () => {
        const t = toks.find(x => x.id === tr.dataset.id);
        if (!confirm(`Revoke "${t.name}"? Anything using it stops working immediately.`)) return;
        try { await api(`/admin/tokens/${t.id}`, { method: 'DELETE' }); toast('Token revoked'); loadTokens(); } catch (e) { toast(e.message, true); }
      };
    });
    $('#tok-usage').textContent = `curl -H "Authorization: Bearer $ATVLOADER_TOKEN" \\\n  -F apk=@app-release.apk ${location.origin}${API}/admin/apps`;
  }
  $('#tok-form').onsubmit = async e => {
    e.preventDefault();
    const scopes = $$('#tok-form input[name=scope]:checked').map(i => i.value);
    if (!scopes.length) { toast('Pick at least one scope', true); return; }
    const body = {
      name: $('#tok-name').value, scopes,
      packages: $('#tok-pkgs').value.split(/[\n,; ]+/).map(x => x.trim()).filter(Boolean),
      expiresDays: Number($('#tok-exp').value),
    };
    try {
      const r = await api('/admin/tokens', { method: 'POST', body: JSON.stringify(body) });
      $('#tok-secret').value = r.secret;
      $('#tok-dialog').showModal();
      $('#tok-secret').select();
      $('#tok-name').value = ''; $('#tok-pkgs').value = '';
      loadTokens();
    } catch (err) { toast(err.message, true); }
  };
  $('#tok-copy').onclick = async () => {
    const v = $('#tok-secret').value;
    try { await navigator.clipboard.writeText(v); $('#tok-copy').textContent = 'Copied'; }
    catch { $('#tok-secret').select(); document.execCommand('copy'); $('#tok-copy').textContent = 'Copied'; }
    setTimeout(() => $('#tok-copy').textContent = 'Copy', 1500);
  };
  $('#tok-dialog').onclose = () => { $('#tok-secret').value = ''; };

  // ---------- boot
  api('/admin/me').then(me => {
    $('#who').textContent = me.user && me.user !== 'anonymous' ? me.user : '';
    if (me.store) { $('#store-name').textContent = me.store; document.title = `${me.store} · ATV Loader admin`; }
  }).catch(() => {});
  loadApps().then(route);
  loadDevices();
})();
