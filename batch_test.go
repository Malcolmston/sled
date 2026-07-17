package sled

import (
	"errors"
	"fmt"
	"testing"
)

func TestBatchAtomicCommit(t *testing.T) {
	db := testDB(t)
	err := db.Batch(func(b *Batch) error {
		b.Set([]byte("a"), []byte("1"))
		b.Set([]byte("b"), []byte("2"))
		b.Delete([]byte("a"))
		b.Set([]byte("c"), []byte("3"))
		return nil
	})
	if err != nil {
		t.Fatalf("Batch: %v", err)
	}
	if _, ok := mustGet(t, db, "a"); ok {
		t.Fatal("a should have been deleted within the batch")
	}
	if v, ok := mustGet(t, db, "b"); !ok || v != "2" {
		t.Fatalf("b = (%q,%v)", v, ok)
	}
	if v, ok := mustGet(t, db, "c"); !ok || v != "3" {
		t.Fatalf("c = (%q,%v)", v, ok)
	}
}

func TestBatchFnErrorWritesNothing(t *testing.T) {
	db := testDB(t)
	if err := db.Set([]byte("pre"), []byte("existing")); err != nil {
		t.Fatal(err)
	}
	sentinel := errors.New("nope")
	err := db.Batch(func(b *Batch) error {
		b.Set([]byte("x"), []byte("1"))
		b.Set([]byte("y"), []byte("2"))
		return sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("Batch err = %v, want sentinel", err)
	}
	if _, ok := mustGet(t, db, "x"); ok {
		t.Fatal("batch write applied despite fn error")
	}
	if v, ok := mustGet(t, db, "pre"); !ok || v != "existing" {
		t.Fatalf("pre-existing key disturbed: (%q,%v)", v, ok)
	}
}

// TestBatchAtomicOnDisk verifies a batch lands as a single all-or-nothing
// record: after reopening, either all of it is present, and it survives.
func TestBatchAtomicOnDisk(t *testing.T) {
	db := testDB(t)
	path := db.Path()
	err := db.Batch(func(b *Batch) error {
		for i := 0; i < 50; i++ {
			b.Set([]byte(fmt.Sprintf("k%02d", i)), []byte(fmt.Sprintf("v%02d", i)))
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db2, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db2.Close() }()
	if db2.Len() != 50 {
		t.Fatalf("Len = %d, want 50", db2.Len())
	}
	for i := 0; i < 50; i++ {
		k := fmt.Sprintf("k%02d", i)
		if v, ok, _ := db2.Get([]byte(k)); !ok || string(v) != fmt.Sprintf("v%02d", i) {
			t.Fatalf("key %s missing after reopen: (%q,%v)", k, v, ok)
		}
	}
}

func TestBatchEmptyKeyError(t *testing.T) {
	db := testDB(t)
	b := db.NewBatch()
	b.Set([]byte("ok"), []byte("v"))
	b.Set(nil, []byte("bad"))
	if err := b.Commit(); err != ErrEmptyKey {
		t.Fatalf("Commit = %v, want ErrEmptyKey", err)
	}
	// The valid write must not have leaked out.
	if _, ok := mustGet(t, db, "ok"); ok {
		t.Fatal("partial batch applied despite invalid op")
	}
}

func TestBatchReuseRejected(t *testing.T) {
	db := testDB(t)
	b := db.NewBatch()
	b.Set([]byte("a"), []byte("1"))
	if err := b.Commit(); err != nil {
		t.Fatal(err)
	}
	if err := b.Commit(); err != ErrTxClosed {
		t.Fatalf("second Commit = %v, want ErrTxClosed", err)
	}
}

func TestEmptyBatchNoWrite(t *testing.T) {
	db := testDB(t)
	before := fileSizeOf(t, db.Path())
	if err := db.Batch(func(b *Batch) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if after := fileSizeOf(t, db.Path()); after != before {
		t.Fatalf("empty batch wrote %d bytes", after-before)
	}
}
