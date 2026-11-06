// Disk-full tests (PR A3): scenarios E1-E3 from docs/PROBLEM.md, the
// DeleteReserve, and the truncate-and-continue rule after a failed write.
package engine_test

import (
	"bytes"
	"errors"
	"fmt"
	"path/filepath"
	"syscall"
	"testing"

	"ninelives/src/engine"
)

// room is how many bytes puts may use in the capped DBs below.
const room = 2000

// val100 is the value size fill uses; a put of it no longer fits once fill stops.
var val100 = bytes.Repeat([]byte("z"), 100)

func openCapped(t *testing.T, path string) *engine.DB {
	t.Helper()
	db, err := engine.Open(path, engine.Options{MaxBytes: engine.FileHeaderSize + engine.DeleteReserve + room})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// fill puts 100-byte values until the cap refuses one and returns what
// was acked.
func fill(t *testing.T, db *engine.DB) map[string]string {
	t.Helper()
	acked := map[string]string{}
	for i := 0; ; i++ {
		k, v := fmt.Sprintf("k%03d", i), string(bytes.Repeat([]byte{'a' + byte(i%26)}, 100))
		_, err := db.Put([]byte(k), []byte(v))
		if errors.Is(err, engine.ErrDiskFull) {
			return acked
		}
		if err != nil {
			t.Fatal(err)
		}
		acked[k] = v
	}
}

// E1: a put at the cap fails cleanly; earlier data stays readable.
func TestE1PutAtCapFailsCleanly(t *testing.T) {
	path := filepath.Join(t.TempDir(), "e1.db")
	db := openCapped(t, path)
	acked := fill(t, db)
	if len(acked) == 0 {
		t.Fatal("nothing fit under the cap")
	}
	before := fileSize(t, path)
	if before > engine.FileHeaderSize+room {
		t.Fatalf("puts used %d bytes, over the %d allowed before DeleteReserve", before, engine.FileHeaderSize+room)
	}
	if _, err := db.Put([]byte("one-more"), val100); !errors.Is(err, engine.ErrDiskFull) {
		t.Fatalf("err = %v, want ErrDiskFull", err)
	}
	if fileSize(t, path) != before {
		t.Fatal("a refused put changed the file")
	}
	wantState(t, db, acked)
	if err := engine.Check(path); err != nil {
		t.Fatalf("Check: %v", err)
	}
}

// E2: a batch that does not fit fails as a whole; none of its ops show.
func TestE2BatchOverCapAppliesNothing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "e2.db")
	db := openCapped(t, path)
	db.Put([]byte("keep"), []byte("1"))
	before := fileSize(t, path)
	ops := make([]engine.Op, 30)
	for i := range ops {
		ops[i] = put(fmt.Sprintf("b%02d", i), string(bytes.Repeat([]byte("v"), 100)))
	}
	if _, err := db.Batch(ops); !errors.Is(err, engine.ErrDiskFull) {
		t.Fatalf("err = %v, want ErrDiskFull", err)
	}
	if fileSize(t, path) != before {
		t.Fatal("a refused batch changed the file")
	}
	wantState(t, db, map[string]string{"keep": "1"})
	if _, err := db.Batch(ops[:5]); err != nil {
		t.Fatalf("a batch that fits: %v", err)
	}
}

// E3: cap reached, then the process dies without Close. Every write was
// fsynced before its ack, so reopening (as after a kill) keeps all of them.
func TestE3ReopenAfterCapKeepsAckedData(t *testing.T) {
	path := filepath.Join(t.TempDir(), "e3.db")
	db := openCapped(t, path)
	acked := fill(t, db)
	// no Close: the file is left exactly as a SIGKILL would leave it
	db2 := openCapped(t, path)
	wantState(t, db2, acked)
	if err := engine.Check(path); err != nil {
		t.Fatalf("Check: %v", err)
	}
	if _, err := db2.Put([]byte("again"), val100); !errors.Is(err, engine.ErrDiskFull) {
		t.Fatalf("still full after reopen: err = %v, want ErrDiskFull", err)
	}
}

