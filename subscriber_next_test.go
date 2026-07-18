package sled

import "testing"

func TestSubscriberNext(t *testing.T) {
	db := testDB(t)
	sub := db.Watch(nil)

	if err := db.Set([]byte("a"), []byte("1")); err != nil {
		t.Fatalf("Set: %v", err)
	}
	ev, ok := sub.Next()
	if !ok || ev.Type != EventInsert || string(ev.Key) != "a" || string(ev.Value) != "1" {
		t.Fatalf("Next = %+v ok=%v; want insert a=1", ev, ok)
	}

	// After Close and drain, Next reports ok=false.
	sub.Close()
	if _, ok := sub.Next(); ok {
		t.Fatalf("Next after Close returned ok=true")
	}
}

func TestSubscriberTryNext(t *testing.T) {
	db := testDB(t)
	sub := db.Watch(nil)
	defer sub.Close()

	if _, ok := sub.TryNext(); ok {
		t.Fatalf("TryNext on empty buffer returned ok=true")
	}
	_ = db.Set([]byte("k"), []byte("v"))
	ev, ok := sub.TryNext()
	if !ok || string(ev.Key) != "k" {
		t.Fatalf("TryNext = %+v ok=%v; want k", ev, ok)
	}
	if _, ok := sub.TryNext(); ok {
		t.Fatalf("second TryNext returned ok=true")
	}
}

func TestSubscriberDrain(t *testing.T) {
	db := testDB(t)
	sub := db.Watch(nil)
	defer sub.Close()

	for i := 0; i < 5; i++ {
		_ = db.Set([]byte{byte('a' + i)}, []byte("v"))
	}
	// Drain at most 3.
	got := sub.Drain(3)
	if len(got) != 3 || string(got[0].Key) != "a" || string(got[2].Key) != "c" {
		t.Fatalf("Drain(3) = %d events %v", len(got), keysOf(got))
	}
	// Drain the rest (max<=0 means all buffered).
	rest := sub.Drain(0)
	if len(rest) != 2 || string(rest[0].Key) != "d" || string(rest[1].Key) != "e" {
		t.Fatalf("Drain(0) = %v", keysOf(rest))
	}
	if extra := sub.Drain(0); len(extra) != 0 {
		t.Fatalf("Drain on empty buffer = %v", keysOf(extra))
	}
}

func keysOf(evs []Event) []string {
	out := make([]string, len(evs))
	for i, e := range evs {
		out[i] = string(e.Key)
	}
	return out
}
