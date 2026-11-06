package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ninelives/src/engine"
)

// buildDB writes a small database with the real engine: two puts, a batch
// whose last op deletes one of its own keys, and a delete.
func buildDB(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "demo.db")
	db, err := engine.Open(path, engine.Options{})
	if err != nil {
		t.Fatal(err)
	}
	must := func(_ uint64, err error) {
		if err != nil {
			t.Fatal(err)
		}
	}
	must(db.Put([]byte("a"), []byte("1")))
	must(db.Put([]byte("b"), []byte{0x00, 0xff}))
	must(db.Batch([]engine.Op{
		{Kind: engine.OpPut, Key: []byte("c"), Value: []byte("3")},
		{Kind: engine.OpPut, Key: []byte("d"), Value: []byte("4")},
		{Kind: engine.OpDel, Key: []byte("c")},
	}))
	must(db.Delete([]byte("a")))
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

// engineStats opens path with the engine itself, for cross-checking.
func engineStats(t *testing.T, path string) engine.Stats {
	t.Helper()
	db, err := engine.Open(path, engine.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	return db.Stats()
}

func TestSnapshotMatchesEngine(t *testing.T) {
	path := buildDB(t)
	s := takeSnapshot(path)
	if !s.Exists || s.Error != "" || s.HeaderErr != "" || s.Torn != nil || s.Corrupt != "" || s.Truncated {
		t.Fatalf("unexpected state: %+v", s)
	}
	var types []string
	for i, r := range s.Records {
		types = append(types, r.Type)
		if r.Seq != uint64(i+1) {
			t.Errorf("record %d seq %d, want %d", i, r.Seq, i+1)
		}
	}
	if got := strings.Join(types, ","); got != "PUT,PUT,BATCH,DEL" {
		t.Errorf("types %s", got)
	}
	if s.ValidEnd != s.Size {
		t.Errorf("validEnd %d != size %d", s.ValidEnd, s.Size)
	}
	if len(s.Index) != 2 || s.Index[0].Key != "b" || s.Index[0].Value != "x:00ff" || s.Index[1].Key != "d" || s.Index[1].Value != "4" {
		t.Errorf("index %+v", s.Index)
	}
	if s.Records[0].Live || !s.Records[1].Live || !s.Records[2].Live || s.Records[3].Live {
		t.Errorf("live flags wrong: %+v", s.Records)
	}
	ops := s.Records[2].Ops
	if s.Records[2].OpCount != 3 || ops[0].Live || !ops[1].Live || ops[2].Live {
		t.Errorf("batch ops %+v", ops)
	}
	st := engineStats(t, path)
	if s.Keys != st.Keys || s.DeadBytes != st.DeadBytes || s.NextSeq != st.NextSeq || s.ValidEnd != st.FileBytes {
		t.Errorf("view keys=%d dead=%d next=%d end=%d, engine %+v", s.Keys, s.DeadBytes, s.NextSeq, s.ValidEnd, st)
	}
}

// The offsets the hex inspector colours must point at the real bytes.
func TestSnapshotOffsets(t *testing.T) {
	path := buildDB(t)
	b, _ := os.ReadFile(path)
	s := takeSnapshot(path)
	want := map[string]string{"a": "1", "b": "\x00\xff", "c": "3", "d": "4"}
	for _, r := range s.Records {
		for _, op := range r.Ops {
			key := string(b[op.KeyOff : op.KeyOff+int64(op.KeyLen)])
			if _, ok := want[key]; !ok {
				t.Errorf("record @%d: key bytes %q at %d", r.Off, key, op.KeyOff)
			}
			if op.Kind == "put" && string(b[op.ValOff:op.ValOff+int64(op.ValLen)]) != want[key] {
				t.Errorf("record @%d: value of %q at %d wrong", r.Off, key, op.ValOff)
			}
		}
	}
	for _, row := range s.Index {
		if row.Off < row.RecAt || row.Off+int64(row.Size) > s.Size {
			t.Errorf("index row %+v out of range", row)
		}
	}
}

func TestSnapshotTornAndCRC(t *testing.T) {
	path := buildDB(t)
	size := takeSnapshot(path).Size
	rec := engine.EncodePut(99, []byte("torn"), []byte("value"))
	f, _ := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0)
	f.Write(rec[:len(rec)/2])
	f.Close()
	s := takeSnapshot(path)
	if s.Torn == nil || s.Torn.Off != size || s.Torn.Bytes != int64(len(rec)/2) || s.ValidEnd != size || len(s.Records) != 4 {
		t.Fatalf("torn tail not found: %+v torn=%+v", s, s.Torn)
	}

	os.Truncate(path, size)
	b, _ := os.ReadFile(path)
	b[len(b)-1] ^= 0xff // flip a byte of the last record's payload
	os.WriteFile(path, b, 0o644)
	s = takeSnapshot(path)
	last := s.Records[len(s.Records)-1]
	if s.Torn == nil || !strings.Contains(s.Torn.Reason, "CRC") || len(s.Records) != 3 || last.Type != "BATCH" {
		t.Fatalf("bad CRC not reported as torn: torn=%+v records=%d", s.Torn, len(s.Records))
	}
}

