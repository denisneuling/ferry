// Package cache keeps what ferry derives from a file — a thumbnail, what the
// file says about itself — in a directory, so that it is not made again
// after a restart or for the next person who may read the same file.
//
// It holds derived data only: never a file itself, a file name or a
// credential. An entry's name is a keyed hash of what identifies the file
// (share, path, size, time of the last write) and of the variant; its
// content is encrypted with a key derived from the same, so the directory
// alone — a backup, a disk taken out — shows neither names nor pictures.
// Whoever also has the key and knows a file's path and state can read its
// entry; docs/operations.md says so.
//
// The cache decides nothing about access: the caller asks for an entry only
// after the person's own session has opened the file for reading on the
// share. A changed file has another size or time and so another entry; old
// ones age out. Every failure of the disk is a miss, never a failed request.
package cache

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hkdf"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

// MaxEntry is the largest entry kept: a thumbnail of 640 pixels or a
// file's details are far below it, and a bound keeps a damaged directory
// from making ferry read a large file into memory.
const MaxEntry = 4 << 20

// MinKey is the shortest key accepted, in bytes.
const MinKey = 32

const (
	// diskTimeout bounds a read of an entry: a disk that hangs is a miss.
	diskTimeout = 2 * time.Second
	// queue is how many entries wait to be written; beyond it, new ones are
	// dropped rather than held in memory.
	queue = 64
	// version leads every entry, for a later change of the format.
	version = 1
	// sweepTo is the share of MaxBytes a sweep leaves, so that it does not
	// run again with the next entry.
	sweepTo = 0.9
)

// Key is what an entry belongs to. Kind and Variant say what was derived
// ("thumbnail", "320"; "metadata", ""); the rest identifies the file in the
// state it was derived from.
type Key struct {
	Kind, Variant string
	Share, Path   string
	Size          int64
	ModTime       time.Time
}

// Observer is told what the cache does, for the metrics. kind is Key.Kind;
// outcome is "hit", "miss" or "error".
type Observer interface {
	CacheRequest(kind, outcome string)
	CacheBytes(n int64)
	CacheEvicted(n int)
}

// Options configure a Cache.
type Options struct {
	Dir      string
	MaxBytes int64
	// Secret keys the names and the encryption; at least MinKey bytes.
	Secret []byte
	// Scope sets this cache's entries apart from another's in the same
	// directory: the SMB server's address, so that a ferry pointed at
	// another server finds none of the first one's.
	Scope    string
	Log      *slog.Logger
	Observer Observer
}

// Cache is a directory of entries. Its methods are safe for concurrent use.
type Cache struct {
	dir    string
	max    int64
	secret []byte
	scope  string
	log    *slog.Logger
	obs    Observer

	total   atomic.Int64 // bytes in entries, as far as known
	writes  chan write
	sweepMu sync.Mutex
	warned  atomic.Int64 // unix seconds of the last warning
	done    chan struct{}
	wg      sync.WaitGroup
}

type write struct {
	name string
	kind string
	data []byte
}

// Open prepares dir — creating it, removing what an interrupted write left
// — and starts the writer. It counts what the directory holds in the
// background and sweeps when that is too much.
func Open(o Options) (*Cache, error) {
	if len(o.Secret) < MinKey {
		return nil, errors.New("cache: the key is shorter than 32 bytes")
	}
	if o.MaxBytes <= 0 {
		return nil, errors.New("cache: no size")
	}
	if err := os.MkdirAll(filepath.Join(o.Dir, "tmp"), 0o700); err != nil {
		return nil, err
	}
	c := &Cache{
		dir: o.Dir, max: o.MaxBytes, secret: o.Secret, scope: o.Scope,
		log: o.Log, obs: o.Observer,
		writes: make(chan write, queue), done: make(chan struct{}),
	}
	if c.log == nil {
		c.log = slog.New(slog.DiscardHandler)
	}
	if c.obs == nil {
		c.obs = nopObserver{}
	}
	tmp, _ := os.ReadDir(filepath.Join(o.Dir, "tmp"))
	for _, e := range tmp {
		_ = os.Remove(filepath.Join(o.Dir, "tmp", e.Name()))
	}
	c.wg.Add(2)
	go c.writer()
	go func() {
		defer c.wg.Done()
		c.sweep()
	}()
	return c, nil
}

