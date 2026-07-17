// Package sled is a small, embedded, transactional, crash-safe key/value
// store written in pure Go with no third-party dependencies.
//
// # Storage model
//
// A sled database is a single append-only write-ahead log (WAL) file on disk.
// Every durable mutation — a single Set/Delete, a Batch, or a read/write
// transaction — is encoded as exactly one self-describing record and appended
// to the log. Each record is framed as:
//
//	[uint32 payload length][uint32 CRC-32 of payload][payload bytes]
//
// The payload holds one or more operations (Set or Delete, each carrying a key
// and, for Set, a value). Because a whole Batch or transaction is written as a
// single record, and the CRC covers the entire payload, a record is applied
// all-or-nothing on recovery: the group of writes is atomic and durable.
//
// On Open the log is replayed from the beginning to rebuild the in-memory
// index. Replay stops at the first record that is truncated (a short read) or
// whose CRC does not match — i.e. the first sign of a crash that occurred while
// a record was being appended. Everything committed before that point is
// recovered intact; the incomplete tail is discarded and physically truncated
// from the file so subsequent appends stay contiguous. This is what makes sled
// crash-safe: a crash mid-write can only ever lose the single in-flight record,
// never previously committed data.
//
// When [DB.Close] with the default options, or any write with sync enabled,
// returns without error, the corresponding records have been flushed to stable
// storage with fsync.
//
// # In-memory index
//
// Keys are held in an ordered, immutable persistent treap (a balanced binary
// search tree). Each write produces a new tree root that structurally shares
// all untouched nodes with the previous version and is published with a single
// atomic pointer store. The ordering supports ascending range scans with
// prefix and lower/upper bounds via [DB.Scan].
//
// # Concurrency model and isolation
//
// sled is a single-writer, multi-reader store:
//
//   - Writers are fully serialized by an internal mutex. At most one write —
//     [DB.Set], [DB.Delete], [DB.Batch], or [DB.Update] — runs at a time.
//
//   - Readers never take locks. [DB.Get], [DB.Has], [DB.Scan] and [DB.View]
//     each capture the current immutable tree root and read from that
//     consistent snapshot. Because tree nodes and the byte slices they hold are
//     never mutated after publication, readers run concurrently with the writer
//     and with each other, with no data races.
//
// The resulting isolation level is serializable. A read/write transaction
// ([DB.Update]) holds the single writer slot for its whole duration and commits
// atomically, so its execution is equivalent to some serial order. A read-only
// transaction ([DB.View]) observes an immutable snapshot taken when it begins
// (snapshot isolation that, combined with the single-writer commit order, is
// serializable) and is unaffected by any writes committed while it runs.
//
// # Durability and compaction
//
// By default every commit calls fsync before returning. This can be relaxed
// with [WithSyncWrites] for workloads that prefer throughput over per-commit
// durability. Because the log is append-only, superseded and deleted keys
// accumulate on disk; [DB.Compact] rewrites the log to contain only the live
// key set, reclaiming space, and installs it atomically with a rename.
//
// The path passed to [Open] is the log file itself; sled creates it if it does
// not exist. Keys must be non-empty; values may be empty and are distinguished
// from absent keys by the boolean returned from [DB.Get].
package sled
