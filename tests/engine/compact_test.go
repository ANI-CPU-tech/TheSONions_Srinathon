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
