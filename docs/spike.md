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
| throughput, 1 GbE | not measured yet: to be taken on the first deployment with `hack/spike` |
| against the mock NAS | see below |

## Against the mock NAS

`hack/spike` against `hack/mock-nas` (Samba 4.15.13-Ubuntu, `map to guest =
never`), over loopback, two runs:

| step | time |
|---|---|
| an unknown user | refused as `auth` in 4–5 ms: the server maps no one to guest |
| login (alex) | 4–5 ms |
| share list | 1 ms |
| a folder of 250 entries | 3–6 ms |
| a 1.5 GB file, streamed | 0.6–0.9 s: 1.8–2.7 GB/s |

Loopback says what ferry and go-smb2 can do, not what a network will: at
1 GbE the line (~117 MB/s) is the limit by a factor of fifteen. The 1 GbE
figure is still open.

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
under `Bad User`. Under `map to guest = Bad Password` it is a guest session
too: go-smb2 alone logs alex in with a wrong password and lists every share.

ferry's answer (`internal/smb/guard.go`): the connection go-smb2 dials over is
wrapped while the session is set up, and the guard reads the session flags of
the final, successful SESSION_SETUP response from the wire itself. Login
succeeds only if that response was seen, parsed, and is neither guest nor
anonymous; otherwise the session is logged off and the login is a 401
(`KindGuest`). Empty user names and passwords are refused before the server is
asked. `TestLoginOutcomes` holds it against the `Bad User` instance,
`TestLoginBadPasswordMappedToGuest` against the `Bad Password` one; the
guard's unit tests cover what it cannot read (no final response, a broken
frame, an overflowing buffer — all refused) and the flags that must not
matter (`SMB2_SESSION_FLAG_ENCRYPT_DATA`).

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

### A request the browser abandons kills the session — a go-smb2 race, worked around

Found in the interface, where a session now and then ended with a 401 after
quick browsing. A browser abandons requests all the time — thumbnails
scrolled away, a folder left before its listing came, a video seeked — and
each such request's context ended in the middle of its SMB call. go-smb2
encodes every request into one buffer its connection shares; when the
context ends after the packet went to the writer goroutine but before the
write is done, the call returns and releases the connection's lock while
the writer still sends from that buffer, and the next request overwrites
it. Samba sees a duplicate message ID (`smb2_validate_sequence_number:
duplicate message_id`) and drops the connection; ferry, which never
reconnects, ends the session.

ferry's answer (`internal/smb/call.go`): go-smb2 never sees the request's
context. A call waits for the session's request slot under it, then runs
under a context of its own; a client that goes away stops the operation
between steps — between two 1 MiB reads of a download — never inside one.
Open files have their own context the same way. What may still end a step is
a watchdog: a call the server does not answer within the call timeout
(`FERRY_REQUEST_TIMEOUT`). A connection cancelled mid-request cannot be
trusted, so the session is then ended deliberately. Without the change,
`TestCancelledCallsKeepTheSession` (400 SMB calls cancelled at random
moments) fails every time and `TestAbortedRequestsKeepTheSession` (aborted
HTTP requests) with the 401 seen in the browser; with it, both pass.

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
  context it was opened with, so the handle of an aborted download could not
  be closed. Done: open files get a context of their own that outlives the
  request's by ten seconds (`handleContext`); `TestAbortedDownloadClosesItsHandle`
  and `TestUploadCanceledLeavesNothingBehind` hold it.
- go-smb2's `Rename` always replaces an existing target
  (`ReplaceIfExists: 1`): "without overwrite" is a check before the rename,
  not an atomic refusal. Worth an option upstream.
- go-smb2's `Remove` retries a permission failure after `Chmod(0666)`, which
  would clear the file's DOS attributes. It cannot trigger today — the
  server's `ResponseError` does not match `os.ErrPermission` — and
  `TestReadOnlyShare` shows a refused delete leaves the file as it was. A
  go-smb2 update that changes that matching would make ferry change files it
  was told it may not delete; read its changes when updating the copy (below).

## Previous versions: what go-smb2 lacks (2026-11-23)

Against a Samba 4.15 with `vfs_shadow_copy2` configured as on a ZFS NAS
(snapshots in `.zfs/snapshot`, names with a prefix and a suffix, nested
datasets with `shadow:snapdirseverywhere`):

- Listing a path's snapshots needs `FSCTL_SRV_ENUMERATE_SNAPSHOTS`, and
  go-smb2's ioctl is unexported. Opening a path as it was needs the `TWrp`
  create context: Samba no longer reads `@GMT-…` tokens in SMB2 paths. Both
  are in ferry's copy of go-smb2, `third_party/go-smb2`, together with a fix
  of the create contexts' length, which go-smb2 counted wrongly for names of
  some lengths (`STATUS_INVALID_PARAMETER`); `PATCHES.md` there has the
  diff, and `hack/check-fork.sh` holds the copy to it. go-smb2 is updated by
  hand since, not by Renovate.
- With prefixed snapshot names, Samba resolves the time in an open against
  the list it last enumerated on the connection: an open after enumerating
  another dataset, or none, fails. internal/smb enumerates the same path in
  the same call before every open at a time (`versions.go`,
  `TestOpenAtEnumeratesItsPath`).
- Reading `.zfs/snapshot` directly was rejected: it is one file system's
  layout, a nested dataset has its own, and it would go around Samba's view
  of the snapshots.
