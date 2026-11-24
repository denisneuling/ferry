# The interface

ferry's interface is its own layout, "Hafen" — ported from the approved
mockup in [mockup/](mockup/index.html) — built with Angular and compiled into
the binary. It speaks English and German, follows the browser's language and
colour scheme, and keeps the viewer's choices (language, theme, tiles or
list) in the browser only.

## Signing in

The login page names the server (`FERRY_SERVER_NAME`) and the idle timeout.
The password goes to ferry once, in the login request, and on to the SMB
server; the form forgets it right after. When the server ends a session —
the idle timeout, the lifetime, a lost connection — the interface returns to
the login and says why.

Signing out clears what the browser cached for ferry (`Clear-Site-Data`),
thumbnails and details included, so that the next person at a shared
browser cannot page back to them. Two cases where it does not:

- **ferry served over plain HTTP.** Browsers honour `Clear-Site-Data` in a
  secure context only — HTTPS, or localhost. Without HTTPS, thumbnails and
  details stay in the browser's cache after signing out, for as long as the
  browser keeps them (up to a year), readable by whoever uses that browser
  profile next. A file's own content is asked for anew each time
  (`no-cache`): showing it again needs a session.
- **A session that times out.** No answer goes to the browser that could
  carry the header.

On a computer several people share, give each their own browser profile or
account, or clear the browsing data after use — or serve ferry over HTTPS
and sign out.

## Browsing

- **Shares** come as the server lists them. One the person may not open
  says so when opened; docs/samba.md has the setting that hides such shares.
- **Folders** show as tiles — folders first, then files with thumbnails for
  JPEG, PNG, GIF, WebP and BMP, the cover a recording carries (MP3,
  FLAC, M4A, Ogg, Opus), and a PDF's first page and HEIC photos where the
  deployment makes those previews — or as a sortable list. The browser keeps a
  thumbnail, and a file's details, until the file changes: their URLs carry
  the version the listing gave (size and time of the last write), so coming
  back to a folder asks the server for its listing and nothing else.
- **Going back** to a folder seen a moment ago shows it at once, as it
  was, while ferry lists it anew; the new listing replaces it when
  complete. The interface keeps the last 20 folders (100000 entries in all,
  none above 20000) in the tab's memory only, never in the browser's
  storage, and forgets them when the session ends.
- **Search this folder** filters the names of the folder open. With
  **Subfolders** switched on, what is typed (two characters or more) is
  looked for below the folder as well: the server lists the folders one by
  one, with the person's own session, and the hits appear as they are found,
  each with the folder it is in; a click opens that folder with the hit
  selected. Case does not matter, nor how an umlaut was written. Folders the
  NAS does not let the person open are skipped and counted. It stops at 1000
  hits, 20000 folders, 32 levels or a minute, and says so; nothing is
  indexed or kept. In a folder shown as it was, the search looks through
  that snapshot. One search runs per session: typing on ends the one
  before. It uses one of the background connections, a folder at a time,
  so thumbnails and downloads still load in between.
- **Free space** shows under a folder's title and on each share's card —
  "3.2 TB free of 18 TB" with a thin bar — exactly as the SMB server reports
  it to the person (a quota included, when the server applies one). ferry
  works nothing out itself; when the server does not answer, the line is
  not there and the folder opens all the same.
- **Large folders** open at once: the entries arrive as the server lists
  them, and the first ones show while the rest come in. The page renders a
  few hundred tiles or rows and more as the end comes into view; sorting,
  the search and "select all" work on the whole folder. A folder with more
  entries than the deployment shows (`FERRY_MAX_LISTING`, 200000 by default)
  says how many that is.

## Folder sizes

**Folder size** under a folder's title, and in a folder's menu, adds up
what it holds: the server lists the folders below one by one with your
session and ferry counts the files and their sizes, the count showing in a
card as it grows. Only on request, one at a time, and stopped with the
card's ✕; it ends after 20000 folders, 200000 entries, 32 levels or a
minute, then saying it is at least that much. Folders the server does not
open to you are left out and said to be. A snapshot folder the server
shows inside a share (`.snapshot`, `.zfs` with its snapshots visible) is
a folder like any other and counted along; one it hides is not.

## Renaming several

