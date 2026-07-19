package sled

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"hash"
	"hash/crc32"
	"io"
	"sort"
)

// exportMagic identifies a sled export stream and its format version. Bumping
// the trailing digit signals an incompatible layout change.
var exportMagic = [8]byte{'S', 'L', 'E', 'D', 'E', 'X', 'P', '1'}

// Export writes a self-describing, CRC-32 checksummed snapshot of every tree to
// w. The stream can be read back with [DB.Import], including into a different
// database, reproducing all trees and keys. Export reads a consistent snapshot
// per tree; it does not block writers for the whole dump.
//
// The layout is: an 8-byte magic, then for each tree its name and entries as
// length-prefixed byte strings, followed by a trailing CRC-32 over everything
// that precedes it. Import verifies the checksum before applying anything.
func (db *DB) Export(w io.Writer) error {
	if db.closed.Load() {
		return ErrClosed
	}
	bw := bufio.NewWriter(w)
	h := crc32.New(crcTable)
	mw := io.MultiWriter(bw, h)

	if _, err := mw.Write(exportMagic[:]); err != nil {
		return fmt.Errorf("sled: export magic: %w", err)
	}

	// Snapshot the tree set, then dump each tree in name order for a
	// deterministic stream.
	db.treesMu.RLock()
	names := make([]string, 0, len(db.trees))
	roots := make(map[string]*node, len(db.trees))
	for name, t := range db.trees {
		names = append(names, name)
		roots[name] = t.snapshot()
	}
	db.treesMu.RUnlock()
	sort.Strings(names)

	if err := writeUvarint(mw, uint64(len(names))); err != nil {
		return err
	}
	for _, name := range names {
		if err := writeChunk(mw, []byte(name)); err != nil {
			return err
		}
		root := roots[name]
		if err := writeUvarint(mw, uint64(count(root))); err != nil {
			return err
		}
		var walkErr error
		inorder(root, func(n *node) bool {
			if walkErr = writeChunk(mw, n.key); walkErr != nil {
				return false
			}
			if walkErr = writeChunk(mw, n.value); walkErr != nil {
				return false
			}
			return true
		})
		if walkErr != nil {
			return walkErr
		}
	}

	// Trailing checksum over the payload only (not into h itself).
	var sum [4]byte
	binary.LittleEndian.PutUint32(sum[:], h.Sum32())
	if _, err := bw.Write(sum[:]); err != nil {
		return fmt.Errorf("sled: export checksum: %w", err)
	}
	return bw.Flush()
}

// Import reads a stream produced by [DB.Export], verifies its checksum, and
// applies every entry, creating trees as needed. Existing keys with the same
// name are overwritten; keys not present in the stream are left untouched. The
// whole import is applied as one atomic, durable record, so a crash mid-import
// leaves the database unchanged. A checksum mismatch returns [ErrCorruptImport]
// and applies nothing.
func (db *DB) Import(r io.Reader) error {
	if db.closed.Load() {
		return ErrClosed
	}
	h := crc32.New(crcTable)
	br := bufio.NewReader(r)
	tr := io.TeeReader(br, h)

	var magic [8]byte
	if _, err := io.ReadFull(tr, magic[:]); err != nil {
		return fmt.Errorf("sled: import magic: %w", err)
	}
	if magic != exportMagic {
		return ErrCorruptImport
	}

	ops, events, err := db.readImport(tr, h)
	if err != nil {
		return err
	}

	db.mu.Lock()
	defer db.mu.Unlock()
	if db.closed.Load() {
		return ErrClosed
	}
	updates := db.importUpdates(ops)
	if len(ops) == 0 {
		return nil
	}
	return db.commit(ops, updates, events)
}

