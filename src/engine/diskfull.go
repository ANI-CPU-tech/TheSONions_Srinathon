// Disk-full handling (docs/SPEC.md section 4, step 2 and the failure rule):
// the max_bytes guard with DeleteReserve, and what happens after a failed
// write.
package engine

import "fmt"

// checkRoom is the disk-full guard: the record must fit under MaxBytes.
// Puts and batches must also leave DeleteReserve free, so deletes can still
// be written when the file is full. MaxBytes 0 means unlimited. It runs
// before WriteAt, so ErrDiskFull means nothing was written.
// shortcut: no compaction retry yet; PR A4 adds "compact once, recheck"
// when there are dead bytes.
func (db *DB) checkRoom(recLen int64, isDelete bool) error {
	if db.opts.MaxBytes == 0 {
		return nil
	}
	limit := db.opts.MaxBytes
	if !isDelete {
		limit -= DeleteReserve
	}
	if db.size+recLen > limit {
		return ErrDiskFull
	}
	return nil
}

// fail poisons the DB after a write error: the file may now end in unknown
// bytes, and appending after them would hide every later record from
// recovery. All later writes return ErrFailed until the DB is reopened.
// shortcut: no truncate-and-continue attempt yet; the next commit adds it.
func (db *DB) fail(err error) error {
	db.failed = true
	return fmt.Errorf("%w: %v", ErrFailed, err)
}
