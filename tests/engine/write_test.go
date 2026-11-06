// Write-path tests (PR A2): Put/Delete/Batch, durability across reopen,
// validation writes nothing, batch order, closed handle, concurrency.
package engine_test

import (
	"bytes"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"testing"

	"ninelives/src/engine"
)

func newDB(t *testing.T) (*engine.DB, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "w.db")
	return mustOpen(t, path), path
}

func TestPutGetDeleteSurviveReopen(t *testing.T) {
	db, path := newDB(t)
	steps := []func() (uint64, error){
		func() (uint64, error) { return db.Put([]byte("a"), []byte("1")) },
		func() (uint64, error) { return db.Put([]byte("b"), []byte("2")) },
		func() (uint64, error) { return db.Put([]byte("a"), []byte("3")) },
		func() (uint64, error) { return db.Delete([]byte("b")) },
		func() (uint64, error) { return db.Delete([]byte("missing")) },
	}
	for i, step := range steps {
		seq, err := step()
		if err != nil || seq != uint64(i+1) {
			t.Fatalf("step %d: seq %d, err %v; want seq %d", i, seq, err, i+1)
		}
	}
	wantState(t, db, map[string]string{"a": "3"})
	if s := db.Stats(); s.FileBytes != fileSize(t, path) || s.NextSeq != 6 {
		t.Fatalf("stats = %+v, file %d", s, fileSize(t, path))
	}
	db.Close()

	db2 := mustOpen(t, path)
	wantState(t, db2, map[string]string{"a": "3"})
	if seq, err := db2.Put([]byte("c"), []byte("4")); err != nil || seq != 6 {
		t.Fatalf("seq after reopen = %d, %v; want 6", seq, err)
	}
}

func TestLargeAndEmptyValues(t *testing.T) {
	db, path := newDB(t)
	big := bytes.Repeat([]byte{0, 1, 2, 0xFF}, engine.MaxValueSize/4)
	key := bytes.Repeat([]byte("k"), engine.MaxKeySize)
	if _, err := db.Put(key, big); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Put([]byte("empty"), []byte{}); err != nil {
		t.Fatal(err)
	}
	db.Close()
	db = mustOpen(t, path)
	if v, found, err := db.Get(key); err != nil || !found || !bytes.Equal(v, big) {
		t.Fatalf("big value: found %v, err %v, equal %v", found, err, bytes.Equal(v, big))
	}
	if v, found, err := db.Get([]byte("empty")); err != nil || !found || len(v) != 0 {
		t.Fatalf("empty value = %q, %v, %v", v, found, err)
	}
}

// Ops in a batch apply in order; the batch gets a single seq (B5).
func TestBatchOrderAndSingleSeq(t *testing.T) {
	db, path := newDB(t)
	db.Put([]byte("y"), []byte("old"))
	seq, err := db.Batch([]engine.Op{put("x", "1"), del("x"), put("y", "2"), put("z", "3"), put("z", "4")})
	if err != nil || seq != 2 {
		t.Fatalf("batch seq %d, err %v; want 2", seq, err)
	}
	want := map[string]string{"y": "2", "z": "4"}
	wantState(t, db, want)
	db.Close()
	wantState(t, mustOpen(t, path), want)
}

func TestEmptyBatchWritesNothing(t *testing.T) {
	db, path := newDB(t)
	before := fileSize(t, path)
	if seq, err := db.Batch(nil); err != nil || seq != 0 {
		t.Fatalf("empty batch = %d, %v; want 0, nil", seq, err)
	}
	if fileSize(t, path) != before || db.Stats().NextSeq != 1 {
		t.Fatal("empty batch changed the file or the seq")
	}
}

