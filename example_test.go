package sled_test

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/malcolmston/sled"
)

// Example demonstrates the core of the sled API: opening a database, basic
// writes, an atomic transaction, and an ordered prefix scan.
func Example() {
	dir, _ := os.MkdirTemp("", "sled-example")
	defer func() { _ = os.RemoveAll(dir) }()

	db, err := sled.Open(filepath.Join(dir, "example.sled"))
	if err != nil {
		panic(err)
	}
	defer func() { _ = db.Close() }()

	// Simple durable writes.
	_ = db.Set([]byte("greeting"), []byte("hello"))

	// An atomic read/write transaction: all writes commit together or not at
	// all.
	_ = db.Update(func(tx *sled.Tx) error {
		_ = tx.Set([]byte("user:alice"), []byte("admin"))
		_ = tx.Set([]byte("user:bob"), []byte("member"))
		return nil
	})

	// Read a value back.
	if v, ok, _ := db.Get([]byte("greeting")); ok {
		fmt.Printf("greeting=%s\n", v)
	}

	// Ordered scan over a key prefix.
	it := db.Scan(sled.Range{Prefix: []byte("user:")})
	for it.Valid() {
		fmt.Printf("%s=%s\n", it.Key(), it.Value())
		it.Next()
	}

	// Output:
	// greeting=hello
	// user:alice=admin
	// user:bob=member
}