// Close stops the writer after the queued entries; it does not remove any.
func (c *Cache) Close() {
	close(c.done)
	c.wg.Wait()
}

// Get returns the entry for k, or false: none, a damaged one, or a disk too
// slow. A hit marks the entry as used, so that a sweep keeps it.
func (c *Cache) Get(k Key) ([]byte, bool) {
	name := c.name(k)
	type result struct {
		data []byte
		err  error
	}
	ch := make(chan result, 1)
	go func() {
		d, err := c.read(name, k)
		ch <- result{d, err}
	}()
	t := time.NewTimer(diskTimeout)
	defer t.Stop()
	select {
	case r := <-ch:
		switch {
		case r.err == nil:
			c.obs.CacheRequest(k.Kind, "hit")
			return r.data, true
		case errors.Is(r.err, fs.ErrNotExist):
			c.obs.CacheRequest(k.Kind, "miss")
		default:
			c.obs.CacheRequest(k.Kind, "error")
			c.warn("cache read failed", r.err)
		}
	case <-t.C:
		c.obs.CacheRequest(k.Kind, "error")
		c.warn("cache read failed", errors.New("timed out"))
	}
	return nil, false
}

// Put keeps data as the entry for k, written in the background; when the
// writer is behind, the entry is dropped.
func (c *Cache) Put(k Key, data []byte) {
	if len(data) == 0 || len(data) > MaxEntry {
		return
	}
	name := c.name(k)
	sealed, err := c.seal(name, k, data)
	if err != nil {
		return
	}
	select {
	case c.writes <- write{name: name, kind: k.Kind, data: sealed}:
	default:
	}
}

func (c *Cache) path(name string) string {
	return filepath.Join(c.dir, name[:2], name)
}

func (c *Cache) read(name string, k Key) ([]byte, error) {
	p := c.path(name)
	f, err := os.Open(p)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	sealed, err := io.ReadAll(io.LimitReader(f, MaxEntry+64))
	if err != nil {
		return nil, err
	}
	data, err := c.open(name, k, sealed)
	if err != nil {
		// Damaged, or from another key: it will be made again and replaced.
		_ = os.Remove(p)
		return nil, err
	}
	now := time.Now()
	_ = os.Chtimes(p, now, now) // the last use, for the sweep; atime may not be kept
	return data, nil
}

func (c *Cache) writer() {
	defer c.wg.Done()
	for {
		select {
		case w := <-c.writes:
			c.store(w)
		case <-c.done:
			for {
				select {
				case w := <-c.writes:
					c.store(w)
				default:
					return
				}
			}
		}
	}
}

// store writes an entry whole or not at all: into tmp, synced, renamed.
func (c *Cache) store(w write) {
	p := c.path(w.name)
	err := func() error {
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			return err
		}
		f, err := os.CreateTemp(filepath.Join(c.dir, "tmp"), "w")
		if err != nil {
			return err
		}
		_, err = f.Write(w.data)
		if err == nil {
			err = f.Sync()
		}
		if cerr := f.Close(); err == nil {
			err = cerr
		}
		if err == nil {
			err = os.Rename(f.Name(), p)
		}
		if err != nil {
			_ = os.Remove(f.Name())
		}
		return err
	}()
	if err != nil {
		c.obs.CacheRequest(w.kind, "error")
		c.warn("cache write failed", err)
		return
	}
	if c.total.Add(int64(len(w.data))) > c.max {
		c.sweep()
	}
	c.obs.CacheBytes(c.total.Load())
}

