package main

import (
	"bytes"
	"math/rand/v2"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// large-values must reach exactly 1 MiB values (A7) and never go over;
// large-batch must reach exactly 10,000-op batches (B6) and never go over.
func TestLargeModesReachTheLimits(t *testing.T) {
	big, full := 0, 0
	g := newGenerator(5, 1, 1, ModeLargeValues)
	for range 200 {
		for _, o := range g.next().ops {
			if len(o.value) > maxValueSize {
				t.Fatalf("value of %d bytes, over the 1 MiB limit", len(o.value))
			}
			if len(o.value) == maxValueSize {
				big++
			}
		}
	}
	g = newGenerator(5, 1, 1, ModeLargeBatch)
	for range 200 {
		r := g.next()
		if len(r.ops) > maxBatchOps {
			t.Fatalf("batch of %d ops, over the limit", len(r.ops))
		}
		if len(r.ops) == maxBatchOps {
			full++
		}
	}
	if big == 0 || full == 0 {
		t.Fatalf("1 MiB values: %d, 10,000-op batches: %d; want both > 0", big, full)
	}
}

// Only the compaction and crashpoints modes send compact requests.
func TestCompactOnlyInCompactingModes(t *testing.T) {
	for _, mode := range allModes {
		g, n := newGenerator(3, 1, 2, mode), 0
		for range 500 {
			if g.next().compact {
				n++
			}
		}
		if want := mode == ModeCompaction || mode == ModeCrashpoints; (n > 0) != want {
			t.Errorf("mode %s: %d compacts in 500 requests", mode, n)
		}
	}
}

// crashpoints mode visits every crash point once per 13 iterations, and a
// recovery point is armed in open() with a torn tail left by mid_write.
func TestCrashpointsPlanCoversEveryPoint(t *testing.T) {
	seen := map[string]bool{}
	for iter := 1; iter <= len(crashPoints); iter++ {
		p := newKillPlan(ModeCrashpoints, iter, rand.New(rand.NewPCG(1, uint64(iter))))
		name := strings.Split(envPoint(p.serveEnv), "/")[0]
		if p.openEnv != nil {
			if name != "mid_write" {
				t.Errorf("iter %d: serve armed with %s before a recovery point, want mid_write", iter, name)
			}
			name = strings.Split(envPoint(p.openEnv), "/")[0]
		}
		seen[name] = true
	}
	if len(seen) != len(crashPoints) {
		t.Fatalf("visited %d of %d crash points: %v", len(seen), len(crashPoints), seen)
	}
}

// buildNinelives compiles the real CLI into a temp dir.
func buildNinelives(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "ninelives")
	if out, err := exec.Command("go", "build", "-o", bin, "../src/cmd/ninelives").CombinedOutput(); err != nil {
		t.Fatalf("build ninelives: %v\n%s", err, out)
	}
	return bin
}

// Every mode passes a short run against the real engine, and its oracle
// log shows the mode did what it claims: ERR DISKFULL replies, compact
// requests, open()s killed during recovery, crash points that fired.
func TestEveryModeAgainstEngine(t *testing.T) {
	if testing.Short() {
		t.Skip("spawns processes")
	}
	bin := buildNinelives(t)
	cases := []struct {
		mode  string
		iters int
		want  string // a line prefix oracle.log must contain
	}{
		{ModeDefault, 2, "ACK "},
		{ModeLargeValues, 2, "ACK "},
		{ModeLargeBatch, 2, "ACK "},
		{ModeDiskFull, 3, "ERR "},
		{ModeRecoveryCrash, 3, "RECOVERY-KILL "},
		{ModeCompaction, 3, "PLAN kill "},
		{ModeCrashpoints, len(crashPoints), "RECOVERY-CRASHPOINT "},
	}
	for _, tc := range cases {
		t.Run(tc.mode, func(t *testing.T) {
			c := config{seed: 21, iterations: tc.iters, mode: tc.mode, bin: bin, dir: t.TempDir()}
			var out bytes.Buffer
			if err := run(c, &out); err != nil {
				t.Fatalf("run: %v\n%s", err, out.String())
			}
			log, err := os.ReadFile(filepath.Join(c.runDir(), "oracle.log"))
			if err != nil {
				t.Fatal(err)
			}
			if countPrefix(string(log), tc.want) == 0 {
				t.Errorf("oracle.log has no %q line", tc.want)
			}
			if tc.mode == ModeCrashpoints && !strings.Contains(string(log), "self (crash point)") {
				t.Error("no crash point ever fired in serve")
			}
		})
	}
}
