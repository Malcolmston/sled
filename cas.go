package sled

import "fmt"

// CompareAndSwapError reports that a [Tree.CompareAndSwapErr] failed because the
// stored value did not match the expected one. Current holds the value found in
// the tree at the time of the attempt (nil if the key was absent) and Proposed
// holds the new value the caller wanted to install (nil for a delete). Both are
// freshly allocated copies owned by the caller. It mirrors sled's
// CompareAndSwapError.
type CompareAndSwapError struct {
	Current  []byte
	Proposed []byte
}

// Error implements the error interface.
func (e *CompareAndSwapError) Error() string {
	return fmt.Sprintf("sled: compare-and-swap failed: current=%q proposed=%q", e.Current, e.Proposed)
}

// CompareAndSwapErr atomically replaces the value at key with newValue, but only
// if the current value equals old. A nil old means "expect the key to be
// absent"; a nil newValue deletes the key. On success it returns nil. On a value
// mismatch it returns a non-nil *[CompareAndSwapError] carrying the current and
// proposed values, so the caller can retry without a separate read. Any other
// (I/O) failure is returned as an ordinary error.
//
// This is the error-returning form of [Tree.CompareAndSwap], matching sled's
// compare_and_swap, which reports the conflicting value on failure. The compare
// and the write happen under the single writer slot, so no other write can
// interleave.
func (t *Tree) CompareAndSwapErr(key, old, newValue []byte) error {
	if len(key) == 0 {
		return ErrEmptyKey
	}
	t.db.mu.Lock()
	defer t.db.mu.Unlock()
	if t.db.closed.Load() {
		return ErrClosed
	}
	root := t.snapshot()
	cur, present := get(root, key)
	if !casMatch(cur, present, old) {
		return &CompareAndSwapError{Current: cloneBytes(cur), Proposed: cloneBytes(newValue)}
	}
	k := cloneBytes(key)
	if newValue == nil {
		if !present {
			return nil // already absent; nothing to write.
		}
		ev := Event{Type: EventDelete, tree: t, Key: k}
		return t.db.commit([]op{t.delOp(k)}, []treeUpdate{{t, remove(root, k)}}, []Event{ev})
	}
	v := cloneBytes(newValue)
	ev := Event{Type: eventKind(root, k, false), tree: t, Key: k, Value: v}
	return t.db.commit([]op{t.setOp(k, v)}, []treeUpdate{{t, insert(root, k, v)}}, []Event{ev})
}

// CompareAndSwapErr performs an error-reporting compare-and-swap on the default
// tree. See [Tree.CompareAndSwapErr].
func (db *DB) CompareAndSwapErr(key, old, newValue []byte) error {
	return db.def.CompareAndSwapErr(key, old, newValue)
}