// When puts are refused, deletes still fit in DeleteReserve.
func TestDeletesUseReserve(t *testing.T) {
	path := filepath.Join(t.TempDir(), "reserve.db")
	db := openCapped(t, path)
	acked := fill(t, db)
	for k := range acked {
		if _, err := db.Delete([]byte(k)); err != nil {
			t.Fatalf("delete with puts full: %v", err)
		}
		delete(acked, k)
		break
	}
	wantState(t, db, acked)

	// Far more deletes than the reserve holds: DEL records are dead bytes,
	// so when the reserve runs out the guard compacts them away and goes on.
	// The file must never pass max_bytes.
	for i := 0; i < 5000; i++ {
		if _, err := db.Delete([]byte(fmt.Sprintf("missing-%d", i))); err != nil {
			t.Fatalf("delete %d: %v", i, err)
		}
		if n := db.Stats().FileBytes; n > engine.FileHeaderSize+engine.DeleteReserve+room {
			t.Fatalf("file is %d bytes, over max_bytes", n)
		}
	}
	wantState(t, db, acked)
}

// A real write error halfway through a record: the process file-size limit
// (RLIMIT_FSIZE) lets WriteAt write part of the record and then fail with
// EFBIG. The engine must cut the partial bytes off, report ErrDiskFull and
// stay usable, not poisoned.
func TestFailedWriteIsTruncatedAndDBStaysUsable(t *testing.T) {
	db, path := newDB(t)
	db.Put([]byte("a"), []byte("1"))
	before := fileSize(t, path)

	var old syscall.Rlimit
	if err := syscall.Getrlimit(syscall.RLIMIT_FSIZE, &old); err != nil {
		t.Skip("no RLIMIT_FSIZE:", err)
	}
	limited := old
	limited.Cur = uint64(before) + 10 // room for 10 bytes of the next record
	if err := syscall.Setrlimit(syscall.RLIMIT_FSIZE, &limited); err != nil {
		t.Skip("cannot set RLIMIT_FSIZE:", err)
	}
	_, err := db.Put([]byte("b"), bytes.Repeat([]byte("v"), 100))
	if rerr := syscall.Setrlimit(syscall.RLIMIT_FSIZE, &old); rerr != nil {
		t.Fatal(rerr)
	}

	if !errors.Is(err, engine.ErrDiskFull) || errors.Is(err, engine.ErrFailed) {
		t.Fatalf("err = %v, want ErrDiskFull and not ErrFailed", err)
	}
	if got := fileSize(t, path); got != before {
		t.Fatalf("file size = %d, want partial record cut back to %d", got, before)
	}
	if _, err := db.Put([]byte("c"), []byte("3")); err != nil {
		t.Fatalf("DB should stay usable after a clean failure: %v", err)
	}
	db.Close()
	wantState(t, mustOpen(t, path), map[string]string{"a": "1", "c": "3"})
	if err := engine.Check(path); err != nil {
		t.Fatalf("Check: %v", err)
	}
}

// E5: the DB is full, the user deletes keys, and the next put works again
// with no manual step: the guard compacts once and rechecks.
func TestE5DeletesThenCompactionFreesSpace(t *testing.T) {
	path := filepath.Join(t.TempDir(), "e5.db")
	db := openCapped(t, path)
	acked := fill(t, db)
	for k := range acked {
		if _, err := db.Delete([]byte(k)); err != nil {
			t.Fatalf("delete: %v", err)
		}
	}
	if _, err := db.Put([]byte("new"), val100); err != nil {
		t.Fatalf("put after deleting everything: %v", err)
	}
	if n := fileSize(t, path); n >= engine.FileHeaderSize+room {
		t.Fatalf("file is %d bytes; compaction should have shrunk it", n)
	}
	wantState(t, db, map[string]string{"new": string(val100)})
	if err := engine.Check(path); err != nil {
		t.Fatalf("Check: %v", err)
	}
	db.Close()
	wantState(t, openCapped(t, path), map[string]string{"new": string(val100)})
}

// When nothing is dead, compaction cannot help: still ErrDiskFull, and no
// temp file is left behind.
func TestFullWithNoDeadBytesStaysFull(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nodead.db")
	db := openCapped(t, path)
	fill(t, db)
	if d := db.Stats().DeadBytes; d != 0 {
		t.Fatalf("dead = %d, want 0", d)
	}
	if _, err := db.Put([]byte("x"), val100); !errors.Is(err, engine.ErrDiskFull) {
		t.Fatalf("err = %v, want ErrDiskFull", err)
	}
}
