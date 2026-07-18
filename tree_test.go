package sled

import (
	"bytes"
	"fmt"
	"path/filepath"
	"sort"
	"testing"
)

func TestTreeIsolation(t *testing.T) {
	db := testDB(t)
	a, err := db.OpenTree("a")
	if err != nil {
		t.Fatalf("OpenTree(a): %v", err)
	}
	b, err := db.OpenTree("b")
	if err != nil {
		t.Fatalf("OpenTree(b): %v", err)
	}

	if err := a.Set([]byte("k"), []byte("va")); err != nil {
		t.Fatalf("a.Set: %v", err)
	}
	if err := b.Set([]byte("k"), []byte("vb")); err != nil {
		t.Fatalf("b.Set: %v", err)
	}
	if err := db.Set([]byte("k"), []byte("vdefault")); err != nil {
		t.Fatalf("db.Set: %v", err)
	}

	// The same key holds an independent value in each tree.
	if v, _, _ := a.Get([]byte("k")); string(v) != "va" {
		t.Fatalf("a[k] = %q, want va", v)
	}
	if v, _, _ := b.Get([]byte("k")); string(v) != "vb" {
		t.Fatalf("b[k] = %q, want vb", v)
	}
	if v, _, _ := db.Get([]byte("k")); string(v) != "vdefault" {
		t.Fatalf("default[k] = %q, want vdefault", v)
	}

	// Deleting from one tree leaves the others intact.
	if err := a.Delete([]byte("k")); err != nil {
		t.Fatalf("a.Delete: %v", err)
	}
	if _, ok, _ := a.Get([]byte("k")); ok {
		t.Fatal("a[k] should be gone")
	}
	if _, ok, _ := b.Get([]byte("k")); !ok {
		t.Fatal("b[k] should still be present")
	}
}

