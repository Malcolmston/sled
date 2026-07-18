package sled

import (
	"bytes"
	"encoding/binary"
	"path/filepath"
	"testing"
)

func TestSubscriberEvents(t *testing.T) {
	db := testDB(t)
	sub := db.Watch([]byte("user:"))
	defer sub.Close()

	// Insert, update, delete under the watched prefix.
	_ = db.Set([]byte("user:1"), []byte("a"))
	_ = db.Set([]byte("user:1"), []byte("b")) // update
	_ = db.Delete([]byte("user:1"))
	// A key outside the prefix must not be delivered.
	_ = db.Set([]byte("other"), []byte("x"))

	want := []struct {
		typ EventType
		val string
	}{
		{EventInsert, "a"},
		{EventUpdate, "b"},
		{EventDelete, ""},
	}
	for i, w := range want {
		ev := <-sub.Events()
		if ev.Type != w.typ {
			t.Fatalf("event %d type = %v, want %v", i, ev.Type, w.typ)
		}
		if string(ev.Value) != w.val {
			t.Fatalf("event %d value = %q, want %q", i, ev.Value, w.val)
		}
		if !bytes.Equal(ev.Key, []byte("user:1")) {
			t.Fatalf("event %d key = %q", i, ev.Key)
		}
		if ev.Tree != DefaultTreeName {
			t.Fatalf("event %d tree = %q", i, ev.Tree)
		}
	}
	// No further event should be queued (the "other" write was filtered).
	select {
	case ev := <-sub.Events():
		t.Fatalf("unexpected extra event: %+v", ev)
	default:
	}
}

