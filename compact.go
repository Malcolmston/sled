package sled

import (
	"fmt"
	"io"
	"os"
)

// compactRecordKeys bounds how many live keys are packed into each record of a
// rewritten log during Compact. Grouping keeps the rewrite efficient while
// keeping any single record a modest size.
const compactRecordKeys = 1024

// Compact rewrites the log so it contains only the current live key set,
// reclaiming the space held by superseded values and deleted keys. The new log
// is written to a temporary file, fsynced, and atomically renamed over the
// existing log, so a crash during compaction leaves the original log intact.
//
// Compact takes the single writer slot for its whole duration; concurrent
// readers are unaffected and continue to observe the in-memory snapshot, which
// does not change.
func (db *DB) Compact() (err error) {
	db.mu.Lock()
	defer db.mu.Unlock()
	if db.closed.Load() {
		return ErrClosed
	}

	// Snapshot every tree so the rewrite reproduces the whole database.
	db.treesMu.RLock()
	trees := make([]*Tree, 0, len(db.trees))
	for _, t := range db.trees {
		trees = append(trees, t)
	}
	db.treesMu.RUnlock()

	tmpPath := db.path + ".compact"

	tmp, err := os.OpenFile(tmpPath, os.O_RDWR|os.O_CREATE|os.O_TRUNC, db.opts.fileMode)
	if err != nil {
		return fmt.Errorf("sled: create compaction file: %w", err)
	}
	// Best-effort cleanup if we bail out before the rename succeeds.
	committed := false
	defer func() {
		if !committed {
			_ = tmp.Close()
			_ = os.Remove(tmpPath)
		}
	}()

	// Walk each tree's live key set in order, writing Set records in bounded
	// groups.
	batch := make([]op, 0, compactRecordKeys)
	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		if _, err := tmp.Write(encodeRecord(batch)); err != nil {
			return fmt.Errorf("sled: write compaction record: %w", err)
		}
		batch = batch[:0]
		return nil
	}

	// Preserve the GenerateID high-water mark so identifiers stay monotonic
	// across a compaction.
	if db.idReserved > 0 {
		batch = append(batch, op{kind: opIDReserve, num: db.idReserved})
	}

	var walkErr error
	for _, t := range trees {
		inorder(t.snapshot(), func(n *node) bool {
			batch = append(batch, t.setOp(n.key, n.value))
			if len(batch) >= compactRecordKeys {
				if walkErr = flush(); walkErr != nil {
					return false
				}
			}
			return true
		})
		if walkErr != nil {
			return walkErr
		}
	}
	if err := flush(); err != nil {
		return err
	}

	if err := tmp.Sync(); err != nil {
		return fmt.Errorf("sled: sync compaction file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("sled: close compaction file: %w", err)
	}

	// Atomically replace the live log, then reopen it for appends.
	if err := os.Rename(tmpPath, db.path); err != nil {
		return fmt.Errorf("sled: install compacted log: %w", err)
	}
	committed = true
	if err := syncDir(db.path); err != nil {
		return err
	}

	if err := db.log.Close(); err != nil {
		return fmt.Errorf("sled: close old log: %w", err)
	}
	f, err := os.OpenFile(db.path, os.O_RDWR, db.opts.fileMode)
	if err != nil {
		return fmt.Errorf("sled: reopen compacted log: %w", err)
	}
	if _, err := f.Seek(0, io.SeekEnd); err != nil {
		_ = f.Close()
		return err
	}
	db.log = f
	return nil
}

// inorder visits every node in ascending key order, stopping early if visit
// returns false.
func inorder(n *node, visit func(*node) bool) {
	if n == nil {
		return
	}
	var walk func(*node) bool
	walk = func(n *node) bool {
		if n == nil {
			return true
		}
		if !walk(n.left) {
			return false
		}
		if !visit(n) {
			return false
		}
		return walk(n.right)
	}
	walk(n)
}
