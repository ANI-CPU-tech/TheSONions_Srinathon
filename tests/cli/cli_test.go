// CLI and serve integration tests (owner: Khushi, PR K3): every command,
// every exit code, token encoding, and the serve protocol (SPEC sections
// 10-12) against a freshly built ninelives binary. The build-from-clean-clone
// test proves scripts/build.sh works with only the Go toolchain present.
package cli_test

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// binPath is the CLI binary, built once in TestMain so every test shares it.
var binPath string

func TestMain(m *testing.M) {
	root, err := filepath.Abs("../..")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	tmp, err := os.MkdirTemp("", "ninelives-cli-test")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	binPath = filepath.Join(tmp, "ninelives")
	build := exec.Command("go", "build", "-o", binPath, "./src/cmd/ninelives")
	build.Dir = root
	if out, err := build.CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "building the CLI: %v\n%s", err, out)
		os.RemoveAll(tmp)
		os.Exit(1)
	}
	code := m.Run()
	os.RemoveAll(tmp)
	os.Exit(code)
}

// --- helpers ---------------------------------------------------------------

// result is one CLI or serve run: both streams and the exit code.
type result struct {
	stdout, stderr string
	code           int
}

// rx makes an exact-match line pattern for expectLines.
func rx(s string) string { return "^" + regexp.QuoteMeta(s) + "$" }

// dbPath returns a fresh database path inside the test's temp dir.
func dbPath(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "test.db")
}

// runCLI runs the CLI synchronously and captures stdout, stderr and the
// exit code (SPEC section 11).
func runCLI(t *testing.T, args ...string) result {
	t.Helper()
	cmd := exec.Command(binPath, args...)
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	res := result{}
	err := cmd.Run()
	if err != nil {
		var ee *exec.ExitError
		if !errors.As(err, &ee) {
			t.Fatalf("running %v: %v", args, err)
		}
		res.code = ee.ExitCode()
	}
	res.stdout, res.stderr = out.String(), errb.String()
	return res
}

// runServe feeds input on stdin until EOF and captures the output. Every
// serve test therefore also proves the EOF shutdown exits 0 (SPEC section 12).
func runServe(t *testing.T, db, input string, flags ...string) result {
	t.Helper()
	args := append([]string{"serve", db}, flags...)
	cmd := exec.Command(binPath, args...)
	cmd.Stdin = strings.NewReader(input)
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	res := result{}
	err := cmd.Run()
	if err != nil {
		var ee *exec.ExitError
		if !errors.As(err, &ee) {
			t.Fatalf("running serve: %v", err)
		}
		res.code = ee.ExitCode()
	}
	res.stdout, res.stderr = out.String(), errb.String()
	return res
}

// expectExit asserts the exit code.
func expectExit(t *testing.T, res result, want int) {
	t.Helper()
	if res.code != want {
		t.Errorf("exit code = %d, want %d\nstdout:\n%s\nstderr:\n%s",
			res.code, want, res.stdout, res.stderr)
	}
}

// expectLines asserts stdout line by line; each want is a regexp (use rx()
// for literal lines).
func expectLines(t *testing.T, res result, want ...string) {
	t.Helper()
	got := splitLines(res.stdout)
	if len(got) != len(want) {
		t.Errorf("stdout has %d lines, want %d\ngot:\n%s\nwant:\n%s\nstderr:\n%s",
			len(got), len(want), res.stdout, strings.Join(want, "\n"), res.stderr)
		return
	}
	for i := range want {
		ok, err := regexp.MatchString(want[i], got[i])
		if err != nil {
			t.Fatalf("bad pattern %q: %v", want[i], err)
		}
		if !ok {
			t.Errorf("stdout line %d = %q, want match for %q", i+1, got[i], want[i])
		}
	}
}

