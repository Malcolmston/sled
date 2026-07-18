package sled

// A MergeOperator folds a value into the current state of a key. It receives the
// key, the current value (nil if the key is absent), and the value passed to
// [Tree.Merge], and returns the new value to store. Returning nil deletes the
// key. A merge operator must be a pure function of its inputs: it may be called
// while the writer slot is held and must not call back into the database.
type MergeOperator func(key, oldValue, mergeValue []byte) []byte

// SetMergeOperator installs (or, with a nil operator, removes) the merge
// operator for this tree. It affects subsequent [Tree.Merge] calls and takes
// effect immediately for all goroutines.
func (t *Tree) SetMergeOperator(op MergeOperator) {
	if op == nil {
		t.merge.Store(nil)
		return
	}
	t.merge.Store(&op)
}

// SetMergeOperator installs the merge operator for the default tree. See
// [Tree.SetMergeOperator].
func (db *DB) SetMergeOperator(op MergeOperator) { db.def.SetMergeOperator(op) }

// Merge folds value into the current value at key using the tree's merge
// operator, and durably stores the result. It returns the newly stored value
// (nil if the operator deleted the key). Calling Merge without a merge operator
// installed returns [ErrNoMergeOperator].
//
// The read of the current value, the fold, and the write are performed while
// holding the single writer slot, so concurrent writes cannot interleave.
func (t *Tree) Merge(key, value []byte) (merged []byte, err error) {
	if len(key) == 0 {
		return nil, ErrEmptyKey
	}
	opp := t.merge.Load()
	if opp == nil {
		return nil, ErrNoMergeOperator
	}
	fn := *opp

	t.db.mu.Lock()
	defer t.db.mu.Unlock()
	if t.db.closed.Load() {
		return nil, ErrClosed
	}
	root := t.snapshot()
	cur, present := get(root, key)
	next := fn(key, cur, value)
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

// Merge folds value into key in the default tree. See [Tree.Merge].
func (db *DB) Merge(key, value []byte) ([]byte, error) { return db.def.Merge(key, value) }
