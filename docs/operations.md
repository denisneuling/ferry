# Operations

## Probes

The operations listener (`FERRY_OPS_LISTEN`, `:8081`) serves:

| path | answers |
|---|---|
| `/healthz` | `200` while the process runs |
| `/readyz` | `200` while the SMB server's port accepts a TCP connection, checked at most every five seconds; `503` otherwise |
| `/metrics` | the Prometheus metrics below |

`/readyz` failing takes ferry out of a Service's endpoints but does not
restart it: the sessions it holds are worth keeping until the server is
back. Point the liveness probe at `/healthz`.

## Memory

ferry keeps nothing on disk, so its memory is what a deployment has to size.
A container that exceeds its limit is killed, and every session with it.
Measured on Linux with Samba 4.21, ferry built with Go 1.27
(`internal/thumb/memory_test.go`, and the process under load):

| what | memory |
|---|---|
| the idle process | ~10 MB |
| a logged-in session, idle | ~0.1 MB with one SMB connection, ~0.13 MB with three (50 sessions: +4.3 and +6.5 MB) |
| a running download | ~1.5 + 1 × GOMAXPROCS MB: 3.5 MB at 2 CPUs, 6.5 MB at 4 |
| a running upload | ~2.5 MB |
| thumbnails in the making, all together | at most `FERRY_THUMBNAIL_MEMORY` (default 320 MiB) |

Downloads spread over a session's background connections take a little
more than on one, since each connection keeps its own buffers: three at
once took 7 MB on one connection and 10.5 MB on two (2 CPUs).

The SMB server pays for connections too: Samba runs one `smbd` process per
connection, which took about 2.8 MB of its own memory each on Samba 4.21 —
some 8.5 MB on the server per person logged in with three connections.
Where the server is small and many log in, lower `FERRY_SMB_CONNECTIONS`.
The server's own limits count connections, not people, and so see each
login `FERRY_SMB_CONNECTIONS` times: in Samba, `max connections` on a share
(each connection that opens the share counts) and `max smbd processes`.
Its audit log shows a login's sessions too; a wrong password, though, is
tried on one connection only, so failed attempts and lockout counts see
one per login.

A download's share grows with the CPUs because go-smb2 pools its receive
buffers per connection in a `sync.Pool`, which keeps one per processor. Go
sets GOMAXPROCS from the container's CPU limit, so set one.

### Thumbnails

Decoding an image needs the whole picture in memory. At the default pixel
limit of 50 megapixels, one thumbnail took at its peak:

| image (50 MP) | peak heap | ferry's estimate |
|---|---|---|
| JPEG (4:2:0, what cameras write) | 98 MB | 205 MB |
| PNG, 8-bit RGBA | 227 MB | 268 MB |
| PNG, 16-bit RGBA | 427 MB | 519 MB |

A 12-megapixel phone photo takes ~30 MB and about a quarter of a second of
one core.

Before it reads an image, ferry estimates what the thumbnail will cost from
the image's header and the file's size (`thumb.Estimate`: the file, the
decoded pixels at the colour model's bytes per pixel, a quarter more and
16 MB) and takes that much from `FERRY_THUMBNAIL_MEMORY`. Thumbnails wait
until their estimates fit together; one whose estimate alone exceeds the
budget is refused with `413` and the interface shows an icon. The estimates
are above every peak measured, so thumbnails never take more than the
budget. With the default of 320 MiB, every JPEG and 8-bit PNG up to 50 MP is
made, one at a time at that size and several at once at phone sizes; a
16-bit PNG of 50 MP is refused. `FERRY_THUMBNAIL_CONCURRENCY` (default 2)
bounds the CPU separately.

### Sizing the limit

    limit ≈ 20 MB
          + 0.1 MB × sessions
          + (1.5 + GOMAXPROCS) MB × transfers running at once
          + FERRY_THUMBNAIL_MEMORY
          + a quarter for the garbage collector

For a household with 2 CPUs, a dozen sessions and ten transfers at once:
20 + 1 + 35 + 320 MB, plus a quarter: a limit of **512 MiB**. Set
`GOMEMLIMIT` a little below the limit (`450MiB` for 512 MiB), so that the
garbage collector works harder before the kernel steps in. Where memory is
short, lower `FERRY_THUMBNAIL_MEMORY`: at 128 MiB, JPEGs up to about
25 megapixels still get thumbnails.

## Logs