func TestSubscriberPerTreeAndTransaction(t *testing.T) {
	db := testDB(t)
	tr, _ := db.OpenTree("t")
	sub := tr.Watch(nil) // whole tree
	defer sub.Close()

	err := db.Update(func(tx *Tx) error {
		if err := tx.SetTree(tr, []byte("a"), []byte("1")); err != nil {
			return err
		}
		return tx.SetTree(tr, []byte("b"), []byte("2"))
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	got := map[string]string{}
	for i := 0; i < 2; i++ {
		ev := <-sub.Events()
		if ev.Tree != "t" {
			t.Fatalf("event tree = %q", ev.Tree)
		}
		got[string(ev.Key)] = string(ev.Value)
	}
	if got["a"] != "1" || got["b"] != "2" {
		t.Fatalf("transaction events = %v", got)
	}
}

func TestSubscriberEventTypeString(t *testing.T) {
	cases := map[EventType]string{
		EventInsert: "insert", EventUpdate: "update",
		EventDelete: "delete", EventType(99): "unknown",
	}
	for et, want := range cases {
		if got := et.String(); got != want {
			t.Fatalf("EventType(%d).String() = %q, want %q", et, got, want)
		}
	}
}

func TestSubscriberCloseIdempotent(t *testing.T) {
	db := testDB(t)
	sub := db.Watch(nil)
	sub.Close()
	sub.Close() // must not panic
	// A write after close must not panic even though delivery finds it closed.
	_ = db.Set([]byte("k"), []byte("v"))
}

// TestSubscriberBufferDrops verifies a slow subscriber drops events rather than
// blocking the writer, keeping commits non-blocking.
func TestSubscriberBufferDrops(t *testing.T) {
	db := testDB(t)
	sub := db.Watch(nil)
	defer sub.Close()
	total := defaultSubBuffer + 50
	for i := 0; i < total; i++ {
		var k [4]byte
		binary.BigEndian.PutUint32(k[:], uint32(i))
		if err := db.Set(k[:], []byte("v")); err != nil {
			t.Fatalf("Set: %v", err)
		}
	}
	// Draining yields at most the buffer capacity; the writer never blocked.
	got := 0
	for {
		select {
		case <-sub.Events():
			got++
			continue
		default:
		}
		break
	}
	if got > defaultSubBuffer {
		t.Fatalf("received %d events, exceeds buffer %d", got, defaultSubBuffer)
	}
}

func TestMergeOperator(t *testing.T) {
	db := testDB(t)
	// A merge operator that appends bytes (a simple concatenation fold).
	db.SetMergeOperator(func(key, old, merge []byte) []byte {
		return append(append([]byte{}, old...), merge...)
	})
	if _, err := db.Merge([]byte("log"), []byte("a")); err != nil {
		t.Fatalf("Merge: %v", err)
	}
	_, _ = db.Merge([]byte("log"), []byte("b"))
	merged, err := db.Merge([]byte("log"), []byte("c"))
	if err != nil {
		t.Fatalf("Merge: %v", err)
	}
	if string(merged) != "abc" {
		t.Fatalf("merged = %q, want abc", merged)
	}
	if v, _, _ := db.Get([]byte("log")); string(v) != "abc" {
		t.Fatalf("stored = %q, want abc", v)
	}
}

func TestMergeCounter(t *testing.T) {
	db := testDB(t)
	tr, _ := db.OpenTree("counters")
	tr.SetMergeOperator(func(key, old, merge []byte) []byte {
		var n uint64
		if len(old) == 8 {
			n = binary.BigEndian.Uint64(old)
		}
		n += binary.BigEndian.Uint64(merge)
		out := make([]byte, 8)
		binary.BigEndian.PutUint64(out, n)
		return out
	})
	inc := make([]byte, 8)
	binary.BigEndian.PutUint64(inc, 1)
	for i := 0; i < 10; i++ {
		if _, err := tr.Merge([]byte("hits"), inc); err != nil {
			t.Fatalf("Merge: %v", err)
		}
	}
	v, _, _ := tr.Get([]byte("hits"))
	if binary.BigEndian.Uint64(v) != 10 {
		t.Fatalf("counter = %d, want 10", binary.BigEndian.Uint64(v))
	}
}

func TestMergeDeletesOnNil(t *testing.T) {
	db := testDB(t)
	_ = db.Set([]byte("k"), []byte("v"))
	db.SetMergeOperator(func(key, old, merge []byte) []byte { return nil })
	if merged, err := db.Merge([]byte("k"), []byte("x")); err != nil || merged != nil {
		t.Fatalf("Merge = %q,%v", merged, err)
	}
	if _, ok, _ := db.Get([]byte("k")); ok {
		t.Fatal("merge returning nil should delete the key")
	}
}

func TestMergeWithoutOperator(t *testing.T) {
	db := testDB(t)
	if _, err := db.Merge([]byte("k"), []byte("v")); err != ErrNoMergeOperator {
		t.Fatalf("Merge err = %v, want ErrNoMergeOperator", err)
	}
}

func TestCompareAndSwap(t *testing.T) {
	db := testDB(t)

	// old == nil means "expect absent": succeeds on a missing key.
	if ok, err := db.CompareAndSwap([]byte("k"), nil, []byte("v1")); err != nil || !ok {
		t.Fatalf("CAS insert = %v,%v", ok, err)
	}
	// Expecting absent now fails since the key exists.
	if ok, _ := db.CompareAndSwap([]byte("k"), nil, []byte("v2")); ok {
		t.Fatal("CAS on present key with nil-old should fail")
	}
	// Wrong expected value fails and does not write.
	if ok, _ := db.CompareAndSwap([]byte("k"), []byte("wrong"), []byte("v3")); ok {
		t.Fatal("CAS with wrong old should fail")
	}
	if v, _, _ := db.Get([]byte("k")); string(v) != "v1" {
		t.Fatalf("value changed on failed CAS: %q", v)
	}
	// Correct expected value succeeds.
	if ok, err := db.CompareAndSwap([]byte("k"), []byte("v1"), []byte("v4")); err != nil || !ok {
		t.Fatalf("CAS update = %v,%v", ok, err)
	}
	if v, _, _ := db.Get([]byte("k")); string(v) != "v4" {
		t.Fatalf("value = %q, want v4", v)
	}
	// newValue == nil deletes when old matches.
	if ok, err := db.CompareAndSwap([]byte("k"), []byte("v4"), nil); err != nil || !ok {
		t.Fatalf("CAS delete = %v,%v", ok, err)
	}
	if _, ok, _ := db.Get([]byte("k")); ok {
		t.Fatal("key should be deleted after CAS to nil")
	}
	if _, err := db.CompareAndSwap(nil, nil, []byte("x")); err != ErrEmptyKey {
		t.Fatalf("CAS empty key err = %v", err)
	}
}

func TestFetchAndUpdate(t *testing.T) {
	db := testDB(t)
	// Update an absent key: old is nil.
	old, newv, err := db.FetchAndUpdate([]byte("n"), func(old []byte) []byte {
		if old != nil {
			t.Fatalf("expected nil old, got %q", old)
		}
		return []byte("1")
	})
	if err != nil || old != nil || string(newv) != "1" {
		t.Fatalf("FetchAndUpdate = %q,%q,%v", old, newv, err)
	}
	// Update present key: sees previous value.
	old, newv, err = db.FetchAndUpdate([]byte("n"), func(old []byte) []byte {
		return append(old, '!')
	})
	if err != nil || string(old) != "1" || string(newv) != "1!" {
		t.Fatalf("FetchAndUpdate = %q,%q,%v", old, newv, err)
	}
	// Returning nil deletes.
	_, newv, err = db.FetchAndUpdate([]byte("n"), func(old []byte) []byte { return nil })
	if err != nil || newv != nil {
		t.Fatalf("FetchAndUpdate delete = %q,%v", newv, err)
	}
	if _, ok, _ := db.Get([]byte("n")); ok {
		t.Fatal("key should be deleted")
	}
	if _, _, err := db.FetchAndUpdate([]byte("k"), nil); err != ErrNilFunc {
		t.Fatalf("nil func err = %v", err)
	}
}

func TestNeighborLookups(t *testing.T) {
	db := testDB(t)
	for _, k := range []string{"b", "d", "f", "h"} {
		_ = db.Set([]byte(k), []byte(k+"v"))
	}

	check := func(name string, gotK, gotV []byte, ok bool, wantK string) {
		if wantK == "" {
			if ok {
				t.Fatalf("%s: expected none, got %q", name, gotK)
			}
			return
		}
		if !ok || string(gotK) != wantK || string(gotV) != wantK+"v" {
			t.Fatalf("%s = %q,%q,%v want %q", name, gotK, gotV, ok, wantK)
		}
	}

	k, v, ok := db.GetGt([]byte("d"))
	check("GetGt(d)", k, v, ok, "f")
	k, v, ok = db.GetGte([]byte("d"))
	check("GetGte(d)", k, v, ok, "d")
	k, v, ok = db.GetGte([]byte("c"))
	check("GetGte(c)", k, v, ok, "d")
	k, v, ok = db.GetLt([]byte("d"))
	check("GetLt(d)", k, v, ok, "b")
	k, v, ok = db.GetLte([]byte("d"))
	check("GetLte(d)", k, v, ok, "d")
	k, v, ok = db.GetLte([]byte("e"))
	check("GetLte(e)", k, v, ok, "d")

	// Out of range.
	_, _, ok = db.GetGt([]byte("h"))
	if ok {
		t.Fatal("GetGt past max should be none")
	}
	_, _, ok = db.GetLt([]byte("b"))
	if ok {
		t.Fatal("GetLt before min should be none")
	}

	k, v, ok = db.First()
	check("First", k, v, ok, "b")
	k, v, ok = db.Last()
	check("Last", k, v, ok, "h")
}

func TestNeighborEmpty(t *testing.T) {
	db := testDB(t)
	if _, _, ok := db.First(); ok {
		t.Fatal("First on empty tree")
	}
	if _, _, ok := db.Last(); ok {
		t.Fatal("Last on empty tree")
	}
	if _, _, ok := db.GetGt([]byte("x")); ok {
		t.Fatal("GetGt on empty tree")
	}
	if _, _, ok := db.GetLte([]byte("x")); ok {
		t.Fatal("GetLte on empty tree")
	}
}

func TestReverseScan(t *testing.T) {
	db := testDB(t)
	keys := []string{"a", "b", "c", "d", "e"}
	for _, k := range keys {
		_ = db.Set([]byte(k), []byte(k))
	}

	// Full reverse scan.
	var got []string
	it := db.Scan(Range{Reverse: true})
	for it.Valid() {
		got = append(got, string(it.Key()))
		it.Next()
	}
	want := []string{"e", "d", "c", "b", "a"}
	if !equalStrings(got, want) {
		t.Fatalf("reverse scan = %v, want %v", got, want)
	}

	// Bounded reverse scan [b, e): descending d, c, b.
	got = nil
	it = db.Scan(Range{Lower: []byte("b"), Upper: []byte("e"), Reverse: true})
	for it.Valid() {
		got = append(got, string(it.Key()))
		it.Next()
	}
	if !equalStrings(got, []string{"d", "c", "b"}) {
		t.Fatalf("bounded reverse = %v", got)
	}
}

func TestReversePrefixScan(t *testing.T) {
	db := testDB(t)
	for _, k := range []string{"user:1", "user:2", "user:3", "z"} {
		_ = db.Set([]byte(k), []byte(k))
	}
	var got []string
	it := db.Scan(Range{Prefix: []byte("user:"), Reverse: true})
	for it.Valid() {
		got = append(got, string(it.Key()))
		it.Next()
	}
	if !equalStrings(got, []string{"user:3", "user:2", "user:1"}) {
		t.Fatalf("reverse prefix scan = %v", got)
	}
}

func TestGenerateID(t *testing.T) {
	path := filepath.Join(t.TempDir(), "id.sled")
	db, _ := Open(path)
	seen := map[uint64]bool{}
	var last uint64
	first := true
	for i := 0; i < 3000; i++ {
		id, err := db.GenerateID()
		if err != nil {
			t.Fatalf("GenerateID: %v", err)
		}
		if seen[id] {
			t.Fatalf("duplicate id %d", id)
		}
		seen[id] = true
		if !first && id <= last {
			t.Fatalf("id not increasing: %d after %d", id, last)
		}
		last, first = id, false
	}
	_ = db.Close()

	// After reopen ids continue strictly above the last handed out.
	db2, _ := Open(path)
	defer func() { _ = db2.Close() }()
	id, err := db2.GenerateID()
	if err != nil {
		t.Fatalf("GenerateID after reopen: %v", err)
	}
	if id <= last {
		t.Fatalf("id after reopen = %d, not above %d", id, last)
	}
}

func TestFlush(t *testing.T) {
	db := testDB(t, WithSyncWrites(false))
	_ = db.Set([]byte("k"), []byte("v"))
	if err := db.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	done := db.FlushAsync()
	if err := <-done; err != nil {
		t.Fatalf("FlushAsync: %v", err)
	}
}