// splitLines drops the trailing empty string after the final newline.
func splitLines(s string) []string {
	s = strings.TrimSuffix(s, "\n")
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

// wantStderr asserts that stderr contains substr.
func wantStderr(t *testing.T, res result, substr string) {
	t.Helper()
	if !strings.Contains(res.stderr, substr) {
		t.Errorf("stderr = %q, want it to contain %q", res.stderr, substr)
	}
}

// --- commands --------------------------------------------------------------

// TestOpenAndReopen: create, print the SPEC line, reopen and recover.
func TestOpenAndReopen(t *testing.T) {
	db := dbPath(t)
	res := runCLI(t, "open", db)
	expectExit(t, res, 0)
	expectLines(t, res, `^keys=0 recovery_ms=\d+$`)

	runCLI(t, "put", db, "a", "1")
	runCLI(t, "put", db, "b", "2")
	res = runCLI(t, "open", db)
	expectExit(t, res, 0)
	expectLines(t, res, `^keys=2 recovery_ms=\d+$`)
}

// TestPutGetDel: the happy path and its exact output lines (SPEC section 11).
func TestPutGetDel(t *testing.T) {
	db := dbPath(t)
	steps := []struct {
		args []string
		want string
	}{
		{[]string{"put", db, "user:1", "hello"}, "OK seq=1"},
		{[]string{"get", db, "user:1"}, "hello"},
		{[]string{"get", db, "ghost"}, "NOTFOUND"},
		{[]string{"del", db, "user:1"}, "OK seq=2"},
		{[]string{"get", db, "user:1"}, "NOTFOUND"},
		{[]string{"del", db, "ghost"}, "OK seq=3"}, // deleting a missing key still writes
	}
	for _, s := range steps {
		res := runCLI(t, s.args...)
		expectExit(t, res, 0)
		expectLines(t, res, rx(s.want))
	}
}

// TestExitCodes walks the whole SPEC section 11 exit-code table.
func TestExitCodes(t *testing.T) {
	db := dbPath(t)
	runCLI(t, "put", db, "a", "b")

	tests := []struct {
		name       string
		args       []string
		wantCode   int
		wantStderr string // "" = no assertion
	}{
		{"no command", nil, 2, "usage:"},
		{"unknown command", []string{"frobnicate"}, 2, "unknown command"},
		{"wrong arity", []string{"put", db, "only"}, 2, "expected 3"},
		{"bad token", []string{"get", db, "x:zz"}, 2, "not lowercase hex"},
		{"bad max-bytes", []string{"open", db, "--max-bytes", "abc"}, 2, "non-negative integer"},
		{"unknown flag", []string{"open", db, "--bogus"}, 2, "unknown flag"},
		{"key too large", []string{"put", db, strings.Repeat("k", 2000), "v"}, 1, "key larger than 1024"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			res := runCLI(t, tc.args...)
			expectExit(t, res, tc.wantCode)
			if tc.wantStderr != "" {
				wantStderr(t, res, tc.wantStderr)
			}
		})
	}

	// Disk full is exit code 3.
	small := filepath.Join(t.TempDir(), "small.db")
	res := runCLI(t, "put", small, "k", strings.Repeat("v", 500), "--max-bytes", "100")
	expectExit(t, res, 3)
	wantStderr(t, res, "disk full")

	// check on a missing file: a problem, so "CORRUPT: ..." and exit 1.
	res = runCLI(t, "check", filepath.Join(t.TempDir(), "missing.db"))
	expectExit(t, res, 1)
	if !strings.HasPrefix(res.stdout, "CORRUPT: ") {
		t.Errorf("check stdout = %q, want a CORRUPT: reason", res.stdout)
	}
}

