package sled

import (
	"os"
	"path/filepath"
	"testing"
)

func TestTreeScanAndName(t *testing.T) {
	db := testDB(t)
	tr, _ := db.OpenTree("scan")
	if tr.Name() != "scan" {
		t.Fatalf("Name = %q", tr.Name())
	}
	for _, k := range []string{"a", "b", "c"} {
		_ = tr.Set([]byte(k), []byte(k))
	}
	var got []string
	it := tr.Scan(Range{})
	for it.Valid() {
		got = append(got, string(it.Key()))
		it.Next()
	}
	if !equalStrings(got, []string{"a", "b", "c"}) {
		t.Fatalf("Tree.Scan = %v", got)
	}
}

func TestDefaultDelegators(t *testing.T) {
	db := testDB(t)
	_ = db.Set([]byte("k"), []byte("v"))
	if ok, _ := db.ContainsKey([]byte("k")); !ok {
		t.Fatal("DB.ContainsKey")
	}
	if err := db.Clear(); err != nil {
		t.Fatalf("DB.Clear: %v", err)
	}
	if db.Len() != 0 {
		t.Fatal("DB.Clear did not empty default tree")
	}
}

func TestTxHasAndScanTree(t *testing.T) {
	db := testDB(t)
	tr, _ := db.OpenTree("t")
	_ = tr.Set([]byte("a"), []byte("1"))
	err := db.View(func(tx *Tx) error {
		ok, err := tx.Has([]byte("nope"))
		if err != nil || ok {
			t.Fatalf("tx.Has(nope) = %v,%v", ok, err)
		}
		var keys []string
		it := tx.ScanTree(tr, Range{})
		for it.Valid() {
			keys = append(keys, string(it.Key()))
			it.Next()
		}
		if !equalStrings(keys, []string{"a"}) {
			t.Fatalf("ScanTree = %v", keys)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("View: %v", err)
	}
}

func TestBatchLen(t *testing.T) {
	db := testDB(t)
	b := db.NewBatch()
	b.Set([]byte("a"), []byte("1")).Delete([]byte("b"))
	if b.Len() != 2 {
		t.Fatalf("Batch.Len = %d, want 2", b.Len())
	}
}

// TestCrashRecoveryNamedTrees verifies that a torn tail after writes into
// several named trees is discarded while all committed cross-tree data survives.
func TestCrashRecoveryNamedTrees(t *testing.T) {
	path := filepath.Join(t.TempDir(), "crash.sled")
	db, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	a, _ := db.OpenTree("a")
	b, _ := db.OpenTree("b")
	_ = a.Set([]byte("k1"), []byte("va"))
	_ = b.Set([]byte("k1"), []byte("vb"))
	_ = db.Update(func(tx *Tx) error {
		if err := tx.SetTree(a, []byte("k2"), []byte("va2")); err != nil {
			return err
		}
		return tx.SetTree(b, []byte("k2"), []byte("vb2"))
	})
	if err := db.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Simulate a crash mid-append.
	appendTornRecord(t, path)

	db2, err := Open(path)
	if err != nil {
		t.Fatalf("reopen after torn tail: %v", err)
	}
	defer func() { _ = db2.Close() }()

	a2, _ := db2.OpenTree("a")
	b2, _ := db2.OpenTree("b")
	if v, ok, _ := a2.Get([]byte("k1")); !ok || string(v) != "va" {
		t.Fatalf("a.k1 = %q,%v", v, ok)
	}
	if v, ok, _ := b2.Get([]byte("k2")); !ok || string(v) != "vb2" {
		t.Fatalf("b.k2 = %q,%v", v, ok)
	}

	// The torn tail must have been physically truncated, leaving a clean file
	// that accepts further writes.
	if err := a2.Set([]byte("k3"), []byte("va3")); err != nil {
		t.Fatalf("write after recovery: %v", err)
	}
}

func TestWithFileMode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mode.sled")
	db, err := Open(path, WithFileMode(0o600))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = db.Close() }()
	_ = db.Set([]byte("k"), []byte("v"))
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %v, want 0600", fi.Mode().Perm())
	}
}
