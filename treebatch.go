package sled

// ApplyBatch durably applies every write staged in b to this tree as one atomic
// record, regardless of which DB the batch was created from. This lets a batch
// built with [DB.NewBatch] target a named tree, mirroring sled's
// Tree::apply_batch. On recovery the whole group is applied all-or-nothing.
//
// If b carries a staging error (for example an empty key) that error is
// returned and nothing is applied. A committed or errored batch must not be
// reused.
func (t *Tree) ApplyBatch(b *Batch) error {
	if b.err != nil {
		return b.err
	}
	if b.written {
		return ErrTxClosed
	}
	b.written = true
	if len(b.ops) == 0 {
		return nil
	}

	t.db.mu.Lock()
	defer t.db.mu.Unlock()
	if t.db.closed.Load() {
		return ErrClosed
	}
	root := t.snapshot()
	ops := make([]op, 0, len(b.ops))
	events := make([]Event, 0, len(b.ops))
	for _, o := range b.ops {
		switch o.kind {
		case opSet:
			events = append(events, Event{Type: eventKind(root, o.key, false), tree: t, Key: o.key, Value: o.value})
			ops = append(ops, t.setOp(o.key, o.value))
			root = insert(root, o.key, o.value)
		case opDelete:
			if _, ok := get(root, o.key); ok {
				events = append(events, Event{Type: EventDelete, tree: t, Key: o.key})
			}
			ops = append(ops, t.delOp(o.key))
			root = remove(root, o.key)
		}
	}
	return t.db.commit(ops, []treeUpdate{{t, root}}, events)
}

// Batch stages writes via fn into a fresh batch and applies them to this tree
// atomically. If fn returns an error the batch is discarded and nothing is
// written. It is the tree-scoped counterpart of [DB.Batch].
func (t *Tree) Batch(fn func(*Batch) error) error {
	b := t.db.NewBatch()
	if err := fn(b); err != nil {
		return err
	}
	return t.ApplyBatch(b)
}