// TestTokenEncoding: SPEC section 10 in both directions.
func TestTokenEncoding(t *testing.T) {
	db := dbPath(t)
	steps := []struct {
		args []string
		want string
	}{
		{[]string{"put", db, "k1", "x:"}, "OK seq=1"}, // empty value spelling
		{[]string{"get", db, "k1"}, "x:"},
		{[]string{"put", db, "k2", "hello"}, "OK seq=2"},            // raw in
		{[]string{"get", db, "k2"}, "hello"},                        // raw out
		{[]string{"put", db, "k3", "x:00ff"}, "OK seq=3"},           // hex in
		{[]string{"get", db, "k3"}, "x:00ff"},                       // hex out (not printable raw)
		{[]string{"put", db, "k4", "x:783a68656c6c6f"}, "OK seq=4"}, // a value that literally starts with x:
		{[]string{"get", db, "k4"}, "x:783a68656c6c6f"},             // must stay hex or it would be ambiguous
	}
	for _, s := range steps {
		res := runCLI(t, s.args...)
		expectExit(t, res, 0)
		expectLines(t, res, rx(s.want))
	}

	// Uppercase hex is not the SPEC spelling: usage error, exit 2.
	res := runCLI(t, "get", db, "x:AB")
	expectExit(t, res, 2)

	// dump encodes: printable space-free value as raw, the rest as hex.
	res = runCLI(t, "dump", db)
	expectExit(t, res, 0)
	expectLines(t, res, rx("k1 x:"), rx("k2 hello"), rx("k3 x:00ff"), rx("k4 x:783a68656c6c6f"))
}

// TestDumpSorted: raw byte order, including non-printable keys.
func TestDumpSorted(t *testing.T) {
	db := dbPath(t)
	for _, kv := range [][2]string{
		{"zeta", "5"},
		{"alpha", "2"},
		{"Beta", "1"},   // 'B' (0x42) < 'a' (0x61)
		{"x:0102", "0"}, // key bytes 0x01 0x02 sort before everything
		{"k", "3"},
	} {
		if res := runCLI(t, "put", db, kv[0], kv[1]); res.code != 0 {
			t.Fatalf("put %v: %v", kv, res)
		}
	}
	res := runCLI(t, "dump", db)
	expectExit(t, res, 0)
	expectLines(t, res,
		rx("x:0102 0"), rx("Beta 1"), rx("alpha 2"), rx("k 3"), rx("zeta 5"))
}

