package sled

// PopMin atomically removes and returns the smallest key in the tree together
// with its value. It reports ok=false (and leaves the tree unchanged) when the
// tree is empty. The removal is committed as one durable record and emits a
// delete event to matching subscribers.
//
// The returned key and value are freshly allocated copies owned by the caller.
// This mirrors sled's Tree::pop_min.
func (t *Tree) PopMin() (key, value []byte, ok bool, err error) {
	return t.popEnd(false)
}

// PopMax atomically removes and returns the largest key in the tree together
// with its value. It reports ok=false (and leaves the tree unchanged) when the
// tree is empty. The removal is committed as one durable record and emits a
// delete event to matching subscribers.
//
// The returned key and value are freshly allocated copies owned by the caller.
// This mirrors sled's Tree::pop_max.
func (t *Tree) PopMax() (key, value []byte, ok bool, err error) {
	return t.popEnd(true)
}

// popEnd is the shared implementation of PopMin (max=false) and PopMax
// (max=true). It runs under the single writer slot so the read of the extreme
// key and its removal cannot interleave with another write.
func (t *Tree) popEnd(max bool) (key, value []byte, ok bool, err error) {
	t.db.mu.Lock()
	defer t.db.mu.Unlock()
	if t.db.closed.Load() {
		return nil, nil, false, ErrClosed
	}
	root := t.snapshot()
	var n *node
	if max {
		n = maxNode(root)
	} else {
		n = minNode(root)
	}
	if n == nil {
		return nil, nil, false, nil
	}
	k, v := cloneBytes(n.key), cloneBytes(n.value)
	ev := Event{Type: EventDelete, tree: t, Key: k}
	if err := t.db.commit([]op{t.delOp(k)}, []treeUpdate{{t, remove(root, k)}}, []Event{ev}); err != nil {
		return nil, nil, false, err
	}
	return k, v, true, nil
}

// PopMin removes and returns the smallest key in the default tree. See
// [Tree.PopMin].
func (db *DB) PopMin() (key, value []byte, ok bool, err error) { return db.def.PopMin() }

// PopMax removes and returns the largest key in the default tree. See
// [Tree.PopMax].
func (db *DB) PopMax() (key, value []byte, ok bool, err error) { return db.def.PopMax() }
