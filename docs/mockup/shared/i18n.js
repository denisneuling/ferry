/* ferry mockups: English and German. In the application this is @angular/localize
   (English source, German as the first translation); here a small dictionary does
   the same job. The language comes from ?lang=, then the viewer's last choice,
   then the browser. */
(function () {
  'use strict';
  const n1 = (one, many) => p => (p.n === 1 ? one : many).replace('{n}', p.n);

  const dict = {
    en: {
      /* shared */
      shares: 'Shares',
      today: 'Today, {t}', yesterday: 'Yesterday, {t}',
      count: n1('1 item', '{n} items'),
      'kind.dir': 'Folder', 'kind.image': 'Image', 'kind.video': 'Video', 'kind.audio': 'Audio', 'kind.pdf': 'PDF',
      'kind.text': 'Text', 'kind.sheet': 'Spreadsheet', 'kind.archive': 'Archive', 'kind.file': 'File',
      'note.alex': 'Personal', 'note.Family': 'For the family', 'note.Public': 'For everyone on the network', 'note.Scans': 'From the printer',
      textFallback: '{name}\n\nThis is a text preview in the mockup.\nThe real preview shows the file’s content.',
      'login.rejected': 'Samba rejected the sign-in. The username or password is wrong.',
      'toast.loggedOut': 'Signed out. The SMB session has ended.',
      'toast.openShareFirst': 'Open a share first.',
      'toast.uploaded': p => (p.n === 1 ? `1 file uploaded to “${p.where}”.` : `${p.n} files uploaded to “${p.where}”.`),
      'toast.download1': 'Downloading {name}',
      'toast.downloadN': '{n} files are downloaded one by one.',
      'toast.mkdir': 'Folder “{name}” created.',
      'toast.move': 'Mockup: the folder picker for moving opens here.',
      'toast.deleted': n1('Deleted.', '{n} items deleted.'),
      'dlg.mkdir.title': 'New folder', 'dlg.mkdir.field': 'Name', 'dlg.mkdir.value': 'New folder', 'dlg.mkdir.ok': 'Create',
      'dlg.rename.title': 'Rename', 'dlg.rename.field': 'New name', 'dlg.rename.ok': 'Rename',
      'dlg.del.title1': 'Delete “{name}”?', 'dlg.del.titleN': 'Delete {n} items?',
      'dlg.del.text': 'Deleting is final. It can only be brought back from a snapshot.',
      'dlg.del.ok': 'Delete', cancel: 'Cancel',
      'act.open': 'Open', 'act.preview': 'Preview', 'act.download': 'Download', 'act.downloadN': 'Download {n}',
      'act.rename': 'Rename', 'act.move': 'Move', 'act.delete': 'Delete',

      /* the page */
      hint: 'Mockup — any password works, “wrong” shows the error',
      'login.tagline': 'Your files from the NAS, in the browser.',
      'login.title': 'Sign in', 'login.sub': 'with your account on {server}',
      'login.user': 'Username', 'login.pass': 'Password', 'login.passPh': 'Samba password', 'login.show': 'Show password',
      'login.submit': 'Sign in',
      'login.trust': 'ferry does not store your password. The NAS checks it when you sign in; after 30 minutes without activity you are signed out.',
      'theme.light': 'Light', 'theme.dark': 'Dark', 'theme.toggle': 'Light or dark',
      'lang.other': 'Deutsch', 'lang.label': 'Language',
      logout: 'Sign out', menu: 'Menu', back: 'Back', close: 'Close',
      'hello.morning': 'Good morning, {name}', 'hello.day': 'Hello, {name}', 'hello.evening': 'Good evening, {name}',
      'home.sub': 'What would you like to open?',
      matching: '{n} matching',
      'search.ph': 'Search this folder',
      'view.grid': 'Tiles', 'view.list': 'List',
      newFolder: 'New folder', upload: 'Upload',
      selected: '{n} selected', clearSel: 'Clear selection',
      'sec.folders': 'Folders', 'sec.files': 'Files',
      'empty.none': 'Nothing found', 'empty.noneText': 'No name in this folder contains “{q}”.',
      'empty.empty': 'This folder is empty', 'empty.emptyText': 'Drag files here or use “Upload”.',
      'col.name': 'Name', 'col.mod': 'Modified', 'col.size': 'Size',
      'tr.running': n1('1 upload running', '{n} uploads running'), 'tr.done': 'Uploads finished',
      'tr.of': '{done} of {total} · to “{where}”',
      'pv.count': '{i} of {n}', 'pv.details': 'Details (i)', 'pv.close': 'Close (Esc)', 'pv.none': 'No preview',
      'info.title': 'Details', 'info.name': 'Name', 'info.kind': 'Type', 'info.size': 'Size', 'info.mod': 'Modified', 'info.where': 'Location',
      'drop.title': 'Release to upload', 'drop.to': 'to “{where}”',
    },
    de: {
      shares: 'Freigaben',
      today: 'Heute, {t}', yesterday: 'Gestern, {t}',
      count: n1('1 Element', '{n} Elemente'),
      'kind.dir': 'Ordner', 'kind.image': 'Bild', 'kind.video': 'Video', 'kind.audio': 'Audio', 'kind.pdf': 'PDF',
      'kind.text': 'Text', 'kind.sheet': 'Tabelle', 'kind.archive': 'Archiv', 'kind.file': 'Datei',
      'note.alex': 'Persönlich', 'note.Family': 'Für die Familie', 'note.Public': 'Für alle im Netz', 'note.Scans': 'Vom Drucker',
      textFallback: '{name}\n\nDies ist eine Textvorschau im Mockup.\nDie echte Vorschau zeigt den Dateiinhalt.',
      'login.rejected': 'Samba hat die Anmeldung abgelehnt. Benutzername oder Passwort stimmen nicht.',
      'toast.loggedOut': 'Abgemeldet. Die SMB-Sitzung ist beendet.',
      'toast.openShareFirst': 'Erst eine Freigabe öffnen.',
      'toast.uploaded': p => (p.n === 1 ? `1 Datei nach »${p.where}« hochgeladen.` : `${p.n} Dateien nach »${p.where}« hochgeladen.`),
      'toast.download1': 'Download: {name}',
      'toast.downloadN': '{n} Dateien werden einzeln heruntergeladen.',
      'toast.mkdir': 'Ordner »{name}« angelegt.',
      'toast.move': 'Mockup: Hier öffnet sich die Ordnerauswahl zum Verschieben.',
      'toast.deleted': n1('Gelöscht.', '{n} Elemente gelöscht.'),
      'dlg.mkdir.title': 'Neuer Ordner', 'dlg.mkdir.field': 'Name', 'dlg.mkdir.value': 'Neuer Ordner', 'dlg.mkdir.ok': 'Anlegen',
      'dlg.rename.title': 'Umbenennen', 'dlg.rename.field': 'Neuer Name', 'dlg.rename.ok': 'Umbenennen',
      'dlg.del.title1': '»{name}« löschen?', 'dlg.del.titleN': '{n} Elemente löschen?',
      'dlg.del.text': 'Das Löschen ist endgültig. Zurück holen lässt es sich nur noch aus einem Snapshot.',
      'dlg.del.ok': 'Löschen', cancel: 'Abbrechen',
      'act.open': 'Öffnen', 'act.preview': 'Vorschau', 'act.download': 'Herunterladen', 'act.downloadN': '{n} herunterladen',
      'act.rename': 'Umbenennen', 'act.move': 'Verschieben', 'act.delete': 'Löschen',

      hint: 'Mockup — beliebiges Passwort, »wrong« zeigt den Fehler',
      'login.tagline': 'Deine Dateien vom NAS, im Browser.',
      'login.title': 'Anmelden', 'login.sub': 'mit deinem Konto auf {server}',
      'login.user': 'Benutzername', 'login.pass': 'Passwort', 'login.passPh': 'Samba-Passwort', 'login.show': 'Passwort zeigen',
      'login.submit': 'Anmelden',
      'login.trust': 'ferry speichert dein Passwort nicht. Der NAS prüft es bei der Anmeldung; nach 30 Minuten ohne Aktivität bist du abgemeldet.',
      'theme.light': 'Hell', 'theme.dark': 'Dunkel', 'theme.toggle': 'Hell oder dunkel',
      'lang.other': 'English', 'lang.label': 'Sprache',
      logout: 'Abmelden', menu: 'Menü', back: 'Zurück', close: 'Schließen',
      'hello.morning': 'Guten Morgen, {name}', 'hello.day': 'Hallo, {name}', 'hello.evening': 'Guten Abend, {name}',
      'home.sub': 'Was möchtest du öffnen?',
      matching: '{n} passend',
      'search.ph': 'In diesem Ordner suchen',
      'view.grid': 'Kacheln', 'view.list': 'Liste',
      newFolder: 'Neuer Ordner', upload: 'Hochladen',
      selected: '{n} ausgewählt', clearSel: 'Auswahl aufheben',
      'sec.folders': 'Ordner', 'sec.files': 'Dateien',
      'empty.none': 'Nichts gefunden', 'empty.noneText': 'Kein Name in diesem Ordner enthält »{q}«.',
      'empty.empty': 'Dieser Ordner ist leer', 'empty.emptyText': 'Zieh Dateien hierher oder nutze »Hochladen«.',
      'col.name': 'Name', 'col.mod': 'Geändert', 'col.size': 'Größe',
      'tr.running': n1('1 Upload läuft', '{n} Uploads laufen'), 'tr.done': 'Uploads fertig',
      'tr.of': '{done} von {total} · nach »{where}«',
      'pv.count': '{i} von {n}', 'pv.details': 'Details (i)', 'pv.close': 'Schließen (Esc)', 'pv.none': 'Keine Vorschau',
      'info.title': 'Details', 'info.name': 'Name', 'info.kind': 'Art', 'info.size': 'Größe', 'info.mod': 'Geändert', 'info.where': 'Ort',
      'drop.title': 'Loslassen zum Hochladen', 'drop.to': 'nach »{where}«',
    },
  };

  const KEY = 'ferry.lang';
  const supported = Object.keys(dict);
  function detect() {
    const q = location.search.match(/[?&]lang=(\w+)/);
    if (q && supported.includes(q[1])) return q[1];
    try { const s = localStorage.getItem(KEY); if (supported.includes(s)) return s; } catch (e) { /* storage blocked */ }
    const nav = (navigator.languages || [navigator.language || 'en']).map(l => l.slice(0, 2));
    return nav.find(l => supported.includes(l)) || 'en';
  }

  let lang = detect();
  document.documentElement.lang = lang;

  function t(key, p = {}) {
    const s = dict[lang][key] ?? dict.en[key] ?? key;
    return typeof s === 'function' ? s(p) : s.replace(/\{(\w+)\}/g, (m, k) => (k in p ? p[k] : m));
  }

  function set(l, persist = true) {
    if (!supported.includes(l)) return;
    lang = l; document.documentElement.lang = l;
    if (persist) { try { localStorage.setItem(KEY, l); } catch (e) { /* storage blocked */ } }
  }

  window.I18N = { t, set, get lang() { return lang; }, locale: () => (lang === 'de' ? 'de-DE' : 'en-GB'), supported };
})();
