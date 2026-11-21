# ferry — Concept

**Status:** Concept v0.1 · **Date:** 2026-11-21
**Project:** `ferry` (mdvc/ferry)
**Primary implementation:** Go (service), Angular (interface)
**First deployment:** a household NAS, replacing Filestash

ferry shows SMB shares in a browser. You log in with your Samba username and
password. ferry opens an SMB session as you and ferries files between the
share and the browser. You see exactly what Samba lets you see. ferry has no
users, no database, no OIDC and no service account: it is one more SMB client
that happens to draw a web page.

---

## 1. Purpose, scope and invariants

### Why

A household NAS shares its disks through Samba. Explorer, Finder and the
phones' file apps use SMB directly. A browser cannot. Filestash fills that gap today with
logins checked by Samba itself, but its interface is unpleasant and the process
is heavy for what it does.

The alternatives looked at (November 2026: Nextcloud, Sambee, File Browser,
SFTPGo, Copyparty, Seafile, Cloudreve, Pydio, smb-web-client, rclone with a
WebDAV page) all break at least one of these rules:

- They keep their own users, OIDC, or stored SMB credentials.
- They read the disks directly instead of going through Samba.
- They are heavy, or they are ugly.

### What it provides

- Login with the Samba username and password, checked by Samba when the SMB
  session is set up.
- The shares the user may see, and inside them:
  - browse;
  - download, streamed, with Range, so videos seek and downloads resume;
  - upload, streamed, with progress;
  - create folders, rename, move and delete.
- Previews for images, PDF, video, audio and text, and thumbnails in a grid
  view.
- A calm, modern interface that works on a phone, with light and dark themes.

### Non-goals

- User, group, password or share administration: that is Samba's job.
- Sharing links, guest access and public URLs. They need state and their own
  authorisation.
- Sync clients, WebDAV and apps. SMB already serves those clients.
- Indexed search: it needs an index, which means state and background reads of
  every share.
- Editing documents in the browser, beyond replacing a file by uploading it.
- Backends other than SMB2/3: no S3, SFTP or local filesystem.
- TLS termination. The gateway in front does it, or the deployment decides
  deliberately against it (§7).

### Fixed stack

