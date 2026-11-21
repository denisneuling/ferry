package smb

import (
	"encoding/binary"
	"net"
	"sync"
)

// SMB2 constants the guard reads from the wire. They are MS-SMB2's, repeated
// here because go-smb2 keeps its own in an internal package.
const (
	smb2HeaderSize          = 64
	smb2CommandSessionSetup = 0x0001
	smb2FlagServerToRedir   = 0x00000001
	sessionFlagIsGuest      = 0x0001
	sessionFlagIsNull       = 0x0002

	// The session setup is a handful of messages of a few hundred bytes. A
	// server that sends more than this before the setup is over is not one
	// the guard can vouch for, and the login fails.
	guardMaxBuffered = 64 << 10
)

// setupGuard watches the bytes a server sends while the SMB session is set up
// and records the session flags of the final, successful SESSION_SETUP
// response.
//
// It exists because go-smb2 cannot answer the one question ferry's login
// depends on: did the server authenticate this user, or did it map the
// attempt to a guest or anonymous session? go-smb2 keeps the session flags
// unexported, and its own guest check (under RequireMessageSigning) reads them
// from the first response of the NTLM exchange — the challenge — which never
// carries the guest flag. Against a Samba with `map to guest = Bad User`, an
// unknown user therefore gets a working session from go-smb2. The guard reads
// the flags from the response that does carry them.
//
// The guard only parses until the login is decided (stop); afterwards it
// passes bytes through untouched.
type setupGuard struct {
	net.Conn

	mu        sync.Mutex
	stopped   bool
	buf       []byte
	overflow  bool
	malformed bool
	// flags of the last SESSION_SETUP response with STATUS_SUCCESS, and
	// whether one was seen at all.
	flags uint16
	seen  bool
}

func newSetupGuard(c net.Conn) *setupGuard {
	return &setupGuard{Conn: c}
}

func (g *setupGuard) Read(p []byte) (int, error) {
	n, err := g.Conn.Read(p)
	if n > 0 {
		g.observe(p[:n])
	}
	return n, err
}

func (g *setupGuard) observe(b []byte) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.stopped || g.overflow || g.malformed {
		return
	}
	if len(g.buf)+len(b) > guardMaxBuffered {
		g.overflow = true
		g.buf = nil
		return
	}
	g.buf = append(g.buf, b...)
	g.parse()
}

// parse consumes every complete Direct TCP frame in buf: a zero byte, a
// 24-bit big-endian length, and that many bytes of SMB2 message.
func (g *setupGuard) parse() {
	for len(g.buf) >= 4 {
		if g.buf[0] != 0 {
			g.malformed = true
			return
		}
		size := int(g.buf[1])<<16 | int(g.buf[2])<<8 | int(g.buf[3])
		if len(g.buf) < 4+size {
			return
		}
		g.message(g.buf[4 : 4+size])
		g.buf = g.buf[4+size:]
	}
}

// message looks at one SMB2 message. Anything that is not a plain SMB2
// response — a transform (encrypted) or compressed header, a request — is
// ignored: neither occurs before the session exists.
func (g *setupGuard) message(m []byte) {
	if len(m) < smb2HeaderSize+4 || m[0] != 0xFE || m[1] != 'S' || m[2] != 'M' || m[3] != 'B' {
		return
	}
	le := binary.LittleEndian
	status := le.Uint32(m[8:12])
	command := le.Uint16(m[12:14])
	flags := le.Uint32(m[16:20])
	if command != smb2CommandSessionSetup || flags&smb2FlagServerToRedir == 0 {
		return
	}
	// STATUS_MORE_PROCESSING_REQUIRED is the challenge; only the final
	// response's flags describe the session.
	if status != 0 {
		return
	}
	// SESSION_SETUP response body: StructureSize (2), SessionFlags (2), ...
	g.flags = le.Uint16(m[smb2HeaderSize+2 : smb2HeaderSize+4])
	g.seen = true
}

// stop ends the parsing and reports whether the server authenticated the
// session as a named user: a successful SESSION_SETUP response was seen, it
// was neither guest nor anonymous, and nothing got in the way of reading it.
func (g *setupGuard) stop() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.stopped = true
	g.buf = nil
	return g.seen && !g.overflow && !g.malformed &&
		g.flags&(sessionFlagIsGuest|sessionFlagIsNull) == 0
}
