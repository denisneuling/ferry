# Configuration

`ferry serve` is configured through environment variables, and only those.
ferry has no credentials of its own: the passwords it sees are the ones
people type at the login, and those are used for one SMB session setup and
kept nowhere. The one secret among the variables is `FERRY_CACHE_KEY`, and
only when the optional cache is on.

## The SMB server

| variable | default | meaning |
|---|---|---|
| `FERRY_SMB_SERVER` | — (required) | The SMB server, `host` or `host:port`; the port defaults to 445. Every login and every file operation goes there. |
| `FERRY_SERVER_NAME` | the host of `FERRY_SMB_SERVER` | What the login page calls the server: "with your account on …". |
| `FERRY_SHARES` | all the server lists | A comma-separated allow-list of share names, compared without case. Shares outside it are neither listed nor opened — also hidden (`$`) shares asked for by name — and answer `404`. It narrows what ferry offers; what each user may do inside an allowed share is still the server's decision. |

## Listening

| variable | default | meaning |
|---|---|---|
| `FERRY_LISTEN` | `:8080` | The interface and the API (`/api/v1`). |
| `FERRY_OPS_LISTEN` | `:8081` | `/healthz` (the process) and `/readyz` (whether the SMB server's port answers). Keep it inside the cluster. |

## Sessions and logins

| variable | default | meaning |
|---|---|---|
| `FERRY_IDLE_TIMEOUT` | `30m` | A session without a request for this long ends. A session in use — a long download — does not idle out. |
| `FERRY_SESSION_LIFETIME` | `12h` | A session ends this long after its login, whatever its activity. At least `FERRY_IDLE_TIMEOUT`. |
| `FERRY_SMB_CONNECTIONS` | `3` | SMB connections one login sets up, `1` to `4`, each an SMB session of its own with the person's credentials, all during the login — the only moment the password exists. One serves browsing (listing, creating, renaming, deleting, free space); the others serve thumbnails, details, previews, downloads and uploads, so that a folder opens while thumbnails load. `1` puts everything on one connection. The SMB server sees up to sessions × this many connections; mind its own limits, such as Samba's `max smbd processes`. Memory per connection: [operations.md](operations.md#memory). |
| `FERRY_MAX_SESSIONS` | `200` | Sessions at most — logins, each with `FERRY_SMB_CONNECTIONS` SMB connections. A login beyond it is `503` (`too_many_sessions`). |
| `FERRY_LOGIN_TIMEOUT` | `10s` | Connecting to the server and the session setup, together. |
| `FERRY_LOGIN_RATE_ADDRESS` | `10` | Login attempts per minute from one client address, successful or not. |
| `FERRY_LOGIN_RATE_USER` | `5` | Login attempts per minute for one username, compared without case. The server's own lockout policy applies as well. |
| `FERRY_TRUSTED_PROXIES` | none | Comma-separated addresses or CIDRs of the proxies in front of ferry. From those, ferry takes the client's address from `X-Forwarded-For` (the nearest hop that is not a trusted proxy); from anyone else it ignores the header. Without it, every client behind a gateway shares the gateway's address — and its login rate. |
| `FERRY_COOKIE_SECURE` | `auto` | Whether the session cookie is `Secure` (and named `__Host-ferry_session`): `auto` when the request came over TLS or with `X-Forwarded-Proto: https`, or `true`, or `false`. |

## Limits

| variable | default | meaning |
|---|---|---|
| `FERRY_MAX_UPLOAD` | `0` (none) | The largest upload, in bytes, with an optional binary suffix: `512Mi`, `4Gi`. Larger is `413`, and nothing is left on the share. |
| `FERRY_MAX_LISTING` | `200000` | Entries in one folder listing. ferry writes a listing out as it reads it, a few hundred entries at a time, so its own memory does not grow with the folder; the bound protects the browser, which holds the whole folder. Beyond it, the folder does not open, and the interface says how many entries this ferry shows. |
| `FERRY_MAX_DELETE` | `10000` | Entries one delete removes, counted before anything is deleted: a folder with more in it is refused whole (`413`) rather than deleted in part, and the interface holds a selection to the same number. |
| `FERRY_MAX_ARCHIVE` | `10000` | Entries one ZIP download holds, folders and everything in them counted, before anything is downloaded: beyond it the ZIP is refused whole (`413`), and the interface holds a selection to the same number. The bytes are not bounded: the archive is written as it is read. |
| `FERRY_THUMBNAIL_MAX_BYTES` | `64Mi` | The largest image ferry reads to make a thumbnail — of a recording, the largest cover; larger is `413` and the interface shows an icon. |
| `FERRY_THUMBNAIL_MAX_PIXELS` | `50000000` | The most pixels an image may have to be decoded — checked from its header before decoding, so a small file claiming a huge image cannot exhaust memory. |
| `FERRY_THUMBNAIL_MEMORY` | `320Mi` | The memory all thumbnails in the making may take together, by ferry's estimate from each image's header — which is above every measured peak. Thumbnails wait until theirs fit; one that alone exceeds it is `413`. The largest single use of memory in ferry: see [operations.md](operations.md#memory) for the measurements and for sizing the container's limit. |
| `FERRY_THUMBNAIL_CONCURRENCY` | `2` | Thumbnails decoded at once, whatever the memory allows: the CPU bound. A 12-megapixel photo takes about a quarter of a second of one core. |
| `FERRY_CACHE_DIR` | — | A directory for the cache of thumbnails and details; empty, ferry writes nothing anywhere. Entries are encrypted and named by a keyed hash, and given only to a session that has opened the file for reading ([operations.md](operations.md#cache)). |
| `FERRY_CACHE_MAX_BYTES` | `1Gi` | The most the cache's entries may take; the least recently used are removed beyond it. |
| `FERRY_CACHE_KEY` | — | Required with `FERRY_CACHE_DIR`: at least 32 bytes that name and encrypt the entries. A secret: with the volume and the key, an entry can be read by whoever knows the file's path and state. A new key makes every entry unreadable, which costs only their making again. |
| `FERRY_REQUEST_TIMEOUT` | `60s` | Every API request but uploads and downloads; and each single SMB call, of transfers too. A call the SMB server does not answer within it ends the session: a connection cancelled in the middle of a request cannot be trusted (docs/spike.md). |
| `FERRY_STALL_TIMEOUT` | `60s` | An upload or download ends when no bytes move for this long, in either direction — whether the client or the SMB server stopped. Its total duration is not bounded. |

## Telemetry

| variable | default | meaning |
|---|---|---|
| `OTEL_EXPORTER_OTLP_ENDPOINT` | none | Where to send traces, over OTLP/HTTP; without it (and without `OTEL_EXPORTER_OTLP_TRACES_ENDPOINT`) ferry sends none. The other standard `OTEL_*` variables apply too. [operations.md](operations.md#traces) |
| `GOMEMLIMIT` | none | The Go runtime's soft memory limit; set it a little below the container's memory limit ([operations.md](operations.md#sizing-the-limit)). The chart sets 450MiB. |

The metrics need no setting: they are on `FERRY_OPS_LISTEN`, path `/metrics`.

## Logging

| variable | default | meaning |
|---|---|---|
| `FERRY_LOG_LEVEL` | `info` | `debug`, `info`, `warn` or `error`. `debug` adds a line per request. |
| `FERRY_LOG_FORMAT` | `json` | `json` or `text`. |

What the log never holds: passwords, usernames, file and folder names,
paths and query strings. A login is logged with its outcome (`ok`, `auth`,
`guest`, `unreachable`, `rate_limited`, …), the NT status and the client's
address; a request with its route pattern (`/api/v1/shares/{share}/content`),
status, size and duration.

go-smb2, the SMB library, writes debug output to stderr when the variable
`DEBUG` is set. Leave it unset in a deployment.
