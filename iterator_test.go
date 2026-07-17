package sled

import (
	"fmt"
	"testing"
)

func seed(t *testing.T, db *DB, keys ...string) {
	t.Helper()
	for _, k := range keys {
		if err := db.Set([]byte(k), []byte("v-"+k)); err != nil {
			t.Fatal(err)
		}
	}
}

func collect(it *Iterator) []string {
	var out []string
	for it.Valid() {
		out = append(out, string(it.Key()))
		it.Next()
	}
	return out
}

func equalSlice(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestScanAscendingOrder(t *testing.T) {
	db := testDB(t)
	// Insert in scrambled order.
	seed(t, db, "d", "a", "c", "b", "e", "aa", "ab")
	got := collect(db.Scan(Range{}))
	want := []string{"a", "aa", "ab", "b", "c", "d", "e"}
	if !equalSlice(got, want) {
		t.Fatalf("scan order = %v, want %v", got, want)
	}
	// Values must accompany keys correctly.
	it := db.Scan(Range{})
	for it.Valid() {
		if string(it.Value()) != "v-"+string(it.Key()) {
			t.Fatalf("value mismatch for %q: %q", it.Key(), it.Value())
		}
		it.Next()
	}
}

func TestScanLowerUpperBounds(t *testing.T) {
	db := testDB(t)
	seed(t, db, "a", "b", "c", "d", "e", "f")

	// [b, e): inclusive lower, exclusive upper.
	got := collect(db.Scan(Range{Lower: []byte("b"), Upper: []byte("e")}))
	if want := []string{"b", "c", "d"}; !equalSlice(got, want) {
		t.Fatalf("bounded scan = %v, want %v", got, want)
	}

	// Lower only.
	got = collect(db.Scan(Range{Lower: []byte("d")}))
	if want := []string{"d", "e", "f"}; !equalSlice(got, want) {
		t.Fatalf("lower-only scan = %v, want %v", got, want)
	}

	// Upper only.
	got = collect(db.Scan(Range{Upper: []byte("c")}))
	if want := []string{"a", "b"}; !equalSlice(got, want) {
		t.Fatalf("upper-only scan = %v, want %v", got, want)
	}

	// Empty range.
	got = collect(db.Scan(Range{Lower: []byte("x"), Upper: []byte("z")}))
	if len(got) != 0 {
		t.Fatalf("empty range scan = %v, want []", got)
	}
}

func TestScanPrefix(t *testing.T) {
	db := testDB(t)
	seed(t, db,
		"user:1", "user:2", "user:10",
		"userx", // adjacent but not under "user:" prefix
		"account:1",
		"zzz",
	)
	got := collect(db.Scan(Range{Prefix: []byte("user:")}))
	want := []string{"user:1", "user:10", "user:2"}
	if !equalSlice(got, want) {
		t.Fatalf("prefix scan = %v, want %v", got, want)
	}

	// Prefix combined with a lower bound.
	got = collect(db.Scan(Range{Prefix: []byte("user:"), Lower: []byte("user:2")}))
	if want := []string{"user:2"}; !equalSlice(got, want) {
		t.Fatalf("prefix+lower scan = %v, want %v", got, want)
	}
}

func TestScanPrefixAllFF(t *testing.T) {
	db := testDB(t)
	if err := db.Set([]byte{0xff, 0xff}, []byte("hi")); err != nil {
		t.Fatal(err)
	}
	if err := db.Set([]byte{0xff, 0xff, 0x00}, []byte("under")); err != nil {
		t.Fatal(err)
	}
	if err := db.Set([]byte{0xfe}, []byte("below")); err != nil {
		t.Fatal(err)
	}
	// Prefix of all-0xff has no finite upper bound; must still scan correctly.
	it := db.Scan(Range{Prefix: []byte{0xff, 0xff}})
	var n int
	for it.Valid() {
		if it.Key()[0] != 0xff {
			t.Fatalf("prefix scan returned out-of-prefix key %v", it.Key())
		}
		n++
		it.Next()
	}
	if n != 2 {
		t.Fatalf("all-ff prefix scan returned %d keys, want 2", n)
	}
}

func TestScanEmptyDB(t *testing.T) {
	db := testDB(t)
	it := db.Scan(Range{})
	if it.Valid() {
		t.Fatal("iterator over empty DB is valid")
	}
	if it.Key() != nil || it.Value() != nil {
		t.Fatal("Key/Value non-nil on invalid iterator")
	}
	it.Next() // must be a safe no-op
	if it.Valid() {
		t.Fatal("Next made an empty iterator valid")
	}
}

func TestScanLargeOrdered(t *testing.T) {
	db := testDB(t)
	const n = 1000
	for i := 0; i < n; i++ {
		k := fmt.Sprintf("key-%05d", (i*7919)%n) // scrambled insertion
		if err := db.Set([]byte(k), []byte("x")); err != nil {
			t.Fatal(err)
		}
	}
	got := collect(db.Scan(Range{}))
	if len(got) != n {
		t.Fatalf("scanned %d keys, want %d", len(got), n)
	}
	for i := 1; i < len(got); i++ {
		if got[i-1] >= got[i] {
			t.Fatalf("scan not strictly ascending at %d: %q >= %q", i, got[i-1], got[i])
		}
	}
}
