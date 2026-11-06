package main

// Read-only view of a NineLives file for the Disk panel. It parses with
// the engine's own codec (src/engine/codec.go) and mirrors the recovery
// scan (scanLog in src/engine/recover.go): it stops at the first record
// that is short, too long or fails its CRC, and calls the rest the torn
// tail. It never opens the file for writing: engine.Open runs recovery and
// may truncate exactly the bytes this view exists to show.

import (
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"ninelives/src/engine"
)

const (
	maxViewBytes   = 16 << 20 // read at most this much of the file
	maxViewRecords = 5000     // list at most this many records
	maxViewOps     = 200      // list at most this many ops per record
	maxViewKeys    = 2000     // list at most this many index rows
	previewBytes   = 48       // bytes of a key or value shown in a token
	maxHexBytes    = 4096     // largest /api/bytes read
)

// snapshot is the database file as it is on disk right now.
type snapshot struct {
	Path      string       `json:"path"`
	Exists    bool         `json:"exists"`
	Size      int64        `json:"size"`
	Inode     uint64       `json:"inode"`
	Error     string       `json:"error,omitempty"`     // the file could not be read
	HeaderErr string       `json:"headerErr,omitempty"` // open() would fail with ErrNotNineLives
	Header    *headerView  `json:"header,omitempty"`
	Records   []recordView `json:"records"`
	Truncated bool         `json:"truncated"` // more records than shown, or the file is over 16 MiB
	ValidEnd  int64        `json:"validEnd"`  // end of the last complete record: what open() keeps
	Torn      *tornView    `json:"torn,omitempty"`
	Corrupt   string       `json:"corrupt,omitempty"` // good CRC, malformed payload: open() refuses
	TempFiles []string     `json:"tempFiles"`
	Keys      int          `json:"keys"`
	Index     []indexRow   `json:"index"`
	LiveBytes int64        `json:"liveBytes"`
	DeadBytes int64        `json:"deadBytes"`
	NextSeq   uint64       `json:"nextSeq"`
}

type headerView struct {
	Magic   string `json:"magic"`
	Version uint16 `json:"version"`
	SeqBase uint64 `json:"seqBase"`
	CRC     uint32 `json:"crc"`
}

// recordView is one committed record. Offsets are absolute file offsets.
type recordView struct {
	Off     int64    `json:"off"`
	Size    int64    `json:"size"` // header + payload
	Type    string   `json:"type"` // PUT, DEL or BATCH
	Seq     uint64   `json:"seq"`
	CRC     uint32   `json:"crc"`
	OpCount int      `json:"opCount"`
	Ops     []opView `json:"ops"` // the first maxViewOps
	Live    bool     `json:"live"`
}

type opView struct {
	Kind   string `json:"kind"` // put or del
	Key    string `json:"key"`
	KeyOff int64  `json:"keyOff"`
	KeyLen int    `json:"keyLen"`
	ValOff int64  `json:"valOff,omitempty"`
	ValLen int    `json:"valLen"`
	Value  string `json:"value,omitempty"`
	Live   bool   `json:"live"` // this op wrote the key's current value
}

// tornView is everything after the last complete record.
type tornView struct {
	Off    int64  `json:"off"`
	Bytes  int64  `json:"bytes"`
	Reason string `json:"reason"`
}

type indexRow struct {
	Key   string `json:"key"`
	Value string `json:"value"`
	Size  int    `json:"size"`
	Seq   uint64 `json:"seq"`
	Off   int64  `json:"off"`   // value offset, as the engine's index stores it
	RecAt int64  `json:"recAt"` // offset of the record holding it
}

// liveRef is what the engine's index keeps per key (index.go entry), plus
// where the op sits in this view.
type liveRef struct {
	rec, op      int // view positions; -1 when not listed
	recAt        int64
	valOff, size int64
	seq          uint64
	cost         int64
}

// takeSnapshot reads path without changing it.
func takeSnapshot(path string) *snapshot {
	s := &snapshot{Path: path, Records: []recordView{}, TempFiles: []string{}, Index: []indexRow{}}
	for _, t := range []string{path + ".compact", path + ".init"} {
		if _, err := os.Stat(t); err == nil {
			s.TempFiles = append(s.TempFiles, filepath.Base(t))
		}
	}
	f, err := os.Open(path)
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			s.Error = err.Error()
		}
		return s
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		s.Error = err.Error()
		return s
	}
	s.Exists, s.Size, s.Inode = true, fi.Size(), inodeOf(fi)
	b := make([]byte, min(s.Size, maxViewBytes))
	n, err := io.ReadFull(f, b)
	if errors.Is(err, io.ErrUnexpectedEOF) { // shrank since Stat (a writer truncated it)
		b, s.Size, err = b[:n], int64(n), nil
	}
	if err != nil {
		s.Error = err.Error()
		return s
	}
	s.decode(b)
	return s
}

