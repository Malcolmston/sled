package sled

import (
	"fmt"
	"testing"
)

func TestTreeChecksumKnownValues(t *testing.T) {
	db := testDB(t)

	// An empty tree hashes to zero (CRC-32 of no bytes). The DB-level checksum
	// additionally folds in tree names, so it is not zero even when empty.
	if got := db.def.Checksum(); got != 0 {
		t.Errorf("empty tree Checksum = 0x%08x; want 0", got)
	}

	_ = db.Set([]byte("a"), []byte("b"))
	if got := db.def.Checksum(); got != 0x6a4a285b {
		t.Errorf("Checksum{a:b} = 0x%08x; want 0x6a4a285b", got)
	}

	_ = db.Clear()
	for _, kv := range [][2]string{{"a", "1"}, {"b", "2"}, {"c", "3"}} {
		_ = db.Set([]byte(kv[0]), []byte(kv[1]))
	}
	if got := db.def.Checksum(); got != 0x337a5b0d {
		t.Errorf("Checksum{a:1,b:2,c:3} = 0x%08x; want 0x337a5b0d", got)
	}
}

func TestChecksumOrderIndependent(t *testing.T) {
	d1 := testDB(t)
	d2 := testDB(t)
	for _, k := range []string{"a", "b", "c", "d", "e"} {
		_ = d1.Set([]byte(k), []byte(k+k))
	}
	for _, k := range []string{"e", "c", "a", "d", "b"} { // different insert order
		_ = d2.Set([]byte(k), []byte(k+k))
	}
	if d1.Checksum() != d2.Checksum() {
		t.Errorf("checksums differ by insertion order: 0x%08x vs 0x%08x", d1.Checksum(), d2.Checksum())
	}

	// A single value change must alter the checksum.
	_ = d2.Set([]byte("c"), []byte("different"))
	if d1.Checksum() == d2.Checksum() {
		t.Errorf("checksum unchanged after a value change")
	}
}

func TestChecksumFramingDisambiguates(t *testing.T) {
	// {"ab":"c"} and {"a":"bc"} share the same concatenated bytes; length
	// framing must give them different checksums.
	d1 := testDB(t)
	d2 := testDB(t)
	_ = d1.Set([]byte("ab"), []byte("c"))
	_ = d2.Set([]byte("a"), []byte("bc"))
	if d1.Checksum() == d2.Checksum() {
		t.Errorf("framing failed: distinct trees share checksum 0x%08x", d1.Checksum())
	}
}

func TestDBChecksumIncludesTreeNames(t *testing.T) {
	d1 := testDB(t)
	d2 := testDB(t)
	t1, _ := d1.OpenTree("alpha")
	_ = t1.Set([]byte("k"), []byte("v"))
	t2, _ := d2.OpenTree("beta")
	_ = t2.Set([]byte("k"), []byte("v"))
	if d1.Checksum() == d2.Checksum() {
		t.Errorf("DB checksum ignored tree name")
	}
}

func BenchmarkTreeChecksum(b *testing.B) {
	db := benchDB(b, WithSyncWrites(false))
	for i := 0; i < 1000; i++ {
		_ = db.Set([]byte(fmt.Sprintf("k%06d", i)), []byte("value"))
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = db.def.Checksum()
	}
}
