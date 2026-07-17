package sled

import (
	"bytes"
	"hash/fnv"
)

// node is one entry in an immutable persistent treap. A node is never mutated
// after it is constructed; updates produce new nodes that structurally share
// the untouched subtrees of the previous version. This immutability is what
// lets readers traverse an old root concurrently with a writer publishing a new
// one without any locking.
//
// The tree is ordered as a binary search tree on key and as a max-heap on pri.
// Because pri is derived deterministically from the key, the shape of the tree
// is a function of its key set alone, which keeps it balanced in expectation.
type node struct {
	key   []byte
	value []byte
	pri   uint64
	left  *node
	right *node
}

// priority derives a node priority from its key. Any deterministic hash with
// good distribution keeps the treap balanced; FNV-1a is cheap and dependency
// free.
func priority(key []byte) uint64 {
	h := fnv.New64a()
	_, _ = h.Write(key)
	return h.Sum64()
}

// get returns the value stored for key and whether it was found.
func get(n *node, key []byte) ([]byte, bool) {
	for n != nil {
		switch cmp := bytes.Compare(key, n.key); {
		case cmp == 0:
			return n.value, true
		case cmp < 0:
			n = n.left
		default:
			n = n.right
		}
	}
	return nil, false
}

// insert returns a new tree with key set to value. If key already exists its
// value is replaced and the tree shape is preserved.
func insert(n *node, key, value []byte) *node {
	if n == nil {
		return &node{key: key, value: value, pri: priority(key)}
	}
	cmp := bytes.Compare(key, n.key)
	if cmp == 0 {
		return &node{key: n.key, value: value, pri: n.pri, left: n.left, right: n.right}
	}
	if cmp < 0 {
		nn := &node{key: n.key, value: n.value, pri: n.pri, left: insert(n.left, key, value), right: n.right}
		if nn.left.pri > nn.pri {
			return rotateRight(nn)
		}
		return nn
	}
	nn := &node{key: n.key, value: n.value, pri: n.pri, left: n.left, right: insert(n.right, key, value)}
	if nn.right.pri > nn.pri {
		return rotateLeft(nn)
	}
	return nn
}

// remove returns a new tree with key deleted. If key is absent the original
// tree is returned unchanged.
func remove(n *node, key []byte) *node {
	if n == nil {
		return nil
	}
	cmp := bytes.Compare(key, n.key)
	switch {
	case cmp < 0:
		return &node{key: n.key, value: n.value, pri: n.pri, left: remove(n.left, key), right: n.right}
	case cmp > 0:
		return &node{key: n.key, value: n.value, pri: n.pri, left: n.left, right: remove(n.right, key)}
	default:
		return merge(n.left, n.right)
	}
}

// rotateRight lifts n.left above n, preserving BST order. n.left is assumed
// non-nil. New nodes are allocated so the input trees are left untouched.
func rotateRight(n *node) *node {
	l := n.left
	newRight := &node{key: n.key, value: n.value, pri: n.pri, left: l.right, right: n.right}
	return &node{key: l.key, value: l.value, pri: l.pri, left: l.left, right: newRight}
}

// rotateLeft lifts n.right above n, preserving BST order. n.right is assumed
// non-nil.
func rotateLeft(n *node) *node {
	r := n.right
	newLeft := &node{key: n.key, value: n.value, pri: n.pri, left: n.left, right: r.left}
	return &node{key: r.key, value: r.value, pri: r.pri, left: newLeft, right: r.right}
}

// merge joins two treaps where every key in l is strictly less than every key
// in r, preserving both the BST and heap invariants.
func merge(l, r *node) *node {
	switch {
	case l == nil:
		return r
	case r == nil:
		return l
	case l.pri > r.pri:
		return &node{key: l.key, value: l.value, pri: l.pri, left: l.left, right: merge(l.right, r)}
	default:
		return &node{key: r.key, value: r.value, pri: r.pri, left: merge(l, r.left), right: r.right}
	}
}

// count returns the number of entries in the tree. It is O(n) and used for
// reporting, not on any hot path.
func count(n *node) int {
	if n == nil {
		return 0
	}
	return 1 + count(n.left) + count(n.right)
}
