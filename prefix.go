package sled

// ScanPrefix returns an iterator over every key in this tree that begins with
// prefix, in ascending key order. A nil or empty prefix scans the whole tree.
// It is shorthand for Scan(Range{Prefix: prefix}) and mirrors sled's
// Tree::scan_prefix. The iterator reads an immutable snapshot and is unaffected
// by concurrent writes.
func (t *Tree) ScanPrefix(prefix []byte) *Iterator {
	return newIterator(t.snapshot(), Range{Prefix: prefix})
}

// ScanPrefix returns an iterator over keys in the default tree that begin with
// prefix. See [Tree.ScanPrefix].
func (db *DB) ScanPrefix(prefix []byte) *Iterator {
	return db.def.ScanPrefix(prefix)
}

// PrefixRange builds a [Range] that selects every key beginning with prefix. It
// is a convenience for callers that want to combine a prefix filter with other
// Range fields, such as Reverse:
//
//	r := sled.PrefixRange([]byte("user:"))
//	r.Reverse = true
//	it := t.Scan(r)
//
// The prefix is copied, so the caller may reuse the buffer.
func PrefixRange(prefix []byte) Range {
	return Range{Prefix: cloneBytes(prefix)}
}
