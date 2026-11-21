package api

import (
	"context"
	"sync"
	"time"
)

// watchdog is a context that ends unless it is kicked in time. Transfers use
// it instead of a deadline: a download of a large file may take hours, but
// it must not sit for a minute without moving a byte, whether the client
// stopped reading or the SMB server stopped answering.
//
// It matters because go-smb2 binds an open file to the context it was opened
// with: the file's reads and writes all use that one context, so it has to
// be the transfer's, and it has to end when the transfer stalls.
type watchdog struct {
	ctx    context.Context
	cancel context.CancelFunc

	mu    sync.Mutex
	timer *time.Timer
}

// newWatchdog derives a context from parent that ends after d unless kicked.
func newWatchdog(parent context.Context, d time.Duration) *watchdog {
	ctx, cancel := context.WithCancel(parent)
	w := &watchdog{ctx: ctx, cancel: cancel}
	w.timer = time.AfterFunc(d, cancel)
	return w
}

// kick gives the transfer another d.
func (w *watchdog) kick(d time.Duration) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.timer.Reset(d)
}

// stop ends the context.
func (w *watchdog) stop() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.timer.Stop()
	w.cancel()
}
