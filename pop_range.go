package sled

// PopMinInRange atomically removes and returns the smallest key that falls
// within r together with its value. It reports ok=false (and leaves the tree
// unchanged) when no key lies in the range. Only r.Lower, r.Upper and r.Prefix
// constrain the search; r.Reverse is ignored because the minimum in-range key
// is always returned.
//
// The half-open range semantics match [Range]: Lower is inclusive, Upper is
// exclusive. To express an inclusive upper bound (Rust's ..=hi), set Upper to a
// value strictly greater than hi.
//
// The returned key and value are freshly allocated copies owned by the caller.
// This mirrors sled's Tree::pop_first_in_range.
func (t *Tree) PopMinInRange(r Range) (key, value []byte, ok bool, err error) {
	return t.popRange(r, false)
}

// PopMaxInRange atomically removes and returns the largest key that falls
// within r together with its value. It reports ok=false (and leaves the tree
// unchanged) when no key lies in the range. Only r.Lower, r.Upper and r.Prefix
// constrain the search; r.Reverse is ignored because the maximum in-range key
// is always returned.
//
// The half-open range semantics match [Range]: Lower is inclusive, Upper is
// exclusive. To express an inclusive upper bound (Rust's ..=hi), set Upper to a
// value strictly greater than hi.
//
// The returned key and value are freshly allocated copies owned by the caller.
// This mirrors sled's Tree::pop_last_in_range.
func (t *Tree) PopMaxInRange(r Range) (key, value []byte, ok bool, err error) {
	return t.popRange(r, true)
}

// popRange is the shared implementation of PopMinInRange (max=false) and
// PopMaxInRange (max=true). It holds the single writer slot for the whole
// operation so that locating the extreme in-range key and removing it cannot
// interleave with another write.
func (t *Tree) popRange(r Range, max bool) (key, value []byte, ok bool, err error) {
	t.db.mu.Lock()
	defer t.db.mu.Unlock()
	if t.db.closed.Load() {
		return nil, nil, false, ErrClosed
	}
	root := t.snapshot()
	// Reverse selects descending traversal, whose first element is the
	// largest in-range key; the ascending first element is the smallest.
	r.Reverse = max
	it := newIterator(root, r)
	if !it.Valid() {
		return nil, nil, false, nil
	}
	k, v := cloneBytes(it.Key()), cloneBytes(it.Value())
	ev := Event{Type: EventDelete, tree: t, Key: k}
	if err := t.db.commit([]op{t.delOp(k)}, []treeUpdate{{t, remove(root, k)}}, []Event{ev}); err != nil {
		return nil, nil, false, err
	}
	return k, v, true, nil
}

// PopMinInRange removes and returns the smallest key of the default tree within
// r. See [Tree.PopMinInRange].
func (db *DB) PopMinInRange(r Range) (key, value []byte, ok bool, err error) {
	return db.def.PopMinInRange(r)
}

// PopMaxInRange removes and returns the largest key of the default tree within
// r. See [Tree.PopMaxInRange].
func (db *DB) PopMaxInRange(r Range) (key, value []byte, ok bool, err error) {
	return db.def.PopMaxInRange(r)
}
