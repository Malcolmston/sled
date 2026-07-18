package sled

import (
	"reflect"
	"testing"
)

func TestScanPrefixMethod(t *testing.T) {
	db := testDB(t)
	seed := []string{"user:1", "user:2", "user:10", "userx", "usa", "user:", "zzz"}
	for _, k := range seed {
		_ = db.Set([]byte(k), []byte("v"))
	}

	cases := []struct {
		prefix string
		want   []string
	}{
		{"user:", []string{"user:", "user:1", "user:10", "user:2"}},
		{"us", []string{"usa", "user:", "user:1", "user:10", "user:2", "userx"}},
		{"user:1", []string{"user:1", "user:10"}},
		{"none", nil},
		{"", []string{"usa", "user:", "user:1", "user:10", "user:2", "userx", "zzz"}},
	}
	for _, tc := range cases {
		var got []string
		for it := db.ScanPrefix([]byte(tc.prefix)); it.Valid(); it.Next() {
			got = append(got, string(it.Key()))
		}
		if !reflect.DeepEqual(got, tc.want) {
			t.Errorf("ScanPrefix(%q) = %v; want %v", tc.prefix, got, tc.want)
		}
	}
}

func TestPrefixRange(t *testing.T) {
	db := testDB(t)
	for _, k := range []string{"a:1", "a:2", "b:1"} {
		_ = db.Set([]byte(k), []byte("v"))
	}
	r := PrefixRange([]byte("a:"))
	r.Reverse = true
	var got []string
	for it := db.Scan(r); it.Valid(); it.Next() {
		got = append(got, string(it.Key()))
	}
	want := []string{"a:2", "a:1"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("reverse PrefixRange = %v; want %v", got, want)
	}

	// PrefixRange must copy the input so later mutation is harmless.
	p := []byte("a:")
	r2 := PrefixRange(p)
	p[0] = 'b'
	if string(r2.Prefix) != "a:" {
		t.Errorf("PrefixRange did not copy prefix: %q", r2.Prefix)
	}
}