// sweep counts the entries and removes the least recently used until they
// take sweepTo of MaxBytes. One runs at a time; another asked for meanwhile
// is not needed.
func (c *Cache) sweep() {
	if !c.sweepMu.TryLock() {
		return
	}
	defer c.sweepMu.Unlock()
	type entry struct {
		path string
		size int64
		used time.Time
	}
	var all []entry
	var total int64
	_ = filepath.WalkDir(c.dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			// tmp holds writes in progress; wasm, beside the entries,
			// the previews' compiled code (FERRY_WASM_CACHE_DIR's
			// default), which is no entry and never evicted.
			if p != c.dir && (d.Name() == "tmp" || d.Name() == "wasm" && filepath.Dir(p) == c.dir) {
				return filepath.SkipDir
			}
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		all = append(all, entry{p, info.Size(), info.ModTime()})
		total += info.Size()
		return nil
	})
	evicted := 0
	if total > c.max {
		sort.Slice(all, func(i, j int) bool { return all[i].used.Before(all[j].used) })
		for _, e := range all {
			if float64(total) <= float64(c.max)*sweepTo {
				break
			}
			if os.Remove(e.path) == nil {
				total -= e.size
				evicted++
			}
		}
	}
	c.total.Store(total)
	c.obs.CacheBytes(total)
	if evicted > 0 {
		c.obs.CacheEvicted(evicted)
	}
}

// warn logs a failure of the disk at most once a minute: a full or
// read-only volume would otherwise write a line for every thumbnail. It
// never names an entry, which would not mean anything anyway.
func (c *Cache) warn(msg string, err error) {
	now := time.Now().Unix()
	last := c.warned.Load()
	if now-last < 60 || !c.warned.CompareAndSwap(last, now) {
		return
	}
	// The error of a file operation carries the entry's path: a hash, but
	// not worth logging; its cause is what helps.
	var pe *fs.PathError
	if errors.As(err, &pe) {
		err = pe.Err
	}
	c.log.Warn(msg, "error", err.Error())
}

// identity encodes a key unambiguously: each field with its length.
func (c *Cache) identity(k Key) []byte {
	var b bytes.Buffer
	for _, s := range []string{c.scope, k.Kind, k.Variant, k.Share, k.Path} {
		_ = binary.Write(&b, binary.BigEndian, uint32(len(s)))
		b.WriteString(s)
	}
	_ = binary.Write(&b, binary.BigEndian, k.Size)
	_ = binary.Write(&b, binary.BigEndian, k.ModTime.UnixNano())
	return b.Bytes()
}

// name is the entry's file name: HMAC-SHA256 of its identity under the
// secret, in hex.
func (c *Cache) name(k Key) string {
	m := hmac.New(sha256.New, c.secret)
	m.Write([]byte("name\x00"))
	m.Write(c.identity(k))
	return hex.EncodeToString(m.Sum(nil))
}

// aead is AES-256-GCM under a key derived from the secret and the entry's
// identity: an entry opens only for the file and state it was made from.
func (c *Cache) aead(k Key) (cipher.AEAD, error) {
	key, err := hkdf.Key(sha256.New, c.secret, nil, "ferry cache v1\x00"+string(c.identity(k)), 32)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

// seal is version, nonce, then the sealed data, the name as associated data.
func (c *Cache) seal(name string, k Key, data []byte) ([]byte, error) {
	a, err := c.aead(k)
	if err != nil {
		return nil, err
	}
	out := make([]byte, 1+a.NonceSize(), 1+a.NonceSize()+len(data)+a.Overhead())
	out[0] = version
	if _, err := rand.Read(out[1:]); err != nil {
		return nil, err
	}
	return a.Seal(out, out[1:1+a.NonceSize()], data, []byte(name)), nil
}

func (c *Cache) open(name string, k Key, sealed []byte) ([]byte, error) {
	a, err := c.aead(k)
	if err != nil {
		return nil, err
	}
	if len(sealed) < 1+a.NonceSize()+a.Overhead() || sealed[0] != version {
		return nil, errors.New("cache: not an entry")
	}
	n := 1 + a.NonceSize()
	return a.Open(nil, sealed[1:n], sealed[n:], []byte(name))
}

type nopObserver struct{}

func (nopObserver) CacheRequest(string, string) {}
func (nopObserver) CacheBytes(int64)            {}
func (nopObserver) CacheEvicted(int)            {}
