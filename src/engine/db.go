package engine

import (
	"errors"
	"io/fs"
	"os"
	"slices"
	"sync"
	"time"
)

// DB is the NineLives database handle. One global RWMutex: writers take the
// write side for the whole validate-write-fsync-index sequence, readers the
// read side, so a reader never sees half of a batch.
type DB struct {
	mu   sync.RWMutex
	path string
	opts Options
	f    *os.File

	idx     index
	size    int64  // end of the last committed record = next write offset
	nextSeq uint64 // seq for the next record

	recoveryTime time.Duration
	failed       bool // a write failed after touching the file; reopen required
	closed       bool
}

var _ Store = (*DB)(nil)

// Open opens or creates the database at path and runs recovery.
func Open(path string, opts Options) (*DB, error) {
	start := time.Now()
	if err := removeLeftovers(path); err != nil {
		return nil, err
	}
	if _, err := os.Stat(path); errors.Is(err, fs.ErrNotExist) {
		if err := createFile(path); err != nil {
			return nil, err
		}
	} else if err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		return nil, err
	}
	db := &DB{path: path, opts: opts, f: f, idx: newIndex()}
	if err := db.recoverLog(); err != nil {
		f.Close()
		return nil, err
	}
	db.recoveryTime = time.Since(start)
	return db, nil
}

// Get returns the current value of key.
func (db *DB) Get(key []byte) ([]byte, bool, error) {
	if err := checkKey(key); err != nil {
		return nil, false, err
	}
	db.mu.RLock()
	defer db.mu.RUnlock()
	if db.closed {
		return nil, false, ErrClosed
	}
	e, ok := db.idx.m[string(key)]
	if !ok {
		return nil, false, nil
	}
	v, err := db.readValue(e)
	return v, err == nil, err
}

// readValue reads one value from the file. Caller holds the lock.
func (db *DB) readValue(e entry) ([]byte, error) {
	v := make([]byte, e.size)
	if _, err := db.f.ReadAt(v, e.off); err != nil {
		return nil, err
	}
	return v, nil
}

// ForEachSorted calls fn for every live key in ascending byte order. It
// holds the read lock throughout, so fn must not call back into db.
func (db *DB) ForEachSorted(fn func(key, value []byte) error) error {
	db.mu.RLock()
	defer db.mu.RUnlock()
	if db.closed {
		return ErrClosed
	}
	keys := make([]string, 0, len(db.idx.m))
	for k := range db.idx.m {
		keys = append(keys, k)
	}
	slices.Sort(keys) // Go string order is raw byte order
	for _, k := range keys {
		v, err := db.readValue(db.idx.m[k])
		if err != nil {
			return err
		}
		if err := fn([]byte(k), v); err != nil {
			return err
		}
	}
	return nil
}

// Stats returns a snapshot of the database state.
func (db *DB) Stats() Stats {
	db.mu.RLock()
	defer db.mu.RUnlock()
	return Stats{
		Keys:         len(db.idx.m),
		FileBytes:    db.size,
		DeadBytes:    db.deadBytes(),
		RecoveryTime: db.recoveryTime,
		NextSeq:      db.nextSeq,
	}
}

// deadBytes is the part of the records area no live key needs: old values,
// deletes and batch framing. Compaction reclaims it. Caller holds the lock.
func (db *DB) deadBytes() int64 { return db.size - FileHeaderSize - db.idx.live }

// Close closes the file. Every acked write is already fsynced, so there is
// nothing to flush. Later calls return ErrClosed.
func (db *DB) Close() error {
	db.mu.Lock()
	defer db.mu.Unlock()
	if db.closed {
		return ErrClosed
	}
	db.closed = true
	return db.f.Close()
}