One JSON object per line on stdout (`FERRY_LOG_FORMAT=text` for people). What
they never hold: passwords, usernames, file and folder names, paths, query
strings, and the text of an error — errors appear as their kind and NT
status (`error_kind`, `nt_status`), or only their Go type.
`TestCredentialsNeverLogged` and `TestPathsNeverLogged` hold that.

| message | fields | when |
|---|---|---|
| `login` | `outcome=ok`, `client`, `duration_ms` | a session was set up |
| `login refused` | `outcome` (`auth`, `guest`, `unreachable`, `rate_limited`, `too_many_sessions`, …), `error_kind`, `nt_status`, `client` | a login failed |
| `request failed` | `route`, `error_kind`, `nt_status` | a request ended in a 5xx |
| `download ended early` | `error_type` or `error_kind`, `bytes` | the client went away or the server stopped answering mid-download |
| `request` (debug) | `method`, `route` (the pattern, never the path), `status`, `bytes`, `duration_ms` | every request |
| `panic` | `route`, `stack` | a bug; the request got a 500 and the process stays up |

`guest` among the refused logins means someone tried a user the server does
not know — or the server maps wrong passwords to the guest account (`map to
guest = Bad Password`) — and ferry refused the guest session the server
offered.

## Metrics

On the operations listener, `/metrics`, for Prometheus to scrape; the chart
names the Service port `metrics` and leaves the scraping to the
installation. No label ever carries a user name, a share, a folder, a file
name or a path: routes are patterns, errors are kinds from a fixed list
(`TestPathsNeverLogged` scrapes the metrics too).

| metric | type | labels | what |
|---|---|---|---|
| `ferry_sessions` | gauge | | sessions open now: logins, each with its SMB connections |
| `ferry_smb_connections` | gauge | | SMB connections open now, of all sessions together |
| `ferry_smb_connections_lost_total` | counter | `pool` | connections that went away on their own: `interactive` (the session ended) or `background` (it carried on with fewer) |
| `ferry_smb_slot_wait_seconds` | histogram | `pool` | how long requests waited for their connection's one request slot, `interactive` or `background`: the interactive one should stay near zero |
| `ferry_logins_total` | counter | `outcome` | login attempts: `ok`, `auth`, `guest`, `unreachable`, `rate_limited`, `too_many_sessions`, `invalid`, `internal` |
| `ferry_transfer_bytes_total` | counter | `direction` | file bytes `up` (uploads) and `down` (downloads and previews) |
| `ferry_smb_errors_total` | counter | `kind` | SMB errors answered to a request: `access_denied`, `not_found`, `exists`, `busy`, `not_empty`, `not_dir`, `is_dir`, `invalid_path`, `limit`, `unreachable`, `session_lost`, `canceled`, `internal` |
| `ferry_archives_total` | counter | `outcome` | ZIP downloads: `ok`, `failed` (broken off when a file failed), `canceled` (the client left); their bytes count in `ferry_transfer_bytes_total{direction="down"}` |
| `ferry_thumbnails_total` | counter | `outcome` | `made`, `not_modified`, `unsupported`, `too_large`, `failed` |
| `ferry_thumbnail_memory_bytes` | gauge | | taken from the thumbnail budget now |
| `ferry_thumbnail_memory_limit_bytes` | gauge | | the budget, `FERRY_THUMBNAIL_MEMORY` |
| `ferry_http_requests_total` | counter | `method`, `route`, `code` | HTTP requests by route pattern, e.g. `GET /api/v1/shares/{share}/content` |
| `ferry_http_request_duration_seconds` | histogram | `route` | their duration; a transfer counts until its last byte |
| `ferry_build_info` | gauge | `version`, `commit` | always 1 |

and the Go runtime's and the process's (`go_*`, `process_*`). Worth an
alert: `guest` or `auth` logins rising fast (someone guessing), `session_lost`
errors and lost connections (the server dropping them), memory near the
limit.

## Traces

With `OTEL_EXPORTER_OTLP_ENDPOINT` (or `OTEL_EXPORTER_OTLP_TRACES_ENDPOINT`)
set, ferry sends traces over OTLP/HTTP; the exporter reads the other
standard `OTEL_*` variables as well. Every request is a span named by its
route pattern, with its method and status; every SMB operation below it a
span of its own (`smb.login`, `smb.readdir`, `smb.open`, `smb.upload`, …),
marked failed with the error's kind. Like the metrics, spans name routes and
operations, never what they act on: no URL, path, share, file or user name.
