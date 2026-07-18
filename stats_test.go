package sled

import (
	"path/filepath"
	"testing"
)

func TestSizeOnDiskGrowsAndCompacts(t *testing.T) {
	db := testDB(t)
	start, err := db.SizeOnDisk()
	if err != nil {
		t.Fatalf("SizeOnDisk: %v", err)
	}
	if start != 0 {
		t.Fatalf("fresh DB size = %d; want 0", start)
	}
	for i := 0; i < 100; i++ {
		key := []byte{byte(i)}
		_ = db.Set(key, make([]byte, 256))
	}
	// Overwrite the same keys to leave dead records behind.
	for i := 0; i < 100; i++ {
		_ = db.Set([]byte{byte(i)}, []byte("x"))
	}
	grown, err := db.SizeOnDisk()
	if err != nil {
		t.Fatalf("SizeOnDisk: %v", err)
	}
	if grown <= start {
		t.Fatalf("size did not grow: %d <= %d", grown, start)
	}
	if err := db.Compact(); err != nil {
		t.Fatalf("Compact: %v", err)
	}
	compacted, err := db.SizeOnDisk()
	if err != nil {
		t.Fatalf("SizeOnDisk: %v", err)
	}
	if compacted >= grown {
		t.Fatalf("compaction did not shrink log: %d >= %d", compacted, grown)
	}
}

func TestWasRecovered(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rec.sled")

	db, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if db.WasRecovered() {
		t.Errorf("fresh DB reports WasRecovered=true")
	}
	if err := db.Set([]byte("k"), []byte("v")); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	db2, err := Open(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer func() { _ = db2.Close() }()
	if !db2.WasRecovered() {
		t.Errorf("reopened DB reports WasRecovered=false")
	}
}

func TestStatsClosed(t *testing.T) {
	db := testDB(t)
	_ = db.Close()
	if _, err := db.SizeOnDisk(); err != ErrClosed {
		t.Errorf("SizeOnDisk after close = %v; want ErrClosed", err)
	}
}
