/* ferry mockups: fake shares, helpers, icons and generated thumbnails.
   Nothing here talks to a server; every page works offline from file://. */
(function () {
  'use strict';

  const now = new Date(2026, 10, 21, 18, 40);
  const ago = (days, h = 10, m = 0) => { const d = new Date(now); d.setDate(d.getDate() - days); d.setHours(h, m); return d; };
  const f = (name, size, mod) => ({ type: 'file', name, size, mod });
  const dir = (name, children, mod) => ({ type: 'dir', name, children, mod: mod || ago(3) });

  const photos = (prefix, n, start, days) => Array.from({ length: n }, (_, i) =>
    f(`${prefix}_${String(start + i).padStart(4, '0')}.jpg`, 2.4e6 + ((i * 7919) % 41) * 1.1e5, ago(days, 9 + (i % 9), (i * 13) % 60)));

  /* fictional sample data: one user, alex, and the shares a household NAS might have */
  const tree = {
    alex: dir('alex', [
      dir('Documents', [
        dir('Taxes 2025', [
          f('Payslips 2025.pdf', 184_000, ago(260)),
          f('Donation receipts.pdf', 412_000, ago(250)),
          f('Home office.xlsx', 38_000, ago(240)),
        ], ago(240)),
        dir('Insurance', [
          f('Household policy.pdf', 920_000, ago(400)),
          f('Car 2026.pdf', 310_000, ago(320)),
        ], ago(320)),
        f('Lease.pdf', 2_400_000, ago(900)),
        f('Notes.md', 6_200, ago(0, 16, 12)),
        f('Floor plan.pdf', 1_800_000, ago(41)),
      ], ago(0, 16, 12)),
      dir('Photos', [
        dir('2026', [
          dir('Summer by the sea', [
            ...photos('IMG', 22, 4012, 120),
            f('Sunset on the beach.mp4', 1_420_000_000, ago(119, 20, 41)),
            f('Gulls.mov', 386_000_000, ago(118, 11, 5)),
          ], ago(118)),
          dir('Winter', photos('DSC', 9, 812, 330), ago(330)),
          dir('Garden', photos('IMG', 7, 5120, 60), ago(60)),
        ], ago(60)),
        dir('2025', [dir('Hiking', photos('IMG', 12, 2201, 480), ago(480))], ago(480)),
      ], ago(60)),
      dir('Music', [
        f('Mixtape 2003.zip', 412_000_000, ago(1400)),
        f('Lullabies.mp3', 6_100_000, ago(700)),
        f('Guitar take.m4a', 14_200_000, ago(12, 21, 30)),
      ], ago(12)),
      dir('Backup', [dir('laptop', [f('README.txt', 300, ago(1))], ago(1))], ago(1)),
      f('Shopping list.txt', 412, ago(0, 9, 3)),
    ]),
    Family: dir('Family', [
      dir('Household', [
        f('Birthdays.xlsx', 22_000, ago(30)),
        f('Emergency numbers.pdf', 88_000, ago(200)),
        dir('School', [f('Parents letter November.pdf', 140_000, ago(5)), f('Lunch menu.pdf', 96_000, ago(2))], ago(2)),
      ], ago(2)),
      dir('Recipes', [
        f('Apple pie.md', 2_100, ago(90)),
        f('Pumpkin soup.md', 1_400, ago(25)),
        f('Pizza dough.jpg', 3_100_000, ago(25)),
      ], ago(25)),
      dir('Holiday 2026', [
        f('Packing list.md', 1_900, ago(140)),
        f('Cottage booking.pdf', 260_000, ago(150)),
        ...photos('IMG', 6, 4100, 117),
      ], ago(117)),
    ]),
    Public: dir('Public', [
      dir('Software', [f('ubuntu-24.04-desktop-amd64.iso', 6_100_000_000, ago(500)), f('installer.zip', 23_000_000, ago(20))], ago(20)),
      dir('Films', [f('Family film 2019.mkv', 4_300_000_000, ago(1000))], ago(1000)),
    ]),
    Scans: dir('Scans', [
      f('scan_2026-11-20_183112.pdf', 640_000, ago(1, 18, 31)),
      f('scan_2026-11-14_094401.pdf', 1_200_000, ago(7, 9, 44)),
      f('scan_2026-11-02_112009.pdf', 380_000, ago(19, 11, 20)),
      f('scan_2026-10-28_201544.pdf', 2_900_000, ago(24, 20, 15)),
    ]),
  };

  const T = (k, p) => (window.I18N ? I18N.t(k, p) : k);
  const loc = () => (window.I18N ? I18N.locale() : 'de-DE');
  /* the note stands for the share's comment on the server */
  const shares = ['alex', 'Family', 'Public', 'Scans'].map(name => ({ name, get note() { return T('note.' + name); } }));

  function node(share, path) {
    let n = tree[share];
    for (const p of path) { n = n && n.children && n.children.find(c => c.type === 'dir' && c.name === p); }
    return n;
  }

  function list(share, path, sort = 'name', dirDesc = false) {
    const n = node(share, path);
    if (!n) return [];
    const cmp = {
      name: (a, b) => a.name.localeCompare(b.name, 'de', { numeric: true }),
      size: (a, b) => (a.size || 0) - (b.size || 0),
      mod: (a, b) => a.mod - b.mod,
    }[sort];
    return [...n.children].sort((a, b) => (a.type !== b.type ? (a.type === 'dir' ? -1 : 1) : (dirDesc ? -cmp(a, b) : cmp(a, b))));
  }

  function kind(e) {
    if (e.type === 'dir') return 'dir';
    const x = e.name.split('.').pop().toLowerCase();
    if (['jpg', 'jpeg', 'png', 'heic', 'webp', 'gif'].includes(x)) return 'image';
    if (['mp4', 'mov', 'mkv', 'webm'].includes(x)) return 'video';
    if (['mp3', 'm4a', 'flac', 'wav'].includes(x)) return 'audio';
    if (x === 'pdf') return 'pdf';
    if (['md', 'txt'].includes(x)) return 'text';
    if (['xlsx', 'csv', 'ods'].includes(x)) return 'sheet';
    if (['zip', 'iso', '7z', 'tar'].includes(x)) return 'archive';
    return 'file';
  }

  const kindLabel = new Proxy({}, { get: (o, k) => T('kind.' + String(k)) });

  function size(n) {
    if (n == null) return '—';
    if (n < 1000) return `${n} B`;
    const u = ['KB', 'MB', 'GB', 'TB']; let i = -1;
    do { n /= 1000; i++; } while (n >= 1000 && i < u.length - 1);
    return `${n.toLocaleString(loc(), { maximumFractionDigits: n < 10 ? 1 : 0 })} ${u[i]}`;
  }

  function date(d) {
    const days = Math.floor((now - d) / 864e5);
    const t = d.toLocaleTimeString(loc(), { hour: '2-digit', minute: '2-digit' });
    if (days === 0 && now.getDate() === d.getDate()) return T('today', { t });
    if (days <= 1) return T('yesterday', { t });
    if (days < 300) return d.toLocaleDateString(loc(), { day: 'numeric', month: 'short' });
    return d.toLocaleDateString(loc(), { day: 'numeric', month: 'short', year: 'numeric' });
  }

  function count(e) {
    if (e.type !== 'dir') return size(e.size);
    return T('count', { n: e.children.length });
  }

  /* deterministic pseudo random from a string */
  function rng(seed) {
    let h = 2166136261;
    for (const c of seed) { h ^= c.charCodeAt(0); h = Math.imul(h, 16777619); }
    return () => { h ^= h << 13; h ^= h >>> 17; h ^= h << 5; return ((h >>> 0) % 10000) / 10000; };
  }

  const palettes = [
    ['#f6d6ad', '#f7a072', '#fff3d6', '#5b8c85', '#2f4f4f', '#7fb7be'],
    ['#a7d8f0', '#e9f5fb', '#fffbe6', '#6a994e', '#386641', '#4a90a4'],
    ['#ffcdb2', '#e5989b', '#fff1e6', '#6d6875', '#3d3a4b', '#b5838d'],
    ['#2b2d42', '#8d99ae', '#edf2f4', '#3a506b', '#1c2541', '#5bc0be'],
    ['#ffe5b4', '#ffb347', '#fffaf0', '#c97c5d', '#7a4e3a', '#4f9da6'],
    ['#cde7e2', '#9fd3c7', '#ffffff', '#385170', '#142d4c', '#ececec'],
  ];

  function thumb(name, w = 320, h = 240) {
    const r = rng(name); const p = palettes[Math.floor(r() * palettes.length)];
    const sunX = 0.2 + r() * 0.6, sunY = 0.18 + r() * 0.25, horizon = 0.55 + r() * 0.15;
    const hill = (base, amp, seg) => {
      let d = `M0 ${h}L0 ${h * base}`;
      for (let i = 1; i <= seg; i++) {
        const x = (w / seg) * i, y = h * (base - amp * (r() - 0.3));
        d += ` Q${x - w / seg / 2} ${y - h * amp * r()} ${x} ${y}`;
      }
      return d + ` L${w} ${h}Z`;
    };
    const sea = r() > 0.5;
    const svg = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 ${w} ${h}" preserveAspectRatio="xMidYMid slice">
<defs><linearGradient id="s" x1="0" y1="0" x2="0" y2="1"><stop offset="0" stop-color="${p[0]}"/><stop offset="1" stop-color="${p[1]}"/></linearGradient></defs>
<rect width="${w}" height="${h}" fill="url(#s)"/>
<circle cx="${w * sunX}" cy="${h * sunY}" r="${h * 0.09}" fill="${p[2]}" opacity=".9"/>
${sea ? `<rect y="${h * horizon}" width="${w}" height="${h}" fill="${p[5]}" opacity=".85"/><path d="M0 ${h * (horizon + 0.08)}h${w}" stroke="${p[2]}" stroke-opacity=".35" stroke-width="2"/>` : ''}
<path d="${hill(horizon + 0.05, 0.12, 3)}" fill="${p[3]}" opacity=".9"/>
<path d="${hill(horizon + 0.18, 0.1, 4)}" fill="${p[4]}"/>
</svg>`;
    return `data:image/svg+xml,${encodeURIComponent(svg)}`;
  }

  function docThumb(name, w = 240, h = 320) {
    const r = rng(name); const lines = [];
    for (let i = 0; i < 14; i++) lines.push(`<rect x="28" y="${92 + i * 15}" width="${120 + r() * 70}" height="5" rx="2.5" fill="#cfd6dd"/>`);
    const svg = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 ${w} ${h}"><rect width="${w}" height="${h}" fill="#ffffff"/>
<rect x="28" y="34" width="110" height="12" rx="3" fill="#33414e"/><rect x="28" y="56" width="70" height="7" rx="3" fill="#9aa7b4"/>${lines.join('')}</svg>`;
    return `data:image/svg+xml,${encodeURIComponent(svg)}`;
  }

  const P = {
    folder: '<path d="M20 20a2 2 0 0 0 2-2V8a2 2 0 0 0-2-2h-7.9a2 2 0 0 1-1.69-.9L9.6 3.9A2 2 0 0 0 7.93 3H4a2 2 0 0 0-2 2v13a2 2 0 0 0 2 2Z"/>',
    file: '<path d="M15 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V7Z"/><path d="M14 2v4a2 2 0 0 0 2 2h4"/>',
    image: '<rect width="18" height="18" x="3" y="3" rx="2"/><circle cx="9" cy="9" r="2"/><path d="m21 15-3.09-3.09a2 2 0 0 0-2.82 0L6 21"/>',
    video: '<path d="m16 13 5.223 3.482a.5.5 0 0 0 .777-.416V7.87a.5.5 0 0 0-.752-.432L16 10.5"/><rect x="2" y="6" width="14" height="12" rx="2"/>',
    audio: '<path d="M9 18V5l12-2v13"/><circle cx="6" cy="18" r="3"/><circle cx="18" cy="16" r="3"/>',
    pdf: '<path d="M15 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V7Z"/><path d="M14 2v4a2 2 0 0 0 2 2h4"/><path d="M10 9H8"/><path d="M16 13H8"/><path d="M16 17H8"/>',
    text: '<path d="M15 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V7Z"/><path d="M14 2v4a2 2 0 0 0 2 2h4"/><path d="M16 13H8"/><path d="M16 17H8"/>',
    sheet: '<path d="M15 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V7Z"/><path d="M14 2v4a2 2 0 0 0 2 2h4"/><path d="M8 13h2"/><path d="M14 13h2"/><path d="M8 17h2"/><path d="M14 17h2"/>',
    archive: '<rect width="20" height="5" x="2" y="3" rx="1"/><path d="M4 8v11a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8"/><path d="M10 12h4"/>',
    upload: '<path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"/><polyline points="17 8 12 3 7 8"/><line x1="12" x2="12" y1="3" y2="15"/>',
    download: '<path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"/><polyline points="7 10 12 15 17 10"/><line x1="12" x2="12" y1="15" y2="3"/>',
    grid: '<rect width="7" height="7" x="3" y="3" rx="1"/><rect width="7" height="7" x="14" y="3" rx="1"/><rect width="7" height="7" x="14" y="14" rx="1"/><rect width="7" height="7" x="3" y="14" rx="1"/>',
    list: '<line x1="8" x2="21" y1="6" y2="6"/><line x1="8" x2="21" y1="12" y2="12"/><line x1="8" x2="21" y1="18" y2="18"/><line x1="3" x2="3.01" y1="6" y2="6"/><line x1="3" x2="3.01" y1="12" y2="12"/><line x1="3" x2="3.01" y1="18" y2="18"/>',
    search: '<circle cx="11" cy="11" r="8"/><path d="m21 21-4.3-4.3"/>',
    right: '<path d="m9 18 6-6-6-6"/>',
    left: '<path d="m15 18-6-6 6-6"/>',
    down: '<path d="m6 9 6 6 6-6"/>',
    x: '<path d="M18 6 6 18"/><path d="m6 6 12 12"/>',
    more: '<circle cx="12" cy="12" r="1"/><circle cx="19" cy="12" r="1"/><circle cx="5" cy="12" r="1"/>',
    trash: '<path d="M3 6h18"/><path d="M19 6v14c0 1-1 2-2 2H7c-1 0-2-1-2-2V6"/><path d="M8 6V4c0-1 1-2 2-2h4c1 0 2 1 2 2v2"/>',
    pencil: '<path d="M17 3a2.85 2.83 0 1 1 4 4L7.5 20.5 2 22l1.5-5.5Z"/>',
    move: '<path d="M2 9V5a2 2 0 0 1 2-2h3.9a2 2 0 0 1 1.69.9l.81 1.2a2 2 0 0 0 1.67.9H20a2 2 0 0 1 2 2v10a2 2 0 0 1-2 2H4a2 2 0 0 1-2-2v-1"/><path d="M2 13h10"/><path d="m9 16 3-3-3-3"/>',
    plus: '<path d="M5 12h14"/><path d="M12 5v14"/>',
    folderPlus: '<path d="M12 10v6"/><path d="M9 13h6"/><path d="M20 20a2 2 0 0 0 2-2V8a2 2 0 0 0-2-2h-7.9a2 2 0 0 1-1.69-.9L9.6 3.9A2 2 0 0 0 7.93 3H4a2 2 0 0 0-2 2v13a2 2 0 0 0 2 2Z"/>',
    logout: '<path d="M9 21H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h4"/><polyline points="16 17 21 12 16 7"/><line x1="21" x2="9" y1="12" y2="12"/>',
    sun: '<circle cx="12" cy="12" r="4"/><path d="M12 2v2"/><path d="M12 20v2"/><path d="m4.93 4.93 1.41 1.41"/><path d="m17.66 17.66 1.41 1.41"/><path d="M2 12h2"/><path d="M20 12h2"/><path d="m6.34 17.66-1.41 1.41"/><path d="m19.07 4.93-1.41 1.41"/>',
    moon: '<path d="M12 3a6 6 0 0 0 9 9 9 9 0 1 1-9-9Z"/>',
    check: '<path d="M20 6 9 17l-5-5"/>',
    drive: '<line x1="22" x2="2" y1="12" y2="12"/><path d="M5.45 5.11 2 12v6a2 2 0 0 0 2 2h16a2 2 0 0 0 2-2v-6l-3.45-6.89A2 2 0 0 0 16.76 4H7.24a2 2 0 0 0-1.79 1.11z"/><line x1="6" x2="6.01" y1="16" y2="16"/><line x1="10" x2="10.01" y1="16" y2="16"/>',
    lock: '<rect width="18" height="11" x="3" y="11" rx="2" ry="2"/><path d="M7 11V7a5 5 0 0 1 10 0v4"/>',
    eye: '<path d="M2 12s3-7 10-7 10 7 10 7-3 7-10 7-10-7-10-7Z"/><circle cx="12" cy="12" r="3"/>',
    info: '<circle cx="12" cy="12" r="10"/><path d="M12 16v-4"/><path d="M12 8h.01"/>',
    sort: '<path d="m21 16-4 4-4-4"/><path d="M17 20V4"/><path d="m3 8 4-4 4 4"/><path d="M7 4v16"/>',
    clock: '<circle cx="12" cy="12" r="10"/><polyline points="12 6 12 12 16 14"/>',
    play: '<polygon points="6 3 20 12 6 21 6 3"/>',
    menu: '<line x1="4" x2="20" y1="12" y2="12"/><line x1="4" x2="20" y1="6" y2="6"/><line x1="4" x2="20" y1="18" y2="18"/>',
    users: '<path d="M16 21v-2a4 4 0 0 0-4-4H6a4 4 0 0 0-4 4v2"/><circle cx="9" cy="7" r="4"/><path d="M22 21v-2a4 4 0 0 0-3-3.87"/><path d="M16 3.13a4 4 0 0 1 0 7.75"/>',
    globe: '<circle cx="12" cy="12" r="10"/><path d="M12 2a14.5 14.5 0 0 0 0 20 14.5 14.5 0 0 0 0-20"/><path d="M2 12h20"/>',
    printer: '<path d="M6 18H4a2 2 0 0 1-2-2v-5a2 2 0 0 1 2-2h16a2 2 0 0 1 2 2v5a2 2 0 0 1-2 2h-2"/><path d="M6 9V3a1 1 0 0 1 1-1h10a1 1 0 0 1 1 1v6"/><rect x="6" y="14" width="12" height="8" rx="1"/>',
    user: '<path d="M19 21v-2a4 4 0 0 0-4-4H9a4 4 0 0 0-4 4v2"/><circle cx="12" cy="7" r="4"/>',
    command: '<path d="M15 6v12a3 3 0 1 0 3-3H6a3 3 0 1 0 3 3V6a3 3 0 1 0-3 3h12a3 3 0 1 0-3-3"/>',
  };

  function icon(name, size = 18, stroke = 1.75) {
    return `<svg class="i i-${name}" width="${size}" height="${size}" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="${stroke}" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">${P[name] || P.file}</svg>`;
  }

  const shareIcon = { alex: 'user', Family: 'users', Public: 'globe', Scans: 'printer' };

  /* the ferry mark: a small boat on two waves */
  function mark(size = 28) {
    return `<svg class="ferry-mark" width="${size}" height="${size}" viewBox="0 0 32 32" fill="none" aria-hidden="true">
<path d="M4.5 16.5h23l-3.2 6.1a2 2 0 0 1-1.77 1.07H9.47a2 2 0 0 1-1.77-1.07Z" fill="currentColor"/>
<path d="M10 16.5v-3.8a1.2 1.2 0 0 1 1.2-1.2h7.3l3 5" stroke="currentColor" stroke-width="2" stroke-linejoin="round"/>
<path d="M15 11.5V7.5" stroke="currentColor" stroke-width="2" stroke-linecap="round"/>
<path d="M3 27.5c2.2 0 2.2-1.6 4.4-1.6s2.2 1.6 4.4 1.6 2.2-1.6 4.4-1.6 2.2 1.6 4.4 1.6 2.2-1.6 4.4-1.6 2.2 1.6 4.4 1.6" stroke="var(--wave, currentColor)" stroke-width="2" stroke-linecap="round"/>
</svg>`;
  }

  const sampleText = {
    'Notes.md': '# Notes\n\n- Order a spare disk\n- Try the scanner with SMB2\n- Sort the summer photos',
    'Shopping list.txt': 'Milk\nOats\nApples\nCoffee\nAA batteries\nWrapping paper',
    'Apple pie.md': '# Apple pie\n\n250 g flour, 125 g butter, 80 g sugar, 1 egg\n\n1 kg apples, cinnamon.\n\n180 °C, 50 minutes.',
  };

  function text(name) {
    return sampleText[name] || T('textFallback', { name });
  }

  function preview(e) {
    const k = kind(e);
    if (k === 'image') return { k, src: thumb(e.name, 1200, 800) };
    if (k === 'video') return { k, src: thumb(e.name + 'v', 1280, 720) };
    if (k === 'pdf') return { k, src: docThumb(e.name, 600, 800) };
    if (k === 'text') return { k, text: text(e.name) };
    return { k };
  }

  /* upload simulation: real dropped files, fake progress */
  function simulateUpload(files, onTick, onDone) {
    const jobs = [...files].map(fl => ({ name: fl.name, size: fl.size || 1e6, done: 0 }));
    const rate = 11e6; /* ~ 1 GbE after overhead, divided among jobs */
    const t = setInterval(() => {
      const active = jobs.filter(j => j.done < j.size);
      if (!active.length) { clearInterval(t); onDone(jobs); return; }
      for (const j of active) j.done = Math.min(j.size, j.done + Math.max(rate / active.length / 5, j.size / 25));
      onTick(jobs);
    }, 200);
    return jobs;
  }

  function addFiles(share, path, jobs) {
    const n = node(share, path);
    for (const j of jobs) {
      const i = n.children.findIndex(c => c.name === j.name);
      const e = f(j.name, j.size, new Date(now.getTime() + 60e3));
      if (i >= 0) n.children[i] = e; else n.children.push(e);
    }
  }

  function remove(share, path, names) {
    const n = node(share, path);
    n.children = n.children.filter(c => !names.includes(c.name));
  }

  function rename(share, path, from, to) {
    const e = node(share, path).children.find(c => c.name === from);
    if (e) e.name = to;
  }

  function mkdir(share, path, name) {
    node(share, path).children.push(dir(name, [], new Date(now)));
  }

  window.FERRY = { now, tree, shares, shareIcon, node, list, kind, kindLabel, size, date, count, thumb, docThumb, icon, mark, preview, simulateUpload, addFiles, remove, rename, mkdir, loc, user: 'alex', server: 'nas.example.lan' };
})();
