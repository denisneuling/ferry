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

ferry keeps nothing on disk but its optional cache, so its memory is what a
deployment has to size.
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
| a PDF page or a HEIC photo in the making | up to 208 MB of `FERRY_THUMBNAIL_MEMORY` (and the photo's bytes) while it runs |
| the previews' compiled code | ~45 MB for pdfium, ~12 MB for libheif, loaded with the first job and let go 5 minutes after the last — outside Go's heap and outside `GOMEMLIMIT` |
| recordings' covers decoded before the budget counts them | at most ~24 MiB × `FERRY_THUMBNAIL_CONCURRENCY`, rarely more than a few hundred KB |

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

A recording's thumbnail is made from the cover it carries. ferry finds the
cover by the tags' headers — at most 4 MiB of them, wherever they are in the
file — and reads the picture alone, not the recording; from there it is an
image like any other, bounded by `FERRY_THUMBNAIL_MAX_BYTES` and the budget
below. A cover stored as base64 in an Ogg comment or in an ID3 tag written
with unsynchronisation has to be decoded in memory first; such covers above
8 MiB get no thumbnail. Decoding one takes up to about three times its size
before the budget can count it — what is read, the comment, the picture —
so only `FERRY_THUMBNAIL_CONCURRENCY` such covers are decoded at once, each
holding its place until its thumbnail has its share of the budget.

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

A file that makes no thumbnail — one no decoder reads, a PDF page that
renders blank, a photo or picture beyond the bounds — is remembered for
ten minutes, in memory, and asked for again answered the same (`415` or
`413`) without being read or decoded: a folder of broken files costs one
attempt each, not one per visit. What is remembered is a keyed hash of the
share, the path, the size asked for and the file's size and time, so a
changed file is tried at once, and another size of the same file is tried
for itself. It is looked up only after the request's own session has opened
the file for reading, as the cache is. What depends on the moment is never
remembered: the share failing, a preview past its time limit, the budget
taken, a client that left. At most 10,000 files are remembered; a restart
forgets them.

### Sizing the limit

    limit ≈ 20 MB
          + 0.1 MB × sessions
          + (1.5 + GOMAXPROCS) MB × transfers running at once
          + FERRY_THUMBNAIL_MEMORY
          + a quarter for the garbage collector

For a household with 2 CPUs, a dozen sessions and ten transfers at once:
20 + 1 + 35 + 320 MB, plus a quarter: a limit of **512 MiB**. Set
`GOMEMLIMIT` below the limit (`400MiB` for 512 MiB), so that the garbage
collector works harder before the kernel steps in; the previews' compiled
code, some 60 MB, is not Go's and not under it. Where memory is
short, lower `FERRY_THUMBNAIL_MEMORY`: at 128 MiB, JPEGs up to about
25 megapixels still get thumbnails.

### Previews of PDFs

A PDF's thumbnail is its first page, rendered by pdfium — Google's PDF
library, as Chrome uses it — compiled to WebAssembly and run by wazero
inside ferry. The module sees only the bytes pdfium asks for: no file
system (go-pdfium would mount the host's root by default; ferry mounts
nothing), no network, at most 96 MiB of memory. Every page gets a new
instance, killed afterwards, never reused; one that runs past the
request's time is aborted inside the module. pdfium reads the file as it
needs it — the cross-reference table, the first page's objects — not all
of it, so `FERRY_THUMBNAIL_MAX_BYTES` does not bound PDFs.

A page pdfium runs out of memory on comes out white rather than as an
error. A thumbnail of one colour is therefore never trusted: the interface
shows an icon, and nothing is cached — a truly empty first page gets the
icon too.

Measured (amd64, 2 CPUs):

| | time | memory |
|---|---|---|
| compiling pdfium | 1.8 s | 340 MB at its peak |
| loading the compiled code (the image ships it) | 50 ms | ~45 MB, freed again 5 minutes after the last page |
| a page of text | 0.1 s | ~10 MB |
| a page that is a 12-megapixel scan | 0.4 s | ~80 MB of Go heap (wazero keeps an instance's memory in a slice and copies it as it grows) |

Each page is charged 208 MB (twice the instance's limit and 16 MB) against
`FERRY_THUMBNAIL_MEMORY`, `FERRY_PREVIEW_CONCURRENCY` (default 1) at a
time — so PDF previews need a budget of at least 208 MiB; below that they
are off, with one warning at the start. The image compiles pdfium when it
is built (`ferry precompile`, into `/usr/lib/ferry/wasm`, read-only), and
ferry loads it while the marker written with it names the same wazero,
go-pdfium, architecture and CPU features as where it runs. wazero's code
depends on the CPU: code compiled on a build machine with AVX2 and BMI is
no use on a NAS's Pentium. Then ferry compiles once — "the image's
compiled pdf code is for another CPU or build; compiling once" in the log
— into `FERRY_WASM_CACHE_DIR` (`FERRY_CACHE_DIR/wasm` with the cache on,
an `emptyDir` from the chart otherwise), and loads from there from then
on: after an idle close, and with the cache's volume after a restart. The
compile takes all of the budget while it runs, up to 400 MB — thumbnails
wait for those seconds rather than run beside it — and fits a pod of the
chart's defaults since the budget is otherwise idle. Should it fail, the
format is off and tried again after the idle time (5 minutes).

Under the chart's limits (512 MiB, the default thumbnail budget;
`hack/loadtest/run.sh`) a scanned PDF at 640 pixels three times over, a wall
of 50 thumbnails of 12-megapixel JPEGs twice over and a 256 MB download at
once peaked at 314 MB (VmHWM), without an OOM. With two 12.6-megapixel HEIC
photos at 640 and 320 on top (`--heic`), the peak was 466–484 MB at a
`GOMEMLIMIT` of 450 MiB — too close to 512 — and 442–444 MB at 400 MiB, the
chart's default since, again without an OOM.

### Previews of HEIC photos

HEIC photos, as phones write them, are decoded by libheif and libde265
compiled to one WebAssembly module (third_party/heif, LGPL 3.0), run like
pdfium: no file system, a memory limit (`FERRY_PREVIEW_HEIC_MEMORY`), a new
instance for every photo, aborted when the request ends.

A phone's photo carries a thumbnail of its own, about 512 pixels wide. A
thumbnail of up to 320 pixels is made from it: ferry takes that item out of
the file as a file of its own and decodes it alone, in about 0.15 s. The
640-pixel one, which the preview shows where the browser cannot show HEIC
itself (all but Safari), is made from the photo: 12.6 megapixels took 7 s
and 80 MB in the instance — once per file with the cache on. The interface
shows the 320-pixel one meanwhile. A photo without a thumbnail item is
decoded whole for every size.

| | time | instance memory |
|---|---|---|
| compiling libheif | 0.2 s | 50 MB at its peak |
| loading the compiled code | 15 ms | ~12 MB |
| a thumbnail item (512²) | 0.15 s | small |
| a photo of 1.8 MP | 1.2 s | small |
| a photo of 12.6 MP | 7 s | 80 MB |
| a photo of 16.8 MP | 10 s | 112 MB |

A photo is charged twice `FERRY_PREVIEW_HEIC_MEMORY` and 16 MB, and its
bytes, against `FERRY_THUMBNAIL_MEMORY`; it must be within
`FERRY_THUMBNAIL_MAX_BYTES` and `FERRY_THUMBNAIL_MAX_PIXELS`, checked from
its header before it is decoded. One that needs more than the instance may
hold fails inside the module and gets an icon — at the default of 96 MiB
that is above about 15 megapixels; a 24-megapixel photo needs 160Mi, and
then `FERRY_THUMBNAIL_MEMORY` of at least 336 MB.

### Time limit

A PDF page or a HEIC photo may run `FERRY_PREVIEW_TIMEOUT` (30 s) once it
has its slot — preparing the runtime, waiting for its share of the budget,
decoding; the wait for the slot itself counts against the request's
`FERRY_REQUEST_TIMEOUT`. Past the limit the instance is aborted, the slot
is free for the next file, and the answer is `503` (`preview_timeout`):
nothing is cached, so the file is tried again when it is asked for again.
`ferry_preview_duration_seconds` shows what the CPU really takes; a photo
of 12.6 MP took 7 s on a desktop CPU, and a small ARM board may take three
or four times that. `FERRY_REQUEST_TIMEOUT` should stay above the limit,
or the request ends first — ferry warns at startup when it does not.

## Cache

Off by default. With `FERRY_CACHE_DIR` (in the chart, `cache.enabled`),
ferry keeps what it derives from a file — thumbnails in their three sizes,
a file's details — in that directory, so that after a restart, and for the
next person who may read the same file, they come at once instead of being
made again.

- **What is on the volume.** Entries only: a thumbnail's JPEG or the details'
  JSON, encrypted (AES-256-GCM) under a key derived from `FERRY_CACHE_KEY`
  and the file's identity — share, path, size, time of the last write, the
  variant, and the SMB server. Each is named by an HMAC of the same: 64 hex
  digits, in folders of their first two. No file name, path, share or user
  appears on the volume, no file itself and no credential.
- **What the encryption protects.** The volume on its own — a backup, a disk
  taken out, a snapshot: without the key, nothing in it can be read or
  matched to a file. Whoever has the volume *and* the key can read an entry
  if they also know the file's share, path, size and time to the
  nanosecond — which takes the file's listing, at least. The key is the
  chart's Secret; keep it as you keep the volume.
- **Who gets an entry.** Only a person whose session has just opened the file
  for reading on the share — the same open the thumbnail would be made
  from. Samba decides that open; a person who sees a file in a listing but
  may not read it gets `403`, whatever the cache holds. A changed file has
  another size or time and so another entry; the old one is never given
  again and ages out.
- **Size.** `FERRY_CACHE_MAX_BYTES` (default 1 GiB). Over it, the entries
  used longest ago are removed until 90 % of it is left; a hit sets the
  entry's modification time, since many file systems do not keep access
  times. A thumbnail of 320 pixels is some 10–30 KB, so 1 GiB holds tens of
  thousands of pictures in all three sizes. In the chart, keep
  `cache.persistence.size` above `cache.maxBytes`.
- **Failures.** A full, read-only or slow volume (a read waits 2 s at most) is
  a miss and a warning at most once a minute — never a failed request.
  Entries are written whole or not at all (a temporary file, synced,
  renamed), in the background; when the writer is behind, new entries are
  dropped rather than held in memory.
- **Deleting it** is always safe, running or not: entries are made again as
  they are asked for. A new key makes every entry unreadable, with the same
  effect; unreadable ones are removed when met.

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
| `ferry_searches_total` | counter | `outcome` | Searches below a folder: `ok`, `truncated` (stopped at a bound: hits, folders, depth, a minute), `canceled` (the client left), `failed` |
| `ferry_search_duration_seconds` | histogram | — | How long a search below a folder ran |
| `ferry_copies_total` | counter | `outcome` | Copies within a share, versions restored as copies included: `ok`, `failed` (what was copied so far stays), `canceled` (the client left) |
| `ferry_archives_total` | counter | `outcome` | ZIP downloads: `ok`, `failed` (broken off when a file failed), `canceled` (the client left); their bytes count in `ferry_transfer_bytes_total{direction="down"}` |
| `ferry_thumbnails_total` | counter | `outcome` | `made`, `cached` (from the cache), `not_modified`, `unsupported`, `too_large`, `remembered` (a file that made no thumbnail a moment ago, answered as then), `timeout` (a PDF page or HEIC photo past `FERRY_PREVIEW_TIMEOUT`, answered `503`), `failed` |
| `ferry_preview_jobs_total` | counter | `format`, `outcome` | PDF pages (`pdf`) and HEIC photos (`heic`) made in WebAssembly: `made`, `unsupported`, `blank`, `too_large`, `timeout`, `unavailable` (off, or no budget), `cancelled` (the request ended first), `failed` (reading the file failed) |
| `ferry_preview_wait_seconds` | histogram | `format` | how long those jobs waited for one of the `FERRY_PREVIEW_CONCURRENCY` slots |
| `ferry_preview_duration_seconds` | histogram | `format` | how long they ran once they had one: what this CPU takes for a page or a photo, in buckets fine between 5 and 30 s |
| `ferry_cache_requests_total` | counter | `kind`, `outcome` | lookups in the cache by `kind` (`thumbnail`, `metadata`): `hit`, `miss`, `error` (the volume failed; failed writes count here too) |
| `ferry_cache_bytes` | gauge | | what the cache's entries take, as last counted |
| `ferry_cache_evictions_total` | counter | | entries removed to stay within `FERRY_CACHE_MAX_BYTES` |
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
