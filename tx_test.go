package sled

import (
	"errors"
	"testing"
)

func TestUpdateCommitVisible(t *testing.T) {
	db := testDB(t)
	err := db.Update(func(tx *Tx) error {
		if err := tx.Set([]byte("a"), []byte("1")); err != nil {
			return err
		}
		if err := tx.Set([]byte("b"), []byte("2")); err != nil {
			return err
		}
		// Reads within the tx see the tx's own writes.
		if v, ok, _ := tx.Get([]byte("a")); !ok || string(v) != "1" {
			t.Fatalf("in-tx read = (%q,%v)", v, ok)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if v, ok := mustGet(t, db, "a"); !ok || v != "1" {
		t.Fatalf("a = (%q,%v) after commit", v, ok)
	}
	if v, ok := mustGet(t, db, "b"); !ok || v != "2" {
		t.Fatalf("b = (%q,%v) after commit", v, ok)
	}
}

func TestUpdateRollbackDiscarded(t *testing.T) {
	db := testDB(t)
	if err := db.Set([]byte("keep"), []byte("original")); err != nil {
		t.Fatal(err)
	}

	sentinel := errors.New("boom")
	err := db.Update(func(tx *Tx) error {
		if err := tx.Set([]byte("keep"), []byte("modified")); err != nil {
			return err
		}
		if err := tx.Set([]byte("new"), []byte("value")); err != nil {
			return err
		}
		if err := tx.Delete([]byte("keep")); err != nil {
			return err
		}
		return sentinel // abort
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("Update err = %v, want sentinel", err)
	}

	// Nothing from the aborted tx must be visible.
	if v, ok := mustGet(t, db, "keep"); !ok || v != "original" {
		t.Fatalf("keep = (%q,%v), want (original,true) after rollback", v, ok)
	}
	if _, ok := mustGet(t, db, "new"); ok {
		t.Fatal("new key survived rollback")
	}
}

func TestUpdateRollbackNotDurable(t *testing.T) {
	db := testDB(t)
	path := db.Path()
	_ = db.Update(func(tx *Tx) error {
		_ = tx.Set([]byte("x"), []byte("y"))
		return errors.New("abort")
	})
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	// Reopen: the rolled-back write must not have been persisted.
	db2, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db2.Close() }()
	if _, ok, _ := db2.Get([]byte("x")); ok {
		t.Fatal("rolled-back write was persisted")
	}
}

func TestUpdatePanicRollsBack(t *testing.T) {
	db := testDB(t)
	if err := db.Set([]byte("k"), []byte("v")); err != nil {
		t.Fatal(err)
	}
	func() {
		defer func() {
			if r := recover(); r == nil {
				t.Fatal("expected panic to propagate")
			}
		}()
		_ = db.Update(func(tx *Tx) error {
			_ = tx.Set([]byte("k"), []byte("changed"))
			panic("kaboom")
		})
	}()
	// After the panic the DB must be usable and the write rolled back.
	if v, ok := mustGet(t, db, "k"); !ok || v != "v" {
		t.Fatalf("k = (%q,%v), want (v,true) after panic rollback", v, ok)
	}
	if err := db.Set([]byte("still"), []byte("works")); err != nil {
		t.Fatalf("DB unusable after panic: %v", err)
	}
}

func TestViewSnapshotIsolation(t *testing.T) {
	db := testDB(t)
	if err := db.Set([]byte("a"), []byte("1")); err != nil {
		t.Fatal(err)
	}

	err := db.View(func(tx *Tx) error {
		if v, ok, _ := tx.Get([]byte("a")); !ok || string(v) != "1" {
			t.Fatalf("view read = (%q,%v)", v, ok)
		}
		// Mutate the DB from outside while the view is open.
		if err := db.Set([]byte("a"), []byte("2")); err != nil {
			return err
		}
		if err := db.Set([]byte("b"), []byte("new")); err != nil {
			return err
		}
		// The snapshot must be unaffected by the concurrent writes.
		if v, ok, _ := tx.Get([]byte("a")); !ok || string(v) != "1" {
			t.Fatalf("snapshot changed: a = (%q,%v), want (1,true)", v, ok)
		}
		if _, ok, _ := tx.Get([]byte("b")); ok {
			t.Fatal("snapshot saw a key written after it began")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("View: %v", err)
	}
	// A fresh read sees the updates.
	if v, ok := mustGet(t, db, "a"); !ok || v != "2" {
		t.Fatalf("post-view a = (%q,%v), want (2,true)", v, ok)
	}
}

func TestViewCannotWrite(t *testing.T) {
	db := testDB(t)
	err := db.View(func(tx *Tx) error {
		if tx.Writable() {
			t.Fatal("view tx reports writable")
		}
		if err := tx.Set([]byte("a"), []byte("b")); err != ErrTxNotWritable {
			t.Fatalf("Set on view = %v, want ErrTxNotWritable", err)
		}
		if err := tx.Delete([]byte("a")); err != ErrTxNotWritable {
			t.Fatalf("Delete on view = %v, want ErrTxNotWritable", err)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestTxScanSeesBufferedWrites(t *testing.T) {
	db := testDB(t)
	if err := db.Set([]byte("a"), []byte("1")); err != nil {
		t.Fatal(err)
	}
	if err := db.Set([]byte("c"), []byte("3")); err != nil {
		t.Fatal(err)
	}
	err := db.Update(func(tx *Tx) error {
		if err := tx.Set([]byte("b"), []byte("2")); err != nil {
			return err
		}
		if err := tx.Delete([]byte("a")); err != nil {
			return err
		}
		var keys []string
		it := tx.Scan(Range{})
		for it.Valid() {
			keys = append(keys, string(it.Key()))
			it.Next()
		}
		want := []string{"b", "c"}
		if len(keys) != len(want) {
			t.Fatalf("tx scan keys = %v, want %v", keys, want)
		}
		for i := range want {
			if keys[i] != want[i] {
				t.Fatalf("tx scan keys = %v, want %v", keys, want)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestEmptyUpdateWritesNothing(t *testing.T) {
	db := testDB(t)
	path := db.Path()
	before := fileSizeOf(t, path)
	if err := db.Update(func(tx *Tx) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if after := fileSizeOf(t, path); after != before {
		t.Fatalf("empty Update wrote %d bytes", after-before)
	}
}
