package sled

import (
	"errors"
	"testing"
)

func TestCompareAndSwapErrSuccess(t *testing.T) {
	db := testDB(t)

	// Install on absent key (old == nil).
	if err := db.CompareAndSwapErr([]byte("k"), nil, []byte("v1")); err != nil {
		t.Fatalf("install = %v; want nil", err)
	}
	// Matching swap.
	if err := db.CompareAndSwapErr([]byte("k"), []byte("v1"), []byte("v2")); err != nil {
		t.Fatalf("swap = %v; want nil", err)
	}
	if v, _, _ := db.Get([]byte("k")); string(v) != "v2" {
		t.Fatalf("value = %q; want v2", v)
	}
	// Delete via nil newValue.
	if err := db.CompareAndSwapErr([]byte("k"), []byte("v2"), nil); err != nil {
		t.Fatalf("delete = %v; want nil", err)
	}
	if has, _ := db.Has([]byte("k")); has {
		t.Fatalf("key not deleted")
	}
}

func TestCompareAndSwapErrMismatch(t *testing.T) {
	db := testDB(t)
	_ = db.Set([]byte("k"), []byte("actual"))

	err := db.CompareAndSwapErr([]byte("k"), []byte("wrong"), []byte("new"))
	var casErr *CompareAndSwapError
	if !errors.As(err, &casErr) {
		t.Fatalf("err = %v; want *CompareAndSwapError", err)
	}
	if string(casErr.Current) != "actual" || string(casErr.Proposed) != "new" {
		t.Fatalf("CAS error = %+v; want current=actual proposed=new", casErr)
	}
	if casErr.Error() == "" {
		t.Errorf("Error() returned empty string")
	}
	// The store must be unchanged after a failed swap.
	if v, _, _ := db.Get([]byte("k")); string(v) != "actual" {
		t.Errorf("value changed on failed CAS: %q", v)
	}

	// Expecting absence when the key is present also fails.
	err = db.CompareAndSwapErr([]byte("k"), nil, []byte("x"))
	if !errors.As(err, &casErr) {
		t.Fatalf("expect-absent on present key = %v; want *CompareAndSwapError", err)
	}
}

func TestCompareAndSwapErrEmptyKey(t *testing.T) {
	db := testDB(t)
	if err := db.CompareAndSwapErr(nil, nil, []byte("v")); err != ErrEmptyKey {
		t.Errorf("empty key = %v; want ErrEmptyKey", err)
	}
}
