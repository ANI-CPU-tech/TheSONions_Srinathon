package engine

// DB is the NineLives database handle.
// Skeleton only: the real implementation lands in PR A2.
type DB struct{}

var _ Store = (*DB)(nil)

// Open opens or creates the database at path and runs recovery.
func Open(path string, opts Options) (*DB, error) { return nil, ErrNotImplemented }

// Check verifies the file at path without modifying it. nil means OK.
func Check(path string) error { return ErrNotImplemented }

// Put stores value under key.
func (db *DB) Put(key, value []byte) (uint64, error) { return 0, ErrNotImplemented }

// Get returns the current value of key.
func (db *DB) Get(key []byte) ([]byte, bool, error) { return nil, false, ErrNotImplemented }

// Delete removes key. Deleting a missing key is not an error.
func (db *DB) Delete(key []byte) (uint64, error) { return 0, ErrNotImplemented }

// Batch applies ops atomically, in listed order.
func (db *DB) Batch(ops []Op) (uint64, error) { return 0, ErrNotImplemented }

// Compact rewrites live keys into a new file and swaps it in.
func (db *DB) Compact() error { return ErrNotImplemented }

// ForEachSorted calls fn for every live key in ascending byte order.
func (db *DB) ForEachSorted(fn func(key, value []byte) error) error { return ErrNotImplemented }

// Stats returns a snapshot of the database state.
func (db *DB) Stats() Stats { return Stats{} }

// Close flushes and closes the database.
func (db *DB) Close() error { return ErrNotImplemented }
