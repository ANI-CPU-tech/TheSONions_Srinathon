// Disk-full handling (docs/SPEC.md section 4, step 2 and the failure rule):
// the max_bytes guard with DeleteReserve, and the truncate-or-poison
// recovery after a failed write.
package engine

import (
	"errors"
	"fmt"
	"syscall"
)

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

// fail handles an error from WriteAt or Sync. The file may now end in a
// partial or unsynced record past db.size. Cut it off and make the cut
// durable; then the write simply did not happen and the DB stays usable.
// If even that fails, poison the DB: appending after unknown bytes would
// hide every later record from recovery.
func (db *DB) fail(err error) error {
	// CRASH: before Truncate: the file may end in a partial record past
	// db.size. It was never acked; open() truncates it as a torn tail (or,
	// if it is complete, applies it: allowed, the op was in flight).
	if terr := db.f.Truncate(db.size); terr == nil {
		// CRASH: after Truncate, before Sync: same as above; open() ends
		// at db.size or truncates there again.
		if serr := db.f.Sync(); serr == nil {
			return writeError(err)
		}
	}
	db.failed = true
	return fmt.Errorf("%w: %v", ErrFailed, err)
}

// writeError reports a write that failed cleanly (nothing written). Out of
// space on the device (ENOSPC) or over the process file-size limit (EFBIG)
// is reported as ErrDiskFull, like our own max_bytes guard.
func writeError(err error) error {
	if errors.Is(err, syscall.ENOSPC) || errors.Is(err, syscall.EFBIG) {
		return fmt.Errorf("%w: %v", ErrDiskFull, err)
	}
	return fmt.Errorf("ninelives: write failed, nothing written: %w", err)
}
