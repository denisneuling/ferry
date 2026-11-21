/* ferry mockups: the shared controller. A page supplies the markup (render
   functions); this file keeps the state and wires clicks, keys, drag and drop,
   uploads, the context menu, dialogs and toasts. Elements act through
   data-act="…" attributes. */
(function () {
  'use strict';
  const F = window.FERRY;
  const t = (k, p) => I18N.t(k, p);
  const esc = s => String(s).replace(/[&<>"']/g, c => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]));

  window.FerryApp = function (o) {
    const prefersDark = window.matchMedia && matchMedia('(prefers-color-scheme: dark)').matches;
    const S = {
      logged: false, error: '', user: F.user, server: F.server,
      share: null, path: [], view: o.view || 'grid', sort: 'name', desc: false,
      sel: new Set(), anchor: null, filter: '', preview: null, info: false,
      transfers: [], transfersOpen: true, drag: false, drawer: false,
      theme: o.theme || (prefersDark ? 'dark' : 'light'),
    };
    if (/[?&]in\b/.test(location.search)) { S.logged = true; }
    const m = location.search.match(/[?&]share=([^&]+)/); if (m) { S.logged = true; S.share = decodeURIComponent(m[1]); }
    const p = location.search.match(/[?&]path=([^&]+)/); if (p) S.path = decodeURIComponent(p[1]).split('/').filter(Boolean);
    const th = location.search.match(/[?&]theme=(light|dark)/); if (th) S.theme = th[1];
    const v = location.search.match(/[?&]view=(grid|list)/); if (v) S.view = v[1];

    const root = document.getElementById('app');
    const layer = name => { let el = document.getElementById('layer-' + name); if (!el) { el = document.createElement('div'); el.id = 'layer-' + name; document.body.appendChild(el); } return el; };
    const L = { preview: layer('preview'), transfers: layer('transfers'), drop: layer('drop'), menu: layer('menu'), modal: layer('modal'), toast: layer('toast') };
    const input = Object.assign(document.createElement('input'), { type: 'file', multiple: true, hidden: true });
    document.body.appendChild(input);
    input.addEventListener('change', () => { if (input.files.length) api.upload(input.files); input.value = ''; });

    const api = {
      S, F, esc, t,
      entries() {
        if (!S.share) return [];
        let l = F.list(S.share, S.path, S.sort, S.desc);
        if (S.filter) l = l.filter(e => e.name.toLowerCase().includes(S.filter.toLowerCase()));
        return l;
      },
      files() { return api.entries().filter(e => e.type === 'file'); },
      current() { return S.share ? F.node(S.share, S.path) : null; },
      selected() { return api.entries().filter(e => S.sel.has(e.name)); },
      title() { return S.path.length ? S.path[S.path.length - 1] : (S.share || t('shares')); },

      render() {
        const a = document.activeElement, id = a && a.id, pos = a && a.selectionStart;
        const sc = root.querySelector('[data-scroll]'), top = sc ? sc.scrollTop : 0, key = S.share + '/' + S.path.join('/');
        document.documentElement.dataset.theme = S.theme;
        root.innerHTML = S.logged ? o.browse(S, api) : o.login(S, api);
        const sc2 = root.querySelector('[data-scroll]'); if (sc2 && api._key === key) sc2.scrollTop = top; api._key = key;
        if (id) { const el = document.getElementById(id); if (el) { el.focus(); if (pos != null && el.setSelectionRange) el.setSelectionRange(pos, pos); } }
        api.renderTransfers(); api.renderPreview();
        o.after && o.after(S, api);
      },
      renderTransfers() { L.transfers.innerHTML = S.logged && S.transfers.length && o.transfers ? o.transfers(S, api) : ''; },
      renderPreview() {
        L.preview.innerHTML = S.preview != null && o.preview ? o.preview(S, api, api.files()[S.preview]) : '';
        document.body.classList.toggle('fx-locked', S.preview != null);
      },

      login(user, pass) {
        if (!user || !pass || pass === 'wrong') { S.error = 'login.rejected'; api.render(); return; }
        S.error = ''; S.user = user; S.logged = true; api.render();
      },
      logout() { Object.assign(S, { logged: false, share: null, path: [], sel: new Set(), preview: null, transfers: [] }); api.render(); api.toast(t('toast.loggedOut')); },
      go(share, path = []) { Object.assign(S, { share, path, sel: new Set(), filter: '', drawer: false, preview: null }); api.render(); },
      up() { if (S.path.length) api.go(S.share, S.path.slice(0, -1)); else if (S.share) api.go(null); },
      open(name) {
        const e = api.entries().find(x => x.name === name); if (!e) return;
        if (e.type === 'dir') api.go(S.share, [...S.path, name]);
        else { S.preview = api.files().indexOf(e); api.renderPreview(); }
      },
      toggle(name, range) {
        const names = api.entries().map(e => e.name);
        if (range && S.anchor != null) {
          const [a, b] = [names.indexOf(S.anchor), names.indexOf(name)].sort((x, y) => x - y);
          names.slice(a, b + 1).forEach(n => S.sel.add(n));
        } else { S.sel.has(name) ? S.sel.delete(name) : S.sel.add(name); S.anchor = name; }
        api.render();
      },
      only(name) { S.sel = new Set([name]); S.anchor = name; api.render(); },
      selectAll() { S.sel = new Set(api.entries().map(e => e.name)); api.render(); },
      clear() { S.sel = new Set(); api.render(); },
      setView(v) { S.view = v; api.render(); },
      setSort(k) { if (S.sort === k) S.desc = !S.desc; else { S.sort = k; S.desc = k !== 'name'; } api.render(); },
      theme() { S.theme = S.theme === 'dark' ? 'light' : 'dark'; api.render(); },

      upload(files) {
        if (!S.share) { api.toast(t('toast.openShareFirst')); return; }
        const share = S.share, path = [...S.path], where = api.title();
        S.transfersOpen = true;
        const jobs = F.simulateUpload(files, () => api.renderTransfers(), done => {
          F.addFiles(share, path, done);
          if (S.share === share && S.path.join('/') === path.join('/')) api.render(); else api.renderTransfers();
          api.toast(t('toast.uploaded', { n: done.length, where }));
        });
        jobs.forEach(j => { j.where = where; j.dir = 'up'; });
        S.transfers.push(...jobs); api.renderTransfers();
      },
      pick() { input.click(); },
      download(names) {
        names = names || [...S.sel];
        if (!names.length) return;
        api.toast(names.length === 1 ? t('toast.download1', { name: names[0] }) : t('toast.downloadN', { n: names.length }));
      },
      mkdir() {
        api.dialog({
          title: t('dlg.mkdir.title'), field: t('dlg.mkdir.field'), value: t('dlg.mkdir.value'), ok: t('dlg.mkdir.ok'),
          done: name => { if (!name) return; F.mkdir(S.share, S.path, name); api.render(); api.toast(t('toast.mkdir', { name })); },
        });
      },
      rename(name) {
        name = name || [...S.sel][0]; if (!name) return;
        api.dialog({
          title: t('dlg.rename.title'), field: t('dlg.rename.field'), value: name, ok: t('dlg.rename.ok'), selectStem: true,
          done: to => { if (!to || to === name) return; F.rename(S.share, S.path, name, to); S.sel = new Set([to]); api.render(); },
        });
      },
      move() { api.toast(t('toast.move')); },
      del(names) {
        names = names || [...S.sel]; if (!names.length) return;
        api.dialog({
          title: names.length === 1 ? t('dlg.del.title1', { name: names[0] }) : t('dlg.del.titleN', { n: names.length }),
          text: t('dlg.del.text'),
          ok: t('dlg.del.ok'), danger: true,
          done: () => { F.remove(S.share, S.path, names); S.sel = new Set(); S.preview = null; api.render(); api.toast(t('toast.deleted', { n: names.length })); },
        });
      },

      dialog(d) {
        L.modal.innerHTML = `<div class="fx-scrim" data-act="dialog-cancel"></div>
<form class="fx-modal" role="dialog" aria-modal="true" data-form="dialog">
  <h2 class="fx-modal-title">${esc(d.title)}</h2>
  ${d.text ? `<p class="fx-modal-text">${esc(d.text)}</p>` : ''}
  ${d.field ? `<label class="fx-field"><span>${esc(d.field)}</span><input id="fx-dialog-input" value="${esc(d.value || '')}" autocomplete="off"></label>` : ''}
  <div class="fx-modal-actions">
    <button type="button" class="fx-btn" data-act="dialog-cancel">${t('cancel')}</button>
    <button type="submit" class="fx-btn ${d.danger ? 'fx-btn-danger' : 'fx-btn-primary'}">${esc(d.ok)}</button>
  </div>
</form>`;
        api._dialog = d;
        const inp = document.getElementById('fx-dialog-input');
        if (inp) { inp.focus(); const dot = inp.value.lastIndexOf('.'); inp.setSelectionRange(0, d.selectStem && dot > 0 ? dot : inp.value.length); }
        else L.modal.querySelector('.fx-btn-danger, .fx-btn-primary').focus();
      },
      closeDialog() { L.modal.innerHTML = ''; api._dialog = null; },

      menu(x, y, name) {
        if (!S.sel.has(name)) { S.sel = new Set([name]); api.render(); }
        const one = S.sel.size === 1, e = api.entries().find(q => q.name === name);
        const item = (act, ic, label, cls = '') => `<button class="fx-menu-item ${cls}" data-act="${act}" role="menuitem">${F.icon(ic, 16)}<span>${label}</span></button>`;
        L.menu.innerHTML = `<div class="fx-menu" role="menu" style="left:${x}px;top:${y}px">
${one ? item('m-open', e && e.type === 'dir' ? 'folder' : 'eye', t(e && e.type === 'dir' ? 'act.open' : 'act.preview')) : ''}
${item('m-download', 'download', one ? t('act.download') : t('act.downloadN', { n: S.sel.size }))}
${one ? item('m-rename', 'pencil', t('act.rename')) : ''}
${item('m-move', 'move', t('act.move'))}
<div class="fx-menu-sep"></div>
${item('m-del', 'trash', t('act.delete'), 'fx-danger')}
</div>`;
        const el = L.menu.firstElementChild, r = el.getBoundingClientRect();
        if (r.right > innerWidth - 8) el.style.left = (innerWidth - r.width - 8) + 'px';
        if (r.bottom > innerHeight - 8) el.style.top = (innerHeight - r.height - 8) + 'px';
        api._menuName = name;
      },
      closeMenu() { L.menu.innerHTML = ''; },

      toast(msg) {
        const el = document.createElement('div'); el.className = 'fx-toast'; el.textContent = msg;
        L.toast.appendChild(el); requestAnimationFrame(() => el.classList.add('in'));
        setTimeout(() => { el.classList.remove('in'); setTimeout(() => el.remove(), 300); }, 3200);
      },
    };

    /* clicks */
    document.addEventListener('click', ev => {
      if (!ev.target.closest('.fx-menu')) api.closeMenu();
      const el = ev.target.closest('[data-act]'); if (!el) return;
      const d = el.dataset, act = d.act;
      const handlers = {
        logout: () => api.logout(),
        home: () => api.go(null),
        share: () => api.go(d.share),
        crumb: () => api.go(S.share, S.path.slice(0, +d.depth)),
        up: () => api.up(),
        open: () => {
          if (ev.shiftKey || ev.ctrlKey || ev.metaKey) { api.toggle(d.name, ev.shiftKey); return; }
          if (S.sel.size) { api.toggle(d.name); return; }
          api.open(d.name);
        },
        go: () => api.open(d.name),
        sel: () => { ev.stopPropagation(); api.toggle(d.name, ev.shiftKey); },
        selall: () => (S.sel.size === api.entries().length ? api.clear() : api.selectAll()),
        clear: () => api.clear(),
        view: () => api.setView(d.view),
        sort: () => api.setSort(d.sort),
        upload: () => api.pick(),
        mkdir: () => api.mkdir(),
        rename: () => api.rename(d.name),
        del: () => api.del(d.name ? [d.name] : null),
        download: () => api.download(d.name ? [d.name] : null),
        move: () => api.move(),
        theme: () => api.theme(),
        lang: () => { I18N.set(d.lang || (I18N.lang === 'de' ? 'en' : 'de')); api.render(); },
        drawer: () => { S.drawer = !S.drawer; api.render(); },
        info: () => { S.info = !S.info; api.renderPreview(); },
        'prev-close': () => { S.preview = null; api.renderPreview(); },
        'prev-next': () => { S.preview = (S.preview + 1) % api.files().length; api.renderPreview(); },
        'prev-prev': () => { S.preview = (S.preview - 1 + api.files().length) % api.files().length; api.renderPreview(); },
        'prev-goto': () => { S.preview = +d.index; api.renderPreview(); },
        'prev-download': () => api.download([api.files()[S.preview].name]),
        'prev-del': () => api.del([api.files()[S.preview].name]),
        'transfers-toggle': () => { S.transfersOpen = !S.transfersOpen; api.renderTransfers(); },
        'transfers-clear': () => { S.transfers = S.transfers.filter(j => j.done < j.size); api.renderTransfers(); },
        'dialog-cancel': () => api.closeDialog(),
        'm-open': () => { api.closeMenu(); api.open(api._menuName); },
        'm-download': () => { api.closeMenu(); api.download(); },
        'm-rename': () => { api.closeMenu(); api.rename(api._menuName); },
        'm-move': () => { api.closeMenu(); api.move(); },
        'm-del': () => { api.closeMenu(); api.del(); },
        pw: () => { const i = document.getElementById(d.for); i.type = i.type === 'password' ? 'text' : 'password'; el.setAttribute('aria-pressed', i.type === 'text'); },
      };
      const h = handlers[act] || (o.actions && o.actions[act] && (() => o.actions[act](d, api, ev)));
      if (h) { ev.preventDefault(); h(); }
    });
    document.addEventListener('contextmenu', ev => {
      const el = ev.target.closest('[data-act="open"][data-name]'); if (!el) return;
      ev.preventDefault(); api.menu(ev.clientX, ev.clientY, el.dataset.name);
    });
    document.addEventListener('submit', ev => {
      const f = ev.target; ev.preventDefault();
      if (f.dataset.form === 'login') api.login(f.user.value.trim(), f.pass.value);
      if (f.dataset.form === 'dialog') { const d = api._dialog, inp = document.getElementById('fx-dialog-input'); api.closeDialog(); d.done(inp ? inp.value.trim() : true); }
    });
    document.addEventListener('input', ev => {
      if (ev.target.dataset.filter != null) { S.filter = ev.target.value; S.sel = new Set(); api.render(); }
    });

    /* keys */
    document.addEventListener('keydown', ev => {
      const typing = /INPUT|TEXTAREA/.test(document.activeElement.tagName);
      if (ev.key === 'Escape') {
        if (api._dialog) return api.closeDialog();
        if (L.menu.innerHTML) return api.closeMenu();
        if (S.preview != null) { S.preview = null; return api.renderPreview(); }
        if (S.drawer) { S.drawer = false; return api.render(); }
        if (S.sel.size) return api.clear();
      }
      if (!S.logged || typing || api._dialog) return;
      if (S.preview != null) {
        if (ev.key === 'ArrowRight') { S.preview = (S.preview + 1) % api.files().length; api.renderPreview(); }
        if (ev.key === 'ArrowLeft') { S.preview = (S.preview - 1 + api.files().length) % api.files().length; api.renderPreview(); }
        if (ev.key === 'i') { S.info = !S.info; api.renderPreview(); }
        return;
      }
      const names = api.entries().map(e => e.name), cur = [...S.sel].pop(), i = names.indexOf(cur);
      if ((ev.ctrlKey || ev.metaKey) && ev.key === 'a') { ev.preventDefault(); api.selectAll(); }
      else if (ev.key === 'Delete' && S.sel.size) api.del();
      else if (ev.key === 'F2' && S.sel.size === 1) { ev.preventDefault(); api.rename(); }
      else if (ev.key === 'Backspace' || (ev.altKey && ev.key === 'ArrowUp')) { ev.preventDefault(); api.up(); }
      else if (ev.key === 'Enter' && cur) api.open(cur);
      else if (['ArrowDown', 'ArrowRight', 'j'].includes(ev.key) && names.length) { ev.preventDefault(); api.only(names[Math.min(names.length - 1, i + 1)]); scrollSel(); }
      else if (['ArrowUp', 'ArrowLeft', 'k'].includes(ev.key) && names.length) { ev.preventDefault(); api.only(names[Math.max(0, i - 1)]); scrollSel(); }
      else if (ev.key === '/' && o.onSlash) { ev.preventDefault(); o.onSlash(api); }
    });
    const scrollSel = () => { const el = root.querySelector('.is-sel'); if (el) el.scrollIntoView({ block: 'nearest' }); };

    /* long press on touch starts selecting */
    let press = null, swallow = false;
    document.addEventListener('pointerdown', ev => {
      const el = ev.target.closest('[data-act="open"][data-name]'); if (!el || ev.pointerType === 'mouse') return;
      press = setTimeout(() => { press = null; swallow = true; if (navigator.vibrate) navigator.vibrate(10); api.toggle(el.dataset.name); }, 480);
    });
    ['pointerup', 'pointercancel', 'pointermove'].forEach(t => document.addEventListener(t, ev => { if (press && (t !== 'pointermove' || Math.abs(ev.movementY) > 4)) { clearTimeout(press); press = null; } }));
    document.addEventListener('click', ev => { if (swallow) { swallow = false; ev.stopPropagation(); ev.preventDefault(); } }, true);

    /* drag and drop from the desktop */
    let depth = 0;
    const hasFiles = ev => ev.dataTransfer && [...ev.dataTransfer.types].includes('Files');
    window.addEventListener('dragenter', ev => {
      if (!hasFiles(ev) || !S.logged || !S.share) return;
      ev.preventDefault(); depth++;
      if (!S.drag) { S.drag = true; L.drop.innerHTML = o.drop ? o.drop(S, api) : ''; }
    });
    window.addEventListener('dragover', ev => { if (hasFiles(ev)) ev.preventDefault(); });
    window.addEventListener('dragleave', () => { if (--depth <= 0) { depth = 0; S.drag = false; L.drop.innerHTML = ''; } });
    window.addEventListener('drop', ev => {
      if (!hasFiles(ev)) return; ev.preventDefault(); depth = 0; S.drag = false; L.drop.innerHTML = '';
      if (S.logged && S.share && ev.dataTransfer.files.length) api.upload(ev.dataTransfer.files);
    });

    api.render();
    return api;
  };
})();
