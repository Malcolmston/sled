package sled

// Tx is a transaction handle. A read-only transaction (created by View) reads
// from immutable snapshots captured when it begins. A read/write transaction
// (created by Update) additionally buffers mutations that become durable
// atomically when the transaction function returns nil, and are discarded if it
// returns an error.
//
// A single transaction may span several trees: the tree-scoped methods
// ([Tx.SetTree], [Tx.GetTree], [Tx.DeleteTree], [Tx.ScanTree]) operate on any
// [Tree] opened from the same DB, and all of a transaction's writes across all
// trees commit together in one durable record or not at all. The plain
// [Tx.Set]/[Tx.Get]/[Tx.Delete]/[Tx.Scan] methods operate on the default tree.
//
// A Tx is only valid inside the function passed to View or Update and must not
// be retained or used after that function returns.
type Tx struct {
	db       *DB
	writable bool
	done     bool

	// working maps a tree name to that tree's view within the transaction: the
	// starting snapshot, plus any of the Tx's own buffered writes so reads
	// observe uncommitted changes. Roots are captured lazily on first touch so
	// a Tx over one tree does not snapshot the others.
	working map[string]*node

	// touched preserves the order in which trees were first written, so commit
	// publishes their new roots deterministically.
	touched []*Tree
	dirty   map[string]bool

	// ops records buffered mutations in order; they are encoded into a single
	// log record on commit. events mirror them for subscriber delivery.
	ops    []op
	events []Event
}

// Update executes fn within a read/write transaction. Writers are serialized,
// so the transaction runs alone with respect to other writers. If fn returns
// nil the buffered writes are committed atomically and durably across every tree
// they touched; if fn returns an error (or panics) the writes are rolled back
// and nothing is persisted. The error from fn (or the commit) is returned.
func (db *DB) Update(fn func(*Tx) error) error {
	db.mu.Lock()
	defer db.mu.Unlock()
	if db.closed.Load() {
		return ErrClosed
	}

	tx := db.newTx(true)
	// If fn panics the deferred unlock still runs and, crucially, we never reach
	// the commit below, so a panic rolls the transaction back.
	defer func() { tx.done = true }()

	if err := fn(tx); err != nil {
		return err // rollback: buffered writes discarded, nothing written.
	}
	if len(tx.ops) == 0 {
		return nil
	}
	updates := make([]treeUpdate, 0, len(tx.touched))
	for _, t := range tx.touched {
		updates = append(updates, treeUpdate{t, tx.working[t.name]})
	}
	return db.commit(tx.ops, updates, tx.events)
}

// View executes fn within a read-only transaction over consistent immutable
// snapshots taken as each tree is first read. Concurrent writes do not affect
// what the transaction sees. Any attempt to mutate through the Tx returns
// ErrTxNotWritable.
func (db *DB) View(fn func(*Tx) error) error {
	if db.closed.Load() {
		return ErrClosed
	}
	tx := db.newTx(false)
	defer func() { tx.done = true }()
	return fn(tx)
}

// newTx builds a transaction handle with lazily populated per-tree working sets.
func (db *DB) newTx(writable bool) *Tx {
	return &Tx{
		db:       db,
		writable: writable,
		working:  make(map[string]*node),
		dirty:    make(map[string]bool),
	}
}

// rootFor returns the transaction's working root for t, capturing the tree's
// current snapshot on first use.
func (tx *Tx) rootFor(t *Tree) *node {
	if r, ok := tx.working[t.name]; ok {
		return r
	}
	r := t.snapshot()
	tx.working[t.name] = r
	return r
}

// Writable reports whether the transaction may perform writes.
func (tx *Tx) Writable() bool { return tx.writable }

// Get returns the value stored under key in the default tree within the
// transaction. A writable transaction observes its own uncommitted writes.
func (tx *Tx) Get(key []byte) ([]byte, bool, error) { return tx.GetTree(tx.db.def, key) }

// Has reports whether key is present in the default tree within the transaction.
func (tx *Tx) Has(key []byte) (bool, error) {
	_, ok, err := tx.GetTree(tx.db.def, key)
	return ok, err
}

// Set buffers a write of value under key in the default tree. See [Tx.SetTree].
func (tx *Tx) Set(key, value []byte) error { return tx.SetTree(tx.db.def, key, value) }

// Delete buffers a deletion of key in the default tree. See [Tx.DeleteTree].
func (tx *Tx) Delete(key []byte) error { return tx.DeleteTree(tx.db.def, key) }

// Scan returns an iterator over the default tree within the transaction. See
// [Tx.ScanTree].
func (tx *Tx) Scan(r Range) *Iterator { return tx.ScanTree(tx.db.def, r) }

// GetTree returns the value stored under key in t within the transaction and
// whether it was present. A writable transaction observes its own uncommitted
// writes to t.
func (tx *Tx) GetTree(t *Tree, key []byte) ([]byte, bool, error) {
	if tx.done {
		return nil, false, ErrTxClosed
	}
	v, ok := get(tx.rootFor(t), key)
	return v, ok, nil
}

// SetTree buffers a write of value under key in tree t. It is only valid on a
// writable transaction. The key and value are copied.
func (tx *Tx) SetTree(t *Tree, key, value []byte) error {
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
	root := tx.rootFor(t)
	tx.events = append(tx.events, Event{Type: eventKind(root, k, false), tree: t, Key: k, Value: v})
	tx.ops = append(tx.ops, t.setOp(k, v))
	tx.mark(t, insert(root, k, v))
	return nil
}

// DeleteTree buffers a deletion of key from tree t. It is only valid on a
// writable transaction.
func (tx *Tx) DeleteTree(t *Tree, key []byte) error {
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
	root := tx.rootFor(t)
	if _, ok := get(root, k); ok {
		tx.events = append(tx.events, Event{Type: EventDelete, tree: t, Key: k})
	}
	tx.ops = append(tx.ops, t.delOp(k))
	tx.mark(t, remove(root, k))
	return nil
}

// ScanTree returns an iterator over tree t within the transaction, reflecting
// buffered writes for a writable transaction. Set r.Reverse for descending
// order.
func (tx *Tx) ScanTree(t *Tree, r Range) *Iterator {
	return newIterator(tx.rootFor(t), r)
}

// mark records a new working root for t, tracking t as touched the first time
// it is written so commit publishes it.
func (tx *Tx) mark(t *Tree, root *node) {
	if !tx.dirty[t.name] {
		tx.dirty[t.name] = true
		tx.touched = append(tx.touched, t)
	}
	tx.working[t.name] = root
}
