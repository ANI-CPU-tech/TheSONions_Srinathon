// Disk-full tests (PR A3): scenarios E1-E3 from docs/PROBLEM.md, the
// DeleteReserve.
package engine_test

import (
	"bytes"
	"errors"
	"fmt"
	"path/filepath"
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

// When puts are refused, deletes still fit in DeleteReserve, until the
// reserve itself runs out.
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

	var err error
	for i := 0; err == nil; i++ {
		_, err = db.Delete([]byte(fmt.Sprintf("missing-%d", i)))
	}
	if !errors.Is(err, engine.ErrDiskFull) {
		t.Fatalf("err = %v, want ErrDiskFull once the reserve is used", err)
	}
	if n := fileSize(t, path); n > engine.FileHeaderSize+engine.DeleteReserve+room {
		t.Fatalf("file is %d bytes, over max_bytes", n)
	}
}