**Rename** with several entries selected renames them by a pattern —
`{name}` the old name without its extension, `{ext}` the extension, `{n}`
or `{n:3}` a counter from where it should start, `{date}` the day it was
last modified, `{taken}` the day a photo was taken — and shows every new
name before anything is renamed. A name the server could not take, two
entries meant to get the same name, or a name another entry of the folder
has (case aside, as SMB compares names), and nothing is renamed at all.
Each entry is renamed by its own move, as one rename is; names swapped
among them go through a temporary name `.ferry-tmp-…` first. There is no
transaction over many renames: when the server refuses one part-way, the
rest is not tried, and the dialog lists which entries got their new
names and any left under a temporary name, to rename back.

## Favourites and recently used

The star next to a folder's title, in an entry's menu and in the preview
keeps a folder or a file among the favourites, listed in the sidebar. The
start page lists what was used last: the folders opened and the files
looked at for a few seconds, twenty at most, each to remove or all to
clear. Both live in this browser only, per server and user — ferry keeps
nothing of them — and are not checked against the share: something moved
or deleted since shows as missing when it is opened. A previous version is
never remembered.

## Selecting and acting

Click an entry to open it; with Shift, Ctrl or ⌘ — or once a selection is
running — a click selects. On touch, a long press starts a selection. The
dark bar above the folder acts on the selection: download, rename (one
entry), move, delete. A right click opens the same as a menu, with **Copy
to…** and **Previous versions** besides. A copy is made by the server's
session through ferry, file by file, and shows how far it got; one that
stops part-way keeps what it copied and says how much. Copied into the
same folder, the copy is named "… (copy)".

| key | does |
|---|---|
| ← → ↑ ↓, `j` `k` | move the selection |
| Enter | open the selected entry |
| Backspace, Alt+↑ | up one folder |
| Ctrl+A, ⌘A | select all |
| Delete | delete the selection, after asking |
| F2 | rename the selected entry, or several by a pattern |
| `/` | search this folder |
| Esc | clear the selection or the search, close the preview or a dialog |

In the preview: ← → for the previous and next file, `i` for the details,
`+` `-` `0` to zoom a picture in, out and back, `r` to turn it, Space for
the slideshow, `f` for full screen, Esc to close. On a touch screen a
swipe steps to the next or the previous file.

A folder of pictures plays as a slideshow: the next picture once the one
shown has loaded and its time — 3, 5 or 10 seconds — is up, other files
passed over; any step of one's own ends it. A picture turned is turned
only on the screen; nothing is written. **By date taken** sorts a folder's
pictures by the time the camera wrote into them — read for at most 500 of
them, four at a time, the order settling as they come — and the rest by
their modification. Where a photo was taken is in its details, behind
**Show**: it is the one detail that says where someone was.

A picture the browser shows itself (JPEG, PNG, GIF, WebP, AVIF, BMP) zooms
with the wheel or a pinch, at the pointer; a double click goes to its own
pixels — the original's — and back, and a drag moves the zoomed picture,
never past its edges. The 640-pixel pictures the server makes of HEIC,
TIFF and raw photos are not zoomed: their own pixels are not the
original's.

## Details

![The lightbox with a photo and its details: name, size and folder, and from the file its dimensions, time taken, camera, lens, exposure and GPS position.](images/lightbox-en.webp)

`i` or the ⓘ button in the preview shows a file's details: its name, type,
size, modification time and folder, and below them what the file says
about itself, read by ferry from its own bytes:

- **photos** — dimensions, when they were taken (by the camera's clock),
  camera and lens, aperture, exposure time, ISO and focal length, an
  orientation other than upright, and a GPS position as coordinates only —
  no map, nothing sent anywhere. JPEG, PNG, WebP, TIFF and HEIC (its EXIF
  read from the container, without decoding the picture);
- **PDFs** — pages, title, author and the program that made them;
- **recordings** — duration, codec, resolution, bitrate, sample rate,
  channels, title, artist and album, from MP4, MOV, M4A, MP3, FLAC and WAV,
  without ffmpeg;
- **texts** — encoding, lines and line endings.

Only a bounded part of a file is read (4 MiB at most), and nothing is
stored or written to a log. What the file says is shown as text: a camera
name or a title is never markup.

## Previous versions

Where the server keeps snapshots ([samba.md](samba.md), *Previous
versions*), **Previous versions** in a file's or folder's menu, the clock
beside a folder's title and the button in a file's details list them,
newest first, with the time and the size — not who changed anything. A
file's version opens in a new tab or downloads; **Restore as copy** puts
it beside the original, named after its time — "letter (version 21.11.2026
04.00).txt" — and never replaces anything; a folder's opens **as it
was**: the folder view with a banner saying when, where a file deleted
since can be found, opened and downloaded. Nothing can be changed there —
no upload, rename, move or delete — and **Back to now** leaves it. A
server or share without snapshots simply has none.

