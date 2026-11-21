package smb

import (
	"encoding/binary"
	"testing"
)

// frame builds a Direct TCP frame holding an SMB2 SESSION_SETUP response with
// the given status and session flags.
func frame(status uint32, sessionFlags uint16) []byte {
	m := make([]byte, smb2HeaderSize+8)
	copy(m, "\xfeSMB")
	le := binary.LittleEndian
	le.PutUint16(m[4:], smb2HeaderSize)
	le.PutUint32(m[8:], status)
	le.PutUint16(m[12:], smb2CommandSessionSetup)
	le.PutUint32(m[16:], smb2FlagServerToRedir)
	le.PutUint16(m[smb2HeaderSize:], 9)
	le.PutUint16(m[smb2HeaderSize+2:], sessionFlags)
	out := []byte{0, byte(len(m) >> 16), byte(len(m) >> 8), byte(len(m))}
	return append(out, m...)
}

func negotiate() []byte {
	f := frame(0, 0)
	binary.LittleEndian.PutUint16(f[4+12:], 0x0000) // NEGOTIATE
	return f
}

const statusMoreProcessing = 0xC0000016

func TestSetupGuard(t *testing.T) {
	cases := []struct {
		name   string
		stream [][]byte
		want   bool
	}{
		{"user session", [][]byte{negotiate(), frame(statusMoreProcessing, 0), frame(0, 0)}, true},
		{"guest session", [][]byte{negotiate(), frame(statusMoreProcessing, 0), frame(0, sessionFlagIsGuest)}, false},
		{"anonymous session", [][]byte{negotiate(), frame(statusMoreProcessing, 0), frame(0, sessionFlagIsNull)}, false},
		// The challenge carries no guest flag; only the final response counts.
		{"guest flag only on the final response", [][]byte{frame(statusMoreProcessing, 0), frame(0, sessionFlagIsGuest)}, false},
		{"encrypt-data flag is fine", [][]byte{frame(statusMoreProcessing, 0), frame(0, 0x0004)}, true},
		{"no successful setup seen", [][]byte{negotiate(), frame(statusMoreProcessing, 0)}, false},
		{"nothing at all", nil, false},
		{"not a direct TCP frame", [][]byte{{1, 0, 0, 4, 0, 0, 0, 0}, frame(0, 0)}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			g := &setupGuard{}
			for _, b := range tc.stream {
				g.observe(b)
			}
			if got := g.stop(); got != tc.want {
				t.Fatalf("stop() = %v, want %v", got, tc.want)
			}
		})
	}
}

// A response split across reads at every possible byte is still read.
func TestSetupGuardSplitReads(t *testing.T) {
	stream := append(frame(statusMoreProcessing, 0), frame(0, sessionFlagIsGuest)...)
	for cut := 1; cut < len(stream); cut++ {
		g := &setupGuard{}
		g.observe(stream[:cut])
		g.observe(stream[cut:])
		if g.stop() {
			t.Fatalf("cut at %d: guest session accepted", cut)
		}
	}
}

func TestSetupGuardOverflow(t *testing.T) {
	g := &setupGuard{}
	// A frame header promising more than the guard buffers, then the bytes.
	big := make([]byte, guardMaxBuffered+1)
	big[1] = 0xff
	g.observe(big)
	g.observe(frame(0, 0))
	if g.stop() {
		t.Fatal("accepted after overflowing its buffer")
	}
}

func TestSetupGuardStopsLooking(t *testing.T) {
	g := &setupGuard{}
	g.observe(frame(0, 0))
	if !g.stop() {
		t.Fatal("user session refused")
	}
	g.observe(frame(0, sessionFlagIsGuest))
	if g.buf != nil {
		t.Fatal("still buffering after stop")
	}
}
