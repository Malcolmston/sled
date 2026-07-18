package sled

import (
	"bytes"
	"encoding/binary"
	"path/filepath"
	"testing"
)

// This file encodes known-answer vectors taken directly from the upstream
// Rust sled crate's own integration tests (spacejam/sled, tests/test_tree.rs
// and tests/tree/mod.rs) as deterministic assertions against this Go port's
// public API. Each TestParity* function names the upstream test it mirrors.

// TestParityFixedStrideInserts mirrors upstream `fixed_stride_inserts`:
// insert 4096 big-endian u16 keys, confirm ascending iteration yields them in
// order, that Len is 4096, that reverse iteration also visits 4096, that
// overwriting values preserves the count, and that removing every key empties
// the tree.
func TestParityFixedStrideInserts(t *testing.T) {
	db := testDB(t)

	const n = 4096
	for k := 0; k < n; k++ {
		var b [2]byte
		binary.BigEndian.PutUint16(b[:], uint16(k))
		if err := db.Set(b[:], []byte{}); err != nil {
			t.Fatalf("Set(%d): %v", k, err)
		}
	}

	// Ascending iteration must visit keys in big-endian order 0..4096.
	count := 0
	for it := db.Scan(Range{}); it.Valid(); it.Next() {
		var b [2]byte
		binary.BigEndian.PutUint16(b[:], uint16(count))
		if !bytes.Equal(it.Key(), b[:]) {
			t.Fatalf("key %d: got %x want %x", count, it.Key(), b[:])
		}
		count++
	}
	if count != n {
		t.Fatalf("ascending count = %d, want %d", count, n)
	}
	if db.Len() != n {
		t.Fatalf("Len = %d, want %d", db.Len(), n)
	}

	// Reverse iteration must visit the same number of keys.
	rcount := 0
	for it := db.Scan(Range{Reverse: true}); it.Valid(); it.Next() {
		rcount++
	}
	if rcount != n {
		t.Fatalf("reverse count = %d, want %d", rcount, n)
	}

	// Overwriting every value leaves the key set (and count) unchanged.
	for k := 0; k < n; k++ {
		var b [2]byte
		binary.BigEndian.PutUint16(b[:], uint16(k))
		if err := db.Set(b[:], []byte{1}); err != nil {
			t.Fatalf("overwrite Set(%d): %v", k, err)
		}
	}
	if db.Len() != n {
		t.Fatalf("Len after overwrite = %d, want %d", db.Len(), n)
	}

	// Removing every key empties the tree.
	for k := 0; k < n; k++ {
		var b [2]byte
		binary.BigEndian.PutUint16(b[:], uint16(k))
		if err := db.Delete(b[:]); err != nil {
			t.Fatalf("Delete(%d): %v", k, err)
		}
	}
	if db.Len() != 0 {
		t.Fatalf("Len after clear = %d, want 0", db.Len())
	}
	if empty, _ := db.Has([]byte{0, 0}); empty {
		t.Fatalf("key 0 should be gone")
	}
	if !db.IsEmpty() {
		t.Fatalf("db should be empty")
	}
}

// TestParitySequentialAndReverseInserts mirrors upstream `sequential_inserts`
// and `reverse_inserts`: for several sizes, inserting that many distinct keys
// (in forward and reverse numeric order) must make forward and reverse
// iteration both visit exactly that many keys.
func TestParitySequentialAndReverseInserts(t *testing.T) {
	for _, length := range []int{1, 16, 32, 4096} {
		t.Run("seq", func(t *testing.T) {
			db := testDB(t)
			for i := 0; i < length; i++ {
				var b [4]byte
				binary.LittleEndian.PutUint32(b[:], uint32(i))
				if err := db.Set(b[:], []byte{}); err != nil {
					t.Fatal(err)
				}
			}
			if got := scanCount(db, Range{}); got != length {
				t.Fatalf("forward count = %d, want %d", got, length)
			}
			if got := scanCount(db, Range{Reverse: true}); got != length {
				t.Fatalf("reverse count = %d, want %d", got, length)
			}
		})
		t.Run("rev", func(t *testing.T) {
			db := testDB(t)
			for i := 0; i < length; i++ {
				var b [4]byte
				binary.LittleEndian.PutUint32(b[:], uint32(0xFFFFFFFF-uint32(i)))
				if err := db.Set(b[:], []byte{}); err != nil {
					t.Fatal(err)
				}
			}
			if got := scanCount(db, Range{}); got != length {
				t.Fatalf("forward count = %d, want %d", got, length)
			}
			if got := scanCount(db, Range{Reverse: true}); got != length {
				t.Fatalf("reverse count = %d, want %d", got, length)
			}
		})
	}
}

