# Security

## Reporting a vulnerability

Please report security problems privately through GitHub's
[private vulnerability reporting](https://github.com/metapatrol/ferry/security/advisories/new)
(*Security → Report a vulnerability*), not in a public issue or pull request.

Include what you found, how to reproduce it, and what an attacker could do with
it. You will get an answer as soon as a maintainer has looked at it.

## Scope

ferry opens SMB sessions with the password a person types, and serves files
from them into a browser. It keeps no users and no state of its own, so these
matter most:

- **a session without the password**: any way to get a ferry session — and so
  an SMB session — without valid credentials, including a wrong password that
  ends up as a guest or anonymous session;
- **someone else's session**: reading or changing files through another
  person's SMB session, a session ID that can be guessed, fixed or stolen
  through ferry, or two sessions crossed under load;
- **more than Samba allows**: reaching a share, a file or an operation that
  the SMB server would refuse to the same user over SMB, or a path that
  escapes its share;
- **credentials at rest or in transit inside ferry**: a password or session
  ID in logs, traces, metrics, problem responses, crash output or anything
  written to disk;
- **files as code**: an uploaded file — HTML, SVG, a PDF, anything — that runs
  script in ferry's origin when it is previewed or downloaded;
- **cross-site requests**: changing or deleting files through a request a
  logged-in person's browser sends from another site;
- **exhaustion**: a request, upload or listing that holds memory, sessions or
  SMB connections without bound or without a timeout.

Plain HTTP between the browser and ferry is a deployment's decision
(CONCEPT.md §7) and not in itself a vulnerability.

## Supported versions

Fixes go into the next release; only the latest release is supported.
