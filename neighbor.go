package sled

import "bytes"

// minNode returns the node holding the smallest key, or nil if the tree is
// empty.
func minNode(n *node) *node {
	if n == nil {
		return nil
	}
	for n.left != nil {
		n = n.left
	}
	return n
}

// maxNode returns the node holding the largest key, or nil if the tree is empty.
func maxNode(n *node) *node {
	if n == nil {
		return nil
	}
	for n.right != nil {
		n = n.right
	}
	return n
}

// neighbor finds an ordered neighbor of key. When greater is true it returns the
// smallest key greater than (orEqual false) or greater than or equal to
// (orEqual true) key; when greater is false it returns the largest key less than
// or less than or equal to key. It returns nil when no such key exists.
func neighbor(n *node, key []byte, greater, orEqual bool) *node {
	var best *node
	for n != nil {
		cmp := bytes.Compare(n.key, key)
		if cmp == 0 && orEqual {
			return n // exact match is always the tightest neighbor.
		}
		if greater {
			if cmp > 0 {
				best = n // candidate; look left for something smaller but still >.
				n = n.left
			} else {
				n = n.right
			}
		} else {
			if cmp < 0 {
				best = n // candidate; look right for something larger but still <.
				n = n.right
			} else {
				n = n.left
			}
		}
	}
	return best
}
