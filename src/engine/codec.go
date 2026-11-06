// Header and record encoding/decoding with CRC32-C (docs/SPEC.md section 3).
// All integers are little-endian. This file only builds and parses bytes;
// it never touches the disk, so it has no // CRASH: points.
package engine

import (
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
)

// Record types: the Type byte at record offset 8.
const (
	RecPut   byte = 0x01
	RecDel   byte = 0x02
	RecBatch byte = 0x03
)

// batchOpOverhead is the framing per batch op: op u8 + key_len u16 + val_len u32.
const batchOpOverhead = 1 + 2 + 4

// MaxRecordPayload is the largest Length a real record can have: a full batch
// (MaxBatchPayload bytes of keys and values) plus its count and per-op framing.
// The write path must keep batches inside MaxBatchPayload (counting key and
// value bytes); recovery treats any larger Length as a torn tail.
const MaxRecordPayload = MaxBatchPayload + 4 + MaxBatchOps*batchOpOverhead

// ErrTornRecord means the bytes at this offset are not a complete, committed
// record: too short, Length out of range, or CRC mismatch. Recovery stops
// here and truncates; Check reports it as corruption.
var ErrTornRecord = errors.New("ninelives: torn record")

// castagnoli is the CRC32-C table used for every checksum in the file.
var castagnoli = crc32.MakeTable(crc32.Castagnoli)

var le = binary.LittleEndian

// EncodeFileHeader returns the 24-byte file header for a file whose first
// record will get sequence number seqBase or higher.
func EncodeFileHeader(seqBase uint64) []byte {
	b := make([]byte, FileHeaderSize) // flags (6:8) and reserved (16:20) stay 0
	copy(b[0:4], FileMagic)
	le.PutUint16(b[4:6], FormatVersion)
	le.PutUint64(b[8:16], seqBase)
	le.PutUint32(b[20:24], crc32.Checksum(b[0:20], castagnoli))
	return b
}

// DecodeFileHeader validates magic, CRC and version and returns SeqBase.
// Any problem returns an error wrapping ErrNotNineLives.
func DecodeFileHeader(b []byte) (seqBase uint64, err error) {
	if len(b) < FileHeaderSize {
		return 0, fmt.Errorf("%w: header is %d bytes, want %d", ErrNotNineLives, len(b), FileHeaderSize)
	}
	if string(b[0:4]) != FileMagic {
		return 0, fmt.Errorf("%w: bad magic %q", ErrNotNineLives, b[0:4])
	}
	if crc32.Checksum(b[0:20], castagnoli) != le.Uint32(b[20:24]) {
		return 0, fmt.Errorf("%w: header CRC mismatch", ErrNotNineLives)
	}
	if v := le.Uint16(b[4:6]); v != FormatVersion {
		return 0, fmt.Errorf("%w: unsupported version %d", ErrNotNineLives, v)
	}
	return le.Uint64(b[8:16]), nil
}

// RecordHeader is the parsed 17-byte record header.
type RecordHeader struct {
	CRC    uint32 // CRC32-C over bytes 4 to the end of the payload
	Length uint32 // payload length in bytes
	Type   byte   // RecPut, RecDel or RecBatch
	Seq    uint64
}

// EncodePut returns one complete PUT record. The caller has already checked
// key and value sizes (write path step 1).
func EncodePut(seq uint64, key, value []byte) []byte {
	buf := make([]byte, RecordHeaderSize, RecordHeaderSize+2+len(key)+4+len(value))
	buf = appendValue(appendKey(buf, key), value)
	return sealRecord(buf, RecPut, seq)
}

// EncodeDel returns one complete DEL record.
func EncodeDel(seq uint64, key []byte) []byte {
	buf := make([]byte, RecordHeaderSize, RecordHeaderSize+2+len(key))
	buf = appendKey(buf, key)
	return sealRecord(buf, RecDel, seq)
}

// EncodeBatch returns one complete BATCH record holding every op, in order.
// One record means one CRC, which is what makes the batch atomic.
func EncodeBatch(seq uint64, ops []Op) []byte {
	size := RecordHeaderSize + 4
	for _, op := range ops {
		size += 1 + 2 + len(op.Key)
		if op.Kind == OpPut {
			size += 4 + len(op.Value)
		}
	}
	buf := make([]byte, RecordHeaderSize, size)
	buf = le.AppendUint32(buf, uint32(len(ops)))
	for _, op := range ops {
		buf = appendKey(append(buf, byte(op.Kind)), op.Key)
		if op.Kind == OpPut {
			buf = appendValue(buf, op.Value)
		}
	}
	return sealRecord(buf, RecBatch, seq)
}

func appendKey(b, key []byte) []byte {
	return append(le.AppendUint16(b, uint16(len(key))), key...)
}

func appendValue(b, value []byte) []byte {
	return append(le.AppendUint32(b, uint32(len(value))), value...)
}

// sealRecord fills the header of buf (payload already appended after the
// first 17 bytes) and computes the CRC last, over bytes 4 to the end.
func sealRecord(buf []byte, typ byte, seq uint64) []byte {
	le.PutUint32(buf[4:8], uint32(len(buf)-RecordHeaderSize))
	buf[8] = typ
	le.PutUint64(buf[9:17], seq)
	le.PutUint32(buf[0:4], crc32.Checksum(buf[4:], castagnoli))
	return buf
}

// DecodeRecordHeader parses the 17 bytes at the start of b. It returns
// ErrTornRecord if b is shorter than a header or Length is out of range.
// It does not check the CRC; call VerifyRecord once the payload is read.
func DecodeRecordHeader(b []byte) (RecordHeader, error) {
	if len(b) < RecordHeaderSize {
		return RecordHeader{}, fmt.Errorf("%w: %d header bytes, want %d", ErrTornRecord, len(b), RecordHeaderSize)
	}
	h := RecordHeader{
		CRC:    le.Uint32(b[0:4]),
		Length: le.Uint32(b[4:8]),
		Type:   b[8],
		Seq:    le.Uint64(b[9:17]),
	}
	if h.Length > MaxRecordPayload {
		return RecordHeader{}, fmt.Errorf("%w: length %d over limit %d", ErrTornRecord, h.Length, MaxRecordPayload)
	}
	return h, nil
}

// VerifyRecord checks that payload is complete and that the CRC matches.
// Only a record that passes this exists (the CRC is the commit marker).
func VerifyRecord(h RecordHeader, payload []byte) error {
	if uint32(len(payload)) != h.Length {
		return fmt.Errorf("%w: %d payload bytes, want %d", ErrTornRecord, len(payload), h.Length)
	}
	var hdr [RecordHeaderSize - 4]byte // Length, Type, Seq as they sit on disk
	le.PutUint32(hdr[0:4], h.Length)
	hdr[4] = h.Type
	le.PutUint64(hdr[5:13], h.Seq)
	crc := crc32.Update(crc32.Checksum(hdr[:], castagnoli), castagnoli, payload)
	if crc != h.CRC {
		return fmt.Errorf("%w: CRC %08x, want %08x", ErrTornRecord, crc, h.CRC)
	}
	return nil
}
