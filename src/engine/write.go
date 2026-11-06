// Write path (docs/SPEC.md section 4): validate, disk-full guard, encode one
// buffer, WriteAt, Sync, update index, return seq. Failure rule: truncate +
// sync, else enter ErrFailed state. PR A2/A3. Every disk-changing syscall
// gets a // CRASH: comment.
package engine

// checkKey enforces the key limits: not empty, at most MaxKeySize bytes.
func checkKey(key []byte) error {
	if len(key) == 0 {
		return ErrEmptyKey
	}
	if len(key) > MaxKeySize {
		return ErrKeyTooLarge
	}
	return nil
}

// Put stores value under key.
func (db *DB) Put(key, value []byte) (uint64, error) { return 0, ErrNotImplemented }

// Delete removes key. Deleting a missing key is not an error.
func (db *DB) Delete(key []byte) (uint64, error) { return 0, ErrNotImplemented }

// Batch applies ops atomically, in listed order.
func (db *DB) Batch(ops []Op) (uint64, error) { return 0, ErrNotImplemented }
