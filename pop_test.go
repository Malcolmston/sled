package sled

import (
	"fmt"
	"path/filepath"
	"testing"
)

// benchDB opens a throwaway database for benchmarks, cleaned up automatically.
func benchDB(b *testing.B, opts ...Option) *DB {
	b.Helper()
	db, err := Open(filepath.Join(b.TempDir(), "bench.sled"), opts...)
	if err != nil {
		b.Fatalf("Open: %v", err)
	}
	b.Cleanup(func() { _ = db.Close() })
	return db
}

func TestPopMinMax(t *testing.T) {
	db := testDB(t)
	for _, k := range []string{"c", "a", "e", "b", "d"} {
		if err := db.Set([]byte(k), []byte("v"+k)); err != nil {
			t.Fatalf("Set(%q): %v", k, err)
		}
	}

	// PopMin walks keys in ascending order.
	wantMin := []string{"a", "b", "c", "d", "e"}
	for _, want := range wantMin {
		k, v, ok, err := db.PopMin()
		if err != nil {
			t.Fatalf("PopMin: %v", err)
		}
		if !ok || string(k) != want || string(v) != "v"+want {
			t.Fatalf("PopMin = %q,%q,%v; want %q,%q,true", k, v, ok, want, "v"+want)
		}
	}
	if _, _, ok, _ := db.PopMin(); ok {
		t.Fatalf("PopMin on empty tree returned ok=true")
	}

	// Refill and PopMax walks keys in descending order.
	for _, k := range wantMin {
		_ = db.Set([]byte(k), []byte("v"+k))
	}
	wantMax := []string{"e", "d", "c", "b", "a"}
	for _, want := range wantMax {
		k, v, ok, err := db.PopMax()
		if err != nil {
			t.Fatalf("PopMax: %v", err)
		}
		if !ok || string(k) != want || string(v) != "v"+want {
			t.Fatalf("PopMax = %q,%q,%v; want %q", k, v, ok, want)
		}
	}
	if _, _, ok, _ := db.PopMax(); ok {
		t.Fatalf("PopMax on empty tree returned ok=true")
	}
}

func TestPopPersistsAndOwnsBytes(t *testing.T) {
	db := testDB(t)
	_ = db.Set([]byte("k"), []byte("value"))
	k, v, ok, err := db.PopMin()
	if err != nil || !ok {
		t.Fatalf("PopMin: %v ok=%v", err, ok)
	}
	// Returned slices are the caller's; mutating them must not corrupt the DB.
	k[0] = 'X'
	v[0] = 'Y'
	if has, _ := db.Has([]byte("k")); has {
		t.Fatalf("key should have been popped")
	}
	if has, _ := db.Has([]byte("Xalue")); has {
		t.Fatalf("mutated key leaked into the DB")
	}
}

func BenchmarkPopMin(b *testing.B) {
	db := benchDB(b, WithSyncWrites(false))
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		_ = db.Set([]byte(fmt.Sprintf("k%08d", i)), []byte("v"))
		b.StartTimer()
		if _, _, ok, err := db.PopMin(); err != nil || !ok {
			b.Fatalf("PopMin: %v ok=%v", err, ok)
		}
	}
}
