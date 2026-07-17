package sled

// Tx is a transaction handle. A read-only transaction (created by View) reads
// from an immutable snapshot captured when it begins. A read/write transaction
// (created by Update) additionally buffers mutations that become durable
// atomically when the transaction function returns nil, and are discarded if it
// returns an error.
//
// A Tx is only valid inside the function passed to View or Update and must not
// be retained or used after that function returns.
type Tx struct {
	db       *DB
	writable bool
	done     bool

	// working is the transaction's view of the index: the starting snapshot for
	// a read-only Tx, or the snapshot with the Tx's own buffered writes applied
	// for a writable Tx (so reads observe the Tx's uncommitted changes).
	working *node

	// ops records buffered mutations in order for a writable Tx; it is encoded
	// into a single log record on commit.
	ops []op
}

// Update executes fn within a read/write transaction. Writers are serialized,
// so the transaction runs alone with respect to other writers. If fn returns
// nil the buffered writes are committed atomically and durably; if fn returns
// an error (or panics) the writes are rolled back and nothing is persisted.
// The error from fn (or the commit) is returned.
func (db *DB) Update(fn func(*Tx) error) error {
	db.mu.Lock()
	defer db.mu.Unlock()
	if db.closed.Load() {
		return ErrClosed
	}

	tx := &Tx{db: db, writable: true, working: db.snapshot()}
	// If fn panics the deferred unlock still runs and, crucially, we never
	// reach the commit below, so a panic rolls the transaction back.
	defer func() { tx.done = true }()

	if err := fn(tx); err != nil {
		return err // rollback: buffered writes discarded, nothing written.
	}
	if len(tx.ops) == 0 {
		return nil
	}
	return db.commit(tx.ops, tx.working)
}

// View executes fn within a read-only transaction over a consistent immutable
// snapshot taken when View is called. Concurrent writes do not affect what the
// transaction sees. Any attempt to mutate through the Tx returns
// ErrTxNotWritable.
func (db *DB) View(fn func(*Tx) error) error {
	if db.closed.Load() {
		return ErrClosed
	}
	tx := &Tx{db: db, writable: false, working: db.snapshot()}
	defer func() { tx.done = true }()
	return fn(tx)
}

// Writable reports whether the transaction may perform writes.
func (tx *Tx) Writable() bool { return tx.writable }

// Get returns the value stored under key within the transaction and whether it
// was present. A writable transaction observes its own uncommitted writes.
func (tx *Tx) Get(key []byte) ([]byte, bool, error) {
	if tx.done {
		return nil, false, ErrTxClosed
	}
	v, ok := get(tx.working, key)
	return v, ok, nil
}

// Has reports whether key is present within the transaction.
func (tx *Tx) Has(key []byte) (bool, error) {
	if tx.done {
		return false, ErrTxClosed
	}
	_, ok := get(tx.working, key)
	return ok, nil
}

// Set buffers a write of value under key. It is only valid on a writable
// transaction. The key and value are copied.
func (tx *Tx) Set(key, value []byte) error {
	if tx.done {
		return ErrTxClosed
	}
	if !tx.writable {
		return ErrTxNotWritable
	}
	if len(key) == 0 {
		return ErrEmptyKey
	}
	k, v := cloneBytes(key), cloneBytes(value)
	tx.ops = append(tx.ops, op{kind: opSet, key: k, value: v})
	tx.working = insert(tx.working, k, v)
	return nil
}

// Delete buffers a deletion of key. It is only valid on a writable transaction.
func (tx *Tx) Delete(key []byte) error {
	if tx.done {
		return ErrTxClosed
	}
	if !tx.writable {
		return ErrTxNotWritable
	}
	if len(key) == 0 {
		return ErrEmptyKey
	}
	k := cloneBytes(key)
	tx.ops = append(tx.ops, op{kind: opDelete, key: k})
	tx.working = remove(tx.working, k)
	return nil
}

// Scan returns an iterator over the transaction's snapshot restricted to r. For
// a writable transaction the iterator reflects buffered writes.
func (tx *Tx) Scan(r Range) *Iterator {
	return newIterator(tx.working, r)
}
