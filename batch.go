package sled

// Batch accumulates a group of writes that are committed together as a single
// atomic, durable unit. Building a Batch performs no I/O; all buffered writes
// are flushed by exactly one log record when Commit is called, so on recovery
// the whole group is applied all-or-nothing.
//
// A Batch is not safe for concurrent use by multiple goroutines. Create one,
// stage writes into it from a single goroutine, then Commit.
type Batch struct {
	db      *DB
	ops     []op
	err     error
	written bool
}

// NewBatch returns an empty Batch bound to db.
func (db *DB) NewBatch() *Batch {
	return &Batch{db: db}
}

// Set stages a write of value under key. The key and value are copied.
func (b *Batch) Set(key, value []byte) *Batch {
	if b.err != nil {
		return b
	}
	if len(key) == 0 {
		b.err = ErrEmptyKey
		return b
	}
	b.ops = append(b.ops, op{kind: opSet, key: cloneBytes(key), value: cloneBytes(value)})
	return b
}

// Delete stages a deletion of key. The key is copied.
func (b *Batch) Delete(key []byte) *Batch {
	if b.err != nil {
		return b
	}
	if len(key) == 0 {
		b.err = ErrEmptyKey
		return b
	}
	b.ops = append(b.ops, op{kind: opDelete, key: cloneBytes(key)})
	return b
}

// Len returns the number of staged operations.
func (b *Batch) Len() int { return len(b.ops) }

// Commit durably applies all staged writes as one atomic record. If any staged
// write was invalid (for example an empty key) that error is returned and
// nothing is applied. A committed or errored Batch must not be reused.
func (b *Batch) Commit() error {
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

	db := b.db
	db.mu.Lock()
	defer db.mu.Unlock()
	if db.closed.Load() {
		return ErrClosed
	}
	newRoot := applyOps(db.snapshot(), b.ops)
	return db.commit(b.ops, newRoot)
}

// Batch is a convenience that stages writes via fn into a fresh Batch and
// commits it atomically. If fn returns an error the batch is discarded and
// nothing is written.
func (db *DB) Batch(fn func(*Batch) error) error {
	b := db.NewBatch()
	if err := fn(b); err != nil {
		return err
	}
	return b.Commit()
}
