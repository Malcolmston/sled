package sled

import "os"

// options holds the resolved configuration for a database. It is populated from
// the defaults and then mutated by any Option values passed to Open.
type options struct {
	syncWrites bool
	fileMode   os.FileMode
}

func defaultOptions() options {
	return options{
		syncWrites: true,
		fileMode:   0o644,
	}
}

// An Option configures a database at Open time.
type Option func(*options)

// WithSyncWrites controls whether each durable commit calls fsync before
// returning. It is enabled by default, which guarantees that a committed write
// survives a crash. Disabling it improves write throughput at the cost of
// possibly losing the most recent commits after a power failure or OS crash
// (a normal process exit still flushes on Close).
func WithSyncWrites(sync bool) Option {
	return func(o *options) { o.syncWrites = sync }
}

// WithFileMode sets the permission bits used when sled creates the log file.
// The default is 0o644.
func WithFileMode(mode os.FileMode) Option {
	return func(o *options) { o.fileMode = mode }
}
