// Compaction tests (PR A4): Compact keeps exactly the live state and the
// seq order, shrinks the file, survives a leftover temp file (C3) and
// fails cleanly when the new file would not fit (E4).
package engine_test

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"ninelives/src/engine"
)

func TestCompactKeepsLiveStateAndShrinks(t *testing.T) {
	db, path := newDB(t)
	for i := 0; i < 50; i++ {
		db.Put([]byte(fmt.Sprintf("k%02d", i%10)), []byte(fmt.Sprintf("v%d", i)))
	}
	db.Batch([]engine.Op{put("b1", "x"), put("b2", "y"), del("k00")})
	db.Delete([]byte("k01"))
	db.Put([]byte("empty"), nil)
	want := state(t, db)
	before := db.Stats()

	if err := db.Compact(); err != nil {
		t.Fatal(err)
	}
	after := db.Stats()
	if after.FileBytes >= before.FileBytes || after.DeadBytes != 0 || after.FileBytes != fileSize(t, path) {
		t.Fatalf("before %+v, after %+v, file %d", before, after, fileSize(t, path))
	}
	if after.NextSeq != before.NextSeq || after.Keys != before.Keys {
		t.Fatalf("NextSeq or Keys changed: before %+v, after %+v", before, after)
	}
	wantState(t, db, want)
	if _, err := os.Stat(path + ".compact"); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("temp file left behind")
	}
	if err := engine.Check(path); err != nil {
		t.Fatalf("Check: %v", err)
	}

	// Writes keep working on the new file, and everything survives reopen.
	if seq, err := db.Put([]byte("after"), []byte("1")); err != nil || seq != before.NextSeq {
		t.Fatalf("put after compact: seq %d, err %v; want seq %d", seq, err, before.NextSeq)
	}
	want["after"] = "1"
	db.Close()
	db2 := mustOpen(t, path)
	wantState(t, db2, want)
	if db2.Stats().NextSeq != before.NextSeq+1 {
		t.Fatalf("NextSeq after reopen = %d", db2.Stats().NextSeq)
	}
}

// If the newest records were deletes, compaction drops them; SeqBase in the
// new header must still keep seqs from going backwards.
func TestCompactKeepsSeqAfterTrailingDeletes(t *testing.T) {
	db, path := newDB(t)
	db.Put([]byte("a"), []byte("1"))
	db.Put([]byte("b"), []byte("2"))
	db.Delete([]byte("b"))
	db.Delete([]byte("a"))
	if err := db.Compact(); err != nil {
		t.Fatal(err)
	}
	if n := fileSize(t, path); n != engine.FileHeaderSize {
		t.Fatalf("file = %d bytes, want header only", n)
	}
	db.Close()
	db2 := mustOpen(t, path)
	if seq, _ := db2.Put([]byte("c"), []byte("3")); seq != 5 {
		t.Fatalf("seq after compaction + reopen = %d, want 5", seq)
	}
}

// C3: killed after the temp file was complete and fsynced, before the
// rename. The old file is authoritative and the temp file is deleted.
func TestOpenIgnoresCompleteTempFile(t *testing.T) {
	path := writeLog(t, 1, engine.EncodePut(1, []byte("a"), []byte("old")))
	other := writeLog(t, 1, engine.EncodePut(1, []byte("a"), []byte("new")))
	data, err := os.ReadFile(other)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path+".compact", data, 0o644); err != nil {
		t.Fatal(err)
	}
	db := mustOpen(t, path)
	wantState(t, db, map[string]string{"a": "old"})
	if _, err := os.Stat(path + ".compact"); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("open() must delete the temp file")
	}
}

