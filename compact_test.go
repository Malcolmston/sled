package sled

import (
	"fmt"
	"testing"
)

func TestCompactReclaimsSpaceAndPreservesData(t *testing.T) {
	db := testDB(t)

	// Write lots of churn: repeatedly overwrite and delete keys so the log
	// accumulates superseded records.
	const keys = 200
	for round := 0; round < 10; round++ {
		for i := 0; i < keys; i++ {
			k := fmt.Sprintf("key-%03d", i)
			v := fmt.Sprintf("round-%d-val-%d", round, i)
			if err := db.Set([]byte(k), []byte(v)); err != nil {
				t.Fatal(err)
			}
		}
	}
	// Delete half of them.
	for i := 0; i < keys; i += 2 {
		if err := db.Delete([]byte(fmt.Sprintf("key-%03d", i))); err != nil {
			t.Fatal(err)
		}
	}

	sizeBefore := fileSizeOf(t, db.Path())

	// Capture the exact live contents before compaction.
	before := map[string]string{}
	it := db.Scan(Range{})
	for it.Valid() {
		before[string(it.Key())] = string(it.Value())
		it.Next()
	}

	if err := db.Compact(); err != nil {
		t.Fatalf("Compact: %v", err)
	}

	sizeAfter := fileSizeOf(t, db.Path())
	if sizeAfter >= sizeBefore {
		t.Fatalf("Compact did not reclaim space: before=%d after=%d", sizeBefore, sizeAfter)
	}

	// In-memory data must be identical after compaction.
	assertEqualDump(t, db, before)

	// The DB must still be writable after compaction.
	if err := db.Set([]byte("post-compact"), []byte("ok")); err != nil {
		t.Fatalf("write after compact: %v", err)
	}

	// And the compacted log must reopen to exactly the same data.
	path := db.Path()
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db2, err := Open(path)
	if err != nil {
		t.Fatalf("reopen after compact: %v", err)
	}
	defer func() { _ = db2.Close() }()

	before["post-compact"] = "ok"
	assertEqualDump(t, db2, before)
}

func TestCompactEmptyDB(t *testing.T) {
	db := testDB(t)
	if err := db.Compact(); err != nil {
		t.Fatalf("Compact empty: %v", err)
	}
	if db.Len() != 0 {
		t.Fatalf("Len after compacting empty = %d", db.Len())
	}
	// Still usable.
	if err := db.Set([]byte("a"), []byte("1")); err != nil {
		t.Fatal(err)
	}
	if v, ok := mustGet(t, db, "a"); !ok || v != "1" {
		t.Fatalf("post-compact write = (%q,%v)", v, ok)
	}
}

func TestCompactDurableAcrossReopen(t *testing.T) {
	path := t.TempDir() + "/compact-reopen.sled"
	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 100; i++ {
		if err := db.Set([]byte(fmt.Sprintf("k%03d", i)), []byte("v")); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Compact(); err != nil {
		t.Fatal(err)
	}
	// Write more after compaction to make sure appends work on the new file.
	if err := db.Set([]byte("extra"), []byte("appended")); err != nil {
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
	if db2.Len() != 101 {
		t.Fatalf("Len after reopen = %d, want 101", db2.Len())
	}
	if v, ok, _ := db2.Get([]byte("extra")); !ok || string(v) != "appended" {
		t.Fatalf("post-compact append lost: (%q,%v)", v, ok)
	}
}
