// Codec tests (PR A1): byte layout matches docs/SPEC.md section 3, round
// trips, torn-record detection and bad CRC detection.
package engine_test

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"testing"

	"ninelives/src/engine"
)

var castagnoli = crc32.MakeTable(crc32.Castagnoli)

func TestFileHeaderLayout(t *testing.T) {
	h := engine.EncodeFileHeader(0x0102030405060708)
	want := []byte{
		'N', 'L', 'V', '1', // magic
		1, 0, // version
		0, 0, // flags
		8, 7, 6, 5, 4, 3, 2, 1, // SeqBase, little-endian
		0, 0, 0, 0, // reserved
	}
	if len(h) != engine.FileHeaderSize {
		t.Fatalf("header len = %d, want %d", len(h), engine.FileHeaderSize)
	}
	if !bytes.Equal(h[:20], want) {
		t.Fatalf("header bytes 0-19 = % x, want % x", h[:20], want)
	}
	if got, crc := binary.LittleEndian.Uint32(h[20:]), crc32.Checksum(want, castagnoli); got != crc {
		t.Fatalf("header CRC = %08x, want CRC32-C %08x", got, crc)
	}
}

func TestFileHeaderRoundTrip(t *testing.T) {
	for _, seq := range []uint64{0, 1, 1 << 40, ^uint64(0)} {
		got, err := engine.DecodeFileHeader(engine.EncodeFileHeader(seq))
		if err != nil || got != seq {
			t.Fatalf("seqBase %d: got %d, err %v", seq, got, err)
		}
	}
}

func TestFileHeaderRejectsBadInput(t *testing.T) {
	good := engine.EncodeFileHeader(42)

	badVersion := append([]byte(nil), good...)
	badVersion[4] = 2
	binary.LittleEndian.PutUint32(badVersion[20:], crc32.Checksum(badVersion[:20], castagnoli))

	cases := map[string][]byte{
		"short":       good[:engine.FileHeaderSize-1],
		"empty":       nil,
		"bad version": badVersion, // CRC is valid, so only the version check can reject it
	}
	for i := range good {
		flipped := append([]byte(nil), good...)
		flipped[i] ^= 0xFF
		cases[fmt.Sprintf("flip byte %d", i)] = flipped
	}
	for name, b := range cases {
		if _, err := engine.DecodeFileHeader(b); !errors.Is(err, engine.ErrNotNineLives) {
			t.Errorf("%s: err = %v, want ErrNotNineLives", name, err)
		}
	}
}