// readImport consumes the payload, verifies the trailing checksum, and returns
// the ops and events the import will commit. It does not mutate the database.
func (db *DB) readImport(tr io.Reader, h hash.Hash32) ([]op, []Event, error) {
	treeCount, err := readUvarint(tr)
	if err != nil {
		return nil, nil, err
	}
	var ops []op
	var events []Event
	for i := uint64(0); i < treeCount; i++ {
		nameBytes, err := readChunk(tr)
		if err != nil {
			return nil, nil, err
		}
		name := treeName(string(nameBytes))
		t := db.getOrCreateTree(name)
		entryCount, err := readUvarint(tr)
		if err != nil {
			return nil, nil, err
		}
		for j := uint64(0); j < entryCount; j++ {
			key, err := readChunk(tr)
			if err != nil {
				return nil, nil, err
			}
			value, err := readChunk(tr)
			if err != nil {
				return nil, nil, err
			}
			if len(key) == 0 {
				return nil, nil, ErrCorruptImport
			}
			ops = append(ops, t.setOp(key, value))
			events = append(events, Event{Type: EventInsert, tree: t, Key: key, Value: value})
		}
	}

	// Capture the checksum of the payload before reading the trailing 4 bytes.
	// Those bytes are folded into h too, but we ignore h afterward, so reading
	// them through the tee is harmless.
	want := h.Sum32()
	var sum [4]byte
	if _, err := io.ReadFull(tr, sum[:]); err != nil {
		return nil, nil, fmt.Errorf("sled: import checksum: %w", err)
	}
	if binary.LittleEndian.Uint32(sum[:]) != want {
		return nil, nil, ErrCorruptImport
	}
	return ops, events, nil
}

// importUpdates folds the import ops onto current roots and returns the per-tree
// updates to publish. The caller must hold db.mu.
func (db *DB) importUpdates(ops []op) []treeUpdate {
	roots := make(map[*Tree]*node)
	order := make([]*Tree, 0)
	for _, o := range ops {
		t := db.getOrCreateTree(treeName(o.tree))
		root, seen := roots[t]
		if !seen {
			root = t.snapshot()
			order = append(order, t)
		}
		roots[t] = insert(root, o.key, o.value)
	}
	updates := make([]treeUpdate, 0, len(order))
	for _, t := range order {
		updates = append(updates, treeUpdate{t, roots[t]})
	}
	return updates
}

func writeUvarint(w io.Writer, v uint64) error {
	var buf [binary.MaxVarintLen64]byte
	n := binary.PutUvarint(buf[:], v)
	if _, err := w.Write(buf[:n]); err != nil {
		return fmt.Errorf("sled: write varint: %w", err)
	}
	return nil
}

func readUvarint(r io.Reader) (uint64, error) {
	return binary.ReadUvarint(byteReaderOf(r))
}

// writeChunk writes a length-prefixed byte string.
func writeChunk(w io.Writer, b []byte) error {
	if err := writeUvarint(w, uint64(len(b))); err != nil {
		return err
	}
	if len(b) == 0 {
		return nil
	}
	if _, err := w.Write(b); err != nil {
		return fmt.Errorf("sled: write chunk: %w", err)
	}
	return nil
}

// readChunk reads a length-prefixed byte string written by writeChunk.
func readChunk(r io.Reader) ([]byte, error) {
	n, err := readUvarint(r)
	if err != nil {
		return nil, err
	}
	if n > maxRecordPayload {
		return nil, ErrCorruptImport
	}
	b := make([]byte, n)
	if _, err := io.ReadFull(r, b); err != nil {
		return nil, fmt.Errorf("sled: read chunk: %w", err)
	}
	return b, nil
}

// byteReaderOf adapts an io.Reader into an io.ByteReader for binary.ReadUvarint,
// reading one byte at a time so it never consumes past the varint.
func byteReaderOf(r io.Reader) io.ByteReader {
	if br, ok := r.(io.ByteReader); ok {
		return br
	}
	return &singleByteReader{r: r}
}

type singleByteReader struct {
	r   io.Reader
	buf [1]byte
}

// ReadByte implements io.ByteReader, reading and returning a single byte from
// the underlying reader. It returns the error from the underlying read (for
// example io.EOF or io.ErrUnexpectedEOF) if a byte cannot be read.
func (s *singleByteReader) ReadByte() (byte, error) {
	if _, err := io.ReadFull(s.r, s.buf[:]); err != nil {
		return 0, err
	}
	return s.buf[0], nil
}
