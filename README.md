# sled

A small, embedded, transactional, crash-safe key/value store for Go. Pure
standard library — no third-party dependencies, no cgo.

`sled` is built for **correctness and durability first**: every committed write
is flushed to disk with `fsync`, and a crash in the middle of a write can only
ever lose the single in-flight record, never previously committed data.

```
import "github.com/malcolmston/sled"
```

## Features

- **Durable append-only WAL.** All state lives in one write-ahead log file.
  Each record is length-prefixed and CRC-32 checksummed.
- **Real crash recovery.** On `Open` the log is replayed and stops at the first
  truncated or corrupt record, so a torn tail from a crash is dropped and
  physically truncated; everything committed before it survives intact.
- **Transactions.** `Update` runs a serializable read/write transaction that
  commits atomically or rolls back on error (or panic). `View` runs against a
  stable, immutable snapshot.
- **Atomic batches.** Group many writes into one all-or-nothing durable record.
- **Ordered range scans.** Iterate keys in ascending order with lower/upper
  bounds and prefix filters via an immutable balanced tree.
- **Compaction.** `Compact` rewrites the log to the live key set, reclaiming
  space, and installs it atomically with a rename.
- **Lock-free concurrent readers.** Readers never block the writer and never
  race, because they read immutable snapshots.

## Quick start

```go
db, err := sled.Open("data.sled")
if err != nil {
    log.Fatal(err)
}
defer db.Close()

// Basic writes (durable + fsync'd by default).
db.Set([]byte("greeting"), []byte("hello"))

v, ok, _ := db.Get([]byte("greeting"))
// v == "hello", ok == true

db.Delete([]byte("greeting"))
```

### Transactions

```go
// Atomic: both writes commit together, or neither does.
err := db.Update(func(tx *sled.Tx) error {
    if err := tx.Set([]byte("a"), []byte("1")); err != nil {
        return err
    }
    return tx.Set([]byte("b"), []byte("2"))
})

// Returning an error rolls the whole transaction back.
_ = db.Update(func(tx *sled.Tx) error {
    tx.Set([]byte("temp"), []byte("x"))
    return errors.New("abort") // "temp" is never persisted
})

// Read-only snapshot; unaffected by concurrent writes.
db.View(func(tx *sled.Tx) error {
    _, _, _ = tx.Get([]byte("a"))
    return nil
})
```

### Batches

```go
err := db.Batch(func(b *sled.Batch) error {
    b.Set([]byte("k1"), []byte("v1"))
    b.Set([]byte("k2"), []byte("v2"))
    b.Delete([]byte("old"))
    return nil
}) // all three land as one atomic, durable record
```

### Ordered scans

```go
// Everything with a prefix, in ascending key order.
it := db.Scan(sled.Range{Prefix: []byte("user:")})
for it.Valid() {
    fmt.Printf("%s = %s\n", it.Key(), it.Value())
    it.Next()
}

// Half-open bounded range [lo, hi).
it = db.Scan(sled.Range{Lower: []byte("b"), Upper: []byte("e")})
```

### Compaction

```go
// Rewrite the log to only the live key set, reclaiming space.
err := db.Compact()
```

## Durability

By default every commit calls `fsync` before returning, so an acknowledged
write survives a power loss or OS crash. For throughput-oriented workloads that
can tolerate losing the most recent commits after a hard crash, disable it:

```go
db, _ := sled.Open("data.sled", sled.WithSyncWrites(false))
```

A normal `Close` always flushes, regardless of this setting.

## Concurrency model & isolation

`sled` is **single-writer, multi-reader**:

- Writers (`Set`, `Delete`, `Batch`, `Update`) are fully serialized by an
  internal mutex — at most one runs at a time.
- Readers (`Get`, `Has`, `Scan`, `View`) take no locks. Each captures the
  current immutable index snapshot and reads from it. Because tree nodes and
  their byte slices are never mutated after publication, readers run
  concurrently with the writer and each other with no data races (verified
  under `go test -race`).

The isolation level is **serializable**. A read/write transaction holds the
writer slot for its whole duration and commits atomically; a read-only
transaction observes a consistent snapshot taken when it begins and is
unaffected by writes committed while it runs.

Returned key/value byte slices are owned by the DB and must not be modified by
the caller. Input keys and values passed to `Set`/`Batch`/`Tx.Set` are copied,
so the caller may reuse their buffers immediately.

## On-disk format

The database is a single append-only log file. Each record is:

```
[uint32 payload length][uint32 CRC-32 of payload][payload]
```

The payload holds one or more operations (Set/Delete with key and, for Set, a
value). A whole batch or transaction is written as a single record, so it is
applied all-or-nothing on recovery. `Open` replays the log from the start,
stops at the first short read or CRC mismatch, and truncates any partial tail.

Keys must be non-empty; values may be empty (a present empty value is
distinguished from an absent key by the boolean returned from `Get`).

## Testing

```
go test -race ./...
```

The test suite covers persistence across `Close`/`Open`, transaction
commit/rollback (including panic rollback and on-disk durability), byte-level
crash recovery (torn tails and CRC corruption), ordered/prefix/bounded scans,
batch atomicity, compaction, and concurrent readers racing a writer.

## Status

Version 0.1.0. Embedded, in-process; not a networked database.

## License

See repository.
