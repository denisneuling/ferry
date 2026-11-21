# ferry

ferry shows SMB shares in a browser. The login is an SMB session setup against
the configured server: ferry has no users, no database, no OIDC and no service
account, and keeps nothing but live SMB sessions in memory. CONCEPT.md is the
design; docs/ describes running and using it; docs/mockup/ is the approved
look ("Hafen") that the interface is ported from. Everything in the repository
is English.

## Ground rules

- Lean on the sibling projects before inventing: websearch (a Go service with
  an embedded Angular interface, the CI, the chart, CONTRIBUTING for the GitHub
  mirror), faber and archon (CI and release, chart, Angular i18n), soulforge
  (OpenAPI with oapi-codegen, the CI setup they all come from). They are models
  for conventions, not dependencies: code is copied with its tests.
- **Samba decides.** Samba validates the password and enforces every access
  decision. ferry adds no right and removes none: no permission check or
  cache of its own, no owner or mode set on a write. What Samba refuses is a
  problem response, never an empty listing and never a retry under other
  credentials.
- **`FERRY_SHARES` is the operator's choice, not a permission.** It says
  which shares this ferry offers at all, for listing and opening alike (a
  share outside it is a 404, a hidden `$` share asked for by name included).
  It can only narrow what ferry offers, never widen what Samba allows; inside
  the offered shares, Samba decides alone.
- **Nothing is persisted.** No database, no volume, no file outside an
  in-memory buffer or the client's stream. Sessions live in the session table
  and die with the process. Thumbnails are made on the fly and never cached on
  disk. A restart logs everyone out and loses nothing else.
- **A wrong password never yields a session.** Not a guest session (`map to
  guest`), not an anonymous one, not a session for another user. The SMB
  session setup must report an authenticated, non-guest, non-anonymous
  session, or the login is a 401. A test against a Samba configured with
  `map to guest = Bad User` holds this.
- **Credentials never leave the login.** The password exists in the login
  handler and the SMB session setup, and nowhere else: never in logs, traces,
  metrics, problem responses, panics or wrapped errors (go-smb2's included).
  The username is not a metric label and not a span attribute. A test drives
  a login with a sentinel password and asserts it appears in none of the
  output. File names and paths are not labels or span attributes either.
- **Servers are generic.** Any SMB2/3 server. Nothing specific to one NAS,
  distribution or Samba configuration in the code, the defaults or the docs;
  examples use `nas.example.lan` and fictional users like `alex`.
- **Every input bounded, every wait timed out.** Request bodies, header and
  path lengths, upload size (`FERRY_MAX_UPLOAD`), listing size, concurrent
  sessions, login attempts per address and per username. Every SMB call,
  dial and stream has a deadline or a context; a stalled client or server
  ends the request, not the process.
- **User content never runs in ferry's origin.** Downloads are
  `Content-Disposition: attachment`; previews carry `Content-Security-Policy:
  sandbox` and `X-Content-Type-Options: nosniff`, and HTML, SVG and anything
  else active is served as an inert type. The interface renders file names,
  share comments and text previews as text, never as HTML.
- **One SMB connection per browser session.** Identities never share a
  connection, a tree connect or a file handle; a dropped connection ends the
  session instead of reconnecting, because reconnecting would need the
  password again.
- **Fail closed and say so.** SMB statuses map to RFC 9457 problem responses
  (403, 404, 409, 502) in one place, `internal/api`. Mutating requests need
  the session cookie and `X-Ferry-Request: 1`.
- The OpenAPI spec (`api/openapi/ferry-v1.yaml`) is the source of truth.
  Change it, then `make generate-api lint-api`; `api/v1/v1.gen.go` is
  committed and a test fails when it is stale. The interface's client is
  generated from it at build time and not committed.
- `internal/smb` talks SMB and knows nothing of HTTP; `internal/session` holds
  the table, timeouts and rate limits; `internal/api` translates and maps
  errors. No SMB in handlers beyond calling `internal/smb`.
- The interface is ferry's own layout, "Hafen" from `docs/mockup/`, not the
  metapatrol shell. Its fonts (Fraunces, DM Sans) are served by ferry itself;
  nothing loads from a third-party origin at runtime.
- The interface speaks English and German (`@angular/localize`, as faber):
  every text marked (`i18n`, `` $localize ``), its German in the du-form in
  `ui/src/locale/messages.de.json`; `make ui-i18n` keeps the extracted texts
  current. File names and share comments come from the server and are never
  marked.
- **The repository is public.** It is push-mirrored to GitHub: no internal
  host names, addresses, real people's names or secrets in code, docs,
  examples, tests or commit messages. Test values only in tests. `make
  secret-scan` before every push.
- Keep the set complete: a new variable goes into `docs/configuration.md` and
  the chart; a changed value into the chart's README (`make helm-docs`); a new
  metric into `docs/operations.md`.
- Diagrams are Mermaid, in Markdown.
- Comments say why, in full sentences, in the voice of the surrounding code.

## Commands

    make test lint secret-scan   # what CI runs for Go
    make dev-samba               # the test Samba on 127.0.0.1:4445 (podman; CONTAINER=docker)
    make test-samba              # every test, the SMB ones against it
    make run                     # ferry on :8095 against it (alex / alex-test-password)
    make dev-nas run-nas         # the mock NAS with sample files, and ferry against it
    make dev-nas-check           # that the mock NAS is what hack/mock-nas/README.md says
    make generate-api lint-api   # after changing the spec
    make build-ferry             # the binary, into bin/

The SMB tests skip without `FERRY_TEST_SMB_SERVER` (and
`FERRY_TEST_SMB_SERVER_BAD_PASSWORD`). hack/samba holds their users,
passwords and shares — test values, fictional — and `setup.sh`, which starts
two instances, `map to guest = Bad User` and `Bad Password`, on purpose. CI
runs the same script. docs/spike.md records what go-smb2 does
wrong and how internal/smb holds it (the guest guard, one request in flight
per session, 1 MiB reads): read it before touching the login or streaming.

## Commits

Conventional Commits; `feat:`/`fix:` release (semantic-release), `feat!:`
raises the minor version. Pull requests target `develop`. Nothing is merged
or pushed to `main` and no tag is made without the maintainer: the first
release is made deliberately (`.forgejo/README.md`, *Releases are locked*).
