package sled

import (
	"bytes"
	"fmt"
	"math/rand"
	"testing"
)

func TestEncodeDecodeRoundTrip(t *testing.T) {
	ops := []op{
		{kind: opSet, key: []byte("a"), value: []byte("1")},
		{kind: opDelete, key: []byte("b")},
		{kind: opSet, key: []byte("empty-value"), value: []byte{}},
		{kind: opSet, key: bytes.Repeat([]byte("x"), 5000), value: bytes.Repeat([]byte("y"), 9000)},
	}
	rec := encodeRecord(ops)
	// Strip the 8-byte frame header to get the payload.
	payload := rec[8:]
	got, err := decodePayload(payload)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got) != len(ops) {
		t.Fatalf("decoded %d ops, want %d", len(got), len(ops))
	}
	for i := range ops {
		if got[i].kind != ops[i].kind ||
			!bytes.Equal(got[i].key, ops[i].key) ||
			!bytes.Equal(got[i].value, ops[i].value) {
			t.Fatalf("op %d mismatch: got %+v want %+v", i, got[i], ops[i])
		}
	}
}

func TestDecodeRejectsGarbage(t *testing.T) {
	// Claims 5 ops but has no bytes for them.
	if _, err := decodePayload([]byte{0x05}); err == nil {
		t.Fatal("expected error decoding truncated payload")
	}
	// Unknown op kind.
	bad := []byte{0x01, 0x09, 0x01, 'k'} // count=1, kind=9, keylen=1, 'k'
	if _, err := decodePayload(bad); err == nil {
		t.Fatal("expected error for unknown op kind")
	}
}

// TestTreapMatchesMap fuzzes the persistent treap against a reference map and
// verifies get/insert/remove semantics plus strictly-ordered traversal.
func TestTreapMatchesMap(t *testing.T) {
	rng := rand.New(rand.NewSource(42))
	ref := map[string]string{}
	var root *node

	for step := 0; step < 20000; step++ {
		k := fmt.Sprintf("k%03d", rng.Intn(300))
		switch rng.Intn(3) {
		case 0, 1:
			v := fmt.Sprintf("v%d", step)
			root = insert(root, []byte(k), []byte(v))
			ref[k] = v
		default:
			root = remove(root, []byte(k))
			delete(ref, k)
		}
	}

	// Every reference entry must be retrievable.
	for k, v := range ref {
		got, ok := get(root, []byte(k))
		if !ok || string(got) != v {
			t.Fatalf("get(%q) = (%q,%v), want (%q,true)", k, got, ok, v)
		}
	}
	if got := count(root); got != len(ref) {
		t.Fatalf("count = %d, want %d", got, len(ref))
	}

	// In-order traversal must be strictly ascending and cover exactly ref.
	var prev []byte
	n := 0
	inorder(root, func(nd *node) bool {
		if prev != nil && bytes.Compare(prev, nd.key) >= 0 {
			t.Fatalf("treap not ordered: %q >= %q", prev, nd.key)
		}
		prev = nd.key
		if _, ok := ref[string(nd.key)]; !ok {
			t.Fatalf("treap has key %q absent from reference", nd.key)
		}
		n++
		return true
	})
	if n != len(ref) {
		t.Fatalf("traversal visited %d, want %d", n, len(ref))
	}
}

// TestTreapPersistence verifies that an older root is unaffected by later
// mutations (structural sharing must not mutate shared nodes).
func TestTreapPersistence(t *testing.T) {
	var v1 *node
	for i := 0; i < 100; i++ {
		v1 = insert(v1, []byte(fmt.Sprintf("k%03d", i)), []byte("original"))
	}
	// Derive a new version that overwrites and deletes.
	v2 := v1
	for i := 0; i < 100; i += 2 {
		v2 = insert(v2, []byte(fmt.Sprintf("k%03d", i)), []byte("changed"))
	}
	for i := 1; i < 100; i += 2 {
		v2 = remove(v2, []byte(fmt.Sprintf("k%03d", i)))
	}

	// v1 must be exactly as it was.
	if count(v1) != 100 {
		t.Fatalf("v1 count = %d, want 100", count(v1))
	}
	for i := 0; i < 100; i++ {
		got, ok := get(v1, []byte(fmt.Sprintf("k%03d", i)))
		if !ok || string(got) != "original" {
			t.Fatalf("old snapshot mutated at k%03d: (%q,%v)", i, got, ok)
		}
	}
}

func TestPrefixUpperBound(t *testing.T) {
	cases := []struct {
		in   []byte
		want []byte
	}{
		{[]byte("a"), []byte("b")},
		{[]byte("az"), []byte("a{")}, // last byte 'z'(0x7a)+1 == '{'(0x7b)
		{[]byte{0x01, 0xff}, []byte{0x02}},
		{[]byte{0xff}, nil},
		{[]byte{0xff, 0xff}, nil},
		{[]byte{}, nil},
		{nil, nil},
	}
	for _, c := range cases {
		got := prefixUpperBound(c.in)
		if !bytes.Equal(got, c.want) {
			t.Fatalf("prefixUpperBound(%v) = %v, want %v", c.in, got, c.want)
		}
	}
}
