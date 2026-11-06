// Compaction (docs/SPEC.md section 7): live keys to <path>.compact, fsync,
// rename over <path>, fsync directory, swap handles. Auto-trigger by dead-byte
// ratio.
package engine

import (
	"bufio"
	"fmt"
	"os"
)

// Compact rewrites live keys into a new file and swaps it in.
func (db *DB) Compact() error {
	db.mu.Lock()
	defer db.mu.Unlock()
	if err := db.writable(); err != nil {
		return err
	}
	return db.compactLocked()
}

// maybeCompact is the automatic trigger, run after each successful write:
// compact when the file is at least CompactMinFile and more than
// CompactDeadRatio of it is dead. The write is already durable, so a failed
// compaction is not that write's error: the old file stays in use, or, if
// compaction put the DB in the failed state, the next write says so.
// shortcut: no back-off, so if compaction keeps failing (E4 on a file over
// 4 MiB) every write retries it; add a back-off if torture shows this.
func (db *DB) maybeCompact() {
	if db.size >= CompactMinFile && float64(db.deadBytes()) > CompactDeadRatio*float64(db.size) {
		_ = db.compactLocked() // see above for why the error is dropped
	}
}

// compactLocked runs the whole compaction. Caller holds the write lock.
// Until the rename the old file is untouched and authoritative; after it
// the new file is complete because it was fsynced first.
func (db *DB) compactLocked() error {
	tmp := compactPath(db.path)
	f, idx, size, err := db.writeCompactFile(tmp)
	if err != nil {
		// CRASH: during cleanup: a partial <path>.compact may remain. It is
		// never authoritative; open() deletes it (C3).
		removeIfExists(tmp)
		return err
	}
	// CRASH: before rename: <path> is the old, complete file and <path>.compact
	// a complete, fsynced copy. open() deletes the copy and uses the old (C3).
	if err := os.Rename(tmp, db.path); err != nil {
		f.Close()
		removeIfExists(tmp)
		return err
	}
	// From here <path> is the new file. Switch to it now, whatever happens
	// next: every later write must go to the file that open() will read.
	old := db.f
	db.f, db.idx, db.size = f, idx, size
	old.Close()
	// CRASH: after rename, before dir fsync: <path> is the new file, complete
	// because it was fsynced before the rename (C4, C5). A process kill keeps
	// the rename; only power loss could undo it, which this fsync prevents.
	if err := syncDir(db.path); err != nil {
		db.failed = true // the rename may not be durable; stop writing
		return fmt.Errorf("%w: directory fsync after compaction: %v", ErrFailed, err)
	}
	return nil
}

// writeCompactFile writes header + one PUT record per live key (with the
// key's original seq) to tmp, fsyncs it and returns the open file, its new
// index and its size. It returns ErrDiskFull if the new file would not fit
// under MaxBytes (E4); the caller then removes tmp.
func (db *DB) writeCompactFile(tmp string) (*os.File, index, int64, error) {
	// CRASH: from create until the Sync below: <path>.compact is partial.
	// <path> is untouched; open() deletes the temp file (C3).
	f, err := os.OpenFile(tmp, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return nil, index{}, 0, err
	}
	idx, size, err := db.copyLive(f)
	if err == nil {
		err = f.Sync()
	}
	if err != nil {
		f.Close()
		return nil, index{}, 0, err
	}
	return f, idx, size, nil
}

// copyLive streams the header and every live key into f through a 1 MiB
// buffer. SeqBase is nextSeq, so seqs never go backwards even if the newest
// records were deletes that compaction drops.
func (db *DB) copyLive(f *os.File) (index, int64, error) {
	w := bufio.NewWriterSize(f, 1<<20)
	if _, err := w.Write(EncodeFileHeader(db.nextSeq)); err != nil {
		return index{}, 0, err
	}
	idx := newIndex()
	size := int64(FileHeaderSize)
	for key, e := range db.idx.m {
		v, err := db.readValue(e)
		if err != nil {
			return index{}, 0, err
		}
		rec := EncodePut(e.seq, []byte(key), v)
		if db.opts.MaxBytes > 0 && size+int64(len(rec)) > db.opts.MaxBytes {
			return index{}, 0, ErrDiskFull // E4: old file stays as it is
		}
		if _, err := w.Write(rec); err != nil {
			return index{}, 0, err
		}
		valOff := size + int64(PutValueOffset(len(key)))
		idx.put([]byte(key), entry{off: valOff, size: e.size, seq: e.seq, cost: uint32(len(rec))})
		size += int64(len(rec))
	}
	return idx, size, w.Flush()
}
