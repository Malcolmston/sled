package sled

import (
	"fmt"
	"os"
)

// SizeOnDisk returns the current size, in bytes, of the database's write-ahead
// log file. Because sled stores all state in a single append-only log, this is
// the total on-disk footprint of the database (a subsequent [DB.Compact] can
// shrink it by discarding superseded and deleted records). It mirrors sled's
// Db::size_on_disk.
func (db *DB) SizeOnDisk() (uint64, error) {
	if db.closed.Load() {
		return 0, ErrClosed
	}
	fi, err := os.Stat(db.path)
	if err != nil {
		return 0, fmt.Errorf("sled: size on disk: %w", err)
	}
	return uint64(fi.Size()), nil
}

// WasRecovered reports whether this database was recovered from a pre-existing,
// non-empty log at [Open] time, as opposed to being created fresh. It lets a
// caller distinguish a first run (where it may want to seed initial data) from
// a restart. It mirrors sled's Db::was_recovered.
func (db *DB) WasRecovered() bool { return db.recovered }
