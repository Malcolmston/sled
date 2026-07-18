package sled

import (
	"bytes"
	"path/filepath"
	"testing"
)

func TestExportImportRoundTrip(t *testing.T) {
	src := testDB(t)
	_ = src.Set([]byte("root1"), []byte("r1"))
	_ = src.Set([]byte("root2"), []byte("r2"))
	users, _ := src.OpenTree("users")
	_ = users.Set([]byte("alice"), []byte("admin"))
	_ = users.Set([]byte("bob"), []byte("member"))
	items, _ := src.OpenTree("items")
	_ = items.Set([]byte("sku"), []byte(""))

	var buf bytes.Buffer
	if err := src.Export(&buf); err != nil {
		t.Fatalf("Export: %v", err)
	}

	dst, err := Open(filepath.Join(t.TempDir(), "dst.sled"))
	if err != nil {
		t.Fatalf("Open dst: %v", err)
	}
	defer func() { _ = dst.Close() }()
	if err := dst.Import(bytes.NewReader(buf.Bytes())); err != nil {
		t.Fatalf("Import: %v", err)
	}

	if v, ok, _ := dst.Get([]byte("root1")); !ok || string(v) != "r1" {
		t.Fatalf("dst default root1 = %q,%v", v, ok)
	}
	du, _ := dst.OpenTree("users")
	if v, ok, _ := du.Get([]byte("alice")); !ok || string(v) != "admin" {
		t.Fatalf("dst users alice = %q,%v", v, ok)
	}
	di, _ := dst.OpenTree("items")
	if v, ok, _ := di.Get([]byte("sku")); !ok || string(v) != "" {
		t.Fatalf("dst items sku = %q,%v", v, ok)
	}

	// Round-trip survives a reopen of the destination (import was durable).
	path := dst.Path()
	_ = dst.Close()
	dst2, _ := Open(path)
	defer func() { _ = dst2.Close() }()
	u2, _ := dst2.OpenTree("users")
	if v, ok, _ := u2.Get([]byte("bob")); !ok || string(v) != "member" {
		t.Fatalf("after reopen bob = %q,%v", v, ok)
	}
}

func TestImportRejectsCorruptChecksum(t *testing.T) {
	src := testDB(t)
	_ = src.Set([]byte("k"), []byte("v"))
	var buf bytes.Buffer
	if err := src.Export(&buf); err != nil {
		t.Fatalf("Export: %v", err)
	}
	// Flip the value-content byte (just before the 4-byte trailing CRC) so the
	// framing stays valid but the checksum no longer matches.
	data := buf.Bytes()
	data[len(data)-5] ^= 0xff

	dst := testDB(t)
	if err := dst.Import(bytes.NewReader(data)); err != ErrCorruptImport {
		t.Fatalf("Import corrupt = %v, want ErrCorruptImport", err)
	}
	// Nothing was applied.
	if _, ok, _ := dst.Get([]byte("k")); ok {
		t.Fatal("corrupt import applied data")
	}
}

func TestImportRejectsBadMagic(t *testing.T) {
	dst := testDB(t)
	if err := dst.Import(bytes.NewReader([]byte("not a sled export"))); err != ErrCorruptImport {
		t.Fatalf("Import bad magic = %v, want ErrCorruptImport", err)
	}
}

func TestExportImportEmpty(t *testing.T) {
	src := testDB(t)
	var buf bytes.Buffer
	if err := src.Export(&buf); err != nil {
		t.Fatalf("Export empty: %v", err)
	}
	dst := testDB(t)
	if err := dst.Import(&buf); err != nil {
		t.Fatalf("Import empty: %v", err)
	}
	if dst.Len() != 0 {
		t.Fatalf("empty import produced %d keys", dst.Len())
	}
}
