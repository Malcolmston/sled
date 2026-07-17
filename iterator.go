package sled

import "bytes"

// Range describes a half-open key range for a scan. All fields are optional:
//
//   - Lower is an inclusive lower bound. Keys strictly less than Lower are
//     skipped. A nil Lower means unbounded below.
//   - Upper is an exclusive upper bound. Keys greater than or equal to Upper are
//     skipped. A nil Upper means unbounded above.
//   - Prefix restricts the scan to keys that begin with Prefix. It composes
//     with Lower/Upper (the effective range is the intersection).
//
// The zero Range scans every key in ascending order.
type Range struct {
	Lower  []byte
	Upper  []byte
	Prefix []byte
}

// Iterator yields key/value pairs in ascending key order over an immutable
// snapshot. Because the snapshot never changes, an Iterator is unaffected by
// concurrent writes. Keys and values returned by the iterator are owned by the
// DB and must not be modified.
//
// Typical use:
//
//	it := db.Scan(sled.Range{Prefix: []byte("user:")})
//	for it.Valid() {
//		k, v := it.Key(), it.Value()
//		_ = k
//		_ = v
//		it.Next()
//	}
type Iterator struct {
	stack  []*node
	upper  []byte
	prefix []byte
	cur    *node
}

// Scan returns an iterator over the current snapshot restricted to r.
func (db *DB) Scan(r Range) *Iterator {
	return newIterator(db.snapshot(), r)
}

func newIterator(root *node, r Range) *Iterator {
	lower := r.Lower
	upper := r.Upper
	if len(r.Prefix) > 0 {
		if lower == nil || bytes.Compare(lower, r.Prefix) < 0 {
			lower = r.Prefix
		}
		if pe := prefixUpperBound(r.Prefix); pe != nil {
			if upper == nil || bytes.Compare(pe, upper) < 0 {
				upper = pe
			}
		}
	}
	it := &Iterator{upper: upper, prefix: r.Prefix}
	it.seek(root, lower)
	it.advance()
	return it
}

// seek descends the tree pushing every node whose key is >= lower onto the
// traversal stack, so the top of the stack becomes the smallest in-range key.
func (it *Iterator) seek(n *node, lower []byte) {
	for n != nil {
		if lower == nil || bytes.Compare(n.key, lower) >= 0 {
			it.stack = append(it.stack, n)
			n = n.left
		} else {
			n = n.right
		}
	}
}

// pop returns the next node in ascending order and prepares the stack for the
// one after it.
func (it *Iterator) pop() *node {
	if len(it.stack) == 0 {
		return nil
	}
	n := it.stack[len(it.stack)-1]
	it.stack = it.stack[:len(it.stack)-1]
	for r := n.right; r != nil; r = r.left {
		it.stack = append(it.stack, r)
	}
	return n
}

// advance moves cur to the next in-range key, or nil if the range is exhausted.
func (it *Iterator) advance() {
	for {
		n := it.pop()
		if n == nil {
			it.cur = nil
			return
		}
		if it.upper != nil && bytes.Compare(n.key, it.upper) >= 0 {
			it.cur = nil
			return
		}
		if len(it.prefix) > 0 && !bytes.HasPrefix(n.key, it.prefix) {
			continue
		}
		it.cur = n
		return
	}
}

// Valid reports whether the iterator is positioned at a key.
func (it *Iterator) Valid() bool { return it.cur != nil }

// Next advances to the following key. Calling Next when the iterator is not
// valid is a no-op.
func (it *Iterator) Next() {
	if it.cur == nil {
		return
	}
	it.advance()
}

// Key returns the current key. It must not be modified and is only valid while
// the iterator is valid.
func (it *Iterator) Key() []byte {
	if it.cur == nil {
		return nil
	}
	return it.cur.key
}

// Value returns the current value. It must not be modified.
func (it *Iterator) Value() []byte {
	if it.cur == nil {
		return nil
	}
	return it.cur.value
}

// prefixUpperBound returns the smallest key that is greater than every key with
// the given prefix, i.e. the exclusive upper bound of the prefix range. It
// returns nil when the prefix is empty or consists entirely of 0xff bytes, in
// which case there is no finite upper bound.
func prefixUpperBound(prefix []byte) []byte {
	for i := len(prefix) - 1; i >= 0; i-- {
		if prefix[i] != 0xff {
			out := make([]byte, i+1)
			copy(out, prefix[:i+1])
			out[i]++
			return out
		}
	}
	return nil
}