// Every rejected write must leave the file and the seq untouched.
func TestValidationWritesNothing(t *testing.T) {
	db, path := newDB(t)
	db.Put([]byte("a"), []byte("1"))
	before, seqBefore := fileSize(t, path), db.Stats().NextSeq

	tooMany := make([]engine.Op, engine.MaxBatchOps+1)
	for i := range tooMany {
		tooMany[i] = put(fmt.Sprint(i), "v")
	}
	mib := string(make([]byte, engine.MaxValueSize))
	tooBig := make([]engine.Op, engine.MaxBatchPayload/engine.MaxValueSize+1)
	for i := range tooBig {
		tooBig[i] = put(fmt.Sprint(i), mib)
	}
	longKey := bytes.Repeat([]byte("k"), engine.MaxKeySize+1)
	bigValue := make([]byte, engine.MaxValueSize+1)

	cases := []struct {
		name string
		do   func() (uint64, error)
		want error
	}{
		{"put empty key", func() (uint64, error) { return db.Put(nil, []byte("v")) }, engine.ErrEmptyKey},
		{"put long key", func() (uint64, error) { return db.Put(longKey, []byte("v")) }, engine.ErrKeyTooLarge},
		{"put big value", func() (uint64, error) { return db.Put([]byte("k"), bigValue) }, engine.ErrValueTooLarge},
		{"del empty key", func() (uint64, error) { return db.Delete([]byte{}) }, engine.ErrEmptyKey},
		{"del long key", func() (uint64, error) { return db.Delete(longKey) }, engine.ErrKeyTooLarge},
		{"batch too many ops", func() (uint64, error) { return db.Batch(tooMany) }, engine.ErrBatchTooLarge},
		{"batch too many bytes", func() (uint64, error) { return db.Batch(tooBig) }, engine.ErrBatchTooLarge},
		{"batch empty key", func() (uint64, error) { return db.Batch([]engine.Op{put("a", "2"), put("", "v")}) }, engine.ErrEmptyKey},
		{"batch big value", func() (uint64, error) {
			return db.Batch([]engine.Op{{Kind: engine.OpPut, Key: []byte("k"), Value: bigValue}})
		}, engine.ErrValueTooLarge},
	}
	for _, tc := range cases {
		if _, err := tc.do(); !errors.Is(err, tc.want) {
			t.Errorf("%s: err = %v, want %v", tc.name, err, tc.want)
		}
	}
	if _, err := db.Batch([]engine.Op{{Kind: 9, Key: []byte("k")}}); err == nil {
		t.Error("batch with unknown op kind: want error")
	}
	if fileSize(t, path) != before || db.Stats().NextSeq != seqBefore {
		t.Fatal("a rejected write changed the file or the seq")
	}
	wantState(t, db, map[string]string{"a": "1"})
}

func TestDeadBytesAccounting(t *testing.T) {
	db, _ := newDB(t)
	db.Put([]byte("a"), []byte("1"))
	if d := db.Stats().DeadBytes; d != 0 {
		t.Fatalf("dead after first put = %d, want 0", d)
	}
	db.Put([]byte("a"), []byte("2"))
	putLen := int64(len(engine.EncodePut(1, []byte("a"), []byte("1"))))
	if d := db.Stats().DeadBytes; d != putLen {
		t.Fatalf("dead after overwrite = %d, want %d", d, putLen)
	}
	db.Delete([]byte("a"))
	delLen := int64(len(engine.EncodeDel(1, []byte("a"))))
	if s := db.Stats(); s.DeadBytes != s.FileBytes-engine.FileHeaderSize || s.DeadBytes != 2*putLen+delLen {
		t.Fatalf("after delete everything is dead: stats = %+v", s)
	}
}

func TestClosedDBRefusesWork(t *testing.T) {
	db, _ := newDB(t)
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Put([]byte("a"), []byte("1")); !errors.Is(err, engine.ErrClosed) {
		t.Errorf("Put after Close: %v", err)
	}
	if _, _, err := db.Get([]byte("a")); !errors.Is(err, engine.ErrClosed) {
		t.Errorf("Get after Close: %v", err)
	}
	if err := db.Close(); !errors.Is(err, engine.ErrClosed) {
		t.Errorf("second Close: %v", err)
	}
}

// Concurrent writers and readers: every acked write is present after reopen
// and every seq is unique.
func TestConcurrentWriters(t *testing.T) {
	db, path := newDB(t)
	const writers, perWriter = 8, 50
	var mu sync.Mutex
	seqs := map[uint64]bool{}
	var wg sync.WaitGroup
	for w := 0; w < writers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := 0; i < perWriter; i++ {
				k := fmt.Sprintf("w%d-%d", w, i)
				seq, err := db.Put([]byte(k), []byte(k))
				if err != nil {
					t.Error(err)
					return
				}
				if v, ok, _ := db.Get([]byte(k)); !ok || string(v) != k {
					t.Errorf("read-your-write failed for %s", k)
				}
				mu.Lock()
				seqs[seq] = true
				mu.Unlock()
			}
		}(w)
	}
	wg.Wait()
	if len(seqs) != writers*perWriter {
		t.Fatalf("%d unique seqs, want %d", len(seqs), writers*perWriter)
	}
	db.Close()
	if n := len(state(t, mustOpen(t, path))); n != writers*perWriter {
		t.Fatalf("%d keys after reopen, want %d", n, writers*perWriter)
	}
}
