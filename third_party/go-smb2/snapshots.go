package smb2

import (
	"encoding/binary"
	"os"
	"time"
	"unicode/utf16"

	"github.com/cloudsoda/go-smb2/internal/smb2"
)

// maxSnapshotArray bounds the snapshot list a server may send: some 20000
// tokens, far beyond any retention.
const maxSnapshotArray = 1 << 20

// Snapshots returns the previous versions the server offers for what f is
// open on, as @GMT tokens ("@GMT-2006.01.02-15.04.05", UTC), newest first
// as the server sorts them, with FSCTL_SRV_ENUMERATE_SNAPSHOTS ([MS-SMB2]
// 3.3.5.15.1, [MS-SMB] 2.2.7.2.2.1). A server without snapshots returns
// none. OpenAt opens a path as it was at one of them.
func (f *File) Snapshots() ([]string, error) {
	f.m.Lock()
	defer f.m.Unlock()

	// The first call has room for the counts only, and so learns the size
	// of the whole array; the second fetches it.
	out, err := f.ioctl(&smb2.IoctlRequest{
		CtlCode:           smb2.FSCTL_SRV_ENUMERATE_SNAPSHOTS,
		MaxOutputResponse: 16,
		Flags:             smb2.SMB2_0_IOCTL_IS_FSCTL,
	})
	if err != nil {
		return nil, &os.PathError{Op: "snapshots", Path: f.name, Err: err}
	}
	if len(out) < 12 {
		return nil, &os.PathError{Op: "snapshots", Path: f.name, Err: &InvalidResponseError{"broken snapshot array"}}
	}
	if binary.LittleEndian.Uint32(out[0:4]) == 0 {
		return nil, nil
	}
	size := binary.LittleEndian.Uint32(out[8:12])
	if size > maxSnapshotArray {
		return nil, &os.PathError{Op: "snapshots", Path: f.name, Err: &InvalidResponseError{"snapshot array too large"}}
	}
	out, err = f.ioctl(&smb2.IoctlRequest{
		CtlCode:           smb2.FSCTL_SRV_ENUMERATE_SNAPSHOTS,
		MaxOutputResponse: 12 + size,
		Flags:             smb2.SMB2_0_IOCTL_IS_FSCTL,
	})
	if err != nil {
		return nil, &os.PathError{Op: "snapshots", Path: f.name, Err: err}
	}
	tokens, ok := parseSnapshotArray(out)
	if !ok {
		return nil, &os.PathError{Op: "snapshots", Path: f.name, Err: &InvalidResponseError{"broken snapshot array"}}
	}
	return tokens, nil
}

// parseSnapshotArray reads an SRV_SNAPSHOT_ARRAY: three counts, then the
// tokens in UTF-16LE, each ending in a null, the list in another.
func parseSnapshotArray(out []byte) ([]string, bool) {
	if len(out) < 12 {
		return nil, false
	}
	returned := binary.LittleEndian.Uint32(out[4:8])
	size := binary.LittleEndian.Uint32(out[8:12])
	if uint64(size) > uint64(len(out)-12) {
		return nil, false
	}
	b := out[12 : 12+size]
	u := make([]uint16, 0, len(b)/2)
	for i := 0; i+1 < len(b); i += 2 {
		u = append(u, binary.LittleEndian.Uint16(b[i:]))
	}
	var tokens []string
	start := 0
	for i, c := range u {
		if uint32(len(tokens)) == returned {
			break
		}
		if c != 0 {
			continue
		}
		if i > start {
			tokens = append(tokens, string(utf16.Decode(u[start:i])))
		}
		start = i + 1
	}
	return tokens, true
}

// OpenAt opens name for reading as it was in the snapshot taken at t,
// with the SMB2_CREATE_TIMEWARP_TOKEN create context ([MS-SMB2]
// 2.2.13.2.7); t is the time of a token Snapshots returned. The handle is
// read-only, as a snapshot is; a folder opened this way lists the
// snapshot's entries.
//
// A server may resolve the time against the list it last enumerated on
// this connection (Samba's shadow_copy2 does when snapshot names carry
// more than the time): call Snapshots on the same path first.
func (fs *Share) OpenAt(name string, t time.Time) (*File, error) {
	name = normPath(name)

	if err := validatePath("open", name, false); err != nil {
		return nil, err
	}

	req := &smb2.CreateRequest{
		SecurityFlags:        0,
		RequestedOplockLevel: smb2.SMB2_OPLOCK_LEVEL_NONE,
		ImpersonationLevel:   smb2.Impersonation,
		SmbCreateFlags:       0,
		DesiredAccess:        smb2.GENERIC_READ,
		FileAttributes:       smb2.FILE_ATTRIBUTE_NORMAL,
		ShareAccess:          smb2.FILE_SHARE_READ | smb2.FILE_SHARE_WRITE,
		CreateDisposition:    smb2.FILE_OPEN,
		CreateOptions:        smb2.FILE_SYNCHRONOUS_IO_NONALERT,
		Mapping:              fs.mapping,
		Contexts:             []smb2.Encoder{timewarp(t)},
	}

	f, err := fs.createFile(name, req, true)
	if err != nil {
		return nil, &os.PathError{Op: "open", Path: name, Err: err}
	}

	return f, nil
}

// timewarp is the TWrp create context: a snapshot's time as a FILETIME.
type timewarp time.Time

func (timewarp) Size() int { return 32 }

func (t timewarp) Encode(p []byte) {
	le := binary.LittleEndian
	le.PutUint32(p[0:4], 0)    // Next: the request sets it when another follows
	le.PutUint16(p[4:6], 16)   // NameOffset
	le.PutUint16(p[6:8], 4)    // NameLength
	le.PutUint16(p[8:10], 0)   // Reserved
	le.PutUint16(p[10:12], 24) // DataOffset
	le.PutUint32(p[12:16], 8)  // DataLength
	copy(p[16:20], "TWrp")
	clear(p[20:24]) // the name padded to 8 bytes
	le.PutUint64(p[24:32], filetime(time.Time(t)))
}

// filetime is t in 100-nanosecond intervals since 1601-01-01 UTC.
func filetime(t time.Time) uint64 {
	return uint64(t.UnixNano()/100 + 116444736000000000)
}
