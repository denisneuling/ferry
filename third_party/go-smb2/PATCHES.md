# ferry's patches to go-smb2

This is [github.com/cloudsoda/go-smb2](https://github.com/cloudsoda/go-smb2)
at the version ferry's `go.mod` requires (commit
`95be04a983b8770c7255d547aa02ee384e9882c7`), with the changes below and
nothing else; `go.mod` replaces the module with this copy. LICENSE is
upstream's. `hack/check-fork.sh` holds the copy to it in CI: it fetches the
upstream module, applies the diff and compares.

ferry sends no changes upstream. Its workarounds for go-smb2's behaviour
(the guest guard, one request per connection, contexts that never reach a
call: docs/spike.md) stay in ferry's `internal/smb`, not here. This copy
carries only what ferry cannot do from outside the package: the two SMB
requests behind previous versions, which need go-smb2's unexported ioctl
and create request.

## The changes

1. **`(*File).Snapshots`** — the previous versions a server offers for a
   path, with `FSCTL_SRV_ENUMERATE_SNAPSHOTS`, as `@GMT-…` tokens. Two
   calls: the first learns the size of the list, the second fetches it,
   bounded at 1 MiB.
2. **`(*Share).OpenAt`** — opens a path read-only as it was at a
   snapshot's time, with the `TWrp` create context. Samba 4.15 and later
   ignore `@GMT-…` tokens in SMB2 paths; the create context is the only
   way. With `shadow:snapprefix` and `shadow:delimiter`, Samba resolves the
   time against the list it last enumerated on the connection, so a caller
   enumerates the same path first (internal/smb does both in one call).
3. **A fix in `CreateRequest.Encode`** — an upstream bug that nothing
   upstream reaches, since go-smb2 itself sends no create contexts:
   `CreateContextsLength` was counted from the end of the name instead of
   from the first context, so a name whose UTF-16 length is no multiple of
   8 made the server see the padding as part of the contexts and answer
   `STATUS_INVALID_PARAMETER`. A rebase onto a newer upstream must check
   whether upstream fixed it; `TestCreateContextsLength` fails without the
   fix.

Tests: `snapshots_test.go` here (the array parser, the context's bytes,
the length), and internal/smb's tests against a Samba with
`vfs_shadow_copy2` (hack/samba).

## Updating

Change the version in ferry's `go.mod`, put the new upstream module here
(`go mod download -json`), apply the diff below (`git apply`), resolve what
no longer applies, then `sh hack/check-fork.sh --update` writes the diff
anew and `sh hack/check-fork.sh` checks it.

## The diff

