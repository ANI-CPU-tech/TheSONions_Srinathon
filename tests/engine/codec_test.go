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

// decode parses a whole encoded record the way recovery will: header first,
// then exactly Length payload bytes, then the CRC check.
func decode(b []byte) (engine.RecordHeader, []byte, error) {
	h, err := engine.DecodeRecordHeader(b)
	if err != nil {
		return h, nil, err
	}
	end := engine.RecordHeaderSize + int(h.Length)
	if end > len(b) {
		end = len(b) // record runs past the end of the bytes we have
	}
	payload := b[engine.RecordHeaderSize:end]
	return h, payload, engine.VerifyRecord(h, payload)
}

func TestPutRecordLayout(t *testing.T) {
	rec := engine.EncodePut(7, []byte("ab"), []byte("xyz"))
	want := []byte{
		11, 0, 0, 0, // Length
		0x01,                   // Type PUT
		7, 0, 0, 0, 0, 0, 0, 0, // Seq
		2, 0, 'a', 'b', // key_len, key
		3, 0, 0, 0, 'x', 'y', 'z', // val_len, value
	}
	if !bytes.Equal(rec[4:], want) {
		t.Fatalf("record bytes 4.. = % x, want % x", rec[4:], want)
	}
	if got, crc := binary.LittleEndian.Uint32(rec[:4]), crc32.Checksum(want, castagnoli); got != crc {
		t.Fatalf("record CRC = %08x, want CRC32-C %08x", got, crc)
	}
}

func TestDelRecordLayout(t *testing.T) {
	rec := engine.EncodeDel(1<<32, []byte("k"))
	want := []byte{
		3, 0, 0, 0, // Length
		0x02,                   // Type DEL
		0, 0, 0, 0, 1, 0, 0, 0, // Seq 1<<32
		1, 0, 'k', // key_len, key
	}
	if !bytes.Equal(rec[4:], want) {
		t.Fatalf("record bytes 4.. = % x, want % x", rec[4:], want)
	}
}

func TestBatchRecordLayout(t *testing.T) {
	rec := engine.EncodeBatch(9, []engine.Op{
		{Kind: engine.OpPut, Key: []byte("a"), Value: []byte("1")},
		{Kind: engine.OpDel, Key: []byte("b"), Value: []byte("ignored")},
	})
	want := []byte{
		17, 0, 0, 0, // Length
		0x03,                   // Type BATCH
		9, 0, 0, 0, 0, 0, 0, 0, // Seq
		2, 0, 0, 0, // count
		0x01, 1, 0, 'a', 1, 0, 0, 0, '1', // put a 1
		0x02, 1, 0, 'b', // del b (no value bytes)
	}
	if !bytes.Equal(rec[4:], want) {
		t.Fatalf("record bytes 4.. = % x, want % x", rec[4:], want)
	}
}

func TestRecordHeaderRoundTrip(t *testing.T) {
	rec := engine.EncodePut(123, []byte("key"), []byte("value"))
	h, payload, err := decode(rec)
	if err != nil {
		t.Fatal(err)
	}
	if h.Type != engine.RecPut || h.Seq != 123 || int(h.Length) != len(rec)-engine.RecordHeaderSize {
		t.Fatalf("header = %+v", h)
	}
	if !bytes.Equal(payload, rec[engine.RecordHeaderSize:]) {
		t.Fatal("payload mismatch")
	}
}

// Every strict prefix of a record is what a kill mid-write can leave behind.
// None of them may pass as a committed record.
func TestTornRecordDetected(t *testing.T) {
	rec := engine.EncodeBatch(5, []engine.Op{
		{Kind: engine.OpPut, Key: []byte("k1"), Value: bytes.Repeat([]byte("v"), 100)},
		{Kind: engine.OpDel, Key: []byte("k2")},
	})
	for n := 0; n < len(rec); n++ {
		if _, _, err := decode(rec[:n]); !errors.Is(err, engine.ErrTornRecord) {
			t.Fatalf("prefix of %d/%d bytes: err = %v, want ErrTornRecord", n, len(rec), err)
		}
	}
}

// Flipping any byte (header, CRC or payload) must be caught.
func TestBadCRCDetected(t *testing.T) {
	rec := engine.EncodePut(1, []byte("key"), []byte("value"))
	for i := range rec {
		bad := append([]byte(nil), rec...)
		bad[i] ^= 0xFF
		if _, _, err := decode(bad); !errors.Is(err, engine.ErrTornRecord) {
			t.Fatalf("flip byte %d: err = %v, want ErrTornRecord", i, err)
		}
	}
}

// A garbage Length must not make recovery try to read gigabytes.
func TestOversizedLengthRejected(t *testing.T) {
	rec := engine.EncodePut(1, []byte("k"), []byte("v"))
	binary.LittleEndian.PutUint32(rec[4:8], engine.MaxRecordPayload+1)
	if _, err := engine.DecodeRecordHeader(rec); !errors.Is(err, engine.ErrTornRecord) {
		t.Fatalf("err = %v, want ErrTornRecord", err)
	}
}

