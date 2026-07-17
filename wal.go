package sled

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"io"
	"os"
)

// opKind identifies the kind of a single operation stored in the log.
type opKind uint8

const (
	opSet    opKind = 1
	opDelete opKind = 2
)

// op is a single mutation. For opDelete, value is nil.
type op struct {
	kind  opKind
	key   []byte
	value []byte
}

// maxRecordPayload bounds the size of a single decoded record. It guards
// replay against absurd length prefixes produced by corruption, so recovery
// never attempts a giant allocation. It comfortably exceeds any realistic
// batch while remaining a sane ceiling.
const maxRecordPayload = 1 << 30 // 1 GiB

var crcTable = crc32.MakeTable(crc32.IEEE)

// encodeRecord serializes a group of operations into a single self-describing,
// CRC-protected record: [uint32 len][uint32 crc][payload].
func encodeRecord(ops []op) []byte {
	// Payload: uvarint count, then per op: kind byte, uvarint keylen, key,
	// and for Set an additional uvarint vallen and value.
	var payload []byte
	var scratch [binary.MaxVarintLen64]byte

	n := binary.PutUvarint(scratch[:], uint64(len(ops)))
	payload = append(payload, scratch[:n]...)

	for _, o := range ops {
		payload = append(payload, byte(o.kind))
		n = binary.PutUvarint(scratch[:], uint64(len(o.key)))
		payload = append(payload, scratch[:n]...)
		payload = append(payload, o.key...)
		if o.kind == opSet {
			n = binary.PutUvarint(scratch[:], uint64(len(o.value)))
			payload = append(payload, scratch[:n]...)
			payload = append(payload, o.value...)
		}
	}

	rec := make([]byte, 8+len(payload))
	binary.LittleEndian.PutUint32(rec[0:4], uint32(len(payload)))
	binary.LittleEndian.PutUint32(rec[4:8], crc32.Checksum(payload, crcTable))
	copy(rec[8:], payload)
	return rec
}

// decodePayload parses a record payload back into its operations. It returns an
// error if the bytes are structurally invalid.
func decodePayload(payload []byte) ([]op, error) {
	r := bytesReader{buf: payload}
	count, err := r.uvarint()
	if err != nil {
		return nil, err
	}
	if count > uint64(len(payload)) {
		return nil, fmt.Errorf("sled: record op count %d exceeds payload size", count)
	}
	ops := make([]op, 0, count)
	for i := uint64(0); i < count; i++ {
		kindByte, err := r.byte()
		if err != nil {
			return nil, err
		}
		kind := opKind(kindByte)
		if kind != opSet && kind != opDelete {
			return nil, fmt.Errorf("sled: unknown op kind %d", kindByte)
		}
		key, err := r.bytes()
		if err != nil {
			return nil, err
		}
		o := op{kind: kind, key: key}
		if kind == opSet {
			val, err := r.bytes()
			if err != nil {
				return nil, err
			}
			o.value = val
		}
		ops = append(ops, o)
	}
	if !r.done() {
		return nil, fmt.Errorf("sled: %d trailing bytes in record payload", r.remaining())
	}
	return ops, nil
}

// bytesReader is a minimal cursor over a byte slice used to decode payloads.
type bytesReader struct {
	buf []byte
	pos int
}

func (r *bytesReader) byte() (byte, error) {
	if r.pos >= len(r.buf) {
		return 0, io.ErrUnexpectedEOF
	}
	b := r.buf[r.pos]
	r.pos++
	return b, nil
}

func (r *bytesReader) uvarint() (uint64, error) {
	v, n := binary.Uvarint(r.buf[r.pos:])
	if n <= 0 {
		return 0, io.ErrUnexpectedEOF
	}
	r.pos += n
	return v, nil
}

func (r *bytesReader) bytes() ([]byte, error) {
	length, err := r.uvarint()
	if err != nil {
		return nil, err
	}
	if uint64(r.pos)+length > uint64(len(r.buf)) {
		return nil, io.ErrUnexpectedEOF
	}
	// Copy so the returned slice does not alias the shared payload buffer.
	out := make([]byte, length)
	copy(out, r.buf[r.pos:r.pos+int(length)])
	r.pos += int(length)
	return out, nil
}

func (r *bytesReader) done() bool     { return r.pos == len(r.buf) }
func (r *bytesReader) remaining() int { return len(r.buf) - r.pos }

// replay reads records from f in order, invoking apply for each intact record's
// operations. It stops at the first truncated or CRC-mismatched record, which
// marks the boundary of the last durable write, and returns the byte offset of
// the end of the last good record. The caller uses that offset to truncate any
// partial tail left behind by a crash.
func replay(f *os.File, apply func(ops []op) error) (int64, error) {
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return 0, err
	}
	size, err := fileSize(f)
	if err != nil {
		return 0, err
	}

	br := bufio.NewReader(f)
	var offset int64
	header := make([]byte, 8)

	// A record needs at least its 8-byte header. Anything less at the tail is a
	// torn write; stop cleanly at the last good offset.
	for size-offset >= 8 {
		if _, err := io.ReadFull(br, header); err != nil {
			break
		}
		payloadLen := binary.LittleEndian.Uint32(header[0:4])
		wantCRC := binary.LittleEndian.Uint32(header[4:8])

		// A length that runs past the end of the file is a torn write.
		if int64(payloadLen) > size-offset-8 || payloadLen > maxRecordPayload {
			break
		}

		payload := make([]byte, payloadLen)
		if _, err := io.ReadFull(br, payload); err != nil {
			break
		}
		if crc32.Checksum(payload, crcTable) != wantCRC {
			// Corrupt record: treat as the crash boundary.
			break
		}

		ops, err := decodePayload(payload)
		if err != nil {
			// Structurally invalid despite a matching CRC is extremely
			// unlikely, but treat it as the boundary rather than corrupting
			// the in-memory state.
			break
		}
		if err := apply(ops); err != nil {
			return offset, err
		}
		offset += int64(8 + payloadLen)
	}
	return offset, nil
}

func fileSize(f *os.File) (int64, error) {
	fi, err := f.Stat()
	if err != nil {
		return 0, err
	}
	return fi.Size(), nil
}