func scanCount(db *DB, r Range) int {
	c := 0
	for it := db.Scan(r); it.Valid(); it.Next() {
		c++
	}
	return c
}

// TestParityPopFirst mirrors upstream `test_pop_first`: inserting keys [0]..[5]
// and popping the smallest repeatedly must yield [0],[1],[2],[3],[4],[5] and
// then report the tree empty. This port exposes the operation as PopMin.
func TestParityPopFirst(t *testing.T) {
	db := testDB(t)
	for i := byte(0); i <= 5; i++ {
		if err := db.Set([]byte{i}, []byte{i * 10}); err != nil {
			t.Fatal(err)
		}
	}
	for want := byte(0); want <= 5; want++ {
		k, _, ok, err := db.PopMin()
		if err != nil {
			t.Fatal(err)
		}
		if !ok || !bytes.Equal(k, []byte{want}) {
			t.Fatalf("PopMin = %x, ok=%v, want %x", k, ok, []byte{want})
		}
	}
	if _, _, ok, _ := db.PopMin(); ok {
		t.Fatalf("PopMin on empty tree should report ok=false")
	}
}

// TestParityPopLastInRange mirrors upstream `test_pop_last_in_range`. The Rust
// vectors use inclusive/exclusive range bounds; this port's [Range] is
// half-open [Lower, Upper), so an inclusive upper bound of "key 3" is expressed
// as Upper="key 3\x00" (any value strictly greater than "key 3").
func TestParityPopLastInRange(t *testing.T) {
	db := testDB(t)
	for _, kv := range [][2]string{{"key 1", "value 1"}, {"key 2", "value 2"}, {"key 3", "value 3"}} {
		if err := db.Set([]byte(kv[0]), []byte(kv[1])); err != nil {
			t.Fatal(err)
		}
	}
	// helper: inclusive upper bound "hi" == exclusive "hi\x00".
	incl := func(s string) []byte { return append([]byte(s), 0x00) }

	// "key 1"..="key 3" -> ("key 3","value 3")
	assertPop(t, db, Range{Lower: []byte("key 1"), Upper: incl("key 3")}, "key 3", "value 3")
	// "key 1".."key 3" -> ("key 2","value 2")
	assertPop(t, db, Range{Lower: []byte("key 1"), Upper: []byte("key 3")}, "key 2", "value 2")
	// "key 4"..  -> None
	assertNoPop(t, db, Range{Lower: []byte("key 4")})
	// "key 2"..="key 3" -> None (both already popped)
	assertNoPop(t, db, Range{Lower: []byte("key 2"), Upper: incl("key 3")})
	// "key 0"..="key 3" -> ("key 1","value 1")
	assertPop(t, db, Range{Lower: []byte("key 0"), Upper: incl("key 3")}, "key 1", "value 1")
	// "key 0"..="key 3" -> None
	assertNoPop(t, db, Range{Lower: []byte("key 0"), Upper: incl("key 3")})
}

func assertPop(t *testing.T, db *DB, r Range, wantK, wantV string) {
	t.Helper()
	k, v, ok, err := db.PopMaxInRange(r)
	if err != nil {
		t.Fatal(err)
	}
	if !ok || string(k) != wantK || string(v) != wantV {
		t.Fatalf("PopMaxInRange(%+v) = (%q,%q,%v), want (%q,%q,true)", r, k, v, ok, wantK, wantV)
	}
}

func assertNoPop(t *testing.T, db *DB, r Range) {
	t.Helper()
	if k, _, ok, err := db.PopMaxInRange(r); err != nil || ok {
		t.Fatalf("PopMaxInRange(%+v) = (%q,ok=%v,err=%v), want empty", r, k, ok, err)
	}
}