func TestSnapshotOddFiles(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "demo.db")
	if s := takeSnapshot(path); s.Exists || s.Error != "" {
		t.Errorf("missing file: %+v", s)
	}
	os.WriteFile(path, make([]byte, 24), 0o644)
	if s := takeSnapshot(path); s.HeaderErr == "" || len(s.Records) != 0 {
		t.Errorf("zero header accepted: %+v", s)
	}
	os.WriteFile(path+".compact", nil, 0o644)
	if s := takeSnapshot(path); len(s.TempFiles) != 1 || s.TempFiles[0] != "demo.db.compact" {
		t.Errorf("temp files %v", s.TempFiles)
	}
}

func TestDiff(t *testing.T) {
	path := filepath.Join(t.TempDir(), "demo.db")
	empty := takeSnapshot(path)
	db, _ := engine.Open(path, engine.Options{})
	db.Put([]byte("k"), []byte("v1"))
	one := takeSnapshot(path)
	if d := diffOf(empty, one); !d.Created || d.Added != 1 || !d.Changed {
		t.Errorf("create+put diff %+v", d)
	}
	db.Put([]byte("k"), []byte("v2"))
	two := takeSnapshot(path)
	if d := diffOf(one, two); d.Created || d.Rewritten || d.Added != 1 {
		t.Errorf("put diff %+v", d)
	}
	if err := db.Compact(); err != nil {
		t.Fatal(err)
	}
	db.Close()
	comp := takeSnapshot(path)
	if d := diffOf(two, comp); !d.Rewritten || d.SizeTo >= d.SizeFrom {
		t.Errorf("compaction diff %+v", d)
	}
	if d := diffOf(comp, comp); d.Changed {
		t.Errorf("no-op diff %+v", d)
	}

	rec := engine.EncodePut(50, []byte("x"), []byte("y"))
	f, _ := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0)
	f.Write(rec[:5])
	f.Close()
	torn := takeSnapshot(path)
	db, _ = engine.Open(path, engine.Options{}) // recovery cuts the 5 bytes
	db.Close()
	if d := diffOf(torn, takeSnapshot(path)); d.TornCut != 5 || d.Added != 0 {
		t.Errorf("recovery diff %+v", d)
	}
}

func TestToken(t *testing.T) {
	long := bytes.Repeat([]byte("a"), previewBytes+1)
	for in, want := range map[string]string{
		"":           "x:",
		"abc":        "abc",
		"a b":        "x:612062",
		"x:41":       "x:783a3431",
		"\x00\xff":   "x:00ff",
		string(long): strings.Repeat("a", previewBytes) + "…",
	} {
		if got := token([]byte(in)); got != want {
			t.Errorf("token(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestReadHex(t *testing.T) {
	path := filepath.Join(t.TempDir(), "f")
	os.WriteFile(path, []byte{1, 2, 3}, 0o644)
	if h, err := readHex(path, 1, 10); err != nil || h != "0203" {
		t.Errorf("readHex = %q, %v", h, err)
	}
	for _, c := range [][2]int64{{-1, 1}, {0, 0}, {0, maxHexBytes + 1}} {
		if _, err := readHex(path, c[0], c[1]); err == nil {
			t.Errorf("readHex(%d, %d) accepted", c[0], c[1])
		}
	}
}
