package sled

// UpdateAndFetch atomically reads the current value at key, calls f with it (nil
// if the key is absent), durably writes f's result back, and returns the newly
// stored value. Returning nil from f deletes the key and yields a nil result.
//
// Where [Tree.FetchAndUpdate] returns the previous value, UpdateAndFetch returns
// the new one; the two mirror sled's fetch_and_update and update_and_fetch. f
// runs while the single writer slot is held, so it must be a pure function of
// its input and must not call back into the database.
func (t *Tree) UpdateAndFetch(key []byte, f func(old []byte) []byte) (newValue []byte, err error) {
	if len(key) == 0 {
		return nil, ErrEmptyKey
	}
	if f == nil {
		return nil, ErrNilFunc
	}
	t.db.mu.Lock()
	defer t.db.mu.Unlock()
	if t.db.closed.Load() {
		return nil, ErrClosed
	}
	root := t.snapshot()
	cur, present := get(root, key)
	next := f(cur)
	k := cloneBytes(key)
	if next == nil {
		if !present {
			return nil, nil
		}
		ev := Event{Type: EventDelete, tree: t, Key: k}
		if err := t.db.commit([]op{t.delOp(k)}, []treeUpdate{{t, remove(root, k)}}, []Event{ev}); err != nil {
			return nil, err
		}
		return nil, nil
	}
	v := cloneBytes(next)
	ev := Event{Type: eventKind(root, k, false), tree: t, Key: k, Value: v}
	if err := t.db.commit([]op{t.setOp(k, v)}, []treeUpdate{{t, insert(root, k, v)}}, []Event{ev}); err != nil {
		return nil, err
	}
	return v, nil
}

// UpdateAndFetch atomically updates key in the default tree and returns the new
// value. See [Tree.UpdateAndFetch].
func (db *DB) UpdateAndFetch(key []byte, f func(old []byte) []byte) ([]byte, error) {
	return db.def.UpdateAndFetch(key, f)
}
