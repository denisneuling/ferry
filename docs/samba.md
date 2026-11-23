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

## Previous versions

ferry shows a file's or a folder's **previous versions** where the server
offers them, the same ones Explorer's *Previous versions* tab shows: from
`vfs_shadow_copy2` on Samba (or shadow copies on Windows). Without it the
feature is simply absent. ferry asks with the person's own session, so
Samba decides who may read a version, as for the file now; a version is
read-only, and restoring one is a copy into the share as it is.

On a ZFS server with snapshots made by a tool such as sanoid, something
like this in the share (or `[global]`), with the names the tool writes —
here `autosnap_2026-11-23_15:00:09_hourly`:

```ini
vfs objects = shadow_copy2
shadow:snapdir = .zfs/snapshot
shadow:snapdirseverywhere = yes   # nested datasets, each with its own .zfs
shadow:crossmountpoints = yes
shadow:snapprefix = ^autosnap
shadow:delimiter = _
shadow:format = _%Y-%m-%d_%H:%M:%S
shadow:sort = desc
```

On Debian and Ubuntu the module is in `samba-vfs-modules`. The times are
UTC unless `shadow:localtime = yes`. Listing a path's versions costs one
open per snapshot; ferry looks at the newest 500 and stops after 20 s,
saying the list is incomplete.

## Worth knowing

- **A file being downloaded cannot be renamed or deleted** until the
  download ends: Samba holds the file open for it, as it would for any
  client. ferry answers such an attempt with `409 busy`.
- **Lockout:** ferry counts login attempts per address and per username, but
  the server's own lockout policy (`pdbedit -P "bad lockout attempt"`)
  applies on top, and is the stronger guard.

ferry has been tried against Samba 4.15 (Ubuntu 22.04, the mock NAS in
`hack/mock-nas`), 4.17 (Debian 12) and 4.21 (Alpine, the tests' Samba);
previous versions against 4.15.
