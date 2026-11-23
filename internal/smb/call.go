package smb

import (
	"context"
	"time"

	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/denisneuling/ferry/internal/tracing"
)

// defaultCallTimeout bounds one SMB call when LoginOptions says nothing.
const defaultCallTimeout = 60 * time.Second

// A call is one operation's time with the SMB connection.
//
// go-smb2 must never see a context end in the middle of a request. It encodes
// every request into one buffer the connection shares; when a request's
// context ends after the packet went to the writer but before the write is
// done, the call returns and gives up the connection's lock while the writer
// still sends from that buffer — and the next request overwrites it. The
// server then sees a garbled or duplicate message ID and drops the
// connection (Samba logs "duplicate message_id"), and the session, which
// never reconnects, is over. A browser abandons requests all the time — a
// thumbnail scrolled away, a folder left before its listing came — so the
// request's context is the wrong one to hand go-smb2.
//
// A call therefore waits for the session's request slot under the request's
// context — waiting touches no connection — and then gives go-smb2 a
// context of its own that the request's end does not reach. A client that
// goes away stops the operation between steps (canceled), never inside one.
// What does end a call inside a step is the watchdog: a step the server does
// not answer within the call timeout. Then the connection cannot be trusted
// any more, and the session is ended deliberately (abandon).
type call struct {
	s      *Session
	parent context.Context
	ctx    context.Context
	cancel context.CancelFunc
	timer  *time.Timer
	span   trace.Span
}

// start takes the session's request slot and begins a call, traced as op
// (smb.readdir, smb.rename, …) — the operation's name and nothing it acts on.
// The caller ends it.
func (s *Session) start(parent context.Context, op string) (*call, error) {
	if err := s.begin(parent); err != nil {
		return nil, err
	}
	sctx, span := tracing.Tracer().Start(parent, op)
	ctx, cancel := context.WithCancel(context.WithoutCancel(sctx))
	c := &call{s: s, parent: parent, ctx: ctx, cancel: cancel, span: span}
	c.timer = time.AfterFunc(s.callTimeout, func() {
		s.abandon()
		cancel()
	})
	return c, nil
}

// kick gives a long operation — a big listing, a recursive delete — another
// call timeout for its next step.
func (c *call) kick() { c.timer.Reset(c.s.callTimeout) }

// canceled reports, between steps, that the request has ended.
func (c *call) canceled() error {
	if err := c.parent.Err(); err != nil {
		return classify(err)
	}
	return nil
}

func (c *call) end() {
	c.timer.Stop()
	c.cancel()
	c.span.End()
	c.s.release()
}

// span begins a span for an operation that is not one call — a login, an
// open, an upload — and returns what ends it, marking it failed with the
// error's kind, never its text.
func span(ctx context.Context, op string) (context.Context, func(error)) {
	ctx, sp := tracing.Tracer().Start(ctx, op)
	return ctx, func(err error) {
		if err != nil {
			sp.SetStatus(codes.Error, KindOf(err).String())
		}
		sp.End()
	}
}

// watch runs one step under a context the request does not reach, cancelling
// it — and ending the session — only if the server does not answer within
// the call timeout. Files use it: go-smb2 binds a file to one context for all
// its calls.
func (s *Session) watch(cancel context.CancelFunc, step func() error) error {
	t := time.AfterFunc(s.callTimeout, func() {
		s.abandon()
		cancel()
	})
	defer t.Stop()
	return step()
}

// abandon ends a session whose connection can no longer be trusted: a call
// was cancelled in the middle of a request. Closing the connection makes
// every call still waiting on it return.
func (s *Session) abandon() {
	s.mu.Lock()
	s.lost = true
	s.mu.Unlock()
	_ = s.conn.Close()
}
