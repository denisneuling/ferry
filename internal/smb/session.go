// Package smb is ferry's SMB client: it logs a person in by setting up an SMB
// session with their credentials, and performs file operations on that
// session. It knows nothing of HTTP.
//
// Every error it returns is an *Error with a kind and an NT status and
// nothing else (errors.go), so that no credential, user name or path can leave
// through an error.
package smb

import (
	"context"
	"errors"
	"io"
	"net"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	smb2 "github.com/cloudsoda/go-smb2"
)

// Credentials are what the person typed. They are used for one session setup
// and not kept: neither Login nor the Session it returns holds on to them.
type Credentials struct {
	User     string
	Password string
	// Domain is optional; a standalone Samba ignores it.
	Domain string
}

// LoginOptions say where and how long to try.
type LoginOptions struct {
	// Server is host:port of the SMB server.
	Server string
	// Timeout bounds the TCP connection, the negotiation and the session
	// setup together. Zero means 10 seconds.
	Timeout time.Duration
	// CallTimeout bounds each SMB call of the session; one the server does
	// not answer within it ends the session (call.go). Zero means a minute.
	CallTimeout time.Duration
	// Connections is how many SMB sessions LoginGroup sets up, each on its
	// own connection (group.go). Zero or one means one. Login ignores it.
	Connections int
	// Observer hears of the connections, for the metrics; nil hears nothing.
	Observer Observer
}

const defaultLoginTimeout = 10 * time.Second

// Login connects to the server and sets up an SMB session with exactly these
// credentials. It succeeds only if the server authenticated them as a named
// user: a guest or anonymous session is closed again and reported as
// KindGuest, however the server came to offer it.
func Login(ctx context.Context, opts LoginOptions, cred Credentials) (_ *Session, err error) {
	ctx, done := span(ctx, "smb.login")
	defer func() { done(err) }()
	// An empty user name asks for an anonymous session by definition. It is
	// refused before the server is asked, so that no server configuration can
	// turn it into a session.
	if cred.User == "" || cred.Password == "" {
		return nil, &Error{Kind: KindAuth}
	}
	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = defaultLoginTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	var nd net.Dialer
	tcp, err := nd.DialContext(ctx, "tcp", opts.Server)
	if err != nil {
		return nil, &Error{Kind: KindUnreachable}
	}
	// go-smb2 honours the context for each request it sends, but a server
	// that accepts the connection and never answers must not hold the login
	// either: the deadline covers the reads go-smb2 makes in the background.
	deadline, _ := ctx.Deadline()
	_ = tcp.SetDeadline(deadline)

	guard := newSetupGuard(tcp)
	d := &smb2.Dialer{
		Initiator: &smb2.NTLMInitiator{User: cred.User, Password: cred.Password, Domain: cred.Domain},
	}
	s, err := d.DialConn(ctx, guard, opts.Server)
	// The guard is consulted before anything else happens on the
	// connection, and stops looking either way.
	authenticated := guard.stop()
	if err != nil {
		_ = tcp.Close()
		e := classify(err)
		// go-smb2 reports a refused session setup as an InvalidResponseError
		// whose text names the status; classify cannot read it and says
		// internal. Whatever the server said, no session came of it.
		if e.Kind == KindInternal || e.Kind == KindSessionLost {
			e = &Error{Kind: KindAuth, Status: e.Status}
		}
		return nil, e
	}
	if !authenticated {
		_ = s.Logoff()
		_ = tcp.Close()
		return nil, &Error{Kind: KindGuest}
	}
	_ = tcp.SetDeadline(time.Time{})
	callTimeout := opts.CallTimeout
	if callTimeout <= 0 {
		callTimeout = defaultCallTimeout
	}
	return &Session{s: s, conn: tcp, callTimeout: callTimeout, op: make(chan struct{}, 1), mounts: map[string]*smb2.Share{},
		obs: opts.Observer, pool: PoolInteractive}, nil
}

// Session is one authenticated SMB session on its own connection. It is safe
// for concurrent use. It never reconnects: that would need the password
// again, which it does not have. Once the connection is gone, every call
// returns KindSessionLost and Lost reports true.
type Session struct {
	s           *smb2.Session
	conn        net.Conn
	callTimeout time.Duration

	// op admits one SMB request at a time on this session. go-smb2 cannot
	// be trusted with concurrent requests: when its credit balance runs
	// short it sends QUERY_DIRECTORY and QUERY_INFO with a smaller credit
	// charge but the full output buffer, and the server rejects them
	// (STATUS_INVALID_PARAMETER, "CreditCharge too low"). With one request
	// in flight the balance is always whole. A session is one person, so
	// this costs little; see docs/spike.md for the measurements.
	// It is a channel rather than a mutex so that waiting for it honours
	// the request's context.
	op chan struct{}
	// obs hears how long requests wait for op, under the pool this
	// session serves in its group.
	obs  Observer
	pool string

	mu     sync.Mutex
	mounts map[string]*smb2.Share
	lost   bool
	closed bool
}

