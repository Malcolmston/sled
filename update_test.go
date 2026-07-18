package sled

import (
	"strconv"
	"testing"
)

func TestUpdateAndFetch(t *testing.T) {
	db := testDB(t)

	// Absent key: f sees nil, we install a value, and the new value is returned.
	nv, err := db.UpdateAndFetch([]byte("n"), func(old []byte) []byte {
		if old != nil {
			t.Fatalf("expected nil old for absent key, got %q", old)
		}
		return []byte("1")
	})
	if err != nil || string(nv) != "1" {
		t.Fatalf("UpdateAndFetch install = %q,%v; want 1,nil", nv, err)
	}

	// Present key: increment.
	for want := 2; want <= 5; want++ {
		nv, err := db.UpdateAndFetch([]byte("n"), func(old []byte) []byte {
			cur, _ := strconv.Atoi(string(old))
			return []byte(strconv.Itoa(cur + 1))
		})
		if err != nil || string(nv) != strconv.Itoa(want) {
			t.Fatalf("increment = %q,%v; want %d", nv, err, want)
		}
	}

	// Returning nil deletes the key and yields a nil result.
	nv, err = db.UpdateAndFetch([]byte("n"), func(old []byte) []byte { return nil })
	if err != nil || nv != nil {
		t.Fatalf("delete via UpdateAndFetch = %q,%v; want nil,nil", nv, err)
	}
	if has, _ := db.Has([]byte("n")); has {
		t.Fatalf("key not deleted")
	}
}

func TestUpdateAndFetchErrors(t *testing.T) {
	db := testDB(t)
	if _, err := db.UpdateAndFetch(nil, func([]byte) []byte { return nil }); err != ErrEmptyKey {
		t.Errorf("empty key = %v; want ErrEmptyKey", err)
	}
	if _, err := db.UpdateAndFetch([]byte("k"), nil); err != ErrNilFunc {
		t.Errorf("nil func = %v; want ErrNilFunc", err)
	}
}
