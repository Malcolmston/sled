package sled

import (
	"fmt"
	"path/filepath"
	"testing"
)

// testDB opens a fresh database in a temp dir and registers cleanup.
func testDB(t *testing.T, opts ...Option) *DB {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.sled")
	db, err := Open(path, opts...)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func mustGet(t *testing.T, db *DB, key string) (string, bool) {
	t.Helper()
	v, ok, err := db.Get([]byte(key))
	if err != nil {
		t.Fatalf("Get(%q): %v", key, err)
	}
	return string(v), ok
}

func TestSetGetHasDelete(t *testing.T) {
	db := testDB(t)

	if _, ok := mustGet(t, db, "missing"); ok {
		t.Fatal("expected missing key to be absent")
	}
	if has, _ := db.Has([]byte("missing")); has {
		t.Fatal("Has reported a missing key present")
	}

	if err := db.Set([]byte("a"), []byte("1")); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if v, ok := mustGet(t, db, "a"); !ok || v != "1" {
		t.Fatalf("got (%q,%v), want (1,true)", v, ok)
	}
	if has, _ := db.Has([]byte("a")); !has {
		t.Fatal("Has reported present key absent")
	}

	// Overwrite.
	if err := db.Set([]byte("a"), []byte("2")); err != nil {
		t.Fatalf("Set overwrite: %v", err)
	}
	if v, _ := mustGet(t, db, "a"); v != "2" {
		t.Fatalf("overwrite got %q, want 2", v)
	}

	// Empty value is present but empty.
	if err := db.Set([]byte("empty"), []byte{}); err != nil {
		t.Fatalf("Set empty: %v", err)
	}
	if v, ok := mustGet(t, db, "empty"); !ok || v != "" {
		t.Fatalf("empty value got (%q,%v), want (\"\",true)", v, ok)
	}

	// Delete.
	if err := db.Delete([]byte("a")); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, ok := mustGet(t, db, "a"); ok {
		t.Fatal("key present after delete")
	}
	// Deleting a missing key is fine.
	if err := db.Delete([]byte("nope")); err != nil {
		t.Fatalf("Delete missing: %v", err)
	}
}

func TestEmptyKeyRejected(t *testing.T) {
	db := testDB(t)
	if err := db.Set(nil, []byte("x")); err != ErrEmptyKey {
		t.Fatalf("Set(nil) err = %v, want ErrEmptyKey", err)
	}
	if err := db.Set([]byte{}, []byte("x")); err != ErrEmptyKey {
		t.Fatalf("Set(empty) err = %v, want ErrEmptyKey", err)
	}
	if err := db.Delete([]byte{}); err != ErrEmptyKey {
		t.Fatalf("Delete(empty) err = %v, want ErrEmptyKey", err)
	}
}

func TestInputBuffersCopied(t *testing.T) {
	db := testDB(t)
	key := []byte("k")
	val := []byte("original")
	if err := db.Set(key, val); err != nil {
		t.Fatal(err)
	}
	// Mutating caller buffers must not affect stored data.
	key[0] = 'X'
	val[0] = 'Y'
	if v, ok := mustGet(t, db, "k"); !ok || v != "original" {
		t.Fatalf("stored value changed by caller mutation: got (%q,%v)", v, ok)
	}
}

func TestClosedOperations(t *testing.T) {
	path := filepath.Join(t.TempDir(), "closed.sled")
	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	// Idempotent close.
	if err := db.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
	if err := db.Set([]byte("a"), []byte("b")); err != ErrClosed {
		t.Fatalf("Set after close = %v, want ErrClosed", err)
	}
	if _, _, err := db.Get([]byte("a")); err != ErrClosed {
		t.Fatalf("Get after close = %v, want ErrClosed", err)
	}
}

func TestPersistenceAcrossReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "persist.sled")

	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 100; i++ {
		k := fmt.Sprintf("key-%03d", i)
		v := fmt.Sprintf("val-%d", i)
		if err := db.Set([]byte(k), []byte(v)); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Delete([]byte("key-050")); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	// Reopen and verify everything survived exactly.
	db2, err := Open(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer func() { _ = db2.Close() }()

	if got := db2.Len(); got != 99 {
		t.Fatalf("Len after reopen = %d, want 99", got)
	}
	for i := 0; i < 100; i++ {
		k := fmt.Sprintf("key-%03d", i)
		v, ok, _ := db2.Get([]byte(k))
		if i == 50 {
			if ok {
				t.Fatalf("deleted key %s survived", k)
			}
			continue
		}
		want := fmt.Sprintf("val-%d", i)
		if !ok || string(v) != want {
			t.Fatalf("key %s = (%q,%v), want (%q,true)", k, v, ok, want)
		}
	}
}

func TestReopenEmpty(t *testing.T) {
	path := filepath.Join(t.TempDir(), "empty.sled")
	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if db.Len() != 0 {
		t.Fatalf("fresh Len = %d, want 0", db.Len())
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db2, err := Open(path)
	if err != nil {
		t.Fatalf("reopen empty: %v", err)
	}
	defer func() { _ = db2.Close() }()
	if db2.Len() != 0 {
		t.Fatalf("reopened empty Len = %d, want 0", db2.Len())
	}
}

func TestPathAccessor(t *testing.T) {
	path := filepath.Join(t.TempDir(), "p.sled")
	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	if db.Path() != path {
		t.Fatalf("Path = %q, want %q", db.Path(), path)
	}
}

// assertEqualDump is a helper asserting the full ordered contents of db.
func assertEqualDump(t *testing.T, db *DB, want map[string]string) {
	t.Helper()
	got := map[string]string{}
	it := db.Scan(Range{})
	for it.Valid() {
		got[string(it.Key())] = string(it.Value())
		it.Next()
	}
	if len(got) != len(want) {
		t.Fatalf("dump size = %d, want %d", len(got), len(want))
	}
	for k, v := range want {
		if got[k] != v {
			t.Fatalf("key %q = %q, want %q", k, got[k], v)
		}
	}
}
