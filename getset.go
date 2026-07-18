package sled

// GetAndSet stores value under key, replacing any previous value, and returns
// the value that was present before the write together with whether the key
// existed. It is the value-returning form of [Tree.Set] and mirrors sled's
// Tree::insert, which returns the previous value.
//
// The key and value are copied, so the caller may reuse their buffers. The
// returned previous value is a freshly allocated copy owned by the caller.
func (t *Tree) GetAndSet(key, value []byte) (old []byte, existed bool, err error) {
	if len(key) == 0 {
		return nil, false, ErrEmptyKey
	}
	k, v := cloneBytes(key), cloneBytes(value)
	t.db.mu.Lock()
	defer t.db.mu.Unlock()
	if t.db.closed.Load() {
		return nil, false, ErrClosed
	}
	root := t.snapshot()
	prev, present := get(root, k)
	if present {
		old = cloneBytes(prev)
	}
	ev := Event{Type: eventKind(root, k, false), tree: t, Key: k, Value: v}
	if err := t.db.commit([]op{t.setOp(k, v)}, []treeUpdate{{t, insert(root, k, v)}}, []Event{ev}); err != nil {
		return nil, false, err
	}
	return old, present, nil
}

// GetAndDelete removes key and returns the value it held together with whether
// it existed. Deleting an absent key is not an error and returns
// (nil, false, nil) without writing to the log. It is the value-returning form
// of [Tree.Delete] and mirrors sled's Tree::remove, which returns the removed
// value.
//
// The returned value is a freshly allocated copy owned by the caller.
func (t *Tree) GetAndDelete(key []byte) (old []byte, existed bool, err error) {
	if len(key) == 0 {
		return nil, false, ErrEmptyKey
	}
	t.db.mu.Lock()
	defer t.db.mu.Unlock()
	if t.db.closed.Load() {
		return nil, false, ErrClosed
	}
	root := t.snapshot()
	prev, present := get(root, key)
	if !present {
		return nil, false, nil
	}
	old = cloneBytes(prev)
	k := cloneBytes(key)
	ev := Event{Type: EventDelete, tree: t, Key: k}
	if err := t.db.commit([]op{t.delOp(k)}, []treeUpdate{{t, remove(root, k)}}, []Event{ev}); err != nil {
		return nil, false, err
	}
	return old, true, nil
}

// GetAndSet stores value under key in the default tree and returns the previous
// value. See [Tree.GetAndSet].
func (db *DB) GetAndSet(key, value []byte) (old []byte, existed bool, err error) {
	return db.def.GetAndSet(key, value)
}

// GetAndDelete removes key from the default tree and returns its previous value.
// See [Tree.GetAndDelete].
func (db *DB) GetAndDelete(key []byte) (old []byte, existed bool, err error) {
	return db.def.GetAndDelete(key)
}
