package sled

import (
	"bytes"
	"sort"
	"sync"
	"sync/atomic"
)

// A Tree is an independent, ordered keyspace within a DB. Every tree shares the
// database's single write-ahead log and writer slot, so writes to different
// trees are still fully serialized and a transaction may span several trees
// atomically. Trees are isolated: a key written to one tree is invisible to
// every other tree, including the default tree.
//
// A Tree handle is safe for concurrent use. Obtain one with [DB.OpenTree]; the
// default tree is also reachable through the DB's own Set/Get/Delete/Scan
// methods.
type Tree struct {
	db   *DB
	name string

	// root is the current published snapshot for this tree, read by readers
	// with an atomic load and replaced by the writer with an atomic store.
	root atomic.Pointer[node]

	// merge holds the optional merge operator installed with SetMergeOperator.
	merge atomic.Pointer[MergeOperator]

	// subsMu guards subs, the set of live subscribers watching this tree.
	subsMu sync.Mutex
	subs   map[*Subscriber]struct{}
}

// getOrCreateTree returns the tree registered under name, creating and
// registering it if absent. It takes treesMu, so callers must not already hold
// it. During single-threaded replay the lock is uncontended.
func (db *DB) getOrCreateTree(name string) *Tree {
	db.treesMu.RLock()
	t := db.trees[name]
	db.treesMu.RUnlock()
	if t != nil {
		return t
	}
	db.treesMu.Lock()
	defer db.treesMu.Unlock()
	if t := db.trees[name]; t != nil {
		return t
	}
	t = &Tree{db: db, name: name}
	db.trees[name] = t
	return t
}

// OpenTree returns the tree named name, creating it if it does not yet exist.
// The returned handle is independent of every other tree and persists in the
// same log. Passing [DefaultTreeName] returns the default tree. The name must be
// non-empty.
//
// A freshly created tree is empty and becomes durable as soon as it receives its
// first write; an opened-but-never-written tree does not survive a restart.
func (db *DB) OpenTree(name string) (*Tree, error) {
	if db.closed.Load() {
		return nil, ErrClosed
	}
	if name == "" {
		return nil, ErrEmptyTreeName
	}
	return db.getOrCreateTree(name), nil
}

// TreeNames returns the names of all currently registered trees, including
// [DefaultTreeName], in sorted order.
func (db *DB) TreeNames() []string {
	db.treesMu.RLock()
	names := make([]string, 0, len(db.trees))
	for name := range db.trees {
		names = append(names, name)
	}
	db.treesMu.RUnlock()
	sort.Strings(names)
	return names
}

// DropTree removes the named tree and all of its keys, durably. Dropping a tree
// that does not exist reports false with a nil error; the default tree cannot be
// dropped. Existing handles to a dropped tree observe it as empty.
func (db *DB) DropTree(name string) (bool, error) {
	if name == DefaultTreeName {
		return false, ErrDropDefaultTree
	}
	db.mu.Lock()
	defer db.mu.Unlock()
	if db.closed.Load() {
		return false, ErrClosed
	}
	db.treesMu.RLock()
	t := db.trees[name]
	db.treesMu.RUnlock()
	if t == nil {
		return false, nil
	}
	o := op{kind: opDropTree, tree: name}
	if err := db.commit([]op{o}, nil, nil); err != nil {
		return false, err
	}
	// Detach from the registry and empty the handle so lingering references
	// see no data.
	db.treesMu.Lock()
	delete(db.trees, name)
	db.treesMu.Unlock()
	t.root.Store(nil)
	return true, nil
}

// isDefault reports whether t is the default tree, which uses the compact,
// tree-less log encoding.
func (t *Tree) isDefault() bool { return t.name == DefaultTreeName }

// setOp builds a set operation encoded for this tree.
func (t *Tree) setOp(key, value []byte) op {
	if t.isDefault() {
		return op{kind: opSet, key: key, value: value}
	}
	return op{kind: opTreeSet, tree: t.name, key: key, value: value}
}

// delOp builds a delete operation encoded for this tree.
func (t *Tree) delOp(key []byte) op {
	if t.isDefault() {
		return op{kind: opDelete, key: key}
	}
	return op{kind: opTreeDelete, tree: t.name, key: key}
}

// snapshot returns the tree's current immutable root without locking.
func (t *Tree) snapshot() *node { return t.root.Load() }

// Name returns the tree's name.
func (t *Tree) Name() string { return t.name }

// Set stores value under key in this tree, replacing any previous value. The key
// and value are copied, so the caller may reuse their buffers after Set returns.
func (t *Tree) Set(key, value []byte) error {
	if len(key) == 0 {
		return ErrEmptyKey
	}
	k, v := cloneBytes(key), cloneBytes(value)
	t.db.mu.Lock()
	defer t.db.mu.Unlock()
	if t.db.closed.Load() {
		return ErrClosed
	}
	old := t.snapshot()
	newRoot := insert(old, k, v)
	ev := Event{Type: eventKind(old, k, false), tree: t, Key: k, Value: v}
	return t.db.commit([]op{t.setOp(k, v)}, []treeUpdate{{t, newRoot}}, []Event{ev})
}

