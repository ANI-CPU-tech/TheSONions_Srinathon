// Recovery benchmark (PR A5, scenario F3): open() of a log holding 1M keys
// written as 1M separate PUT records must finish in under 2 s.
package engine_test

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"ninelives/src/engine"
)

const (
	benchKeys     = 1_000_000
	benchValueLen = 100
)

// writeMillionPuts writes the file 1M individual Put calls would leave
// behind (one PUT record each, seq 1..1M) without paying for 1M fsyncs.
func writeMillionPuts(tb testing.TB) string {
	tb.Helper()
	path := filepath.Join(tb.TempDir(), "million.db")
	f, err := os.Create(path)
	if err != nil {
		tb.Fatal(err)
	}
	w := bufio.NewWriterSize(f, 1<<20)
	w.Write(engine.EncodeFileHeader(1))
	value := bytes.Repeat([]byte("v"), benchValueLen)
	for i := 0; i < benchKeys; i++ {
		w.Write(engine.EncodePut(uint64(i+1), []byte(fmt.Sprintf("key-%07d", i)), value))
	}
	if err := w.Flush(); err != nil {
		tb.Fatal(err)
	}
	if err := f.Close(); err != nil {
		tb.Fatal(err)
	}
	return path
}

func TestRecoveryMillionKeysUnder2s(t *testing.T) {
	if testing.Short() {
		t.Skip("writes a 130 MB file")
	}
	path := writeMillionPuts(t)
	start := time.Now()
	db, err := engine.Open(path, engine.Options{})
	took := time.Since(start)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	s := db.Stats()
	t.Logf("1M keys, %d MB file: open %v (Stats.RecoveryTime %v)", s.FileBytes>>20, took, s.RecoveryTime)
	if s.Keys != benchKeys || s.NextSeq != benchKeys+1 {
		t.Fatalf("stats = %+v", s)
	}
	if took > 2*time.Second {
		t.Fatalf("recovery took %v, limit is 2 s", took)
	}
}

func BenchmarkRecoveryMillionKeys(b *testing.B) {
	path := writeMillionPuts(b)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		db, err := engine.Open(path, engine.Options{})
		if err != nil {
			b.Fatal(err)
		}
		db.Close()
	}
}
