package sled

import "sync"

// EventType classifies a subscriber event.
type EventType uint8

const (
	// EventInsert reports a key that did not previously exist being written.
	EventInsert EventType = iota
	// EventUpdate reports an existing key's value being replaced.
	EventUpdate
	// EventDelete reports a key being removed. The event's Value is nil.
	EventDelete
)

// String returns the event type's name ("insert", "update" or "delete").
func (e EventType) String() string {
	switch e {
	case EventInsert:
		return "insert"
	case EventUpdate:
		return "update"
	case EventDelete:
		return "delete"
	default:
		return "unknown"
	}
}

// An Event describes a single committed mutation delivered to a subscriber. Key
// and Value are owned by the DB and must not be modified; Value is nil for a
// delete.
type Event struct {
	Type  EventType
	Tree  string
	Key   []byte
	Value []byte

	// tree is the originating tree, used internally for delivery. It is not
	// exported; subscribers read the tree name via the Tree field.
	tree *Tree
}

// defaultSubBuffer is the per-subscriber channel capacity. Delivery is
// non-blocking, so a subscriber that falls this far behind will miss events
// rather than stall the committing writer.
const defaultSubBuffer = 1024

// A Subscriber is a stream of [Event]s for keys under a watched prefix in one
// tree. Receive events from the channel returned by [Subscriber.Events]. Always
// call [Subscriber.Close] when finished so the tree stops retaining the
// subscriber.
//
// Delivery is best-effort and non-blocking: events are buffered, and a
// subscriber that does not keep up may miss events rather than block the writer.
type Subscriber struct {
	tree   *Tree
	prefix []byte
	ch     chan Event

	mu     sync.Mutex
	closed bool
}

// Watch returns a [Subscriber] that receives an [Event] for every committed
// mutation whose key begins with prefix in this tree. A nil or empty prefix
// watches the whole tree. Events are delivered in commit order.
func (t *Tree) Watch(prefix []byte) *Subscriber {
	s := &Subscriber{
		tree:   t,
		prefix: cloneBytes(prefix),
		ch:     make(chan Event, defaultSubBuffer),
	}
	t.subsMu.Lock()
	if t.subs == nil {
		t.subs = make(map[*Subscriber]struct{})
	}
	t.subs[s] = struct{}{}
	t.subsMu.Unlock()
	return s
}

// Watch returns a subscriber over the default tree. See [Tree.Watch].
func (db *DB) Watch(prefix []byte) *Subscriber { return db.def.Watch(prefix) }

// Events returns the channel on which the subscriber's events arrive. The
// channel is closed by [Subscriber.Close].
func (s *Subscriber) Events() <-chan Event { return s.ch }

// Close stops the subscription and closes the event channel. It is idempotent
// and safe to call concurrently with delivery.
func (s *Subscriber) Close() {
	s.tree.subsMu.Lock()
	delete(s.tree.subs, s)
	s.tree.subsMu.Unlock()

	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.closed {
		s.closed = true
		close(s.ch)
	}
}

// matches reports whether key falls under the subscriber's prefix.
func (s *Subscriber) matches(key []byte) bool {
	return len(s.prefix) == 0 || hasPrefix(key, s.prefix)
}

// send delivers ev to the subscriber unless it is closed or its buffer is full.
// A full buffer drops the event, preserving the non-blocking contract.
func (s *Subscriber) send(ev Event) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	select {
	case s.ch <- ev:
	default:
	}
}

// publish delivers ev to every subscriber of the tree whose prefix matches the
// event's key. The event's Tree field is populated here. It is called by commit
// while the writer slot is held; sends are non-blocking so it never stalls.
func (t *Tree) publish(ev Event) {
	ev.Tree = t.name
	t.subsMu.Lock()
	subs := make([]*Subscriber, 0, len(t.subs))
	for s := range t.subs {
		if s.matches(ev.Key) {
			subs = append(subs, s)
		}
	}
	t.subsMu.Unlock()
	for _, s := range subs {
		s.send(ev)
	}
}

// eventKind classifies a write against the pre-write root: a key already present
// yields EventUpdate, otherwise EventInsert. isDelete short-circuits to
// EventDelete.
func eventKind(old *node, key []byte, isDelete bool) EventType {
	if isDelete {
		return EventDelete
	}
	if _, ok := get(old, key); ok {
		return EventUpdate
	}
	return EventInsert
}

// hasPrefix reports whether b begins with prefix, without importing bytes at
// every call site.
func hasPrefix(b, prefix []byte) bool {
	if len(b) < len(prefix) {
		return false
	}
	for i := range prefix {
		if b[i] != prefix[i] {
			return false
		}
	}
	return true
}