// Delete removes key from this tree. Deleting a key that does not exist is not
// an error and produces no subscriber event.
func (t *Tree) Delete(key []byte) error {
	if len(key) == 0 {
		return ErrEmptyKey
	}
	k := cloneBytes(key)
	t.db.mu.Lock()
	defer t.db.mu.Unlock()
	if t.db.closed.Load() {
		return ErrClosed
	}
	old := t.snapshot()
	if _, ok := get(old, k); !ok {
		return nil // nothing to do; skip the log write and the event.
	}
	newRoot := remove(old, k)
	ev := Event{Type: EventDelete, tree: t, Key: k}
	return t.db.commit([]op{t.delOp(k)}, []treeUpdate{{t, newRoot}}, []Event{ev})
}

// Get returns the value stored under key in this tree and whether the key was
// present. The returned slice is owned by the DB and must not be modified.
func (t *Tree) Get(key []byte) ([]byte, bool, error) {
	if t.db.closed.Load() {
		return nil, false, ErrClosed
	}
	v, ok := get(t.snapshot(), key)
	return v, ok, nil
}

// Has reports whether key is present in this tree.
func (t *Tree) Has(key []byte) (bool, error) {
	if t.db.closed.Load() {
		return false, ErrClosed
	}
	_, ok := get(t.snapshot(), key)
	return ok, nil
}

// ContainsKey reports whether key is present in this tree. It is an alias for
// Has that mirrors the sled API.
func (t *Tree) ContainsKey(key []byte) (bool, error) { return t.Has(key) }

// Len returns the number of live keys in this tree. It is O(n).
func (t *Tree) Len() int { return count(t.snapshot()) }

// IsEmpty reports whether the tree holds no keys.
func (t *Tree) IsEmpty() bool { return t.snapshot() == nil }

// Clear removes every key from this tree in a single durable record and emits a
// delete event for each key that was present.
func (t *Tree) Clear() error {
	t.db.mu.Lock()
	defer t.db.mu.Unlock()
	if t.db.closed.Load() {
		return ErrClosed
	}
	old := t.snapshot()
	if old == nil {
		return nil
	}
	var events []Event
	inorder(old, func(n *node) bool {
		events = append(events, Event{Type: EventDelete, tree: t, Key: n.key})
		return true
	})
	o := op{kind: opClearTree, tree: t.wireName()}
	return t.db.commit([]op{o}, []treeUpdate{{t, nil}}, events)
}

// wireName returns the tree name used on the wire: empty for the default tree
// (which uses the compact tree-less encoding), the name otherwise.
func (t *Tree) wireName() string {
	if t.isDefault() {
		return ""
	}
	return t.name
}

// Scan returns an iterator over this tree restricted to r. Set r.Reverse for
// descending order. The iterator reads an immutable snapshot and is unaffected
// by concurrent writes.
func (t *Tree) Scan(r Range) *Iterator {
	return newIterator(t.snapshot(), r)
}

// First returns the smallest key and its value, or ok=false if the tree is
// empty.
func (t *Tree) First() (key, value []byte, ok bool) {
	n := minNode(t.snapshot())
	if n == nil {
		return nil, nil, false
	}
	return n.key, n.value, true
}

// Last returns the largest key and its value, or ok=false if the tree is empty.
func (t *Tree) Last() (key, value []byte, ok bool) {
	n := maxNode(t.snapshot())
	if n == nil {
		return nil, nil, false
	}
	return n.key, n.value, true
}

// GetGt returns the smallest key strictly greater than key, with its value, or
// ok=false if there is none.
func (t *Tree) GetGt(key []byte) (k, value []byte, ok bool) {
	return nodeResult(neighbor(t.snapshot(), key, true, false))
}

// GetGte returns the smallest key greater than or equal to key, with its value,
// or ok=false if there is none.
func (t *Tree) GetGte(key []byte) (k, value []byte, ok bool) {
	return nodeResult(neighbor(t.snapshot(), key, true, true))
}

// GetLt returns the largest key strictly less than key, with its value, or
// ok=false if there is none.
func (t *Tree) GetLt(key []byte) (k, value []byte, ok bool) {
	return nodeResult(neighbor(t.snapshot(), key, false, false))
}

// GetLte returns the largest key less than or equal to key, with its value, or
// ok=false if there is none.
func (t *Tree) GetLte(key []byte) (k, value []byte, ok bool) {
	return nodeResult(neighbor(t.snapshot(), key, false, true))
}

// nodeResult unpacks a node into the (key, value, ok) result triple.
func nodeResult(n *node) (key, value []byte, ok bool) {
	if n == nil {
		return nil, nil, false
	}
	return n.key, n.value, true
}

