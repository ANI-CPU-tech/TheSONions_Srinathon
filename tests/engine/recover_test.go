// Recovery tests (PR A2): files are built by hand from the A1 encoders, so
// these check Open independently of the write path.
package engine_test

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"ninelives/src/engine"
)

// writeLog writes header(seqBase) followed by the given records to a new
// file and returns its path.
func writeLog(t *testing.T, seqBase uint64, records ...[]byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.db")
	buf := engine.EncodeFileHeader(seqBase)
	for _, r := range records {
		buf = append(buf, r...)
	}
	if err := os.WriteFile(path, buf, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func mustOpen(t *testing.T, path string) *engine.DB {
	t.Helper()
	db, err := engine.Open(path, engine.Options{})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// state dumps the whole DB as key -> value.
func state(t *testing.T, db *engine.DB) map[string]string {
	t.Helper()
	m := map[string]string{}
	err := db.ForEachSorted(func(k, v []byte) error {
		m[string(k)] = string(v)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func wantState(t *testing.T, db *engine.DB, want map[string]string) {
	t.Helper()
	got := state(t, db)
	if len(got) != len(want) {
		t.Fatalf("state = %v, want %v", got, want)
	}
	for k, v := range want {
		if got[k] != v {
			t.Fatalf("state = %v, want %v", got, want)
		}
	}
}

func fileSize(t *testing.T, path string) int64 {
	t.Helper()
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return fi.Size()
}

func put(k, v string) engine.Op {
	return engine.Op{Kind: engine.OpPut, Key: []byte(k), Value: []byte(v)}
}
func del(k string) engine.Op { return engine.Op{Kind: engine.OpDel, Key: []byte(k)} }

func TestOpenCreatesEmptyFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "new.db")
	db := mustOpen(t, path)
	if s := db.Stats(); s.Keys != 0 || s.FileBytes != engine.FileHeaderSize || s.NextSeq != 1 {
		t.Fatalf("stats = %+v", s)
	}
	if n := fileSize(t, path); n != engine.FileHeaderSize {
		t.Fatalf("file size = %d, want %d", n, engine.FileHeaderSize)
	}
	if _, err := os.Stat(path + ".init"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf(".init left behind: %v", err)
	}
}

func TestOpenRemovesLeftoverTempFiles(t *testing.T) {
	path := writeLog(t, 1, engine.EncodePut(1, []byte("a"), []byte("1")))
	for _, suffix := range []string{".init", ".compact"} {
		if err := os.WriteFile(path+suffix, []byte("junk"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	db := mustOpen(t, path)
	wantState(t, db, map[string]string{"a": "1"})
	for _, suffix := range []string{".init", ".compact"} {
		if _, err := os.Stat(path + suffix); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("%s not removed: %v", suffix, err)
		}
	}
}

func TestRecoveryRebuildsIndex(t *testing.T) {
	path := writeLog(t, 1,
		engine.EncodePut(1, []byte("a"), []byte("1")),
		engine.EncodePut(2, []byte("b"), []byte("2")),
		engine.EncodeDel(3, []byte("a")),
		engine.EncodeBatch(4, []engine.Op{put("c", "3"), put("a", "4"), del("b"), put("x", "9"), del("x")}),
		engine.EncodePut(5, []byte("e"), nil),
	)
	db := mustOpen(t, path)
	wantState(t, db, map[string]string{"a": "4", "c": "3", "e": ""})
	if s := db.Stats(); s.NextSeq != 6 || s.Keys != 3 || s.FileBytes != fileSize(t, path) {
		t.Fatalf("stats = %+v", s)
	}
	v, found, err := db.Get([]byte("e"))
	if err != nil || !found || len(v) != 0 {
		t.Fatalf("Get(e) = %q, %v, %v; want empty value found", v, found, err)
	}
	if _, found, _ := db.Get([]byte("b")); found {
		t.Fatal("b should be deleted by the batch")
	}
}

func TestNextSeqRespectsSeqBase(t *testing.T) {
	db := mustOpen(t, writeLog(t, 100))
	if s := db.Stats(); s.NextSeq != 100 {
		t.Fatalf("NextSeq = %d, want SeqBase 100", s.NextSeq)
	}
	db2 := mustOpen(t, writeLog(t, 100, engine.EncodePut(250, []byte("k"), []byte("v"))))
	if s := db2.Stats(); s.NextSeq != 251 {
		t.Fatalf("NextSeq = %d, want 251", s.NextSeq)
	}
}

// Every prefix of the last record is a possible kill mid-write. Recovery
// must keep everything before it, drop it, and truncate the file.
func TestRecoveryTruncatesTornTail(t *testing.T) {
	first := engine.EncodePut(1, []byte("a"), []byte("1"))
	last := engine.EncodeBatch(2, []engine.Op{put("a", "2"), put("b", string(bytes.Repeat([]byte("v"), 100)))})
	validEnd := int64(engine.FileHeaderSize + len(first))
	for n := 0; n < len(last); n++ {
		path := writeLog(t, 1, first, last[:n])
		db := mustOpen(t, path)
		wantState(t, db, map[string]string{"a": "1"})
		if s := db.Stats(); s.FileBytes != validEnd || s.NextSeq != 2 {
			t.Fatalf("prefix %d: stats = %+v", n, s)
		}
		if got := fileSize(t, path); got != validEnd {
			t.Fatalf("prefix %d: file size = %d, want truncated to %d", n, got, validEnd)
		}
		db.Close()
	}
	// The complete record is committed and applies as a whole.
	db := mustOpen(t, writeLog(t, 1, first, last))
	wantState(t, db, map[string]string{"a": "2", "b": string(bytes.Repeat([]byte("v"), 100))})
}

// A record with a bad CRC ends the log: it and everything after it are cut.
func TestRecoveryStopsAtBadCRC(t *testing.T) {
	first := engine.EncodePut(1, []byte("a"), []byte("1"))
	bad := engine.EncodePut(2, []byte("b"), []byte("2"))
	bad[len(bad)-1] ^= 0xFF
	path := writeLog(t, 1, first, bad, engine.EncodePut(3, []byte("c"), []byte("3")))
	db := mustOpen(t, path)
	wantState(t, db, map[string]string{"a": "1"})
	if got := fileSize(t, path); got != int64(engine.FileHeaderSize+len(first)) {
		t.Fatalf("file size = %d, want truncated after first record", got)
	}
}

func TestOpenRejectsBadHeader(t *testing.T) {
	path := filepath.Join(t.TempDir(), "junk.db")
	for _, content := range [][]byte{[]byte("not a database at all, really"), []byte("NLV1")} {
		if err := os.WriteFile(path, content, 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := engine.Open(path, engine.Options{}); !errors.Is(err, engine.ErrNotNineLives) {
			t.Fatalf("Open(%q) err = %v, want ErrNotNineLives", content, err)
		}
	}
}

// A good CRC around a malformed payload is a writer bug, not a torn write:
// Open must refuse it rather than silently drop records.
func TestOpenRejectsMalformedPayload(t *testing.T) {
	path := writeLog(t, 1, seal(engine.RecPut, []byte{5, 0, 'k'}))
	if _, err := engine.Open(path, engine.Options{}); !errors.Is(err, engine.ErrCorrupt) {
		t.Fatalf("err = %v, want ErrCorrupt", err)
	}
}

func TestForEachSortedByteOrder(t *testing.T) {
	path := writeLog(t, 1, engine.EncodeBatch(1, []engine.Op{
		put("b", "2"), put("a", "1"), put("\xff", "4"), put("B", "0"), put("ab", "3"),
	}))
	db := mustOpen(t, path)
	var keys []string
	db.ForEachSorted(func(k, v []byte) error {
		keys = append(keys, string(k))
		return nil
	})
	want := []string{"B", "a", "ab", "b", "\xff"}
	if len(keys) != len(want) {
		t.Fatalf("keys = %q, want %q", keys, want)
	}
	for i := range want {
		if keys[i] != want[i] {
			t.Fatalf("keys = %q, want %q", keys, want)
		}
	}
}
