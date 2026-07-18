package sled

// Next blocks until the next event is available and returns it with ok=true.
// When the subscriber is closed (by [Subscriber.Close]) and its buffer is
// drained, Next returns the zero Event with ok=false. It is a blocking
// convenience over the [Subscriber.Events] channel, mirroring sled's blocking
// Subscriber iteration:
//
//	for {
//		ev, ok := sub.Next()
//		if !ok {
//			break
//		}
//		// handle ev
//	}
//
// Next must be called from a single goroutine at a time; concurrent receivers
// share one channel and would each get a subset of events.
func (s *Subscriber) Next() (Event, bool) {
	ev, ok := <-s.ch
	return ev, ok
}

// TryNext returns the next buffered event without blocking. ok is false when no
// event is currently buffered (whether the subscriber is still open or already
// closed). Use [Subscriber.Next] to wait for an event instead.
func (s *Subscriber) TryNext() (Event, bool) {
	select {
	case ev, ok := <-s.ch:
		return ev, ok
	default:
		return Event{}, false
	}
}

// Drain returns up to max buffered events without blocking, in delivery order.
// It stops as soon as the buffer is empty, so it may return fewer than max (or
// none). A non-positive max drains every currently buffered event. Drain never
// waits for new events to arrive.
func (s *Subscriber) Drain(max int) []Event {
	var out []Event
	for max <= 0 || len(out) < max {
		select {
		case ev, ok := <-s.ch:
			if !ok {
				return out
			}
			out = append(out, ev)
		default:
			return out
		}
	}
	return out
}