// CompareAndSwap atomically replaces the value at key with newValue, but only if
// the current value equals old. A nil old means "expect the key to be absent"; a
// nil newValue deletes the key. It reports whether the swap happened. A failed
// compare is not an error.
//
// The comparison and the write are performed while holding the single writer
// slot, so no other write can interleave.
func (t *Tree) CompareAndSwap(key, old, newValue []byte) (swapped bool, err error) {
	if len(key) == 0 {
		return false, ErrEmptyKey
	}
	t.db.mu.Lock()
	defer t.db.mu.Unlock()
	if t.db.closed.Load() {
		return false, ErrClosed
	}
	root := t.snapshot()
	cur, present := get(root, key)
	if !casMatch(cur, present, old) {
		return false, nil
	}
	k := cloneBytes(key)
	if newValue == nil {
		if !present {
			return true, nil // already absent; nothing to write.
		}
		ev := Event{Type: EventDelete, tree: t, Key: k}
		if err := t.db.commit([]op{t.delOp(k)}, []treeUpdate{{t, remove(root, k)}}, []Event{ev}); err != nil {
			return false, err
		}
		return true, nil
	}
	v := cloneBytes(newValue)
	ev := Event{Type: eventKind(root, k, false), tree: t, Key: k, Value: v}
	if err := t.db.commit([]op{t.setOp(k, v)}, []treeUpdate{{t, insert(root, k, v)}}, []Event{ev}); err != nil {
		return false, err
	}
	return true, nil
}

// casMatch reports whether the current state (cur/present) matches the expected
// old value under compare-and-swap rules.
func casMatch(cur []byte, present bool, old []byte) bool {
	if old == nil {
		return !present
	}
	return present && bytes.Equal(cur, old)
}

// FetchAndUpdate atomically reads the current value at key, calls f with it
// (nil if absent), and writes f's result back. Returning nil from f deletes the
// key. It returns the previous and new values. f runs while the writer slot is
// held, so it must not call back into the database.
func (t *Tree) FetchAndUpdate(key []byte, f func(old []byte) []byte) (oldValue, newValue []byte, err error) {
	if len(key) == 0 {
		return nil, nil, ErrEmptyKey
	}
	if f == nil {
		return nil, nil, ErrNilFunc
	}
	t.db.mu.Lock()
	defer t.db.mu.Unlock()
	if t.db.closed.Load() {
		return nil, nil, ErrClosed
	}
	root := t.snapshot()
	cur, present := get(root, key)
	next := f(cur)
	k := cloneBytes(key)
	if next == nil {
		if !present {
			return cur, nil, nil
		}
		ev := Event{Type: EventDelete, tree: t, Key: k}
		if err := t.db.commit([]op{t.delOp(k)}, []treeUpdate{{t, remove(root, k)}}, []Event{ev}); err != nil {
			return nil, nil, err
		}
		return cur, nil, nil
	}
	v := cloneBytes(next)
	ev := Event{Type: eventKind(root, k, false), tree: t, Key: k, Value: v}
	if err := t.db.commit([]op{t.setOp(k, v)}, []treeUpdate{{t, insert(root, k, v)}}, []Event{ev}); err != nil {
		return nil, nil, err
	}
	return cur, v, nil
}

// The following methods delegate the corresponding [Tree] operations to the
// default tree, so a caller that never opens a named tree can use the DB
// directly.

// ContainsKey reports whether key is present in the default tree.
func (db *DB) ContainsKey(key []byte) (bool, error) { return db.def.ContainsKey(key) }

// Clear removes every key from the default tree. See [Tree.Clear].
func (db *DB) Clear() error { return db.def.Clear() }

// First returns the smallest key and value in the default tree. See
// [Tree.First].
func (db *DB) First() (key, value []byte, ok bool) { return db.def.First() }

// Last returns the largest key and value in the default tree. See [Tree.Last].
func (db *DB) Last() (key, value []byte, ok bool) { return db.def.Last() }

// GetGt returns the smallest key strictly greater than key in the default tree.
// See [Tree.GetGt].
func (db *DB) GetGt(key []byte) (k, value []byte, ok bool) { return db.def.GetGt(key) }

// GetGte returns the smallest key >= key in the default tree. See [Tree.GetGte].
func (db *DB) GetGte(key []byte) (k, value []byte, ok bool) { return db.def.GetGte(key) }

// GetLt returns the largest key strictly less than key in the default tree. See
// [Tree.GetLt].
func (db *DB) GetLt(key []byte) (k, value []byte, ok bool) { return db.def.GetLt(key) }

// GetLte returns the largest key <= key in the default tree. See [Tree.GetLte].
func (db *DB) GetLte(key []byte) (k, value []byte, ok bool) { return db.def.GetLte(key) }

// CompareAndSwap atomically swaps the value at key in the default tree. See
// [Tree.CompareAndSwap].
func (db *DB) CompareAndSwap(key, old, newValue []byte) (bool, error) {
	return db.def.CompareAndSwap(key, old, newValue)
}

// FetchAndUpdate atomically updates key in the default tree. See
// [Tree.FetchAndUpdate].
func (db *DB) FetchAndUpdate(key []byte, f func(old []byte) []byte) (oldValue, newValue []byte, err error) {
	return db.def.FetchAndUpdate(key, f)
}