// Lost reports whether the session can no longer be used.
func (s *Session) Lost() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lost || s.closed
}

// wentAway reports whether the connection was lost rather than closed by
// ferry.
func (s *Session) wentAway() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lost
}

// Close logs off and closes the connection. It is idempotent.
func (s *Session) Close() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	mounts := s.mounts
	s.mounts = nil
	s.mu.Unlock()

	// Logging off is a courtesy to the server; it must not hold up a logout
	// on a server that stopped answering, or behind a request that is stuck.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if s.acquire(ctx) != nil {
		return s.conn.Close()
	}
	defer s.release()
	for _, m := range mounts {
		_ = m.WithContext(ctx).Umount()
	}
	_ = s.s.WithContext(ctx).Logoff()
	return s.conn.Close()
}

// fail classifies err and marks the session lost when the connection is gone.
func (s *Session) fail(err error) error {
	e := classify(err)
	if e.Kind == KindSessionLost {
		s.mu.Lock()
		s.lost = true
		s.mu.Unlock()
	}
	return e
}

// acquire waits for the session's one request slot (see Session.op).
func (s *Session) acquire(ctx context.Context) error {
	// An ended context loses even against a free slot: select would pick
	// between the two at random.
	if err := ctx.Err(); err != nil {
		return classify(err)
	}
	select {
	case s.op <- struct{}{}:
		return nil
	case <-ctx.Done():
		return classify(ctx.Err())
	}
}

func (s *Session) release() { <-s.op }

// begin checks the session is usable and takes its request slot; the caller
// releases it.
func (s *Session) begin(ctx context.Context) error {
	if err := s.usable(); err != nil {
		return err
	}
	start := time.Now()
	if err := s.acquire(ctx); err != nil {
		return err
	}
	if s.obs != nil {
		s.obs.SlotWait(s.pool, time.Since(start))
	}
	if err := s.usable(); err != nil {
		s.release()
		return err
	}
	return nil
}

func (s *Session) usable() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.lost || s.closed {
		return &Error{Kind: KindSessionLost}
	}
	return nil
}

// Share is a share as the server lists it.
type Share struct {
	Name    string
	Comment string
}

// Shares lists the disk shares the server enumerates for this session
// (srvsvc NetShareEnum), without IPC$, printers and names ending in `$`,
// sorted by name.
//
// The server may list shares the user cannot open. Shares does not try them:
// whether a share opens is decided when it is opened, by the server.
func (s *Session) Shares(ctx context.Context) ([]Share, error) {
	c, err := s.start(ctx, "smb.shares")
	if err != nil {
		return nil, err
	}
	defer c.end()
	infos, err := s.s.WithContext(c.ctx).ListShares()
	if err != nil {
		return nil, s.fail(err)
	}
	out := make([]Share, 0, len(infos))
	for _, in := range infos {
		if in.Type() != smb2.ShareTypeDiskTree || in.IsSpecial() || strings.HasSuffix(in.Name, "$") {
			continue
		}
		out = append(out, Share{Name: in.Name, Comment: in.Comment})
	}
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name) })
	return out, nil
}

