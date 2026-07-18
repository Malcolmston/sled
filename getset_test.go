package sled

import (
	"bytes"
	"testing"
)

func TestGetAndSet(t *testing.T) {
	db := testDB(t)

	old, existed, err := db.GetAndSet([]byte("k"), []byte("v1"))
	if err != nil {
		t.Fatalf("GetAndSet: %v", err)
	}
	if existed || old != nil {
		t.Fatalf("first GetAndSet = %q,%v; want nil,false", old, existed)
	}

	old, existed, err = db.GetAndSet([]byte("k"), []byte("v2"))
	if err != nil {
		t.Fatalf("GetAndSet: %v", err)
	}
	if !existed || string(old) != "v1" {
		t.Fatalf("second GetAndSet = %q,%v; want v1,true", old, existed)
	}
	if v, _, _ := db.Get([]byte("k")); string(v) != "v2" {
		t.Fatalf("current value = %q; want v2", v)
	}

	if _, _, err := db.GetAndSet(nil, []byte("x")); err != ErrEmptyKey {
		t.Errorf("empty key = %v; want ErrEmptyKey", err)
	}
}

func TestGetAndDelete(t *testing.T) {
	db := testDB(t)
	_ = db.Set([]byte("k"), []byte("val"))

	old, existed, err := db.GetAndDelete([]byte("k"))
	if err != nil || !existed || string(old) != "val" {
		t.Fatalf("GetAndDelete = %q,%v,%v; want val,true,nil", old, existed, err)
	}
	if has, _ := db.Has([]byte("k")); has {
		t.Fatalf("key not deleted")
	}

	// Deleting an absent key is not an error.
	old, existed, err = db.GetAndDelete([]byte("k"))
	if err != nil || existed || old != nil {
		t.Fatalf("delete absent = %q,%v,%v; want nil,false,nil", old, existed, err)
	}
}

func TestGetAndSetReturnedBytesOwned(t *testing.T) {
	db := testDB(t)
	_ = db.Set([]byte("k"), []byte("original"))
	old, _, _ := db.GetAndSet([]byte("k"), []byte("new"))
	old[0] = 'X' // mutate the caller's copy
	v, _, _ := db.Get([]byte("k"))
	if bytes.Equal(v, old) {
		t.Fatalf("returned old value aliased DB storage")
	}
}
