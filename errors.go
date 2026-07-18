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

	// ErrEmptyTreeName is returned by OpenTree when given an empty tree name.
	ErrEmptyTreeName = errors.New("sled: tree name must be non-empty")

	// ErrDropDefaultTree is returned by DropTree when asked to drop the default
	// tree, which always exists and cannot be removed.
	ErrDropDefaultTree = errors.New("sled: cannot drop the default tree")

	// ErrNoMergeOperator is returned by Merge when no merge operator has been
	// installed on the tree with SetMergeOperator.
	ErrNoMergeOperator = errors.New("sled: no merge operator set")

	// ErrNilFunc is returned when a nil callback is passed to an operation that
	// requires one, such as FetchAndUpdate.
	ErrNilFunc = errors.New("sled: callback function must be non-nil")

	// ErrCorruptImport is returned by Import when the stream's magic or checksum
	// does not verify, or its framing is invalid. Nothing is applied.
	ErrCorruptImport = errors.New("sled: corrupt or truncated import stream")
)
