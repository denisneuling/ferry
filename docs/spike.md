# Milestone 1: the SMB spike

What CONCEPT.md §9 asked the spike to prove, and what §11's hazards turned
out to be, measured with go-smb2 (cloudsoda fork,
`v0.0.0-20261105190815-95be04a983b8`) against Samba 4.21 in a container
(`make dev-samba`, `hack/samba`). The code is `internal/smb`; its tests
(`make test-samba`) hold every finding below. `hack/spike` runs the same steps
against any server, for the check against a real NAS over 1 GbE.

## Results

| question | answer |
|---|---|
| NTLMSSP against a current Samba | works, with the guard below for guest sessions |
| NetShareEnum on the user's session | works (srvsvc over IPC$); lists every browseable share, also those the user may not open, and hidden `$` shares |
| throughput, loopback | ~1.5 GB/s for one stream with 1 MiB reads; the network, not ferry, is the limit at 1 GbE (~117 MB/s) |
| throughput, 1 GbE | to be measured on the first deployment with `hack/spike` |

## Hazards

### A wrong password yields a guest session — real, and go-smb2 does not catch it

Under `map to guest = Bad User`, Samba answers a login for an **unknown user**
(or with an empty user name) with a successful session setup carrying
`SMB2_SESSION_FLAG_IS_GUEST`. With go-smb2 alone that is a working session:
it lists the shares and reads `Public`. `Negotiator.RequireMessageSigning`
does not help: go-smb2 checks the guest flag on the *first* SESSION_SETUP
response — the NTLM challenge, which never carries it — and takes the final
response's flags without checking them. The flags are not exported.

A wrong password for an existing user is refused by Samba (`STATUS_LOGON_FAILURE`)
under `Bad User`; under `map to guest = Bad Password` it would be a guest
session too.

ferry's answer (`internal/smb/guard.go`): the connection go-smb2 dials over is
wrapped while the session is set up, and the guard reads the session flags of
the final, successful SESSION_SETUP response from the wire itself. Login
succeeds only if that response was seen, parsed, and is neither guest nor
anonymous; otherwise the session is logged off and the login is a 401
(`KindGuest`). Empty user names and passwords are refused before the server is
asked. `TestLoginOutcomes` holds it against the container.

Worth proposing upstream: an exported `Session.IsGuest()`/`IsAnonymous()`, and
the guest check moved to the final response.

### NetShareEnum shows shares the user cannot open — real, and left to Samba

Samba enumerates every browseable share to every user, so alex sees `sam`.
Opening it is refused (`STATUS_ACCESS_DENIED` → 403). ferry does not probe
shares to hide them — that would be a tree connect per share and listing, and
a second opinion on access — and filters only `IPC$`, printers and names
ending in `$`. A server that should not show them sets `access based share
enum = yes`. A hidden share Samba lets the user open (`hidden$`) opens in
ferry too, when asked for by name.

*Product question:* show such shares (locked on opening, as now), or hide them
in the interface? See the open questions below.

### Concurrent requests on one session fail — a go-smb2 bug, worked around

Not in §11, found by the concurrency test: with several requests in flight on
one session, most QUERY_DIRECTORY and QUERY_INFO requests fail with
`STATUS_INVALID_PARAMETER`; Samba logs `CreditCharge too low, given 1, needed
16`. When its credit balance is short, go-smb2 borrows fewer credits than a
request needs and shrinks the request accordingly — but only for READ and
WRITE. Directory listings and `stat` keep their 1 MiB output buffer with the
smaller charge, and the server rejects them.

ferry's answer: one SMB request in flight per session (`Session.op`), taken
with the request's context so waiting for it is bounded. A session is one
person, so the cost is small: a download takes the slot per 1 MiB read, not
for the whole file, and browsing interleaves with it. Worth fixing upstream
(clamp `OutputBufferLength` to the granted payload size), after which the
slot can go.

### Throughput depends on the read size

One SMB READ per call: 32 KiB reads reach ~400 MB/s on loopback, 1 MiB reads
~2 GB/s. `io.Discard`'s `ReadFrom` and net/http's response writer read in
8–32 KiB pieces, so a `*File` handed to `http.ServeContent` as it is would
stream at a fraction of the speed. `File.WriteTo` reads in 1 MiB chunks and
`io.Copy` uses it; the download handler (milestone 2) must keep that, for
ranges too, rather than let `ServeContent` copy.

### A dropped connection is retried with the password — not the case

go-smb2 never reconnects, and ferry keeps no password to reconnect with. A
dropped connection marks the session lost (`KindSessionLost`), every later
call fails the same way, and the session table (milestone 2) ends it.
`TestDroppedConnectionEndsTheSession`.

### Two users' sessions get crossed — not the case

Each login has its own TCP connection and session; `TestSessionsDoNotCross`
runs two users' requests interleaved under the race detector, each refused
the other's share and keeping its own.

### A credential appears in a log line through a wrapped error — prevented

go-smb2's errors carry the server's status text and, from `os.PathError`, the
path; they hold no password today. ferry does not rely on that:
`internal/smb` returns only `*Error` — a kind and an NT status, no message, no
path, no user name, and no wrapped cause. `TestCredentialsNeverInErrorsOrLogs`
logs every login failure (wrong password, guest mapping, unreachable server)
and session error through slog and `%v`/`%+v`/`%#v`, and asserts that
neither the password nor the user name appears.

go-smb2 has a debug logger that writes to stderr when the environment
variable `DEBUG` is set. It logs packet errors, not credentials; the chart
should not set `DEBUG` regardless.

### Not yet tested

- A failed or interrupted upload leaving a half-written file under the real
  name: uploads are milestone 2 (temporary name, rename on completion).
- An uploaded HTML or SVG running script when previewed: milestone 2's
  download and preview headers.

## Open for milestone 2

- `File.Close` after the request's context ended: go-smb2's file keeps the
  context it was opened with, so closing a handle of a cancelled download may
  fail and leave it to the server's own cleanup. Close with a fresh context.
- The Samba service in CI: `hack/samba` is built locally; the CI test job
  needs it as a job service from the registry mirror, or a public image
  configured to the same users and shares.