// TestBatch: atomic file batch, then the failure paths write nothing.
func TestBatch(t *testing.T) {
	db := dbPath(t)
	dir := t.TempDir()
	good := filepath.Join(dir, "ops.txt")
	if err := os.WriteFile(good, []byte("put k1 v1\nput k2 v2\ndel k1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	res := runCLI(t, "batch", db, good)
	expectExit(t, res, 0)
	expectLines(t, res, rx("OK seq=1"))

	res = runCLI(t, "dump", db)
	expectExit(t, res, 0)
	expectLines(t, res, rx("k2 v2")) // put X; del X leaves X deleted (B5)

	// A malformed line: exit 2 and nothing written (dump unchanged).
	bad := filepath.Join(dir, "bad.txt")
	if err := os.WriteFile(bad, []byte("put ok v\nfrobnicate x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	res = runCLI(t, "batch", db, bad)
	expectExit(t, res, 2)
	wantStderr(t, res, "bad.txt:2")
	res = runCLI(t, "dump", db)
	expectExit(t, res, 0)
	expectLines(t, res, rx("k2 v2"))

	// Missing file: an I/O error, exit 1.
	res = runCLI(t, "batch", db, filepath.Join(dir, "missing.txt"))
	expectExit(t, res, 1)

	// An empty file is an empty batch: a successful no-op, seq 0.
	empty := filepath.Join(dir, "empty.txt")
	if err := os.WriteFile(empty, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	res = runCLI(t, "batch", db, empty)
	expectExit(t, res, 0)
	expectLines(t, res, rx("OK seq=0"))

	// # comments and blank lines are ignored.
	cmt := filepath.Join(dir, "cmt.txt")
	if err := os.WriteFile(cmt, []byte("# a comment\n\nput k3 v3\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	res = runCLI(t, "batch", db, cmt)
	expectExit(t, res, 0)
	expectLines(t, res, rx("OK seq=2"))
	res = runCLI(t, "dump", db)
	expectExit(t, res, 0)
	expectLines(t, res, rx("k2 v2"), rx("k3 v3"))
}

// TestCheck: OK on a good file, CORRUPT + exit 1 on a damaged one.
func TestCheck(t *testing.T) {
	db := dbPath(t)
	runCLI(t, "put", db, "a", "b")

	res := runCLI(t, "check", db)
	expectExit(t, res, 0)
	expectLines(t, res, rx("OK"))

	// Overwrite the record area with garbage: torn CRC.
	f, err := os.OpenFile(db, os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteAt([]byte("GARBAGEGARBAGEGARBAGE"), 40); err != nil {
		t.Fatal(err)
	}
	f.Close()

	res = runCLI(t, "check", db)
	expectExit(t, res, 1)
	if !strings.HasPrefix(res.stdout, "CORRUPT: ") {
		t.Errorf("check stdout = %q, want a CORRUPT: reason", res.stdout)
	}
}

// TestCompact accepts either outcome while A4 (engine compaction) is
// pending: OK + intact data once it lands, or an honest not-implemented
// error with exit 1 today. It must never claim success without doing the
// work, and must never corrupt the data.
func TestCompact(t *testing.T) {
	db := dbPath(t)
	runCLI(t, "put", db, "a", "1")
	runCLI(t, "put", db, "b", "2")

	res := runCLI(t, "compact", db)
	if res.code == 0 {
		expectLines(t, res, rx("OK"))
	} else {
		expectExit(t, res, 1)
		wantStderr(t, res, "not implemented")
		t.Log("engine Compact() still ErrNotImplemented (A4 pending); CLI behaved correctly")
	}
	// Whatever happened, the data survived and check passes.
	if res := runCLI(t, "dump", db); res.code != 0 {
		t.Fatalf("dump after compact: %v", res)
	} else {
		expectLines(t, res, rx("a 1"), rx("b 2"))
	}
	if res := runCLI(t, "check", db); res.code != 0 {
		t.Fatalf("check after compact failed: %v", res)
	}
}

// --- serve -----------------------------------------------------------------

// TestServeBasic: READY, the full request set with auto op-ids, EOF exit 0,
// and durability of everything that was ACKed.
func TestServeBasic(t *testing.T) {
	db := dbPath(t)
	res := runServe(t, db, strings.Join([]string{
		"put a 1",
		"get a",
		"get ghost",
		"del a",
		"put b x:",
		"get b",
		"stats",
	}, "\n")+"\n")
	expectExit(t, res, 0)
	expectLines(t, res,
		`^READY keys=0 recovery_ms=\d+$`,
		rx("ACK 1 1"),
		rx("VAL 2 1"),
		rx("NOTFOUND 3"),
		rx("ACK 4 2"),
		rx("ACK 5 3"),
		rx("VAL 6 x:"),
		`^STATS 7 keys=1 file_bytes=\d+ dead_bytes=\d+$`)

	// Everything ACKed is on disk after EOF.
	if res := runCLI(t, "check", db); res.code != 0 {
		t.Fatalf("check after serve: %v", res)
	}
	res = runCLI(t, "open", db)
	expectExit(t, res, 0)
	expectLines(t, res, `^keys=1 recovery_ms=\d+$`)
}

// TestServePrefixes: @<writer>:<opid> is echoed back verbatim. Two runs so
// no assertion depends on which of two concurrent writers wins a seq.
func TestServePrefixes(t *testing.T) {
	db := dbPath(t)
	// All three requests on writer 7 so their order is the guaranteed
	// per-writer FIFO; cross-writer ordering is not guaranteed.
	res := runServe(t, db, "@7:42 put a 1\n@7:42 get a\n@7:7 stats\n")
	expectExit(t, res, 0)
	expectLines(t, res,
		`^READY keys=0 recovery_ms=\d+$`,
		rx("ACK 42 1"),
		rx("VAL 42 1"),
		`^STATS 7 keys=1 file_bytes=\d+ dead_bytes=\d+$`)

	// No prefix: writer 0, auto op-id starting at 1.
	res = runServe(t, db, "put b 2\n")
	expectExit(t, res, 0)
	expectLines(t, res,
		`^READY keys=1 recovery_ms=\d+$`,
		rx("ACK 1 2"))
}

// TestServeBatch: contiguous batch blocks, empty batch, get after batch.
func TestServeBatch(t *testing.T) {
	db := dbPath(t)
	res := runServe(t, db, strings.Join([]string{
		"batch 2",
		"put k1 v1",
		"put k2 v2",
		"batch 0",
		"get k1",
	}, "\n")+"\n")
	expectExit(t, res, 0)
	expectLines(t, res,
		`^READY keys=0 recovery_ms=\d+$`,
		rx("ACK 1 1"),
		rx("ACK 2 0"),
		rx("VAL 3 v1"))
}

// TestServeBadRequests: every BADREQ path, then the stream stays usable.
func TestServeBadRequests(t *testing.T) {
	db := dbPath(t)
	res := runServe(t, db, strings.Join([]string{
		"bogus",
		"put x",
		"get x:zz",
		"stats extra",
		"batch -1",
		"@nope put a b",
		"put k v",
	}, "\n")+"\n")
	expectExit(t, res, 0)
	expectLines(t, res,
		`^READY keys=0 recovery_ms=\d+$`,
		rx(`ERR 1 BADREQ bad request: unknown operation "bogus"`),
		rx(`ERR 2 BADREQ bad request: want "put <key> <value>"`),
		rx(`ERR 3 BADREQ bad request: bad key token: token "x:zz" is not lowercase hex (SPEC section 10)`),
		rx(`ERR 4 BADREQ bad request: stats takes no arguments`),
		rx(`ERR 5 BADREQ bad request: batch count must be a non-negative integer, got "-1"`),
		rx(`ERR 0 BADREQ bad request: writer id in "@nope" must be a non-negative integer`),
		rx("ACK 6 1")) // the valid request after five errors still runs
}

// TestServeRejectedBatch: one malformed line rejects the whole batch and
// consumes exactly n lines, so the stream never desyncs.
func TestServeRejectedBatch(t *testing.T) {
	db := dbPath(t)
	res := runServe(t, db, strings.Join([]string{
		"batch 2",
		"put ok v",
		"GARBAGE LINE",
		"get ok",
		"put after yes",
	}, "\n")+"\n")
	expectExit(t, res, 0)
	expectLines(t, res,
		`^READY keys=0 recovery_ms=\d+$`,
		rx(`ERR 1 BADREQ bad request: batch line 2: want "put <key> <value>" or "del <key>", got "GARBAGE LINE"`),
		rx("NOTFOUND 2"), // request 2 got its response: in sync
		rx("ACK 3 1"))
	// Nothing from the rejected batch was written.
	dump := runCLI(t, "dump", db)
	if dump.code != 0 {
		t.Fatalf("dump: %v", dump)
	}
	expectLines(t, dump, rx("after yes"))
}

// TestServeEOFMidBatch: EOF inside a block reports BADREQ and exits 0.
func TestServeEOFMidBatch(t *testing.T) {
	db := dbPath(t)
	res := runServe(t, db, "batch 5\nput a v\n")
	expectExit(t, res, 0)
	expectLines(t, res,
		`^READY keys=0 recovery_ms=\d+$`,
		rx("ERR 1 BADREQ bad request: unexpected EOF inside batch block"))
	dump := runCLI(t, "dump", db)
	if dump.code != 0 {
		t.Fatalf("dump: %v", dump)
	}
	if len(splitLines(dump.stdout)) != 0 {
		t.Errorf("dump = %q, want nothing: the partial batch must not be applied", dump.stdout)
	}
}

// TestServeErrCodes: DISKFULL and TOOLARGE exactly as SPEC section 12.
// FAILED is engine-internal (unrecoverable write error) and cannot be
// triggered from a test without fault injection in the engine (Anirudh's
// crash points, NINELIVES_CRASH_AT).
func TestServeErrCodes(t *testing.T) {
	db := dbPath(t)
	res := runServe(t, db, "put k "+strings.Repeat("v", 500)+"\nget k\n", "--max-bytes", "60")
	expectExit(t, res, 0)
	expectLines(t, res,
		`^READY keys=0 recovery_ms=\d+$`,
		rx("ERR 1 DISKFULL ninelives: disk full (max_bytes reached)"),
		rx("NOTFOUND 2")) // ERR guarantees nothing was written

	db2 := dbPath(t)
	res = runServe(t, db2, "put "+strings.Repeat("k", 2000)+" v\nput ok v\n")
	expectExit(t, res, 0)
	expectLines(t, res,
		`^READY keys=0 recovery_ms=\d+$`,
		rx("ERR 1 TOOLARGE ninelives: key larger than 1024 bytes"),
		rx("ACK 2 1")) // the bad request did not poison the connection
}

// TestServeLargeValue: a 1 MiB value arrives as ~2 MiB of hex on one line
// (SPEC section 12: the reader must accept lines of at least 4 MiB) and
// round-trips byte-exact.
func TestServeLargeValue(t *testing.T) {
	db := dbPath(t)
	hexValue := strings.Repeat("00", 1<<20) // 2 MiB of hex -> 1 MiB of NUL bytes
	res := runServe(t, db, "put big x:"+hexValue+"\nget big\n")
	expectExit(t, res, 0)
	lines := splitLines(res.stdout)
	if len(lines) != 3 {
		t.Fatalf("got %d output lines, want 3 (READY, ACK, VAL):\n%.200s...", len(lines), res.stdout)
	}
	if !regexp.MustCompile(`^READY keys=0 recovery_ms=\d+$`).MatchString(lines[0]) {
		t.Errorf("line 1 = %q", lines[0])
	}
	if lines[1] != "ACK 1 1" {
		t.Errorf("line 2 = %q, want ACK 1 1", lines[1])
	}
	want := "VAL 2 x:" + hexValue
	if lines[2] != want {
		t.Errorf("VAL line is %d bytes, want %d bytes (prefix %q)",
			len(lines[2]), len(want), lines[2][:min(30, len(lines[2]))])
	}
}

// TestServeTwoWriters: distinct id ranges per writer make every response
// attributable; each writer's responses must come back in its own send
// order while the two writers interleave, and the final state must hold
// each writer's last write.
func TestServeTwoWriters(t *testing.T) {
	db := dbPath(t)
	res := runServe(t, db, strings.Join([]string{
		"@0:101 put k0 w0o101",
		"@0:102 put k0 w0o102",
		"@1:201 put k1 w1o201",
		"@1:202 put k1 w1o202",
		"@0:103 put k0 w0o103",
		"@1:203 put k1 w1o203",
		"@0:104 get k0",
		"@1:204 get k1",
	}, "\n")+"\n")
	expectExit(t, res, 0)
	lines := splitLines(res.stdout)
	if len(lines) != 9 {
		t.Fatalf("got %d lines, want 9:\n%s", len(lines), res.stdout)
	}
	if !regexp.MustCompile(`^READY keys=0 recovery_ms=\d+$`).MatchString(lines[0]) {
		t.Fatalf("line 1 = %q, want READY", lines[0])
	}

	// Collect response op-ids per writer and assert per-writer order.
	idOf := regexp.MustCompile(`^(?:ACK|VAL|NOTFOUND|STATS|ERR) (\d+) `)
	var w0, w1 []string
	for _, ln := range lines[1:] {
		m := idOf.FindStringSubmatch(ln)
		if m == nil {
			t.Fatalf("unparsable response line %q", ln)
		}
		switch m[1] {
		case "101", "102", "103", "104":
			w0 = append(w0, ln)
		case "201", "202", "203", "204":
			w1 = append(w1, ln)
		default:
			t.Fatalf("response with unexpected op id: %q", ln)
		}
	}
	expectLines(t, result{stdout: strings.Join(w0, "\n") + "\n"},
		`^ACK 101 \d+$`, `^ACK 102 \d+$`, `^ACK 103 \d+$`, rx("VAL 104 w0o103")) // seqs checked below
	expectLines(t, result{stdout: strings.Join(w1, "\n") + "\n"},
		`^ACK 201 \d+$`, `^ACK 202 \d+$`, `^ACK 203 \d+$`, rx("VAL 204 w1o203"))

	// Exact seq values race between the two writer goroutines, so assert
	// what must hold: every acked write got a distinct seq from 1..6, and
	// each writer's own seqs increase with its own send order.
	seqOf := regexp.MustCompile(`^ACK (\d+) (\d+)$`)
	seen := make(map[string]bool)
	last := map[string]uint64{"101": 0, "201": 0}
	owner := map[string]string{"101": "101", "102": "101", "103": "101", "201": "201", "202": "201", "203": "201"}
	for _, ln := range append(append([]string{}, w0...), w1...) {
		m := seqOf.FindStringSubmatch(ln)
		if m == nil {
			continue // the VAL lines
		}
		seq := m[2]
		if seen[seq] {
			t.Errorf("seq %s assigned twice: %q", seq, ln)
		}
		seen[seq] = true
		w := owner[m[1]]
		var s uint64
		fmt.Sscanf(seq, "%d", &s)
		if s <= last[w] {
			t.Errorf("writer %s seq went backwards: %s then %q", w, seq, ln)
		}
		last[w] = s
	}
	if len(seen) != 6 {
		t.Errorf("got %d distinct acked seqs, want 6 (%v)", len(seen), seen)
	}

	// Final state: each writer's last write won its own key.
	dump := runCLI(t, "dump", db)
	if dump.code != 0 {
		t.Fatalf("dump: %v", dump)
	}
	expectLines(t, dump, rx("k0 w0o103"), rx("k1 w1o203"))
}

// --- build -----------------------------------------------------------------

// TestBuildScriptCleanClone copies the working tree (as a clone would
// contain it) into a temp dir and runs scripts/build.sh there: the K3
// acceptance criterion "build from clean clone" with only the Go toolchain.
func TestBuildScriptCleanClone(t *testing.T) {
	if testing.Short() {
		t.Skip("clean-clone build test skipped in -short mode")
	}
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	clone := t.TempDir()
	copyTree(t, root, clone, map[string]bool{".git": true, "bin": true, "runs": true})

	cmd := exec.Command("sh", "scripts/build.sh")
	cmd.Dir = clone
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("scripts/build.sh in a clean copy: %v\n%s", err, out)
	}
	for _, bin := range []string{"bin/ninelives", "bin/torture"} {
		if fi, err := os.Stat(filepath.Join(clone, bin)); err != nil || fi.Size() == 0 {
			t.Errorf("%s missing or empty after build.sh", bin)
		}
	}
	// The built CLI must run: no args is a usage error, exit code 2.
	cmd = exec.Command("./bin/ninelives")
	cmd.Dir = clone
	err = cmd.Run()
	var ee *exec.ExitError
	if !errors.As(err, &ee) || ee.ExitCode() != 2 {
		t.Errorf("clean-clone bin/ninelives exit = %v, want 2 (usage)", err)
	}
}

// copyTree recursively copies src into dst, skipping the named directories
// (build output, VCS metadata, torture run logs) and keeping file modes so
// scripts stay executable.
func copyTree(t *testing.T, src, dst string, skipDirs map[string]bool) {
	t.Helper()
	err := filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		if d.IsDir() && skipDirs[d.Name()] {
			return filepath.SkipDir
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, info.Mode().Perm())
	})
	if err != nil {
		t.Fatalf("copying the tree: %v", err)
	}
}