// TestParityTreeRange mirrors upstream `tree_range`: with keys "0".."5",
// verify forward and reverse bounded ranges yield exactly the expected keys.
func TestParityTreeRange(t *testing.T) {
	db := testDB(t)
	for i := 0; i <= 5; i++ {
		if err := db.Set([]byte{byte('0' + i)}, []byte{byte(i * 10)}); err != nil {
			t.Fatal(err)
		}
	}
	// range "2".."4" ascending -> "2","3"
	assertKeys(t, db, Range{Lower: []byte("2"), Upper: []byte("4")}, "2", "3")
	// range "2".."4" reversed -> "3","2"
	assertKeys(t, db, Range{Lower: []byte("2"), Upper: []byte("4"), Reverse: true}, "3", "2")
	// range "2".. ascending -> "2","3","4","5"
	assertKeys(t, db, Range{Lower: []byte("2")}, "2", "3", "4", "5")
	// range ..="2" reversed -> "2","1","0"
	assertKeys(t, db, Range{Upper: append([]byte("2"), 0x00), Reverse: true}, "2", "1", "0")
}

func assertKeys(t *testing.T, db *DB, r Range, want ...string) {
	t.Helper()
	var got []string
	for it := db.Scan(r); it.Valid(); it.Next() {
		got = append(got, string(it.Key()))
	}
	if len(got) != len(want) {
		t.Fatalf("Scan(%+v) = %v, want %v", r, got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("Scan(%+v)[%d] = %q, want %q (full %v)", r, i, got[i], want[i], got)
		}
	}
}

// TestParityRecoverTree mirrors upstream `recover_tree`: inserting N keys,
// closing, reopening and reading them back must return the same values; after
// removing them all and reopening, they must all be absent.
func TestParityRecoverTree(t *testing.T) {
	path := filepath.Join(t.TempDir(), "recover.sled")
	const n = 500

	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < n; i++ {
		k := []byte(kvKey(i))
		if err := db.Set(k, k); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	db, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < n; i++ {
		k := []byte(kvKey(i))
		v, ok, err := db.Get(k)
		if err != nil || !ok || !bytes.Equal(v, k) {
			t.Fatalf("after recover Get(%q) = (%q,%v,%v)", k, v, ok, err)
		}
		if err := db.Delete(k); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	db, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for i := 0; i < n; i++ {
		k := []byte(kvKey(i))
		if _, ok, _ := db.Get(k); ok {
			t.Fatalf("key %q should have been deleted", k)
		}
	}
	if !db.IsEmpty() {
		t.Fatalf("recovered db should be empty")
	}
}

// kvKey builds a fixed-width decimal key, mirroring the upstream `kv` helper's
// role of producing distinct ordered keys.
func kvKey(i int) string {
	var b [8]byte
	binary.BigEndian.PutUint64(b[:], uint64(i))
	return string(b[:])
}

// TestParityPopFirstInRange checks the symmetric range-bounded minimum pop,
// mirroring upstream Tree::pop_first_in_range semantics.
func TestParityPopFirstInRange(t *testing.T) {
	db := testDB(t)
	for _, kv := range [][2]string{{"key 1", "value 1"}, {"key 2", "value 2"}, {"key 3", "value 3"}} {
		if err := db.Set([]byte(kv[0]), []byte(kv[1])); err != nil {
			t.Fatal(err)
		}
	}
	k, v, ok, err := db.PopMinInRange(Range{Lower: []byte("key 2")})
	if err != nil || !ok || string(k) != "key 2" || string(v) != "value 2" {
		t.Fatalf("PopMinInRange = (%q,%q,%v,%v), want key 2/value 2", k, v, ok, err)
	}
	// Now smallest overall is "key 1"; bounded below by "key 2" leaves "key 3".
	k, _, ok, _ = db.PopMinInRange(Range{Lower: []byte("key 2")})
	if !ok || string(k) != "key 3" {
		t.Fatalf("PopMinInRange = %q, want key 3", k)
	}
	if _, _, ok, _ := db.PopMinInRange(Range{Lower: []byte("key 2")}); ok {
		t.Fatalf("range should now be empty")
	}
}