// mount returns the tree connect for a share, making it on first use, bound
// to ctx — a call's, never a request's (call.go). The caller holds the
// request slot.
func (s *Session) mount(ctx context.Context, name string) (*smb2.Share, error) {
	if !validShareName(name) {
		return nil, &Error{Kind: KindInvalidPath}
	}
	s.mu.Lock()
	m, ok := s.mounts[name]
	s.mu.Unlock()
	if ok {
		return m.WithContext(ctx), nil
	}
	m, err := s.s.WithContext(ctx).Mount(name)
	if err != nil {
		return nil, s.fail(err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		_ = m.Umount()
		return nil, &Error{Kind: KindSessionLost}
	}
	s.mounts[name] = m
	return m.WithContext(ctx), nil
}

// Entry is one name in a folder.
type Entry struct {
	Name    string
	Dir     bool
	Size    int64
	ModTime time.Time
}

// ReadDir lists a folder whole: at most max entries (a bound of ferry's own,
// KindLimit beyond it), unsorted — sorting is the interface's. A listing
// that is written out as it is read uses OpenDir instead, and holds one
// batch at a time rather than the folder.
func (s *Session) ReadDir(ctx context.Context, share, p string, max int) (_ []Entry, err error) {
	d, err := s.OpenDir(ctx, share, p, max)
	if err != nil {
		return nil, err
	}
	defer func() { _ = d.Close() }()
	var out []Entry
	for {
		batch, err := d.Next()
		out = append(out, batch...)
		if errors.Is(err, io.EOF) {
			return out, nil
		}
		if err != nil {
			return nil, err
		}
	}
}

// File is an open file for reading: an io.ReadSeekCloser with ReadAt, for
// http.ServeContent and Range requests.
//
// go-smb2 binds a file to one context for all its calls, Close included. A
// File's is its own, detached from the request that opened it (call.go):
// the request's end stops a download between reads, never inside one, and
// the handle can always be closed — a video's every seek aborts a download.
// Each call on the file has the call timeout's watchdog.
type File struct {
	f       *smb2.File
	sess    *Session
	parent  context.Context    // the opening request's: checked between reads
	cancel  context.CancelFunc // ends the file's own context
	Size    int64
	ModTime time.Time
}

// Open opens a file for reading. A folder is KindIsDir.
func (s *Session) Open(ctx context.Context, share, p string) (*File, error) {
	return s.open(ctx, share, p, time.Time{})
}

// OpenAt opens a file for reading as it was in the snapshot taken at at
// (versions.go); a time that is no snapshot of the path is KindNotFound.
func (s *Session) OpenAt(ctx context.Context, share, p string, at time.Time) (*File, error) {
	return s.open(ctx, share, p, at)
}

func (s *Session) open(ctx context.Context, share, p string, at time.Time) (_ *File, err error) {
	ctx, done := span(ctx, "smb.open")
	defer func() { done(err) }()
	sp, err := CleanPath(p)
	if err != nil {
		return nil, err
	}
	if sp == "" {
		return nil, &Error{Kind: KindIsDir}
	}
	if err := s.begin(ctx); err != nil {
		return nil, err
	}
	defer s.release()
	fctx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	var f *smb2.File
	var fi os.FileInfo
	err = s.watch(cancel, func() error {
		m, err := s.mount(fctx, share)
		if err != nil {
			return err
		}
		if at.IsZero() {
			if f, err = m.OpenFile(sp, os.O_RDONLY, 0); err != nil {
				return s.fail(err)
			}
		} else if f, err = s.openAt(m, sp, at); err != nil {
			return err
		}
		if fi, err = f.Stat(); err != nil {
			_ = f.Close()
			return s.fail(err)
		}
		if fi.IsDir() {
			_ = f.Close()
			return &Error{Kind: KindIsDir}
		}
		return nil
	})
	if err != nil {
		cancel()
		return nil, err
	}
	return &File{f: f, sess: s, parent: ctx, cancel: cancel, Size: fi.Size(), ModTime: fi.ModTime()}, nil
}

// step runs one call on the file: the session's request slot taken under the
// opening request's context — so a request that ended stops here, before the
// call — and the call itself under the file's own, watched.
func (f *File) step(parent context.Context, call func() error) error {
	if err := f.sess.begin(parent); err != nil {
		return err
	}
	defer f.sess.release()
	return f.sess.watch(f.cancel, call)
}

func (f *File) Read(p []byte) (n int, err error) {
	serr := f.step(f.parent, func() error {
		n, err = f.f.Read(p)
		return nil
	})
	if serr != nil {
		return 0, serr
	}
	return n, f.err(err)
}

func (f *File) ReadAt(p []byte, off int64) (n int, err error) {
	serr := f.step(f.parent, func() error {
		n, err = f.f.ReadAt(p, off)
		return nil
	})
	if serr != nil {
		return 0, serr
	}
	return n, f.err(err)
}

// StreamChunk is the size of the reads WriteTo makes. Throughput depends on
// it far more than on anything else: against Samba, 32 KiB reads — what
// net/http and io.Discard read with — reach a quarter of what 1 MiB reads do
// (docs/spike.md).
const StreamChunk = 1 << 20

// WriteTo copies the rest of the file to w in StreamChunk reads, taking the
// session's request slot for each read only, so that a download does not
// hold up the same person's browsing. io.Copy uses it.
func (f *File) WriteTo(w io.Writer) (int64, error) {
	buf := make([]byte, StreamChunk)
	var n int64
	for {
		nr, er := f.Read(buf)
		if nr > 0 {
			nw, ew := w.Write(buf[:nr])
			n += int64(nw)
			if ew != nil {
				return n, ew
			}
			if nw != nr {
				return n, io.ErrShortWrite
			}
		}
		if er == io.EOF {
			return n, nil
		}
		if er != nil {
			return n, er
		}
	}
}

// Seek only moves the offset; it sends nothing for io.SeekStart and
// io.SeekCurrent, but io.SeekEnd asks the server for the size.
func (f *File) Seek(offset int64, whence int) (n int64, err error) {
	serr := f.step(f.parent, func() error {
		n, err = f.f.Seek(offset, whence)
		return nil
	})
	if serr != nil {
		return 0, serr
	}
	return n, f.err(err)
}

// closeWait bounds how long Close waits for the session's request slot.
const closeWait = 10 * time.Second

// Close closes the handle on the server, also after the opening request
// ended: the file's context is its own, so an aborted download still closes
// its handle.
func (f *File) Close() error {
	defer f.cancel()
	ctx, cancel := context.WithTimeout(context.Background(), closeWait)
	defer cancel()
	var err error
	if serr := f.step(ctx, func() error { err = f.f.Close(); return nil }); serr != nil {
		return serr
	}
	return f.err(err)
}

// err keeps io.EOF as it is — readers depend on it — and classifies the rest.
func (f *File) err(err error) error {
	switch err {
	case nil:
		return nil
	case io.EOF:
		return io.EOF
	}
	return f.sess.fail(err)
}
