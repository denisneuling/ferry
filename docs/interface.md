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

## Browsing

- **Shares** come as the server lists them. One the person may not open
  says so when opened; docs/samba.md has the setting that hides such shares.
- **Folders** show as tiles — folders first, then files with thumbnails for
  JPEG, PNG, GIF, WebP and BMP — or as a sortable list.
- **Search this folder** filters the names of the folder open; it searches
  nothing else.

## Selecting and acting

Click an entry to open it; with Shift, Ctrl or ⌘ — or once a selection is
running — a click selects. On touch, a long press starts a selection. The
dark bar above the folder acts on the selection: download, rename (one
entry), move, delete. A right click opens the same as a menu.

| key | does |
|---|---|
| ← → ↑ ↓, `j` `k` | move the selection |
| Enter | open the selected entry |
| Backspace, Alt+↑ | up one folder |
| Ctrl+A, ⌘A | select all |
| Delete | delete the selection, after asking |
| F2 | rename the selected entry |
| `/` | search this folder |
| Esc | clear the selection or the search, close the preview or a dialog |

In the preview: ← → for the previous and next file, `i` for the details,
Esc to close.

## Uploading

**Upload**, or drop files anywhere on the folder. Uploads run two at a time
with their progress in the panel at the bottom right. A file of the same name
is replaced only after asking. An upload that fails or is cancelled leaves
nothing on the share: ferry writes to a temporary name and renames it only
once the file is complete.

## Previews

Images, video, audio and PDF are shown by the browser itself, straight from
the server, sandboxed: nothing in a file runs in ferry's page. Text files —
HTML and SVG among them — are shown as text, the first 512 KB. Anything else
offers a download. HEIC photos and videos get an icon rather than a
thumbnail.

## Downloading

A file downloads through the browser's own download, with Range, so a large
download can resume. Several files download one after the other. A folder as
one download is not there yet.

## Development

    make dev-nas run-nas   # the mock NAS, and ferry against it on :8095
    make ui-start          # the interface with live reload on :4200, against it
    make ui-lint ui-i18n ui-test ui-build

Every text is marked for translation (`i18n`, `` $localize ``) with an id;
`make ui-i18n` extracts them to `ui/src/locale/messages.json`, and their
German — in the du-form — goes into `messages.de.json`. A text without its
German fails the build. File names and share comments come from the server
and are never translated.
