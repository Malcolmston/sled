package sled

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
)

// DB is an embedded, transactional key/value store backed by an append-only
// write-ahead log. A DB is safe for concurrent use: a single writer is
// serialized internally while any number of readers proceed concurrently
// against immutable snapshots. See the package documentation for the full
// concurrency and durability model.
type DB struct {
	// mu serializes all writers (single-writer model) and protects the log
	// file and the mutable bookkeeping fields below.
	mu   sync.Mutex
	log  *os.File
	opts options
	path string

	// root is the current published index snapshot. It is read by readers with
	// an atomic load (no lock) and replaced by the writer with an atomic store.
	root atomic.Pointer[node]

	closed atomic.Bool
}

// Open opens (creating it if necessary) the database whose write-ahead log
// lives at path. The log is replayed to rebuild the in-memory index, stopping
// at the first truncated or corrupt record so that a crash mid-write loses only
// the incomplete tail. Any such partial tail is physically truncated from the
// file before Open returns.
func Open(path string, opts ...Option) (*DB, error) {
	o := defaultOptions()
	for _, fn := range opts {
		fn(&o)
	}

	if dir := filepath.Dir(path); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("sled: create directory: %w", err)
		}
	}

	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, o.fileMode)
	if err != nil {
		return nil, fmt.Errorf("sled: open log: %w", err)
	}

	db := &DB{log: f, opts: o, path: path}

	// Rebuild the index by replaying the log into a fresh tree.
	var root *node
	goodEnd, err := replay(f, func(ops []op) error {
		root = applyOps(root, ops)
		return nil
	})
	if err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("sled: replay log: %w", err)
	}
	db.root.Store(root)

	// Discard any partial/corrupt tail left by a crash so future appends stay
	// contiguous and a subsequent Open replays cleanly.
	size, err := fileSize(f)
	if err != nil {
		_ = f.Close()
		return nil, err
	}
	if goodEnd < size {
		if err := f.Truncate(goodEnd); err != nil {
			_ = f.Close()
			return nil, fmt.Errorf("sled: truncate partial tail: %w", err)
		}
	}
	if _, err := f.Seek(goodEnd, 0); err != nil {
		_ = f.Close()
		return nil, err
	}

	// Ensure the file's existence (and any truncation) are durable.
	if err := syncDir(path); err != nil {
		_ = f.Close()
		return nil, err
	}

	return db, nil
}

// applyOps folds a group of operations onto an index snapshot, returning the
// resulting snapshot. It is used both during replay and on the commit path.
func applyOps(root *node, ops []op) *node {
	for _, o := range ops {
		switch o.kind {
		case opSet:
			root = insert(root, o.key, o.value)
		case opDelete:
			root = remove(root, o.key)
		}
	}
	return root
}

// Close flushes any buffered data, fsyncs the log and releases the underlying
// file. The DB must not be used after Close returns. Close is idempotent.
func (db *DB) Close() error {
	db.mu.Lock()
	defer db.mu.Unlock()
	if db.closed.Load() {
		return nil
	}
	db.closed.Store(true)
	if err := db.log.Sync(); err != nil {
		_ = db.log.Close()
		return fmt.Errorf("sled: sync on close: %w", err)
	}
	return db.log.Close()
}

// commit is the single durable write primitive. It appends one record holding
// ops to the log, optionally fsyncs, and then atomically publishes newRoot as
// the current index. The caller must hold db.mu. If the log write fails the
// in-memory index is left untouched so on-disk and in-memory state stay
// consistent.
func (db *DB) commit(ops []op, newRoot *node) error {
	if db.closed.Load() {
		return ErrClosed
	}
	rec := encodeRecord(ops)
	if _, err := db.log.Write(rec); err != nil {
		return fmt.Errorf("sled: append record: %w", err)
	}
	if db.opts.syncWrites {
		if err := db.log.Sync(); err != nil {
			return fmt.Errorf("sled: fsync: %w", err)
		}
	}
	db.root.Store(newRoot)
	return nil
}

// snapshot returns the current immutable index root without locking.
func (db *DB) snapshot() *node { return db.root.Load() }

// Set stores value under key, replacing any previous value. The key and value
// are copied, so the caller may reuse their buffers after Set returns.
func (db *DB) Set(key, value []byte) error {
	if len(key) == 0 {
		return ErrEmptyKey
	}
	o := op{kind: opSet, key: cloneBytes(key), value: cloneBytes(value)}

	db.mu.Lock()
	defer db.mu.Unlock()
	if db.closed.Load() {
		return ErrClosed
	}
	newRoot := insert(db.snapshot(), o.key, o.value)
	return db.commit([]op{o}, newRoot)
}

// Delete removes key. Deleting a key that does not exist is not an error.
func (db *DB) Delete(key []byte) error {
	if len(key) == 0 {
		return ErrEmptyKey
	}
	o := op{kind: opDelete, key: cloneBytes(key)}

	db.mu.Lock()
	defer db.mu.Unlock()
	if db.closed.Load() {
		return ErrClosed
	}
	newRoot := remove(db.snapshot(), o.key)
	return db.commit([]op{o}, newRoot)
}

// Get returns the value stored under key and whether the key was present. A
// present key with an empty value returns (nil-or-empty, true, nil); an absent
// key returns (nil, false, nil). The returned slice is owned by the DB and must
// not be modified by the caller.
func (db *DB) Get(key []byte) ([]byte, bool, error) {
	if db.closed.Load() {
		return nil, false, ErrClosed
	}
	v, ok := get(db.snapshot(), key)
	return v, ok, nil
}

// Has reports whether key is present.
func (db *DB) Has(key []byte) (bool, error) {
	if db.closed.Load() {
		return false, ErrClosed
	}
	_, ok := get(db.snapshot(), key)
	return ok, nil
}

// Len returns the number of live keys. It is O(n) and intended for reporting
// and tests rather than hot-path use.
func (db *DB) Len() int {
	return count(db.snapshot())
}

// Path returns the path of the database's log file.
func (db *DB) Path() string { return db.path }

// cloneBytes returns a copy of b, or nil if b is nil.
func cloneBytes(b []byte) []byte {
	if b == nil {
		return nil
	}
	c := make([]byte, len(b))
	copy(c, b)
	return c
}

// syncDir fsyncs the directory containing path so that file creation and rename
// operations are durable.
func syncDir(path string) error {
	dir := filepath.Dir(path)
	if dir == "" {
		dir = "."
	}
	d, err := os.Open(dir)
	if err != nil {
		return fmt.Errorf("sled: open dir for sync: %w", err)
	}
	defer func() { _ = d.Close() }()
	if err := d.Sync(); err != nil {
		return fmt.Errorf("sled: sync dir: %w", err)
	}
	return nil
}