// decode fills the view from the file bytes b (all of them, or the first
// maxViewBytes).
func (s *snapshot) decode(b []byte) {
	seqBase, err := engine.DecodeFileHeader(b)
	if err != nil {
		s.HeaderErr = err.Error()
		return
	}
	s.Header = &headerView{Magic: string(b[0:4]), Version: binary.LittleEndian.Uint16(b[4:6]),
		SeqBase: seqBase, CRC: binary.LittleEndian.Uint32(b[20:24])}
	s.NextSeq = seqBase
	idx := map[string]liveRef{}
	s.scan(b, idx)
	s.fillIndex(b, idx)
}

// scan walks the records like the engine's scanLog and sets ValidEnd.
func (s *snapshot) scan(b []byte, idx map[string]liveRef) {
	off := int64(engine.FileHeaderSize)
	defer func() { s.ValidEnd = off }()
	for off < s.Size {
		left := s.Size - off
		if left < engine.RecordHeaderSize {
			s.torn(off, fmt.Sprintf("only %d bytes left; a record header is %d", left, engine.RecordHeaderSize))
			return
		}
		if off+engine.RecordHeaderSize > int64(len(b)) {
			s.Truncated = true // past the part of the file we read
			return
		}
		h, err := engine.DecodeRecordHeader(b[off:])
		if err != nil {
			s.torn(off, err.Error())
			return
		}
		end := off + engine.RecordHeaderSize + int64(h.Length)
		if end > s.Size {
			s.torn(off, fmt.Sprintf("record needs %d payload bytes, only %d left", h.Length, left-engine.RecordHeaderSize))
			return
		}
		if end > int64(len(b)) {
			s.Truncated = true
			return
		}
		payload := b[off+engine.RecordHeaderSize : end]
		if err := engine.VerifyRecord(h, payload); err != nil {
			s.torn(off, err.Error())
			return
		}
		if err := s.apply(h, payload, off, idx); err != nil {
			s.Corrupt = fmt.Sprintf("record at offset %d: %v", off, err)
			return
		}
		off = end
	}
}

func (s *snapshot) torn(off int64, reason string) {
	s.Torn = &tornView{Off: off, Bytes: s.Size - off, Reason: reason}
}

// apply lists one committed record and replays it into idx the way the
// engine's applyRecord does: ops in order, a put replaces, a delete removes.
func (s *snapshot) apply(h engine.RecordHeader, payload []byte, off int64, idx map[string]liveRef) error {
	rv := recordView{Off: off, Size: engine.RecordHeaderSize + int64(h.Length), Type: recordType(h.Type), Seq: h.Seq, CRC: h.CRC, Ops: []opView{}}
	rec := -1
	if len(s.Records) < maxViewRecords {
		rec = len(s.Records)
	} else {
		s.Truncated = true
	}
	base := off + engine.RecordHeaderSize
	pos := 0 // where the next op starts in the payload
	if h.Type == engine.RecBatch {
		pos = 4 // count u32
	}
	err := engine.WalkPayload(h.Type, payload, func(kind engine.OpKind, key []byte, valOff, valLen int) error {
		if h.Type == engine.RecBatch {
			pos++ // op byte
		}
		keyOff := pos + 2 // after key_len u16
		pos = keyOff + len(key)
		ov := opView{Kind: "del", Key: token(key), KeyOff: base + int64(keyOff), KeyLen: len(key)}
		if kind == engine.OpPut {
			ov.Kind, ov.ValOff, ov.ValLen, ov.Value = "put", base+int64(valOff), valLen, token(payload[valOff:valOff+valLen])
			pos = valOff + valLen
		}
		op := -1
		if rec >= 0 && len(rv.Ops) < maxViewOps {
			op = len(rv.Ops)
			rv.Ops = append(rv.Ops, ov)
		}
		rv.OpCount++
		if kind == engine.OpDel {
			delete(idx, string(key))
			return nil
		}
		cost := int64(7 + len(key) + valLen) // a put inside a batch: op u8 + key_len u16 + val_len u32
		if h.Type == engine.RecPut {
			cost = rv.Size // a lone put: the whole record
		}
		idx[string(key)] = liveRef{rec: rec, op: op, recAt: off, valOff: base + int64(valOff), size: int64(valLen), seq: h.Seq, cost: cost}
		return nil
	})
	if err != nil {
		return err
	}
	if rec >= 0 {
		s.Records = append(s.Records, rv)
	}
	if h.Seq >= s.NextSeq {
		s.NextSeq = h.Seq + 1 // nextSeq = max(SeqBase, maxRecordSeq + 1)
	}
	return nil
}