func TestTreeIsolationPersists(t *testing.T) {
	path := filepath.Join(t.TempDir(), "trees.sled")
	db, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	users, _ := db.OpenTree("users")
	items, _ := db.OpenTree("items")
	_ = users.Set([]byte("alice"), []byte("admin"))
	_ = items.Set([]byte("sku1"), []byte("widget"))
	_ = db.Set([]byte("root"), []byte("v"))
	if err := db.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	db2, err := Open(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer func() { _ = db2.Close() }()

	u2, _ := db2.OpenTree("users")
	i2, _ := db2.OpenTree("items")
	if v, ok, _ := u2.Get([]byte("alice")); !ok || string(v) != "admin" {
		t.Fatalf("users[alice] = %q,%v after reopen", v, ok)
	}
	if v, ok, _ := i2.Get([]byte("sku1")); !ok || string(v) != "widget" {
		t.Fatalf("items[sku1] = %q,%v after reopen", v, ok)
	}
	if v, ok, _ := db2.Get([]byte("root")); !ok || string(v) != "v" {
		t.Fatalf("default[root] = %q,%v after reopen", v, ok)
	}
}

func TestTreeNames(t *testing.T) {
	db := testDB(t)
	_, _ = db.OpenTree("zebra")
	_, _ = db.OpenTree("apple")
	names := db.TreeNames()
	want := []string{DefaultTreeName, "apple", "zebra"}
	sort.Strings(want)
	if !equalStrings(names, want) {
		t.Fatalf("TreeNames = %v, want %v", names, want)
	}
}

func TestOpenTreeEmptyName(t *testing.T) {
	db := testDB(t)
	if _, err := db.OpenTree(""); err != ErrEmptyTreeName {
		t.Fatalf("OpenTree(\"\") err = %v, want ErrEmptyTreeName", err)
	}
	// The default tree name returns the default tree.
	dt, err := db.OpenTree(DefaultTreeName)
	if err != nil {
		t.Fatalf("OpenTree(default): %v", err)
	}
	_ = db.Set([]byte("x"), []byte("1"))
	if v, ok, _ := dt.Get([]byte("x")); !ok || string(v) != "1" {
		t.Fatalf("default handle disagrees: %q,%v", v, ok)
	}
}

func TestDropTree(t *testing.T) {
	path := filepath.Join(t.TempDir(), "drop.sled")
	db, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	tr, _ := db.OpenTree("temp")
	_ = tr.Set([]byte("k"), []byte("v"))

	// Cannot drop the default tree.
	if _, err := db.DropTree(DefaultTreeName); err != ErrDropDefaultTree {
		t.Fatalf("DropTree(default) err = %v, want ErrDropDefaultTree", err)
	}

	dropped, err := db.DropTree("temp")
	if err != nil || !dropped {
		t.Fatalf("DropTree(temp) = %v,%v", dropped, err)
	}
	if _, ok, _ := tr.Get([]byte("k")); ok {
		t.Fatal("dropped tree should be empty")
	}
	// Dropping again reports false.
	if dropped, err := db.DropTree("temp"); err != nil || dropped {
		t.Fatalf("second DropTree = %v,%v", dropped, err)
	}

	if err := db.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	db2, err := Open(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer func() { _ = db2.Close() }()
	for _, name := range db2.TreeNames() {
		if name == "temp" {
			t.Fatal("dropped tree survived reopen")
		}
	}
}

func TestContainsKeyLenClear(t *testing.T) {
	db := testDB(t)
	tr, _ := db.OpenTree("t")
	for i := 0; i < 5; i++ {
		_ = tr.Set([]byte{byte('a' + i)}, []byte{byte(i)})
	}
	if tr.Len() != 5 {
		t.Fatalf("Len = %d, want 5", tr.Len())
	}
	if ok, _ := tr.ContainsKey([]byte("c")); !ok {
		t.Fatal("ContainsKey(c) = false")
	}
	if ok, _ := tr.ContainsKey([]byte("z")); ok {
		t.Fatal("ContainsKey(z) = true")
	}
	if tr.IsEmpty() {
		t.Fatal("IsEmpty on non-empty tree")
	}
	if err := tr.Clear(); err != nil {
		t.Fatalf("Clear: %v", err)
	}
	if tr.Len() != 0 || !tr.IsEmpty() {
		t.Fatalf("after Clear Len = %d empty=%v", tr.Len(), tr.IsEmpty())
	}
}

func TestClearPersists(t *testing.T) {
	path := filepath.Join(t.TempDir(), "clear.sled")
	db, _ := Open(path)
	tr, _ := db.OpenTree("t")
	_ = tr.Set([]byte("a"), []byte("1"))
	_ = tr.Set([]byte("b"), []byte("2"))
	_ = tr.Clear()
	_ = db.Close()

	db2, _ := Open(path)
	defer func() { _ = db2.Close() }()
	t2, _ := db2.OpenTree("t")
	if t2.Len() != 0 {
		t.Fatalf("cleared tree has %d keys after reopen", t2.Len())
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestCrossTreeTransactionAtomic(t *testing.T) {
	path := filepath.Join(t.TempDir(), "xtx.sled")
	db, _ := Open(path)
	accounts, _ := db.OpenTree("accounts")
	ledger, _ := db.OpenTree("ledger")

	_ = accounts.Set([]byte("alice"), []byte("100"))
	_ = accounts.Set([]byte("bob"), []byte("0"))

	// A committed cross-tree transaction publishes both trees.
	err := db.Update(func(tx *Tx) error {
		if err := tx.SetTree(accounts, []byte("alice"), []byte("70")); err != nil {
			return err
		}
		if err := tx.SetTree(accounts, []byte("bob"), []byte("30")); err != nil {
			return err
		}
		return tx.SetTree(ledger, []byte("txn1"), []byte("alice->bob:30"))
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if v, _, _ := accounts.Get([]byte("bob")); string(v) != "30" {
		t.Fatalf("bob = %q, want 30", v)
	}
	if v, _, _ := ledger.Get([]byte("txn1")); string(v) != "alice->bob:30" {
		t.Fatalf("ledger = %q", v)
	}

	// A rolled-back cross-tree transaction leaves both trees untouched.
	errBoom := fmt.Errorf("boom")
	err = db.Update(func(tx *Tx) error {
		_ = tx.SetTree(accounts, []byte("alice"), []byte("999"))
		_ = tx.SetTree(ledger, []byte("txn2"), []byte("bad"))
		return errBoom
	})
	if err != errBoom {
		t.Fatalf("Update err = %v, want boom", err)
	}
	if v, _, _ := accounts.Get([]byte("alice")); string(v) != "70" {
		t.Fatalf("alice rolled forward: %q", v)
	}
	if _, ok, _ := ledger.Get([]byte("txn2")); ok {
		t.Fatal("ledger txn2 should not exist after rollback")
	}

	// Atomicity survives reopen: the whole committed transaction is one record.
	_ = db.Close()
	db2, _ := Open(path)
	defer func() { _ = db2.Close() }()
	a2, _ := db2.OpenTree("accounts")
	l2, _ := db2.OpenTree("ledger")
	if v, _, _ := a2.Get([]byte("bob")); string(v) != "30" {
		t.Fatalf("after reopen bob = %q", v)
	}
	if _, ok, _ := l2.Get([]byte("txn2")); ok {
		t.Fatal("after reopen txn2 present")
	}
}

func TestCrossTreeTransactionReadsOwnWrites(t *testing.T) {
	db := testDB(t)
	x, _ := db.OpenTree("x")
	y, _ := db.OpenTree("y")
	err := db.Update(func(tx *Tx) error {
		if err := tx.SetTree(x, []byte("k"), []byte("1")); err != nil {
			return err
		}
		v, ok, _ := tx.GetTree(x, []byte("k"))
		if !ok || string(v) != "1" {
			t.Fatalf("tx did not read its own write: %q,%v", v, ok)
		}
		return tx.SetTree(y, []byte("k"), v)
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if v, _, _ := y.Get([]byte("k")); !bytes.Equal(v, []byte("1")) {
		t.Fatalf("y[k] = %q", v)
	}
}