## Uploading

**Upload**, or drop files anywhere on the folder. Uploads run two at a time
with their progress in the panel at the bottom right. A file of the same name
is replaced only after asking. An upload that fails or is cancelled leaves
nothing on the share: ferry writes to a temporary name and renames it only
once the file is complete.

A file of 16 MB or more is uploaded in pieces of 8 MB. A broken
connection costs a piece: it is tried again, waiting a little longer each
time, from where the server says the upload has come to. After a reload,
or in another tab, the same file uploaded to the same place again goes on
where it stopped — the browser remembers which upload it was, by the
target and the file's name, size and time. Until it is complete the file
is a hidden `.ferry-partial-…` beside the target, which the folder view
leaves out; cancelling removes it. One begun more than a day ago and never
finished is not removed by itself — it may be someone else's, going on
tomorrow — but the folder says so, with its size, and offers to remove it.

**Folder** uploads a folder with everything in it, as does dropping a
folder: its folders are made first — one that is there already is used as
it is — then its files are uploaded into them. One upload takes as many
files as a ZIP may hold (`FERRY_MAX_ARCHIVE`), 32 levels deep.

Entries dragged within the page are moved where they are dropped: onto a
folder, or onto a part of the path above the folder's title. Copies made
by the server show in the same panel, with their progress.

## Previews

Images, video, audio and PDF are shown by the browser itself, straight from
the server, sandboxed: nothing in a file runs in ferry's page. Text files —
HTML and SVG among them — are shown as text, the first 512 KB. Anything else
offers a download. A recording shows its cover above the player when it
carries one. A HEIC photo, which browsers other than Safari cannot show,
is shown as the server's 640-pixel thumbnail, with the 320-pixel one in its
place until it has come. So are TIFF images and a camera's raw photos
(CR2, NEF, ARW, DNG, PEF, ORF, RW2, RAF), from the JPEG preview each
camera writes into the raw file, turned as the raw file says; a raw file
without one gets an icon. Documents and books show the picture they carry
for it: OpenDocument files (`.odt`, `.ods`, `.odp`) nearly always, Office
files (`.docx`, `.xlsx`, `.pptx`) when they were saved with one, an EPUB
its cover; the rest an icon. A Word or OpenDocument text shows its plain
text in the preview — the paragraphs, no formatting, nothing of the
document that could run. A CSV or TSV file shows as a table of its first
1000 rows — commas, the semicolons a German spreadsheet writes, or tabs —
its cells as text. An Excel workbook (`.xlsx`) or an OpenDocument
spreadsheet (`.ods`) shows its first sheet the same way, read by the server:
the first 1000 rows and 100 columns, each cell as it was when the file was
last saved — a formula's stored result, never the formula worked out again.
Excel's dates and times are written as 2026-11-24 or 14:30:00 where the
cell is formatted as one; an OpenDocument cell shows the text it displays.
A workbook of several sheets shows them as tabs above the table, each read
when it is chosen.
A ZIP archive shows what it holds: each file
with its size, to open in a new tab or to download on its own, unpacked
as it is sent — nothing is unpacked onto the share. Entries that are
encrypted, or packed otherwise than ZIP's usual way, are listed but not
unpacked. An MP4 or QuickTime video shows the cover it carries, as music
does — something a film or a series often has and a camera's clip
seldom; the rest get an icon, as do recordings without a cover. Frames
are not taken from videos: that needs a video decoder, which ferry does
not carry.

A video plays with the subtitles that lie next to it: `Film.srt` or
`Film.vtt`, and `Film.de.srt`, `Film.en.forced.vtt` and the like, the
language taken from the name; the player's menu chooses among them. SubRip
files are turned into WebVTT in the browser, read as UTF-8 or else as
Windows-1252; a subtitle file over 2 MB is left out. Their text goes only
to the browser's own subtitle display, never into the page.

While an image is shown, the next and the previous one load ahead, so that
stepping to them shows them at once; videos and PDFs do not, and nothing
loads ahead when the browser is set to save data.

## Editing text

