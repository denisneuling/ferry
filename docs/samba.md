# Samba for ferry

ferry is one more SMB client: it needs nothing from the server that
Explorer or Finder would not, and the server's configuration stays the
server's. A few settings make the experience better.

## Recommended

**`access based share enum = yes`** (per share, or in `[global]` for all).
Without it, Samba lists every browseable share to every user, including
shares they cannot open; ferry shows what the server lists and answers
"The server does not allow this" when one is opened. With it, each person
sees only the shares they may open — in ferry, in Explorer and in Finder
alike. ferry deliberately does not probe shares to hide them itself: that
would cost a connection per share on every listing, and second-guess the
server.

**`map to guest = never`** (the default of current Samba). ferry refuses a
guest session whatever the server offers — a wrong password never becomes a
login — but with `Bad User` or `Bad Password`, other clients on the network
may quietly get one.

## Works either way

- **Protocols:** ferry speaks SMB 2.0.2 to 3.1.1 and takes the newest the
  server offers. `server min protocol` may be as low as a NAS likes; SMB1 is
  never used.
- **Signing and encryption** as the server requires them.
- **`force group`, `create mask`, `directory mask`:** files ferry writes get
  exactly what Samba gives them, as files copied from Explorer do.
- **Hidden shares** (`name$`) are not listed, and open when asked for by
  name if the server allows — unless the deployment narrows what ferry
  offers with `FERRY_SHARES` ([configuration.md](configuration.md)).

## Worth knowing

- **A file being downloaded cannot be renamed or deleted** until the
  download ends: Samba holds the file open for it, as it would for any
  client. ferry answers such an attempt with `409 busy`.
- **Lockout:** ferry counts login attempts per address and per username, but
  the server's own lockout policy (`pdbedit -P "bad lockout attempt"`)
  applies on top, and is the stronger guard.

ferry has been tried against Samba 4.15 (Ubuntu 22.04, the mock NAS in
`hack/mock-nas`), 4.17 (Debian 12) and 4.21 (Alpine, the tests' Samba).