// fillIndex marks live ops, sums live bytes and lists the index sorted by
// raw key bytes, like dump.
func (s *snapshot) fillIndex(b []byte, idx map[string]liveRef) {
	keys := make([]string, 0, len(idx))
	for k, ref := range idx {
		keys = append(keys, k)
		s.LiveBytes += ref.cost
		if ref.rec >= 0 {
			s.Records[ref.rec].Live = true
			if ref.op >= 0 {
				s.Records[ref.rec].Ops[ref.op].Live = true
			}
		}
	}
	slices.Sort(keys)
	s.Keys = len(keys)
	s.DeadBytes = s.ValidEnd - engine.FileHeaderSize - s.LiveBytes
	for _, k := range keys[:min(len(keys), maxViewKeys)] {
		ref := idx[k]
		s.Index = append(s.Index, indexRow{Key: token([]byte(k)), Value: token(b[ref.valOff : ref.valOff+ref.size]),
			Size: int(ref.size), Seq: ref.seq, Off: ref.valOff, RecAt: ref.recAt})
	}
	if len(keys) > maxViewKeys {
		s.Truncated = true
	}
}

func recordType(t byte) string {
	switch t {
	case engine.RecPut:
		return "PUT"
	case engine.RecDel:
		return "DEL"
	case engine.RecBatch:
		return "BATCH"
	}
	return fmt.Sprintf("0x%02x", t)
}

// token renders bytes the way dump does (SPEC section 10): raw text when
// every byte is printable ASCII other than space and it does not start
// with "x:", else x:<hex>. Longer than previewBytes: cut, with "…".
func token(b []byte) string {
	cut := len(b) > previewBytes
	if cut {
		b = b[:previewBytes]
	}
	s := "x:" + hex.EncodeToString(b)
	if len(b) > 0 && rawText(b) && !strings.HasPrefix(string(b), "x:") {
		s = string(b)
	}
	if cut {
		s += "…"
	}
	return s
}

func rawText(b []byte) bool {
	for _, c := range b {
		if c <= 0x20 || c > 0x7e {
			return false
		}
	}
	return true
}

// diffView is what one step changed in the file.
type diffView struct {
	Changed   bool     `json:"changed"`
	Created   bool     `json:"created,omitempty"`
	Removed   bool     `json:"removed,omitempty"`
	Rewritten bool     `json:"rewritten,omitempty"` // a different file is at the path now (compaction rename)
	Added     int      `json:"added"`               // complete records past the old end
	TornCut   int64    `json:"tornCut,omitempty"`   // torn bytes recovery cut off
	SizeFrom  int64    `json:"sizeFrom"`
	SizeTo    int64    `json:"sizeTo"`
	TempNew   []string `json:"tempNew,omitempty"`
	TempGone  []string `json:"tempGone,omitempty"`
}

// diffOf compares two snapshots of the same path, a before b.
func diffOf(a, b *snapshot) diffView {
	d := diffView{SizeFrom: a.Size, SizeTo: b.Size,
		Created: !a.Exists && b.Exists, Removed: a.Exists && !b.Exists}
	if a.Exists && b.Exists {
		d.Rewritten = a.Inode != b.Inode || (a.Header != nil && b.Header != nil && a.Header.SeqBase != b.Header.SeqBase)
	}
	for _, r := range b.Records {
		if d.Created || d.Rewritten || r.Off >= a.ValidEnd {
			d.Added++
		}
	}
	if a.Torn != nil && b.Exists && !d.Rewritten && (b.Torn == nil || b.Torn.Off != a.Torn.Off) {
		d.TornCut = a.Torn.Bytes
	}
	d.TempNew = missingFrom(b.TempFiles, a.TempFiles)
	d.TempGone = missingFrom(a.TempFiles, b.TempFiles)
	d.Changed = d.Created || d.Removed || d.Rewritten || a.Size != b.Size || d.TempNew != nil || d.TempGone != nil
	return d
}

// missingFrom lists the items of xs that are not in ys.
func missingFrom(xs, ys []string) []string {
	var out []string
	for _, x := range xs {
		if !slices.Contains(ys, x) {
			out = append(out, x)
		}
	}
	return out
}

// readHex returns up to n bytes of path at off, as hex, for the inspector.
func readHex(path string, off, n int64) (string, error) {
	if off < 0 || n < 1 || n > maxHexBytes {
		return "", fmt.Errorf("want 0 <= off and 1 <= len <= %d", maxHexBytes)
	}
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	b := make([]byte, n)
	got, err := f.ReadAt(b, off)
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	return hex.EncodeToString(b[:got]), nil
}
