# Changelog

All notable changes to this project are documented here. This project adheres
to semantic versioning.

## 0.3.0

Expanded the API toward closer parity with the Rust `sled` crate. All additions
are pure standard library, deterministic, and covered by known-answer tests.

### Added

- **Pop operations.** `Tree.PopMin` / `Tree.PopMax` (and the `DB` shortcuts)
  atomically remove and return the smallest or largest entry, mirroring sled's
  `pop_min` / `pop_max`.
- **Prefix scans.** `Tree.ScanPrefix` / `DB.ScanPrefix` iterate every key under
  a prefix, and the `PrefixRange` helper builds a prefix `Range` for composing
  with other options (e.g. reverse order), mirroring `scan_prefix`.
- **Value-returning writes.** `Tree.GetAndSet` returns the previous value (like
  sled's `insert`) and `Tree.GetAndDelete` returns the removed value (like
  `remove`), with `DB` shortcuts.
- **`Tree.UpdateAndFetch`** returns the new value after an atomic update,
  complementing the existing `FetchAndUpdate` (which returns the old value);
  together they mirror `update_and_fetch` / `fetch_and_update`.
- **Error-reporting compare-and-swap.** `Tree.CompareAndSwapErr` and the
  `CompareAndSwapError` type report the conflicting current/proposed values on
  failure, mirroring sled's `compare_and_swap`.
- **Checksums.** `Tree.Checksum` and `DB.Checksum` compute a deterministic,
  order-independent CRC-32 digest of live contents, mirroring `Tree::checksum`.
- **Introspection.** `DB.SizeOnDisk` reports the log's on-disk footprint
  (`size_on_disk`) and `DB.WasRecovered` reports whether an existing database
  was recovered at open time (`was_recovered`).
- **Config builder.** `Config` with `DefaultConfig`, `Path`, `SyncWrites`,
  `Temporary`, `FileMode`, and `Open`, mirroring sled's `Config`. Added the
  `WithTemporary` option and temporary-database semantics (backing files are
  removed on a clean `Close`).
- **Tree-scoped batches.** `Tree.ApplyBatch` / `Tree.Batch` apply a batch to a
  named tree, mirroring `Tree::apply_batch`.
- **Blocking subscriber consumption.** `Subscriber.Next`, `Subscriber.TryNext`,
  and `Subscriber.Drain` complement the existing `Events` channel.

## 0.2.0

- Named trees, transactions across trees, batches, ordered scans, compaction,
  export/import, merge operators, subscribers, and monotonic ID generation.
