# Configuration

`ferry serve` is configured through environment variables, and only those.
None of them is a secret: ferry has no credentials of its own. The passwords
it sees are the ones people type at the login, and those are used for one SMB
session setup and kept nowhere.

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
| `FERRY_MAX_SESSIONS` | `200` | Sessions at most, each one SMB connection. A login beyond it is `503` (`too_many_sessions`). |
| `FERRY_LOGIN_TIMEOUT` | `10s` | Connecting to the server and the session setup, together. |
| `FERRY_LOGIN_RATE_ADDRESS` | `10` | Login attempts per minute from one client address, successful or not. |
| `FERRY_LOGIN_RATE_USER` | `5` | Login attempts per minute for one username, compared without case. The server's own lockout policy applies as well. |
| `FERRY_TRUSTED_PROXIES` | none | Comma-separated addresses or CIDRs of the proxies in front of ferry. From those, ferry takes the client's address from `X-Forwarded-For` (the nearest hop that is not a trusted proxy); from anyone else it ignores the header. Without it, every client behind a gateway shares the gateway's address — and its login rate. |
| `FERRY_COOKIE_SECURE` | `auto` | Whether the session cookie is `Secure` (and named `__Host-ferry_session`): `auto` when the request came over TLS or with `X-Forwarded-Proto: https`, or `true`, or `false`. |

## Limits

| variable | default | meaning |
|---|---|---|
| `FERRY_MAX_UPLOAD` | `0` (none) | The largest upload, in bytes, with an optional binary suffix: `512Mi`, `4Gi`. Larger is `413`, and nothing is left on the share. |
| `FERRY_MAX_LISTING` | `10000` | Entries in one folder listing, and in one recursive delete. Beyond it, `413`. |
| `FERRY_THUMBNAIL_MAX_BYTES` | `64Mi` | The largest image ferry reads to make a thumbnail; larger is `413` and the interface shows an icon. |
| `FERRY_THUMBNAIL_MAX_PIXELS` | `50000000` | The most pixels an image may have to be decoded — checked from its header before decoding, so a small file claiming a huge image cannot exhaust memory. |
| `FERRY_THUMBNAIL_MEMORY` | `320Mi` | The memory all thumbnails in the making may take together, by ferry's estimate from each image's header — which is above every measured peak. Thumbnails wait until theirs fit; one that alone exceeds it is `413`. The largest single use of memory in ferry: see [operations.md](operations.md#memory) for the measurements and for sizing the container's limit. |
| `FERRY_THUMBNAIL_CONCURRENCY` | `2` | Thumbnails decoded at once, whatever the memory allows: the CPU bound. A 12-megapixel photo takes about a quarter of a second of one core. |
| `FERRY_REQUEST_TIMEOUT` | `60s` | Every API request but uploads and downloads. |
| `FERRY_STALL_TIMEOUT` | `60s` | An upload or download ends when no bytes move for this long, in either direction — whether the client or the SMB server stopped. Its total duration is not bounded. |

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
