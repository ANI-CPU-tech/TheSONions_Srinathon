// Write path (docs/SPEC.md section 4): validate, disk-full guard, encode one
// buffer, WriteAt, Sync, update index, return seq. Failure rule: truncate +
// sync, else enter ErrFailed state (diskfull.go). Every disk-changing syscall
// gets a // CRASH: comment.
package engine

import "errors"

// errBadOpKind rejects a batch op that is neither OpPut nor OpDel.
var errBadOpKind = errors.New("ninelives: batch op kind must be put or del")

// Put stores value under key. It returns only after the record is fsynced.
func (db *DB) Put(key, value []byte) (uint64, error) {
	db.mu.Lock()
	defer db.mu.Unlock()
	if err := checkKey(key); err != nil {
		return 0, err
	}
	if err := checkValue(value); err != nil {
		return 0, err
	}
	if err := db.writable(); err != nil {
		return 0, err
	}
	return db.commit(EncodePut(db.nextSeq, key, value), false)
}

// Delete removes key. Deleting a missing key is not an error; it still
// writes a DEL record, so every acked delete has its own seq.
func (db *DB) Delete(key []byte) (uint64, error) {
	db.mu.Lock()
	defer db.mu.Unlock()
	if err := checkKey(key); err != nil {
		return 0, err
	}
	if err := db.writable(); err != nil {
		return 0, err
	}
	return db.commit(EncodeDel(db.nextSeq, key), true)
}

// Batch applies ops atomically, in listed order: one record, one CRC.
// An empty batch writes nothing and returns seq 0.
func (db *DB) Batch(ops []Op) (uint64, error) {
	db.mu.Lock()
	defer db.mu.Unlock()
	if err := checkBatch(ops); err != nil {
		return 0, err
	}
	if err := db.writable(); err != nil {
		return 0, err
	}
	if len(ops) == 0 {
		return 0, nil
	}
	return db.commit(EncodeBatch(db.nextSeq, ops), false)
}

// commit runs the disk-full guard, then appends one encoded record at the
// end of the log, fsyncs it and applies it to the index (SPEC section 4,
// steps 2 and 4 to 6). Encoding first is safe: it touches no disk. Caller
// holds the write lock, so nobody reads or writes until the index is updated.
func (db *DB) commit(rec []byte, isDelete bool) (uint64, error) {
	if err := db.checkRoom(int64(len(rec)), isDelete); err != nil {
		return 0, err
	}
	h, err := DecodeRecordHeader(rec) // the fields we just encoded
	if err != nil {
		return 0, err
	}
	off := db.size
	// CRASH: before WriteAt: no byte of rec is on disk. open() ends at the
	// old size; the op was never acked and is simply absent.
	if _, err := db.f.WriteAt(rec, off); err != nil {
		return 0, db.fail(err)
	}
	// CRASH: after WriteAt, before Sync: rec (or, if killed mid-write, a
	// prefix of it) is in the page cache, which a process kill does not
	// lose. open() applies it if complete with a good CRC (allowed: the op
	// was in flight, not acked), otherwise truncates it as a torn tail.
	if err := db.f.Sync(); err != nil {
		return 0, db.fail(err)
	}
	// After Sync rec is durable but not yet acked; open() applies it, which
	// is allowed because the op was in flight. No disk changes from here on.
	if err := db.applyRecord(h, rec[RecordHeaderSize:], off); err != nil {
		return 0, db.fail(err)
	}
	db.size += int64(len(rec))
	db.maybeCompact()
	return h.Seq, nil
}

// writable reports why the DB cannot take writes, or nil.
func (db *DB) writable() error {
	if db.closed {
		return ErrClosed
	}
	if db.failed {
		return ErrFailed
	}
	return nil
}

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

// checkValue enforces the value limit; an empty value is allowed.
func checkValue(value []byte) error {
	if len(value) > MaxValueSize {
		return ErrValueTooLarge
	}
	return nil
}

// checkBatch validates every op and the batch limits: at most MaxBatchOps
// ops and MaxBatchPayload bytes of keys plus values.
func checkBatch(ops []Op) error {
	if len(ops) > MaxBatchOps {
		return ErrBatchTooLarge
	}
	total := 0
	for _, op := range ops {
		if err := checkKey(op.Key); err != nil {
			return err
		}
		switch op.Kind {
		case OpPut:
			if err := checkValue(op.Value); err != nil {
				return err
			}
			total += len(op.Key) + len(op.Value)
		case OpDel:
			total += len(op.Key)
		default:
			return errBadOpKind
		}
	}
	if total > MaxBatchPayload {
		return ErrBatchTooLarge
	}
	return nil
}