// collect decodes a record into the ops it holds, with values copied out
// of the payload by offset, exactly as recovery locates them.
func collect(t *testing.T, rec []byte) (engine.RecordHeader, []engine.Op) {
	t.Helper()
	h, payload, err := decode(rec)
	if err != nil {
		t.Fatal(err)
	}
	var ops []engine.Op
	err = engine.WalkPayload(h.Type, payload, func(kind engine.OpKind, key []byte, valOff, valLen int) error {
		op := engine.Op{Kind: kind, Key: append([]byte(nil), key...)}
		if kind == engine.OpPut {
			op.Value = append([]byte{}, payload[valOff:valOff+valLen]...)
		}
		ops = append(ops, op)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return h, ops
}

func sameOps(a, b []engine.Op) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Kind != b[i].Kind || !bytes.Equal(a[i].Key, b[i].Key) || !bytes.Equal(a[i].Value, b[i].Value) {
			return false
		}
	}
	return true
}

func TestPutDelRoundTrip(t *testing.T) {
	big := bytes.Repeat([]byte{0xAB}, engine.MaxValueSize)
	key := bytes.Repeat([]byte("k"), engine.MaxKeySize)
	for _, tc := range []struct {
		rec  []byte
		want engine.Op
	}{
		{engine.EncodePut(1, []byte("a"), []byte("1")), engine.Op{Kind: engine.OpPut, Key: []byte("a"), Value: []byte("1")}},
		{engine.EncodePut(2, []byte("a"), nil), engine.Op{Kind: engine.OpPut, Key: []byte("a"), Value: []byte{}}},
		{engine.EncodePut(3, key, big), engine.Op{Kind: engine.OpPut, Key: key, Value: big}},
		{engine.EncodeDel(4, []byte("a")), engine.Op{Kind: engine.OpDel, Key: []byte("a")}},
	} {
		if _, ops := collect(t, tc.rec); !sameOps(ops, []engine.Op{tc.want}) {
			t.Fatalf("decoded %d ops, want key %q", len(ops), tc.want.Key)
		}
	}
}

// Ops come back in listed order, so "put X; del X" ends with X deleted (B5).
func TestBatchRoundTripKeepsOrder(t *testing.T) {
	in := []engine.Op{
		{Kind: engine.OpPut, Key: []byte("x"), Value: []byte("1")},
		{Kind: engine.OpDel, Key: []byte("x")},
		{Kind: engine.OpPut, Key: []byte("y"), Value: []byte{}},
		{Kind: engine.OpPut, Key: []byte{0, 0xFF}, Value: []byte{0, 1, 2}},
	}
	h, ops := collect(t, engine.EncodeBatch(77, in))
	if h.Type != engine.RecBatch || h.Seq != 77 {
		t.Fatalf("header = %+v", h)
	}
	if !sameOps(ops, in) {
		t.Fatalf("ops = %+v, want %+v", ops, in)
	}
}

// seal builds a record with a valid CRC around an arbitrary payload, to
// reach the payload checks that a real torn write never gets past.
func seal(typ byte, payload []byte) []byte {
	rec := make([]byte, engine.RecordHeaderSize, engine.RecordHeaderSize+len(payload))
	binary.LittleEndian.PutUint32(rec[4:8], uint32(len(payload)))
	rec[8] = typ
	rec = append(rec, payload...)
	binary.LittleEndian.PutUint32(rec[0:4], crc32.Checksum(rec[4:], castagnoli))
	return rec
}

func TestMalformedPayloadIsCorruptAndAppliesNothing(t *testing.T) {
	goodBatch := engine.EncodeBatch(1, []engine.Op{
		{Kind: engine.OpPut, Key: []byte("a"), Value: []byte("1")},
		{Kind: engine.OpPut, Key: []byte("b"), Value: []byte("2")},
	})[engine.RecordHeaderSize:]

	cases := map[string][]byte{
		"unknown type":      seal(0x09, []byte{1, 0, 'k'}),
		"put cut short":     seal(engine.RecPut, []byte{5, 0, 'k'}),
		"del trailing byte": seal(engine.RecDel, []byte{1, 0, 'k', 0}),
		"batch count high":  seal(engine.RecBatch, binary.LittleEndian.AppendUint32(nil, engine.MaxBatchOps+1)),
		"batch bad op kind": seal(engine.RecBatch, []byte{1, 0, 0, 0, 0x07, 1, 0, 'k'}),
		// first op is fine, second is cut: fn must not see the first one
		"batch second op cut": seal(engine.RecBatch, goodBatch[:len(goodBatch)-1]),
	}
	for name, rec := range cases {
		h, payload, err := decode(rec)
		if err != nil {
			t.Fatalf("%s: CRC should pass, got %v", name, err)
		}
		calls := 0
		err = engine.WalkPayload(h.Type, payload, func(engine.OpKind, []byte, int, int) error {
			calls++
			return nil
		})
		if !errors.Is(err, engine.ErrCorrupt) || calls != 0 {
			t.Errorf("%s: err = %v, calls = %d; want ErrCorrupt and 0 calls", name, err, calls)
		}
	}
}

func TestWalkPayloadReturnsCallbackError(t *testing.T) {
	stop := errors.New("stop")
	rec := engine.EncodeDel(1, []byte("k"))
	h, payload, _ := decode(rec)
	if err := engine.WalkPayload(h.Type, payload, func(engine.OpKind, []byte, int, int) error { return stop }); err != stop {
		t.Fatalf("err = %v, want callback error", err)
	}
}
