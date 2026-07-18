package sled

// Flush fsyncs the write-ahead log, forcing all previously written records to
// stable storage. With the default synchronous write mode every commit already
// fsyncs, so Flush is chiefly useful after opening with [WithSyncWrites](false)
// to make a batch of writes durable on demand.
func (db *DB) Flush() error {
	db.mu.Lock()
	defer db.mu.Unlock()
	if db.closed.Load() {
		return ErrClosed
	}
	return db.log.Sync()
}

// FlushAsync starts a [DB.Flush] on a background goroutine and returns a channel
// that receives the result (nil on success) exactly once and is then closed.
// This lets a caller trigger durability without blocking, for example:
//
//	done := db.FlushAsync()
//	// ... do other work ...
//	err := <-done
func (db *DB) FlushAsync() <-chan error {
	done := make(chan error, 1)
	go func() {
		done <- db.Flush()
		close(done)
	}()
	return done
}
