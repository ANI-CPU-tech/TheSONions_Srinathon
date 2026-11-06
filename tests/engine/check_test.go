// Check tests (PR A2): read-only verification used by the CLI and torture.
package engine_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"ninelives/src/engine"
)

func TestCheckOKAfterWrites(t *testing.T) {
	db, path := newDB(t)
	db.Put([]byte("a"), []byte("1"))
	db.Batch([]engine.Op{put("b", "2"), del("a")})
	if err := engine.Check(path); err != nil {
		t.Fatalf("Check on an open, healthy DB: %v", err)
	}
	db.Close()
	if err := engine.Check(path); err != nil {
		t.Fatalf("Check after Close: %v", err)
	}
}

func TestCheckReportsProblemsWithoutFixingThem(t *testing.T) {
	first := engine.EncodePut(1, []byte("a"), []byte("1"))
	second := engine.EncodePut(2, []byte("b"), []byte("2"))
	badCRC := append([]byte(nil), second...)
	badCRC[len(badCRC)-1] ^= 0xFF
	junk := filepath.Join(t.TempDir(), "junk.db")
	if err := os.WriteFile(junk, []byte("garbage garbage garbage!"), 0o644); err != nil {
		t.Fatal(err)
	}

	cases := map[string]struct {
		path string
		want error
	}{
		"torn tail":         {writeLog(t, 1, first, second[:len(second)-3]), engine.ErrCorrupt},
		"bad CRC":           {writeLog(t, 1, first, badCRC), engine.ErrCorrupt},
		"malformed payload": {writeLog(t, 1, seal(engine.RecPut, []byte{5, 0, 'k'})), engine.ErrCorrupt},
		"bad header":        {junk, engine.ErrNotNineLives},
	}
	for name, tc := range cases {
		before := fileSize(t, tc.path)
		if err := engine.Check(tc.path); !errors.Is(err, tc.want) {
			t.Errorf("%s: err = %v, want %v", name, err, tc.want)
		}
		if fileSize(t, tc.path) != before {
			t.Errorf("%s: Check modified the file", name)
		}
	}
}

func TestCheckReportsLeftoverTempFile(t *testing.T) {
	path := writeLog(t, 1, engine.EncodePut(1, []byte("a"), []byte("1")))
	if err := os.WriteFile(path+".compact", nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := engine.Check(path); !errors.Is(err, engine.ErrCorrupt) {
		t.Fatalf("err = %v, want ErrCorrupt for leftover .compact", err)
	}
	if _, err := os.Stat(path + ".compact"); err != nil {
		t.Fatal("Check must not delete the temp file")
	}
}

func TestCheckMissingFile(t *testing.T) {
	if err := engine.Check(filepath.Join(t.TempDir(), "nope.db")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("err = %v, want not-exist", err)
	}
}
