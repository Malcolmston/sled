package sled

import (
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"os"
	"path/filepath"
	"testing"
)

// TestCrashRecoveryTruncatedTail simulates a crash mid-write by truncating the
// log inside its final record, then reopening. All records that were fully
// written before the crash must survive; the partial tail must be dropped.
func TestCrashRecoveryTruncatedTail(t *testing.T) {
	path := filepath.Join(t.TempDir(), "crash.sled")

	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 20; i++ {
		if err := db.Set([]byte(fmt.Sprintf("k%02d", i)), []byte(fmt.Sprintf("v%02d", i))); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	// Record the healthy file size, then append a partial (torn) record: a
	// valid-looking header claiming a payload longer than the bytes that
	// follow — exactly what a crash mid-append leaves behind.
	fullSize := fileSizeOf(t, path)
	appendTornRecord(t, path)

	// Reopen: recovery must stop at the torn tail and truncate it away.
	db2, err := Open(path)
	if err != nil {
		t.Fatalf("reopen after crash: %v", err)
	}
	defer func() { _ = db2.Close() }()

	if db2.Len() != 20 {
		t.Fatalf("Len after recovery = %d, want 20", db2.Len())
	}
	for i := 0; i < 20; i++ {
		k := fmt.Sprintf("k%02d", i)
		v, ok, _ := db2.Get([]byte(k))
		want := fmt.Sprintf("v%02d", i)
		if !ok || string(v) != want {
			t.Fatalf("committed key %s lost: got (%q,%v)", k, v, ok)
		}
	}

	// The partial tail must have been physically truncated back to the last
	// good offset.
	if got := fileSizeOf(t, path); got != fullSize {
		t.Fatalf("log size after recovery = %d, want %d (tail not truncated)", got, fullSize)
	}

	// And the recovered DB must remain writable and durable afterwards.
	if err := db2.Set([]byte("after"), []byte("crash")); err != nil {
		t.Fatalf("write after recovery: %v", err)
	}
	if err := db2.Close(); err != nil {
		t.Fatal(err)
	}
	db3, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db3.Close() }()
	if v, ok, _ := db3.Get([]byte("after")); !ok || string(v) != "crash" {
		t.Fatalf("post-recovery write not durable: (%q,%v)", v, ok)
	}
	if db3.Len() != 21 {
		t.Fatalf("Len = %d, want 21", db3.Len())
	}
}

// TestCrashRecoveryMidRecordByteTruncation truncates the file at many byte
// offsets inside the last record and asserts that recovery always keeps the
// records committed before it and never errors or corrupts.
func TestCrashRecoveryMidRecordByteTruncation(t *testing.T) {
	for cut := 1; cut <= 40; cut++ {
		t.Run(fmt.Sprintf("cut=%d", cut), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "cut.sled")
			db, err := Open(path)
			if err != nil {
				t.Fatal(err)
			}
			// Two committed records, then one more that we will chop into.
			if err := db.Set([]byte("alpha"), []byte("one")); err != nil {
				t.Fatal(err)
			}
			if err := db.Set([]byte("beta"), []byte("two")); err != nil {
				t.Fatal(err)
			}
			safeSize := fileSizeOf(t, path)
			if err := db.Set([]byte("gamma"), []byte("three-partial")); err != nil {
				t.Fatal(err)
			}
			fullSize := fileSizeOf(t, path)
			if err := db.Close(); err != nil {
				t.Fatal(err)
			}

			// Chop `cut` bytes off the end (inside the third record).
			newSize := fullSize - int64(cut)
			if newSize < safeSize {
				t.Skip("cut larger than final record")
			}
			if err := os.Truncate(path, newSize); err != nil {
				t.Fatal(err)
			}

			db2, err := Open(path)
			if err != nil {
				t.Fatalf("reopen: %v", err)
			}
			defer func() { _ = db2.Close() }()

			// The two fully-committed records must always be intact.
			if v, ok, _ := db2.Get([]byte("alpha")); !ok || string(v) != "one" {
				t.Fatalf("alpha lost after cut=%d: (%q,%v)", cut, v, ok)
			}
			if v, ok, _ := db2.Get([]byte("beta")); !ok || string(v) != "two" {
				t.Fatalf("beta lost after cut=%d: (%q,%v)", cut, v, ok)
			}
			// The partial third record must not appear.
			if _, ok, _ := db2.Get([]byte("gamma")); ok {
				t.Fatalf("partial gamma record was applied at cut=%d", cut)
			}
			// The file must be truncated back to the last good record.
			if got := fileSizeOf(t, path); got != safeSize {
				t.Fatalf("cut=%d: size after recovery = %d, want %d", cut, got, safeSize)
			}
		})
	}
}

// TestCrashRecoveryCorruptCRC flips a byte in a committed record's payload so
// its CRC no longer matches, then reopens. Everything up to the corruption must
// survive; the corrupt record and anything after it is dropped.
func TestCrashRecoveryCorruptCRC(t *testing.T) {
	path := filepath.Join(t.TempDir(), "corrupt.sled")
	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Set([]byte("keep"), []byte("safe")); err != nil {
		t.Fatal(err)
	}
	boundary := fileSizeOf(t, path)
	if err := db.Set([]byte("corruptme"), []byte("payload")); err != nil {
		t.Fatal(err)
	}
	if err := db.Set([]byte("after"), []byte("later")); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	// Corrupt one byte inside the payload of the second record.
	f, err := os.OpenFile(path, os.O_RDWR, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	// The payload of record 2 starts at boundary+8 (after its header).
	corruptAt := boundary + 8 + 2
	buf := make([]byte, 1)
	if _, err := f.ReadAt(buf, corruptAt); err != nil {
		t.Fatal(err)
	}
	buf[0] ^= 0xff
	if _, err := f.WriteAt(buf, corruptAt); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	db2, err := Open(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer func() { _ = db2.Close() }()

	if v, ok, _ := db2.Get([]byte("keep")); !ok || string(v) != "safe" {
		t.Fatalf("record before corruption lost: (%q,%v)", v, ok)
	}
	// The corrupt record and everything after it must be gone.
	if _, ok, _ := db2.Get([]byte("corruptme")); ok {
		t.Fatal("corrupt record was applied")
	}
	if _, ok, _ := db2.Get([]byte("after")); ok {
		t.Fatal("record after corruption was applied")
	}
	if got := fileSizeOf(t, path); got != boundary {
		t.Fatalf("size after recovery = %d, want %d", got, boundary)
	}
}

func fileSizeOf(t *testing.T, path string) int64 {
	t.Helper()
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return fi.Size()
}

// appendTornRecord appends a header that claims a large payload followed by
// only a few payload bytes, mimicking a crash in the middle of writing a record.
func appendTornRecord(t *testing.T, path string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	var hdr [8]byte
	binary.LittleEndian.PutUint32(hdr[0:4], 1000)                    // claim 1000-byte payload
	binary.LittleEndian.PutUint32(hdr[4:8], crc32.ChecksumIEEE(nil)) // bogus crc
	if _, err := f.Write(hdr[:]); err != nil {
		t.Fatal(err)
	}
	if _, err := f.Write([]byte("only a few bytes")); err != nil { // far fewer than 1000
		t.Fatal(err)
	}
}
