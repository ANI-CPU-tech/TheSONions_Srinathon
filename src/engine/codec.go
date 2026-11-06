// Header and record encoding/decoding with CRC32-C (docs/SPEC.md section 3).
// All integers are little-endian. This file only builds and parses bytes;
// it never touches the disk, so it has no // CRASH: points.
package engine

import (
	"encoding/binary"
	"fmt"
	"hash/crc32"
)

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
