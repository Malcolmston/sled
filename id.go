package sled

// idReserveBlock is how many identifiers GenerateID reserves per durable log
// record. A larger block amortizes the reservation write across more IDs at the
// cost of a larger gap after a crash.
const idReserveBlock = 1024

// GenerateID returns a process-unique, strictly increasing 64-bit identifier.
// IDs are monotonic across restarts: on recovery the counter resumes past the
// last reserved block, so an ID is never reused (though a crash may leave a gap
// where the tail of a reserved block is skipped).
//
// Reservations are persisted in blocks, so most calls perform no I/O; only the
// call that crosses a block boundary writes and fsyncs a small record.
func (db *DB) GenerateID() (uint64, error) {
	db.mu.Lock()
	defer db.mu.Unlock()
	if db.closed.Load() {
		return 0, ErrClosed
	}
	if db.idNext >= db.idReserved {
		newReserved := db.idReserved + idReserveBlock
		o := op{kind: opIDReserve, num: newReserved}
		if err := db.commit([]op{o}, nil, nil); err != nil {
			return 0, err
		}
		db.idReserved = newReserved
	}
	id := db.idNext
	db.idNext++
	return id, nil
}
