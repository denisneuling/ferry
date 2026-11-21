# Operations

## Probes

The operations listener (`FERRY_OPS_LISTEN`, `:8081`) serves:

| path | answers |
|---|---|
| `/healthz` | `200` while the process runs |
| `/readyz` | `200` while the SMB server's port accepts a TCP connection, checked at most every five seconds; `503` otherwise |

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
| a logged-in session, idle | ~0.1 MB (50 sessions: +5 MB) |
| a running download | ~1.5 + 1 × GOMAXPROCS MB: 3.5 MB at 2 CPUs, 6.5 MB at 4 |
| a running upload | ~2.5 MB |
| thumbnails in the making, all together | at most `FERRY_THUMBNAIL_MEMORY` (default 320 MiB) |

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

Metrics and traces follow with milestone 4.