A text file up to 1 MiB — notes, a recipe, a configuration — has
**Edit** in its preview: the whole file in a plain text area, saved with
**Save** or Ctrl+S (⌘S). Saving replaces the file only while it is still
the version that was opened; when someone saved it in the meantime, ferry
says so and asks before writing over it. Line endings and a byte-order
mark stay as the file had them. A file that is not UTF-8 is not edited
here — saving it could change it — and neither is a previous version. A
version is told by the file's size and modification time, as the server
reports it: on a file system that keeps times coarsely (FAT, two seconds),
two saves of the same length within that window look alike; ZFS, ext4 and
NTFS keep them finely enough.

**Text file** next to **New folder** makes an empty text file, `Notes.txt`
unless named otherwise, and opens it in the editor; a file of that name is
never replaced.

Markdown (`.md`) is shown laid out — headings, lists and task lists,
quotes, tables, code, links — with its source a click away. It is read by
ferry's own small reader into a fixed set of elements and text: HTML in it
shows as the text it is, pictures are not loaded but described, and a
link is a link only to an `http` or `https` address, opened in a new tab
without a referrer. Source code (JavaScript, TypeScript, Go, Python, Java,
C, Rust, shell, SQL, JSON, YAML, HTML, CSS and more) is coloured:
comments, strings, numbers and keywords.

## Playing music

A folder with recordings has **Play**: its recordings play one after the
other, in the folder's order, from the first or — **Play from here** in a
recording's menu — from that one. The player stays at the bottom while you
browse on: the cover when the recording carries one, the time, previous,
next, shuffle and stop. The system's media controls — a phone's lock
screen, a keyboard's media keys — work too. The browser streams each
recording from ferry as it would download it; nothing is kept, and logging
out stops it.

## Downloading

A file downloads through the browser's own download, with Range, so a large
download can resume. Several entries, or a folder, download as one ZIP,
named after the folder they are in (or the one entry): ferry lists what goes
in first — so that what the server refuses, or a selection beyond the
deployment's bound (`FERRY_MAX_ARCHIVE`, 10000 entries by default, folders'
contents counted), is said before anything downloads — and then writes the
archive as it reads the files, without a file of its own in between. The
files are stored, not compressed: photos and videos would not shrink, and
the download starts at once. Names keep their accents and emoji, files their
times, and archives beyond 4 GB are ZIP64, which every current unpacker
reads. Should a file fail once the archive has begun — deleted in the
meantime, or the connection gone — the download is broken off and the
browser reports it failed: never an archive that looks complete with a file
missing. A ZIP download cannot resume, and its size is not known before
it ends — each file's checksum is written after the file — so the browser
shows how much has come, not how much is left.

## ZIP files on the share

**Save as ZIP here…** in the menu packs the selection into a ZIP file in
the same folder, as a download would pack it, but written to the share —
to a temporary name, renamed once complete, with its progress in the
transfers panel. A name that exists is not replaced.

**Unpack here…** on a ZIP file unpacks it into a new folder beside it.
Every entry is checked before anything is written, and an archive that
fails a check is refused whole — nothing is renamed or left out silently:
a name that would leave the folder (`..`, absolute), one with a character
an SMB server refuses (`\ : * ? " < > |`, control characters), two names
the server takes for one (equal but for upper and lower case, or written
in two Unicode forms), an entry encrypted or packed otherwise than Store
or Deflate, more than 32 levels, more entries than `FERRY_MAX_ARCHIVE` or
more than the server reports free. Symbolic links in an archive are passed
over. Names are read as the archive wrote them — UTF-8, or the old DOS
code page where it says neither — and a backslash is a folder separator in
an archive made on Windows, as older Windows tools wrote them. Each file is
written as an upload is; what was unpacked before a failure stays, and the
message says so.

Deleting asks first and names how many. A selection larger than the
deployment's delete bound (`FERRY_MAX_DELETE`) is refused before anything is
deleted, as a folder with more in it is.

## Development

    make dev-nas run-nas   # the mock NAS, and ferry against it on :8095
    make ui-start          # the interface with live reload on :4200, against it
    make ui-lint ui-i18n ui-test ui-build

Every text is marked for translation (`i18n`, `` $localize ``) with an id;
`make ui-i18n` extracts them to `ui/src/locale/messages.json`, and their
German — in the du-form — goes into `messages.de.json`. A text without its
German fails the build. File names and share comments come from the server
and are never translated.