| concern | choice |
|---|---|
| service | Go 1.27, one binary with the interface embedded |
| SMB | [cloudsoda/go-smb2](https://github.com/cloudsoda/go-smb2): the maintained fork of hirochachacha/go-smb2 that rclone uses; pure Go, no cgo, no libsmbclient |
| contract | OpenAPI 3.0.3 first, with oapi-codegen for server and types (soulforge's setup). The interface's TypeScript client is generated from the same spec |
| state | none persisted. Sessions in memory only |
| authentication | the SMB session setup (NTLMSSP) against the configured server. No tokens, no OIDC |
| interface | Angular with its own layout and theme (“Hafen”, §5), not the metapatrol shell; embedded in the binary. English with German as the first translation (`@angular/localize`) |
| operations | Kubernetes, Helm (OCI), Flux; Prometheus metrics, OTEL traces; Forgejo Actions CI and semantic-release, as in soulforge |

### Principles

- **Samba decides.** Samba validates the password and enforces every access
  decision. ferry adds no rights of its own and removes none. A file written
  through ferry is owned and permissioned exactly as one copied through
  Explorer.
- **Nothing kept.**
  - Credentials live only for the duration of the login request.
  - The session, meaning the live SMB connection, lives in memory and nowhere
    else.
  - A restart logs everyone out, and nothing is lost by it.
- **Servers are generic.** A household NAS is the first deployment, but ferry contains
  nothing specific to it. Any SMB2/3 server will do.
- **Small.**
  - A distroless image with no volumes, running as non-root on a read-only
    root filesystem.
  - Idle memory in tens of MB.
- **Fail closed and say so.** An SMB error becomes a precise HTTP problem
  response (403, 404, 409, 502). It never turns into an empty listing.

---

## 2. Architecture

```
Browser ──HTTP──▶ gateway ──▶ ferry (one Go process)
                               ├── embedded Angular interface (static)
                               ├── /api/v1  JSON and streams
                               └── session table (memory)
                                     │
                                     └── SMB2/3 as the user ──▶ Samba
```

- **One process.** No workers, queues or sidecars, and no dependency to run
  next to it.
- **Session table.** It maps a random session ID to one live SMB session: the
  connection, the authenticated session, the shares mounted so far and the
  time of last use.
- **One SMB connection per browser session.** This keeps identities strictly
  apart and makes logout final. Samba handles a few dozen sessions without
  effort.
- **Share enumeration** goes through srvsvc NetShareEnum on the user's own
  session. Hidden `$` shares are left out. A deployment can narrow the list
  further (`FERRY_SHARES`).

---

## 3. Login and sessions

1. `POST /api/v1/login` with the username and password.
2. ferry dials the configured server and runs the session setup with exactly
   these credentials.
   - **Failure:** 401 with a generic message. The SMB status is logged and
     counted, without the username or password.
   - **Success:** ferry creates a session ID (256 random bits) and keeps the SMB
     session under it. It sets the session cookie: `HttpOnly`,
     `SameSite=Strict`, `Path=/`, and `Secure` when served over HTTPS.
3. The password goes out of scope. It is never written anywhere and never sent
   again. If the SMB connection drops, the session is over and the user logs in
   again.
4. The session ends after an idle timeout (default 30 min), after an absolute
   lifetime (default 12 h), on `POST /api/v1/logout`, or when the process
   stops. Ending a session logs off SMB and closes the connection.
5. Logins are rate limited per client address and per username, in memory.
   Samba's own lockout policy applies as well, if one is configured.

---

## 4. The file API

Illustrative. The contract is `api/openapi/ferry-v1.yaml`.

| method | path | purpose |
|---|---|---|
| POST | `/api/v1/login`, `/api/v1/logout` | open and close a session |
| GET | `/api/v1/me` | username, server, session expiry |
| GET | `/api/v1/shares` | shares visible to the user |
| GET | `/api/v1/shares/{share}/entries?path=` | directory entries: name, type, size, mtime |
| GET | `/api/v1/shares/{share}/content?path=` | download, streamed, with Range |
| PUT | `/api/v1/shares/{share}/content?path=` | upload, streamed to a temporary name, renamed on completion |
| POST | `/api/v1/shares/{share}/folders?path=` | create a folder |
| POST | `/api/v1/shares/{share}/move` | rename or move within a share, `{from, to}` |
| DELETE | `/api/v1/shares/{share}/entries?path=` | delete a file or an empty folder; recursive only when asked |
| GET | `/api/v1/shares/{share}/thumbnail?path=` | image thumbnail, made on the fly, never cached on disk |

- **Paths** are cleaned and resolved within the share. `..`, absolute paths and
  NUL bytes are refused before anything reaches SMB.
- **Mutating requests** need the session cookie and the header
  `X-Ferry-Request: 1`. Together with `SameSite=Strict` that is the CSRF
  protection.
- **Errors** are problem responses (RFC 9457) that map the SMB status:
  - access denied → 403
  - not found → 404
  - sharing violation or exists → 409
  - server unreachable → 502

---

## 5. The interface

Angular with its own layout, not the metapatrol shell. Unlike websearch's
console, the interface *is* the product, so it is designed for people who never
think about SMB. The repository keeps the metapatrol lockup under `.metapatrol/`
for the README, as the other projects do.

The look is **direction A, “Hafen”**, from the mockups in
[docs/mockup/](docs/mockup/index.html) (2026-11-21):

- Warm paper tones with a deep harbour teal and an orange accent.
- Serif headings (Fraunces) over a plain sans (DM Sans), both served by ferry
  itself.
- A sidebar with the shares, large thumbnails, a dark selection bar, and a
  lightbox with a filmstrip.
- Light and dark themes.
- The login is one centred card on the harbour.

The mockups share a small controller (`docs/mockup/shared/`), so behaviour can
be tried without a backend.

- **Screens:**
  - Login.
  - Shares.
  - Folder: list or grid, breadcrumbs, sorting, multi-select.
  - Preview overlay, using the browser's own viewers for PDF, video and audio.
  - Transfers panel for uploads and downloads with progress.
- **Interaction:**
  - Drag and drop onto a folder.
  - Keyboard navigation.
  - A context menu, or a long press on touch.
- **Languages:**
  - English source and German as the first translation, through
    `@angular/localize`, with German in the du-form as in iam and websearch.
  - The language comes from the browser. The viewer can switch on the login page
    and in the sidebar, and the choice is kept in the browser only, because
    ferry has nowhere else to keep it.
  - Dates, times and sizes follow the language.
  - Share comments and file names come from the server and are not translated.

---

## 6. Observability

- `/healthz` reports the process. `/readyz` reports whether the SMB port of the
  configured server is reachable.
- **Prometheus metrics:**
  - active sessions;
  - logins by outcome;
  - bytes up and down;
  - SMB errors by status;
  - request latency per route.
  
  No usernames or paths as labels.
- OTEL traces per request. SMB calls are spans, and their attributes carry
  neither credentials nor file names.
- `charts/ferry-grafana-dashboard` as in the other projects.

---

## 7. Security

- **Credentials** exist only in the login handler and the SMB session setup.
  They never reach logs, traces, metrics, error messages or crash output, and a
  test asserts this on the log and trace output.
- **Plain HTTP is the deployment's decision.** Over HTTP, the password and the
  session cookie cross the network readable, as with any plain-HTTP web app.
  That can be acceptable on a home LAN; elsewhere TLS belongs at the
  gateway.
- **Headers:**
  - A strict Content-Security-Policy: no inline script, no third-party origins.
  - `X-Content-Type-Options: nosniff`.
  - `Referrer-Policy: same-origin`.
- **User content cannot run in ferry's origin.**
  - Downloads carry `Content-Disposition: attachment`.
  - Previews are served with `Content-Security-Policy: sandbox` and a
    non-active content type for HTML and SVG.
- **Process:**
  - non-root;
  - read-only root filesystem;
  - all capabilities dropped;
  - no service account token;
  - no volumes besides an `emptyDir` for `/tmp`, if any.

---

## 8. Repository and delivery

As in the other projects (websearch, iam, token-proxy):

```
api/openapi/ferry-v1.yaml   the contract
cmd/ferry/                  the binary
internal/smb/               session setup, share enumeration, file operations
internal/session/           session table, timeouts, rate limits
internal/api/               REST handlers, problem responses, streaming
ui/                         the interface
charts/ferry/               Helm chart
charts/ferry-grafana-dashboard/
```

- **Tooling:**
  - Forgejo Actions CI, with integration tests against a Samba container;
  - semantic-release with Conventional Commits;
  - Renovate;
  - `metapatrol.json`;
  - Apache-2.0.
- **README:** the same structure as the other projects (lockup, *Why*, *How it
  works*, scope).
- **Configuration**, through environment variables, with no secrets:
  - `FERRY_SMB_SERVER` (host[:port])
  - `FERRY_SHARES` (optional allow-list)
  - `FERRY_IDLE_TIMEOUT`, `FERRY_SESSION_LIFETIME`
  - `FERRY_MAX_UPLOAD`
  - `FERRY_LISTEN`

The first deployment installs the chart through Flux and runs it next to
Filestash for a week before Filestash goes.

---

## 9. Milestones

0. **Mockups.** Done: the clickable static mockup in `docs/mockup/` (login,
   shares, folder, preview, transfers, phone, English and German). No backend.
1. **Spike.** A Go program against a real Samba server: log in, list shares, list a folder,
   stream a file. It proves three things: go-smb2's NTLMSSP against a current Samba,
   NetShareEnum, and the throughput over 1 GbE.
2. **Core.** The OpenAPI spec, the file API, sessions, limits, problem
   responses, tests against a Samba container.
3. **Interface.** The screens of §5 in the chosen look, English and German.
4. **Operations.** Metrics, traces, the Helm chart and the dashboard. Rollout on
   the first deployment in parallel to Filestash.
5. **Later, if wanted:**
   - download a folder as a streamed ZIP;
   - a live, bounded filename search below a folder;
   - previous versions.

---

## 10. Decisions

| decision | why |
|---|---|
| Own small Go service, not an existing file manager | none of the ones looked at keeps Samba as the only authority without own users, stored credentials or pool access, and is also pleasant to use |
| SMB session setup as the login, no OIDC | the accounts already exist in Samba; a second identity system costs resources and drifts |
| One SMB connection per browser session | keeps identities apart and makes logout real; simpler than pooling |
| Nothing persisted | no secret to protect at rest, nothing to back up, a restart is harmless |
| go-smb2 (cloudsoda fork), not libsmbclient | pure Go, distroless image, the same code rclone runs against many servers |
| Session cookie, not Basic auth or tokens in the browser | the password is sent once; JavaScript never holds it; logout works |
| No sharing links | they would need state and a second authorisation model |
| Own interface components, no `@metapatrol/ng` | the look is ferry's own (“Hafen”), so shared primitives would be restyled everywhere; and the repository is public, so the build must not need a private registry or a token: `npm ci` is enough. Angular CDK supplies behaviour only (focus trap, a11y, overlay positioning), without its styles |

---

## 11. Hazards to test first

- go-smb2's session setup fails against our Samba (NTLM settings,
  `server min protocol`), or it succeeds as a guest when the password is wrong
  (`map to guest`). A wrong password must never yield a session.
- NetShareEnum shows shares the user cannot open, or ferry hides shares Samba
  would allow.
- A failed or interrupted upload leaves a half-written file under the real
  name.
- A dropped SMB connection is retried silently in a way that needs the
  password again. It must end the session instead.
- Two sessions of different users get crossed under concurrency.
- An uploaded HTML or SVG file runs script when previewed.
- A credential appears in a log line through a wrapped error from the SMB
  library.

What the spike found for each of them, and how ferry holds it:
[docs/spike.md](docs/spike.md).

---

## 12. Open questions

- **Previous versions:** show ZFS snapshots through SMB's `@GMT` tokens? This
  needs `vfs_shadow_copy2` configured in Samba; ferry would only use it where a
  server offers it.
