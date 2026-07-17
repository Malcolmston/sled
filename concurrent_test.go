package sled

import (
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestConcurrentReadersDuringWrites runs many reader goroutines (Get, Has and
// Scan) concurrently with a single writer that continuously mutates the store.
// It is meant to be run under -race to prove the immutable-snapshot model is
// free of data races, and it asserts readers only ever observe consistent,
// committed state.
func TestConcurrentReadersDuringWrites(t *testing.T) {
	db := testDB(t, WithSyncWrites(false)) // fsync off to keep the writer hot

	const nKeys = 256
	// Seed a known baseline: every key maps to a value whose numeric suffix is
	// monotonically increasing, so a reader can validate any value it sees.
	for i := 0; i < nKeys; i++ {
		if err := db.Set(keyOf(i), valOf(i, 0)); err != nil {
			t.Fatal(err)
		}
	}

	var stop atomic.Bool
	var wg sync.WaitGroup

	// Single writer: repeatedly bumps every key's version.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for round := 1; !stop.Load(); round++ {
			for i := 0; i < nKeys; i++ {
				if err := db.Set(keyOf(i), valOf(i, round)); err != nil {
					t.Errorf("writer Set: %v", err)
					return
				}
			}
			// Occasionally run a batch and a transaction too.
			_ = db.Batch(func(b *Batch) error {
				b.Set([]byte("batchkey"), []byte(fmt.Sprintf("%d", round)))
				return nil
			})
			_ = db.Update(func(tx *Tx) error {
				return tx.Set([]byte("txkey"), []byte(fmt.Sprintf("%d", round)))
			})
		}
	}()

	// Many readers.
	const nReaders = 8
	var reads int64
	for r := 0; r < nReaders; r++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for !stop.Load() {
				// Point reads: every key must always be present with a
				// well-formed value (writer only ever overwrites, never
				// deletes these keys).
				for i := 0; i < nKeys; i++ {
					v, ok, err := db.Get(keyOf(i))
					if err != nil {
						t.Errorf("Get: %v", err)
						return
					}
					if !ok {
						t.Errorf("key %d unexpectedly absent", i)
						return
					}
					if !validValue(i, v) {
						t.Errorf("key %d has malformed value %q", i, v)
						return
					}
					atomic.AddInt64(&reads, 1)
				}
				// Range scan must always yield a consistent, ordered snapshot.
				var last []byte
				it := db.Scan(Range{Prefix: []byte("k:")})
				n := 0
				for it.Valid() {
					if last != nil && string(it.Key()) <= string(last) {
						t.Errorf("scan out of order")
						return
					}
					last = append(last[:0], it.Key()...)
					n++
					it.Next()
				}
				if n != nKeys {
					t.Errorf("scan saw %d keys, want %d (torn snapshot)", n, nKeys)
					return
				}
			}
		}()
	}

	time.Sleep(300 * time.Millisecond)
	stop.Store(true)
	wg.Wait()

	if atomic.LoadInt64(&reads) == 0 {
		t.Fatal("readers performed no reads")
	}
}

func keyOf(i int) []byte { return []byte(fmt.Sprintf("k:%04d", i)) }

func valOf(i, round int) []byte { return []byte(fmt.Sprintf("val-%04d-r%d", i, round)) }

// validValue checks that v looks like a value produced by valOf for key i.
func validValue(i int, v []byte) bool {
	prefix := fmt.Sprintf("val-%04d-r", i)
	return len(v) > len(prefix) && string(v[:len(prefix)]) == prefix
}

// TestConcurrentViewSnapshotStable proves a View snapshot is stable even under a
// storm of concurrent writes.
func TestConcurrentViewSnapshotStable(t *testing.T) {
	db := testDB(t, WithSyncWrites(false))
	if err := db.Set([]byte("target"), []byte("v0")); err != nil {
		t.Fatal(err)
	}

	var stop atomic.Bool
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 1; !stop.Load(); i++ {
			_ = db.Set([]byte("target"), []byte(fmt.Sprintf("v%d", i)))
		}
	}()

	for i := 0; i < 200; i++ {
		err := db.View(func(tx *Tx) error {
			v0, _, _ := tx.Get([]byte("target"))
			snapshot := string(v0)
			for j := 0; j < 100; j++ {
				v, ok, _ := tx.Get([]byte("target"))
				if !ok || string(v) != snapshot {
					t.Errorf("snapshot changed within View: %q -> %q", snapshot, v)
					return nil
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}

	stop.Store(true)
	wg.Wait()
}
