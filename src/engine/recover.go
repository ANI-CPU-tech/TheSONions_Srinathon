// Recovery run by Open (docs/SPEC.md section 6): remove leftover temp files,
// validate header, scan records, truncate torn tail + sync, rebuild index.
// Must stay under 2 s at 1M keys (benchmark in PR A5).
package engine

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
)

// scanBufSize is the read buffer for the recovery scan (SPEC: at least 1 MiB).
const scanBufSize = 1 << 20

// applyFunc receives one committed record. off is the record's file offset.
// payload is reused for the next record, so applyFunc must not keep it.
type applyFunc func(h RecordHeader, payload []byte, off int64) error

// removeLeftovers deletes <path>.compact and <path>.init (SPEC section 6
// step 1). Neither is ever authoritative, so deleting them loses nothing.
func removeLeftovers(path string) error {
	// CRASH: during either remove: the temp file is still there or gone.
	// Both are harmless; the next open() simply removes it again.
	if err := removeIfExists(compactPath(path)); err != nil {
		return err
	}
	return removeIfExists(initPath(path))
}

// readHeader reads and validates the 24-byte file header of f.
func readHeader(f *os.File) (seqBase uint64, err error) {
	b := make([]byte, FileHeaderSize)
	n, err := f.ReadAt(b, 0)
	if err != nil && !errors.Is(err, io.EOF) {
		return 0, err
	}
	return DecodeFileHeader(b[:n])
}

// scanLog reads records from offset FileHeaderSize to fileSize and calls
// apply for each committed one, in file order. It stops without error at
// the first torn record and returns validEnd, the offset just past the last
// committed record. A good CRC around a malformed payload is ErrCorrupt.
func scanLog(f *os.File, fileSize int64, apply applyFunc) (validEnd int64, err error) {
	r := bufio.NewReaderSize(io.NewSectionReader(f, FileHeaderSize, fileSize-FileHeaderSize), scanBufSize)
	var hdr [RecordHeaderSize]byte
	var payload []byte
	off := int64(FileHeaderSize)
	for {
		if fileSize-off < RecordHeaderSize {
			return off, nil // clean end, or a torn header
		}
		if _, err := io.ReadFull(r, hdr[:]); err != nil {
			return off, err
		}
		h, err := DecodeRecordHeader(hdr[:])
		if err != nil || int64(h.Length) > fileSize-off-RecordHeaderSize {
			return off, nil // Length out of range or runs past EOF: torn
		}
		if cap(payload) < int(h.Length) {
			payload = make([]byte, h.Length)
		}
		payload = payload[:h.Length]
		if _, err := io.ReadFull(r, payload); err != nil {
			return off, err
		}
		if VerifyRecord(h, payload) != nil {
			return off, nil // CRC mismatch: torn
		}
		if err := apply(h, payload, off); err != nil {
			return off, fmt.Errorf("record at offset %d: %w", off, err)
		}
		off += RecordHeaderSize + int64(h.Length)
	}
}

// recoverLog validates the header, rebuilds the index from the log and
// cuts off a torn tail. On return db.size and db.nextSeq are set.
func (db *DB) recoverLog() error {
	seqBase, err := readHeader(db.f)
	if err != nil {
		return err
	}
	fi, err := db.f.Stat()
	if err != nil {
		return err
	}
	db.nextSeq = seqBase
	validEnd, err := scanLog(db.f, fi.Size(), db.applyRecord)
	if err != nil {
		return err
	}
	if validEnd < fi.Size() {
		if err := truncateTail(db.f, validEnd); err != nil {
			return err
		}
	}
	db.size = validEnd
	return nil
}

// truncateTail cuts the torn bytes after validEnd and makes the cut durable.
// The torn record was never acked: its fsync had not returned when we died.
func truncateTail(f *os.File, validEnd int64) error {
	// CRASH: before Truncate: the torn tail is still on disk. Nothing was
	// applied from it; the next open() scans, stops at the same offset and
	// truncates again (idempotent).
	if err := f.Truncate(validEnd); err != nil {
		return err
	}
	// CRASH: after Truncate, before Sync: the cut may not be durable yet.
	// Either way the next open() sees a clean end or the same torn tail.
	return f.Sync()
}

// applyRecord puts one committed record into the index. Recovery and the
// write path both use it, so both build exactly the same index.
func (db *DB) applyRecord(h RecordHeader, payload []byte, off int64) error {
	valBase := off + RecordHeaderSize
	err := WalkPayload(h.Type, payload, func(kind OpKind, key []byte, valOff, valLen int) error {
		if kind == OpDel {
			db.idx.del(key)
			return nil
		}
		cost := uint32(batchOpOverhead + len(key) + valLen) // a put inside a batch
		if h.Type == RecPut {
			cost = RecordHeaderSize + h.Length // the whole record
		}
		db.idx.put(key, entry{off: valBase + int64(valOff), size: uint32(valLen), seq: h.Seq, cost: cost})
		return nil
	})
	if err != nil {
		return err
	}
	if h.Seq >= db.nextSeq {
		db.nextSeq = h.Seq + 1 // nextSeq = max(SeqBase, maxRecordSeq + 1)
	}
	return nil
}

// Check verifies the file at path without modifying it. nil means OK.
func Check(path string) error { return ErrNotImplemented }
