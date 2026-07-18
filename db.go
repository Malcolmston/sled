package sled

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
)

// DefaultTreeName is the name of the always-present default keyspace, the one
// backing the DB's own Set/Get/Delete/Scan methods. Passing it to [DB.OpenTree]
// returns that same default tree, and it appears in [DB.TreeNames]. It cannot be
// dropped.
const DefaultTreeName = "__sled__default"

// DB is an embedded, transactional key/value store backed by an append-only
// write-ahead log. A DB is safe for concurrent use: a single writer is
// serialized internally while any number of readers proceed concurrently
// against immutable snapshots. See the package documentation for the full
// concurrency and durability model.
//
// A DB holds one or more independent, ordered keyspaces called trees. The
// default tree backs the DB's own Set/Get/Delete/Scan methods; additional named
// trees are obtained with [DB.OpenTree]. All trees share the single write-ahead
// log and the single writer slot, which is what lets a transaction span several
// trees atomically.
type DB struct {
	// mu serializes all writers (single-writer model) and protects the log
	// file and the mutable bookkeeping fields below.
	mu   sync.Mutex
	log  *os.File
	opts options
	path string

	// treesMu guards the trees registry. Individual tree roots are published
	// atomically and read without any lock; treesMu only covers adding, finding
	// and removing tree handles.
	treesMu sync.RWMutex
	trees   map[string]*Tree
	def     *Tree

	// idNext is the next identifier GenerateID will hand out; idReserved is the
	// durable high-water mark below which identifiers have been reserved on
	// disk. Both are protected by mu.
	idNext     uint64
	idReserved uint64

	// recovered reports whether Open found an existing, non-empty log and
	// rebuilt state from it rather than creating a fresh database. It is set once
	// during Open and read by [DB.WasRecovered].
	recovered bool

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

	db := &DB{log: f, opts: o, path: path, trees: make(map[string]*Tree)}
	db.def = db.getOrCreateTree(DefaultTreeName)

	// A non-empty file at open time means we are recovering an existing
	// database rather than creating a new one. Record it before replay.
	if fi, err := f.Stat(); err == nil {
		db.recovered = fi.Size() > 0
	}

	// Rebuild every tree by replaying the log. Replay is single-threaded, so it
	// mutates roots and bookkeeping directly without locking.
	goodEnd, err := replay(f, db.replayOps)
	if err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("sled: replay log: %w", err)
	}

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

// replayOps applies a group of operations during log replay, routing each to
// its target tree and updating durable bookkeeping. It runs single-threaded
// from Open, so it stores roots directly.
func (db *DB) replayOps(ops []op) error {
	for _, o := range ops {
		switch o.kind {
		case opSet, opTreeSet:
			t := db.getOrCreateTree(treeName(o.tree))
			t.root.Store(insert(t.root.Load(), o.key, o.value))
		case opDelete, opTreeDelete:
			t := db.getOrCreateTree(treeName(o.tree))
			t.root.Store(remove(t.root.Load(), o.key))
		case opClearTree:
			db.getOrCreateTree(treeName(o.tree)).root.Store(nil)
		case opDropTree:
			delete(db.trees, treeName(o.tree))
		case opIDReserve:
			if o.num > db.idReserved {
				db.idReserved = o.num
				db.idNext = o.num
			}
		}
	}
	return nil
}

// treeName maps a raw op tree field to a registry key. Legacy tree-less ops
// (opSet/opDelete) carry an empty tree and belong to the default tree.
func treeName(raw string) string {
	if raw == "" {
		return DefaultTreeName
	}
	return raw
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
	if err := db.log.Close(); err != nil {
		return err
	}
	// A temporary database removes its backing files once it is cleanly closed.
	if db.opts.temporary {
		_ = os.Remove(db.path)
		_ = os.Remove(db.path + ".compact")
	}
	return nil
}

// treeUpdate pairs a tree with the new root the commit will publish for it.
type treeUpdate struct {
	tree *Tree
	root *node
}

// commit is the single durable write primitive. It appends one record holding
// ops to the log, optionally fsyncs, then atomically publishes each update's new
// root and delivers events to matching subscribers. The caller must hold db.mu.
// If the log write fails no root is published, so on-disk and in-memory state
// stay consistent. Publishing all updates from one record is what makes a
// multi-tree transaction atomic.
func (db *DB) commit(ops []op, updates []treeUpdate, events []Event) error {
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
	for _, u := range updates {
		u.tree.root.Store(u.root)
	}
	for i := range events {
		events[i].tree.publish(events[i])
	}
	return nil
}

// Set stores value under key in the default tree, replacing any previous value.
// The key and value are copied, so the caller may reuse their buffers after Set
// returns.
func (db *DB) Set(key, value []byte) error { return db.def.Set(key, value) }

// Delete removes key from the default tree. Deleting a key that does not exist
// is not an error.
func (db *DB) Delete(key []byte) error { return db.def.Delete(key) }

// Get returns the value stored under key in the default tree and whether the key
// was present. A present key with an empty value returns (nil-or-empty, true,
// nil); an absent key returns (nil, false, nil). The returned slice is owned by
// the DB and must not be modified by the caller.
func (db *DB) Get(key []byte) ([]byte, bool, error) { return db.def.Get(key) }

// Has reports whether key is present in the default tree.
func (db *DB) Has(key []byte) (bool, error) { return db.def.Has(key) }

// Len returns the number of live keys in the default tree. It is O(n) and
// intended for reporting and tests rather than hot-path use.
func (db *DB) Len() int { return db.def.Len() }

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
