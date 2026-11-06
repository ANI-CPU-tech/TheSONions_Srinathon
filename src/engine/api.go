// Package engine is the NineLives storage engine: an append-only log with
// an in-memory hash index. See docs/SPEC.md.
//
// FROZEN CONTRACT: this file changes only through a contract/* PR approved
// by all three teammates.
package engine

import (
	"errors"
	"time"
)

// On-disk format and limits (docs/SPEC.md sections 2 and 3).
const (
	FileMagic        = "NLV1"
	FormatVersion    = 1
	FileHeaderSize   = 24
	RecordHeaderSize = 17
	MaxKeySize       = 1024
	MaxValueSize     = 1 << 20 // 1 MiB
	MaxBatchOps      = 10000
	MaxBatchPayload  = 64 << 20 // 64 MiB total payload per batch (documented limit)
	DeleteReserve    = 64 << 10 // bytes kept free for deletes under max_bytes
	CompactMinFile   = 4 << 20  // auto-compaction only for files of at least 4 MiB
	CompactDeadRatio = 0.5      // auto-compact when dead bytes > 50% of file
)

// OpKind is the kind of one operation inside a batch.
type OpKind uint8

// Batch operation kinds. Values match the on-disk op byte.
const (
	OpPut OpKind = 1
	OpDel OpKind = 2
)

// Op is one put or delete inside a batch. Value is ignored for OpDel.
type Op struct {
	Kind       OpKind
	Key, Value []byte
}

// Options configures Open.
type Options struct {
	MaxBytes int64 // 0 = unlimited
}

// Stats is a snapshot of the database state.
type Stats struct {
	Keys                 int
	FileBytes, DeadBytes int64
	RecoveryTime         time.Duration
	NextSeq              uint64
}

// Errors returned by the engine.
var (
	ErrDiskFull       = errors.New("ninelives: disk full (max_bytes reached)")
	ErrKeyTooLarge    = errors.New("ninelives: key larger than 1024 bytes")
	ErrEmptyKey       = errors.New("ninelives: empty key")
	ErrValueTooLarge  = errors.New("ninelives: value larger than 1 MiB")
	ErrBatchTooLarge  = errors.New("ninelives: batch exceeds 10000 ops or 64 MiB payload")
	ErrFailed         = errors.New("ninelives: database in failed state, reopen required")
	ErrClosed         = errors.New("ninelives: database closed")
	ErrNotNineLives   = errors.New("ninelives: not a NineLives file (bad header)")
	ErrCorrupt        = errors.New("ninelives: corrupt file")
	ErrNotImplemented = errors.New("ninelives: not implemented")
)

// Store is the engine API that the CLI and serve code against.
type Store interface {
	Put(key, value []byte) (seq uint64, err error)
	Get(key []byte) (value []byte, found bool, err error)
	Delete(key []byte) (seq uint64, err error)
	Batch(ops []Op) (seq uint64, err error)
	Compact() error
	ForEachSorted(fn func(key, value []byte) error) error // for dump
	Stats() Stats
	Close() error
}

// Implemented in db.go (owned by Anirudh):
//   func Open(path string, opts Options) (*DB, error)
//   func Check(path string) error   // read-only integrity verification, nil = OK
//   var _ Store = (*DB)(nil)