// E4: compaction would need more than max_bytes. A put inside a batch costs
// 7 framing bytes, as a standalone PUT 23, so rewriting a big batch of small
// keys grows the file. Compact must fail with ErrDiskFull, leave the old
// file and the data alone, and leave no temp file.
func TestE4CompactionOverCapFailsCleanly(t *testing.T) {
	ops := make([]engine.Op, 5000)
	for i := range ops {
		ops[i] = put(fmt.Sprintf("%05d", i), "v")
	}
	path := filepath.Join(t.TempDir(), "e4.db")
	batchLen := int64(len(engine.EncodeBatch(1, ops)))
	db, err := engine.Open(path, engine.Options{MaxBytes: engine.FileHeaderSize + batchLen + engine.DeleteReserve})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Batch(ops); err != nil {
		t.Fatal(err)
	}
	want := state(t, db)
	before := fileSize(t, path)

	if err := db.Compact(); !errors.Is(err, engine.ErrDiskFull) {
		t.Fatalf("err = %v, want ErrDiskFull", err)
	}
	if fileSize(t, path) != before {
		t.Fatal("failed compaction changed the database file")
	}
	if _, err := os.Stat(path + ".compact"); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("failed compaction left its temp file")
	}
	wantState(t, db, want)
	if _, err := db.Delete([]byte("00000")); err != nil {
		t.Fatalf("DB must stay usable after failed compaction: %v", err)
	}
	if err := engine.Check(path); err != nil {
		t.Fatalf("Check: %v", err)
	}
}

func TestCompactOnClosedDB(t *testing.T) {
	db, _ := newDB(t)
	db.Close()
	if err := db.Compact(); !errors.Is(err, engine.ErrClosed) {
		t.Fatalf("err = %v, want ErrClosed", err)
	}
}

// A file of at least CompactMinFile with more than half dead bytes is
// compacted automatically after a write.
func TestAutoCompaction(t *testing.T) {
	db, path := newDB(t)
	big := make([]byte, engine.MaxValueSize)
	for i := 0; i < 6; i++ {
		big[0] = byte(i)
		if _, err := db.Put([]byte("k"), big); err != nil {
			t.Fatal(err)
		}
	}
	// Without compaction the file would hold 6 values. Put 4 crosses
	// CompactMinFile with 3 of 4 values dead, so it compacts to one value;
	// puts 5 and 6 then grow it to 3 values, still under the trigger size.
	s := db.Stats()
	if s.FileBytes >= engine.CompactMinFile || s.FileBytes != fileSize(t, path) {
		t.Fatalf("stats %+v, file %d: want auto-compacted below %d", s, fileSize(t, path), engine.CompactMinFile)
	}
	if v, _, _ := db.Get([]byte("k")); len(v) != engine.MaxValueSize || v[0] != 5 {
		t.Fatal("latest value lost by auto-compaction")
	}
}

// Small files are never auto-compacted, however much of them is dead.
func TestNoAutoCompactionBelowMinFile(t *testing.T) {
	db, _ := newDB(t)
	for i := 0; i < 100; i++ {
		db.Put([]byte("k"), []byte(fmt.Sprint(i)))
	}
	if s := db.Stats(); s.DeadBytes == 0 {
		t.Fatalf("small file was compacted: %+v", s)
	}
}

// TaskSplit A4 acceptance: after 1M puts and 1M deletes the file shrinks.
// Written as batches of 10,000 so the test does not wait on 2M fsyncs.
func TestMillionPutsThenDeletesShrinks(t *testing.T) {
	if testing.Short() {
		t.Skip("slow")
	}
	db, path := newDB(t)
	const n, batch = 1_000_000, 10_000
	ops := make([]engine.Op, batch)
	for kind := range []engine.OpKind{engine.OpPut, engine.OpDel} {
		for b := 0; b < n/batch; b++ {
			for i := range ops {
				ops[i] = engine.Op{Kind: engine.OpKind(kind + 1), Key: []byte(fmt.Sprintf("key-%07d", b*batch+i)), Value: []byte("value")}
			}
			if _, err := db.Batch(ops); err != nil {
				t.Fatal(err)
			}
		}
		if kind == 0 {
			if s := db.Stats(); s.Keys != n {
				t.Fatalf("after puts: %+v", s)
			}
			t.Logf("after 1M puts: %d bytes", fileSize(t, path))
		}
	}
	if err := db.Compact(); err != nil {
		t.Fatal(err)
	}
	if s := db.Stats(); s.Keys != 0 || s.FileBytes != engine.FileHeaderSize || fileSize(t, path) != engine.FileHeaderSize {
		t.Fatalf("after deletes + compact: %+v, file %d", s, fileSize(t, path))
	}
	db.Close()
	if s := mustOpen(t, path).Stats(); s.Keys != 0 || s.NextSeq != 2*n/batch+1 {
		t.Fatalf("after reopen: %+v", s)
	}
}