<!-- diff:begin -->
```diff
diff --git a/internal/smb2/request.go b/internal/smb2/request.go
index 815559f..0d0cf25 100644
--- a/internal/smb2/request.go
+++ b/internal/smb2/request.go
@@ -527,11 +527,13 @@ func (c *CreateRequest) Encode(pkt []byte) {
 
 	var ctx []byte
 	var next int
+	first := off
 
 	for i, c := range c.Contexts {
 		off = Roundup(off, 8)
 
 		if i == 0 {
+			first = off
 			le.PutUint32(req[48:52], uint32(64+off)) // CreateContextsOffset
 		} else {
 			le.PutUint32(ctx[:4], uint32(next)) // Next
@@ -546,7 +548,9 @@ func (c *CreateRequest) Encode(pkt []byte) {
 		off += next
 	}
 
-	le.PutUint32(req[52:56], uint32(off-(56+nlen))) // CreateContextsLength
+	// CreateContextsLength counts from the first context, not from the end
+	// of the name: the padding before it is not part of the contexts.
+	le.PutUint32(req[52:56], uint32(off-first)) // CreateContextsLength
 }
 
 type CreateRequestDecoder []byte
diff --git a/snapshots.go b/snapshots.go
new file mode 100644
index 0000000..e9c6302
--- /dev/null
+++ b/snapshots.go
@@ -0,0 +1,152 @@
+package smb2
+
+import (
+	"encoding/binary"
+	"os"
+	"time"
+	"unicode/utf16"
+
+	"github.com/cloudsoda/go-smb2/internal/smb2"
+)
+
+// maxSnapshotArray bounds the snapshot list a server may send: some 20000
+// tokens, far beyond any retention.
+const maxSnapshotArray = 1 << 20
+
+// Snapshots returns the previous versions the server offers for what f is
+// open on, as @GMT tokens ("@GMT-2006.01.02-15.04.05", UTC), newest first
+// as the server sorts them, with FSCTL_SRV_ENUMERATE_SNAPSHOTS ([MS-SMB2]
+// 3.3.5.15.1, [MS-SMB] 2.2.7.2.2.1). A server without snapshots returns
+// none. OpenAt opens a path as it was at one of them.
+func (f *File) Snapshots() ([]string, error) {
+	f.m.Lock()
+	defer f.m.Unlock()
+
+	// The first call has room for the counts only, and so learns the size
+	// of the whole array; the second fetches it.
+	out, err := f.ioctl(&smb2.IoctlRequest{
+		CtlCode:           smb2.FSCTL_SRV_ENUMERATE_SNAPSHOTS,
+		MaxOutputResponse: 16,
+		Flags:             smb2.SMB2_0_IOCTL_IS_FSCTL,
+	})
+	if err != nil {
+		return nil, &os.PathError{Op: "snapshots", Path: f.name, Err: err}
+	}
+	if len(out) < 12 {
+		return nil, &os.PathError{Op: "snapshots", Path: f.name, Err: &InvalidResponseError{"broken snapshot array"}}
+	}
+	if binary.LittleEndian.Uint32(out[0:4]) == 0 {
+		return nil, nil
+	}
+	size := binary.LittleEndian.Uint32(out[8:12])
+	if size > maxSnapshotArray {
+		return nil, &os.PathError{Op: "snapshots", Path: f.name, Err: &InvalidResponseError{"snapshot array too large"}}
+	}
+	out, err = f.ioctl(&smb2.IoctlRequest{
+		CtlCode:           smb2.FSCTL_SRV_ENUMERATE_SNAPSHOTS,
+		MaxOutputResponse: 12 + size,
+		Flags:             smb2.SMB2_0_IOCTL_IS_FSCTL,
+	})
+	if err != nil {
+		return nil, &os.PathError{Op: "snapshots", Path: f.name, Err: err}
+	}
+	tokens, ok := parseSnapshotArray(out)
+	if !ok {
+		return nil, &os.PathError{Op: "snapshots", Path: f.name, Err: &InvalidResponseError{"broken snapshot array"}}
+	}
+	return tokens, nil
+}
+
+// parseSnapshotArray reads an SRV_SNAPSHOT_ARRAY: three counts, then the
+// tokens in UTF-16LE, each ending in a null, the list in another.
+func parseSnapshotArray(out []byte) ([]string, bool) {
+	if len(out) < 12 {
+		return nil, false
+	}
+	returned := binary.LittleEndian.Uint32(out[4:8])
+	size := binary.LittleEndian.Uint32(out[8:12])
+	if uint64(size) > uint64(len(out)-12) {
+		return nil, false
+	}
+	b := out[12 : 12+size]
+	u := make([]uint16, 0, len(b)/2)
+	for i := 0; i+1 < len(b); i += 2 {
+		u = append(u, binary.LittleEndian.Uint16(b[i:]))
+	}
+	var tokens []string
+	start := 0
+	for i, c := range u {
+		if uint32(len(tokens)) == returned {
+			break
+		}
+		if c != 0 {
+			continue
+		}
+		if i > start {
+			tokens = append(tokens, string(utf16.Decode(u[start:i])))
+		}
+		start = i + 1
+	}
+	return tokens, true
+}
+
+// OpenAt opens name for reading as it was in the snapshot taken at t,
+// with the SMB2_CREATE_TIMEWARP_TOKEN create context ([MS-SMB2]
+// 2.2.13.2.7); t is the time of a token Snapshots returned. The handle is
+// read-only, as a snapshot is; a folder opened this way lists the
+// snapshot's entries.
+//
+// A server may resolve the time against the list it last enumerated on
+// this connection (Samba's shadow_copy2 does when snapshot names carry
+// more than the time): call Snapshots on the same path first.
+func (fs *Share) OpenAt(name string, t time.Time) (*File, error) {
+	name = normPath(name)
+
+	if err := validatePath("open", name, false); err != nil {
+		return nil, err
+	}
+
+	req := &smb2.CreateRequest{
+		SecurityFlags:        0,
+		RequestedOplockLevel: smb2.SMB2_OPLOCK_LEVEL_NONE,
+		ImpersonationLevel:   smb2.Impersonation,
+		SmbCreateFlags:       0,
+		DesiredAccess:        smb2.GENERIC_READ,
+		FileAttributes:       smb2.FILE_ATTRIBUTE_NORMAL,
+		ShareAccess:          smb2.FILE_SHARE_READ | smb2.FILE_SHARE_WRITE,
+		CreateDisposition:    smb2.FILE_OPEN,
+		CreateOptions:        smb2.FILE_SYNCHRONOUS_IO_NONALERT,
+		Mapping:              fs.mapping,
+		Contexts:             []smb2.Encoder{timewarp(t)},
+	}
+
+	f, err := fs.createFile(name, req, true)
+	if err != nil {
+		return nil, &os.PathError{Op: "open", Path: name, Err: err}
+	}
+
+	return f, nil
+}
+
+// timewarp is the TWrp create context: a snapshot's time as a FILETIME.
+type timewarp time.Time
+
+func (timewarp) Size() int { return 32 }
+
+func (t timewarp) Encode(p []byte) {
+	le := binary.LittleEndian
+	le.PutUint32(p[0:4], 0)    // Next: the request sets it when another follows
+	le.PutUint16(p[4:6], 16)   // NameOffset
+	le.PutUint16(p[6:8], 4)    // NameLength
+	le.PutUint16(p[8:10], 0)   // Reserved
+	le.PutUint16(p[10:12], 24) // DataOffset
+	le.PutUint32(p[12:16], 8)  // DataLength
+	copy(p[16:20], "TWrp")
+	clear(p[20:24]) // the name padded to 8 bytes
+	le.PutUint64(p[24:32], filetime(time.Time(t)))
+}
+
+// filetime is t in 100-nanosecond intervals since 1601-01-01 UTC.
+func filetime(t time.Time) uint64 {
+	return uint64(t.UnixNano()/100 + 116444736000000000)
+}
diff --git a/snapshots_test.go b/snapshots_test.go
new file mode 100644
index 0000000..bb2e464
--- /dev/null
+++ b/snapshots_test.go
@@ -0,0 +1,83 @@
+package smb2
+
+import (
+	"encoding/binary"
+	"reflect"
+	"testing"
+	"time"
+	"unicode/utf16"
+
+	"github.com/cloudsoda/go-smb2/internal/smb2"
+)
+
+func snapshotArray(tokens ...string) []byte {
+	var u []uint16
+	for _, t := range tokens {
+		u = append(append(u, utf16.Encode([]rune(t))...), 0)
+	}
+	u = append(u, 0)
+	out := make([]byte, 12+2*len(u))
+	binary.LittleEndian.PutUint32(out[0:4], uint32(len(tokens)))
+	binary.LittleEndian.PutUint32(out[4:8], uint32(len(tokens)))
+	binary.LittleEndian.PutUint32(out[8:12], uint32(2*len(u)))
+	for i, c := range u {
+		binary.LittleEndian.PutUint16(out[12+2*i:], c)
+	}
+	return out
+}
+
+func TestParseSnapshotArray(t *testing.T) {
+	want := []string{"@GMT-2026.11.23-15.00.09", "@GMT-2026.11.23-14.00.09"}
+	got, ok := parseSnapshotArray(snapshotArray(want...))
+	if !ok || !reflect.DeepEqual(got, want) {
+		t.Fatalf("got %q, %v", got, ok)
+	}
+	if got, ok := parseSnapshotArray(snapshotArray()); !ok || len(got) != 0 {
+		t.Fatalf("an empty array: %q, %v", got, ok)
+	}
+	// A size beyond the bytes sent, and too short a response, are refused.
+	b := snapshotArray(want...)
+	binary.LittleEndian.PutUint32(b[8:12], uint32(len(b)))
+	if _, ok := parseSnapshotArray(b); ok {
+		t.Fatal("a size beyond the response was read")
+	}
+	if _, ok := parseSnapshotArray(b[:8]); ok {
+		t.Fatal("a short response was read")
+	}
+	// No more tokens than the server says it returned.
+	b = snapshotArray(want...)
+	binary.LittleEndian.PutUint32(b[4:8], 1)
+	if got, _ := parseSnapshotArray(b); !reflect.DeepEqual(got, want[:1]) {
+		t.Fatalf("got %q", got)
+	}
+}
+
+func TestTimewarp(t *testing.T) {
+	at := time.Date(2026, 11, 23, 15, 0, 9, 0, time.UTC)
+	p := make([]byte, timewarp(at).Size())
+	timewarp(at).Encode(p)
+	if string(p[16:20]) != "TWrp" || binary.LittleEndian.Uint32(p[12:16]) != 8 {
+		t.Fatalf("not a TWrp context: % x", p)
+	}
+	// 2026-11-23 15:00:09 UTC in 100 ns since 1601.
+	if got := binary.LittleEndian.Uint64(p[24:32]); got != 134399196090000000 {
+		t.Fatalf("FILETIME %d", got)
+	}
+}
+
+// The create contexts' length counts from the first context: a name whose
+// length is no multiple of 8 is followed by padding that is not part of it.
+func TestCreateContextsLength(t *testing.T) {
+	for _, name := range []string{"Sub", `Docs\letter.txt`, `Docs\new.txt`, ""} {
+		req := &smb2.CreateRequest{Name: name, Contexts: []smb2.Encoder{timewarp(time.Unix(0, 0))}}
+		pkt := make([]byte, req.Size())
+		req.Encode(pkt)
+		r := smb2.CreateRequestDecoder(pkt[64:])
+		if r.CreateContextsLength() != 32 {
+			t.Errorf("%q: contexts length %d, want 32", name, r.CreateContextsLength())
+		}
+		if off := r.CreateContextsOffset(); off%8 != 0 || string(pkt[off+16:off+20]) != "TWrp" {
+			t.Errorf("%q: the context is not at offset %d", name, off)
+		}
+	}
+}
```
<!-- diff:end -->
