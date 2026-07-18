package sled

import (
	"errors"
	"testing"
)

func TestTreeApplyBatch(t *testing.T) {
	db := testDB(t)
	tr, err := db.OpenTree("t")
	if err != nil {
		t.Fatalf("OpenTree: %v", err)
	}
	_ = tr.Set([]byte("old"), []byte("x"))

	b := db.NewBatch()
	b.Set([]byte("k1"), []byte("v1"))
	b.Set([]byte("k2"), []byte("v2"))
	b.Delete([]byte("old"))
	if err := tr.ApplyBatch(b); err != nil {
		t.Fatalf("ApplyBatch: %v", err)
	}

	if v, ok, _ := tr.Get([]byte("k1")); !ok || string(v) != "v1" {
		t.Errorf("k1 = %q,%v", v, ok)
	}
	if v, ok, _ := tr.Get([]byte("k2")); !ok || string(v) != "v2" {
		t.Errorf("k2 = %q,%v", v, ok)
	}
	if has, _ := tr.Has([]byte("old")); has {
		t.Errorf("old not deleted")
	}
	// The batch must have targeted the named tree, not the default tree.
	if has, _ := db.Has([]byte("k1")); has {
		t.Errorf("batch leaked into the default tree")
	}

	// Reusing a committed batch is rejected.
	if err := tr.ApplyBatch(b); err != ErrTxClosed {
		t.Errorf("reuse = %v; want ErrTxClosed", err)
	}
}

func TestTreeApplyBatchPropagatesStagingError(t *testing.T) {
	db := testDB(t)
	b := db.NewBatch()
	b.Set(nil, []byte("v")) // empty key -> staging error
	if err := db.def.ApplyBatch(b); err != ErrEmptyKey {
		t.Errorf("ApplyBatch = %v; want ErrEmptyKey", err)
	}
}

func TestTreeBatchClosure(t *testing.T) {
	db := testDB(t)
	tr, _ := db.OpenTree("t")

	if err := tr.Batch(func(b *Batch) error {
		b.Set([]byte("a"), []byte("1"))
		b.Set([]byte("b"), []byte("2"))
		return nil
	}); err != nil {
		t.Fatalf("Batch: %v", err)
	}
	if tr.Len() != 2 {
		t.Errorf("len = %d; want 2", tr.Len())
	}

	// A returned error discards the whole batch.
	sentinel := errors.New("abort")
	if err := tr.Batch(func(b *Batch) error {
		b.Set([]byte("c"), []byte("3"))
		return sentinel
	}); !errors.Is(err, sentinel) {
		t.Errorf("Batch error = %v; want sentinel", err)
	}
	if has, _ := tr.Has([]byte("c")); has {
		t.Errorf("aborted batch wrote c")
	}
}

func TestTreeApplyBatchPersists(t *testing.T) {
	db := testDB(t)
	tr, _ := db.OpenTree("persist")
	_ = tr.Batch(func(b *Batch) error {
		b.Set([]byte("x"), []byte("y"))
		return nil
	})
	path := db.Path()
	_ = db.Close()

	db2, err := Open(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer func() { _ = db2.Close() }()
	tr2, _ := db2.OpenTree("persist")
	if v, ok, _ := tr2.Get([]byte("x")); !ok || string(v) != "y" {
		t.Errorf("after reopen x = %q,%v; want y", v, ok)
	}
}
