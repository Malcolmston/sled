package sled

import "os"

// Config is a builder for opening a database with explicit settings, mirroring
// sled's Config. It offers the same knobs as the functional [Option] values in
// a chainable form, which some callers find clearer:
//
//	db, err := sled.DefaultConfig().
//		Path("data.sled").
//		SyncWrites(false).
//		Temporary(true).
//		Open()
//
// A Config is not safe for concurrent use while being built. Construct it,
// chain the setters, then call [Config.Open]. The zero Config is not valid;
// obtain one from [DefaultConfig].
type Config struct {
	path string
	opts options
}

// DefaultConfig returns a Config populated with sled's defaults: synchronous
// (fsync'd) writes, file mode 0o644, and no path set. Call the setter methods to
// customize it and [Config.Open] to open the database.
func DefaultConfig() *Config {
	return &Config{opts: defaultOptions()}
}

// Path sets the filesystem path of the database's write-ahead log. It must be
// set (non-empty) before [Config.Open] is called. Path returns the receiver so
// calls can be chained.
func (c *Config) Path(path string) *Config {
	c.path = path
	return c
}

// SyncWrites controls whether each durable commit calls fsync before returning.
// See [WithSyncWrites]. It returns the receiver so calls can be chained.
func (c *Config) SyncWrites(sync bool) *Config {
	c.opts.syncWrites = sync
	return c
}

// Temporary marks the database temporary, deleting its backing files on a clean
// Close. See [WithTemporary]. It returns the receiver so calls can be chained.
func (c *Config) Temporary(temporary bool) *Config {
	c.opts.temporary = temporary
	return c
}

// FileMode sets the permission bits used when sled creates the log file. See
// [WithFileMode]. It returns the receiver so calls can be chained.
func (c *Config) FileMode(mode os.FileMode) *Config {
	c.opts.fileMode = mode
	return c
}

// Open opens the database described by the Config. It is equivalent to calling
// [Open] with the corresponding [Option] values. Open returns [ErrNoPath] if no
// path was set.
func (c *Config) Open() (*DB, error) {
	if c.path == "" {
		return nil, ErrNoPath
	}
	resolved := c.opts
	return Open(c.path, func(o *options) { *o = resolved })
}
