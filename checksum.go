package sled

import (
	"encoding/binary"
	"hash"
	"hash/crc32"
)

// Checksum returns a deterministic CRC-32 (IEEE) digest of the tree's entire
// live key/value set. The digest depends only on the set of keys and their
// values, not on insertion order or internal tree shape, so two trees with the
// same logical contents always produce the same checksum. It is useful for
// cheaply comparing trees or verifying a round-trip through Export/Import.
//
// This mirrors sled's Tree::checksum. It is computed over an immutable snapshot
// and does not block writers.
func (t *Tree) Checksum() uint32 {
	h := crc32.New(crcTable)
	checksumTree(h, t.snapshot())
	return h.Sum32()
}

// Checksum returns a deterministic CRC-32 digest over every tree in the
// database, folding in each tree's name so trees are distinguished. Databases
// with identical logical contents (same trees, keys and values) produce the
// same value regardless of internal layout. See [Tree.Checksum].
func (db *DB) Checksum() uint32 {
	h := crc32.New(crcTable)
	for _, name := range db.TreeNames() {
		db.treesMu.RLock()
		t := db.trees[name]
		db.treesMu.RUnlock()
		if t == nil {
			continue
		}
		checksumChunk(h, []byte(name))
		checksumTree(h, t.snapshot())
	}
	return h.Sum32()
}

// checksumTree folds every entry of root, in ascending key order, into h using
// length-prefixed framing so that (key, value) boundaries are unambiguous.
func checksumTree(h hash.Hash32, root *node) {
	inorder(root, func(n *node) bool {
		checksumChunk(h, n.key)
		checksumChunk(h, n.value)
		return true
	})
}

// checksumChunk writes b into h preceded by its length, so distinct splits of
// the same concatenated bytes hash differently.
func checksumChunk(h hash.Hash32, b []byte) {
	var lenBuf [binary.MaxVarintLen64]byte
	n := binary.PutUvarint(lenBuf[:], uint64(len(b)))
	_, _ = h.Write(lenBuf[:n])
	_, _ = h.Write(b)
}
