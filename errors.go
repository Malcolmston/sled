package sled

import "errors"

// Sentinel errors returned by the store and its transactions.
var (
	// ErrClosed is returned when an operation is attempted on a database that
	// has already been closed.
	ErrClosed = errors.New("sled: database is closed")

	// ErrEmptyKey is returned when a write is attempted with a nil or
	// zero-length key. Keys must be non-empty.
	ErrEmptyKey = errors.New("sled: key must be non-empty")

	// ErrTxClosed is returned when a transaction handle is used after the
	// transaction function has returned.
	ErrTxClosed = errors.New("sled: transaction is closed")

	// ErrTxNotWritable is returned when a mutating operation is attempted on a
	// read-only transaction created by View.
	ErrTxNotWritable = errors.New("sled: transaction is not writable")
)
