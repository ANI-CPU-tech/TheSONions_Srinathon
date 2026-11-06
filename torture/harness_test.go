package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// A seed must reproduce each writer's op stream exactly (SPEC rule 10).
func TestGeneratorIsDeterministic(t *testing.T) {
	a, b, other := newGenerator(42, 3, 1, "default", 0), newGenerator(42, 3, 1, "default", 0), newGenerator(42, 4, 1, "default", 0)
	same := true
	for range 200 {
		ra, rb, ro := a.next(), b.next(), other.next()
		if !bytes.Equal(ra.wire(), rb.wire()) {
			t.Fatalf("same seed, different ops:\n%s\n%s", ra.wire(), rb.wire())
		}
		same = same && bytes.Equal(ra.wire(), ro.wire())
	}
	if same {
		t.Fatal("iterations 3 and 4 produced the same stream")
	}
}

// Op ids, and so value tags, never repeat across writers or iterations.
func TestOpIDsAreUnique(t *testing.T) {
	seen := map[uint64]bool{}
	for iter := 1; iter <= 3; iter++ {
		for w := 1; w <= writers; w++ {
			g := newGenerator(1, iter, w, "default", 0)
			for range 1000 {
				r := g.next()
				if seen[r.opid] {
					t.Fatalf("op id %d used twice", r.opid)
				}
				seen[r.opid] = true
				for _, o := range r.ops {
					if !o.del && !bytes.HasPrefix(o.value, []byte("w")) {
						t.Fatalf("value %q has no tag", o.value)
					}
				}
			}
		}
	}
}

func TestBatchWireFormat(t *testing.T) {
	r := request{writer: 2, opid: 7, batch: true, ops: []op{
		{key: []byte("a"), value: []byte("1")},
		{del: true, key: []byte("a")},
	}}
	want := "@2:7 batch 2\nput x:61 x:31\ndel x:61\n"
	if got := string(r.wire()); got != want {
		t.Fatalf("wire = %q, want %q", got, want)
	}
}

func TestParseDump(t *testing.T) {
	got, err := parseDump("k1 x:\nx:00ff raw\n")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got["k1"] != "" || got["\x00\xff"] != "raw" {
		t.Fatalf("parsed %q", got)
	}
	if got, err := parseDump(""); err != nil || len(got) != 0 {
		t.Fatalf("empty dump = %v, %v", got, err)
	}
	for _, bad := range []string{"onlykey\n", "a b c\n", "x:zz v\n", "a 1\na 2\n"} {
		if _, err := parseDump(bad); err == nil {
			t.Errorf("parseDump(%q): want error", bad)
		}
	}
}

// buildFakeserve compiles torture/fakeserve into a temp dir.
func buildFakeserve(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "fakeserve")
	if out, err := exec.Command("go", "build", "-o", bin, "./fakeserve").CombinedOutput(); err != nil {
		t.Fatalf("build fakeserve: %v\n%s", err, out)
	}
	return bin
}

// The whole loop against the honest fakeserve: every iteration kills serve
// with SIGKILL, recovers, and logs SENT, ACK and KILL lines.
func TestRunAgainstFakeserve(t *testing.T) {
	if testing.Short() {
		t.Skip("spawns processes")
	}
	c := config{seed: 7, iterations: 3, mode: "default", bin: buildFakeserve(t), dir: t.TempDir()}
	var out bytes.Buffer
	if err := run(c, &out); err != nil {
		t.Fatalf("run: %v\n%s", err, out.String())
	}
	if !strings.Contains(out.String(), "PASS 3/3") {
		t.Fatalf("no PASS line:\n%s", out.String())
	}
	log, err := os.ReadFile(filepath.Join(c.runDir(), "oracle.log"))
	if err != nil {
		t.Fatal(err)
	}
	for prefix, want := range map[string]int{"ITER ": 3, "KILL ": 3, "PASS ": 3} {
		if n := countPrefix(string(log), prefix); n != want {
			t.Errorf("oracle.log has %d %q lines, want %d", n, prefix, want)
		}
	}
	if countPrefix(string(log), "SENT ") == 0 || countPrefix(string(log), "ACK ") == 0 {
		t.Error("oracle.log has no SENT or ACK lines")
	}
}

// A serve that never prints READY, or exits by itself, is a failure, not a
// pass: the tool must see its own SIGKILL end the process.
func TestRunFailsWhenServeMisbehaves(t *testing.T) {
	if testing.Short() {
		t.Skip("spawns processes")
	}
	dir := t.TempDir()
	quits := filepath.Join(dir, "quits")
	if err := os.WriteFile(quits, []byte("#!/bin/sh\necho READY keys=0 recovery_ms=0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	trueBin, err := exec.LookPath("true")
	if err != nil {
		t.Skip("no true binary")
	}
	for bin, want := range map[string]string{
		trueBin: "before printing READY",
		quits:   "exited by itself",
	} {
		c := config{seed: 1, iterations: 1, mode: "default", bin: bin, dir: t.TempDir()}
		var out bytes.Buffer
		err := run(c, &out)
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("bin %s: err = %v, want %q\n%s", filepath.Base(bin), err, want, out.String())
		}
		if !strings.Contains(out.String(), "repro: ./bin/torture --seed 1 --iterations 1") {
			t.Errorf("bin %s: no repro line:\n%s", filepath.Base(bin), out.String())
		}
	}
}

func countPrefix(s, prefix string) int {
	n := 0
	for _, line := range strings.Split(s, "\n") {
		if strings.HasPrefix(line, prefix) {
			n++
		}
	}
	return n
}

// TestFakeserveBugsCaught runs the torture loop against fakeserve with each
// injected bug and asserts the oracle catches it, while --bug=none passes.
// This is the L2 self-test that runs before the real engine is ready.
func TestFakeserveBugsCaught(t *testing.T) {
	if testing.Short() {
		t.Skip("spawns processes")
	}
	bin := buildFakeserve(t)
	for _, bug := range []string{"drop-ack", "partial-batch", "garbage", "stale"} {
		t.Run(bug, func(t *testing.T) {
			c := config{seed: 11, iterations: 5, mode: "default", bin: bin, bug: bug, dir: t.TempDir()}
			var out bytes.Buffer
			if err := run(c, &out); err == nil {
				t.Fatalf("bug %q: expected failure, got pass\n%s", bug, out.String())
			}
		})
	}
	t.Run("none", func(t *testing.T) {
		c := config{seed: 12, iterations: 3, mode: "default", bin: bin, bug: "none", dir: t.TempDir()}
		var out bytes.Buffer
		if err := run(c, &out); err != nil {
			t.Fatalf("bug=none: expected pass, got %v\n%s", err, out.String())
		}
		if !strings.Contains(out.String(), "PASS 3/3") {
			t.Fatalf("bug=none: no PASS line:\n%s", out.String())
		}
		log, err := os.ReadFile(filepath.Join(c.runDir(), "oracle.log"))
		if err != nil {
			t.Fatal(err)
		}
		if countPrefix(string(log), "KILL ") != 3 {
			t.Errorf("oracle.log has %d KILL lines, want 3", countPrefix(string(log), "KILL "))
		}
	})
}
