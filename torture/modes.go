// Modes: large-values, large-batch, disk-full (PR L3); recovery-crash,
// compaction, crashpoints (PR L4).
//
// A mode changes the op stream (value sizes, batch sizes, compact ops), the
// serve config (a small --max-bytes) or how and where the kill happens. The
// dump-vs-oracle verification (verify.go) is the same for every mode:
// refused ops never apply, compact ops change no key, and an op in flight at
// a kill may or may not show (docs/SPEC.md section 13).
package main

import (
	"fmt"
	"math/rand/v2"
	"strings"
	"time"
)

// Mode names (docs/SPEC.md section 13, PRs L3/L4).
const (
	ModeDefault       = "default"
	ModeLargeValues   = "large-values"
	ModeLargeBatch    = "large-batch"
	ModeDiskFull      = "disk-full"
	ModeRecoveryCrash = "recovery-crash"
	ModeCompaction    = "compaction"
	ModeCrashpoints   = "crashpoints"
)

// Limits from docs/SPEC.md section 2, copied rather than imported so the
// tool stays independent of the engine.
const (
	maxValueSize = 1 << 20 // largest value serve accepts: 1 MiB
	maxBatchOps  = 10_000  // most ops in one batch
)

// allModes lists every mode, in the order --help shows them.
var allModes = []string{
	ModeDefault, ModeLargeValues, ModeLargeBatch, ModeDiskFull,
	ModeRecoveryCrash, ModeCompaction, ModeCrashpoints,
}

// validMode reports whether m is a known mode name.
func validMode(m string) bool {
	for _, mode := range allModes {
		if m == mode {
			return true
		}
	}
	return false
}

// fillLen is how many random bytes follow the tag in one put's value.
func fillLen(mode string, tagLen int, rng *rand.Rand) int {
	switch mode {
	case ModeLargeValues:
		most := maxValueSize - tagLen
		if rng.IntN(4) == 0 {
			return most // a value of exactly 1 MiB (A7)
		}
		return rng.IntN(most + 1)
	case ModeDiskFull:
		return rng.IntN(4<<10 + 1) // 64 keys x ~2 KiB live is over the put limit of the cap
	case ModeRecoveryCrash:
		return rng.IntN(16<<10 + 1) // a file of several MiB, so recovery takes long enough to kill
	}
	return rng.IntN(maxFill + 1)
}

// batchLen is the number of ops in one batch.
func batchLen(mode string, rng *rand.Rand) int {
	if mode == ModeLargeBatch {
		if rng.IntN(2) == 0 {
			return maxBatchOps // a batch of exactly 10,000 ops (B6)
		}
		return 2 + rng.IntN(maxBatchOps-1)
	}
	return 2 + rng.IntN(maxBatch-1)
}

// compactPercent is the share of requests that are a compact op.
func compactPercent(mode string) int {
	switch mode {
	case ModeCompaction:
		return 5 // each compaction fsyncs a whole file: more would starve the other ops
	case ModeCrashpoints:
		return 10 // the compact_* crash points need compactions to hit
	}
	return 0
}

// diskFullBytes returns the --max-bytes to pass to serve: the flag if set,
// else 128 KiB in disk-full mode, else 0 (unlimited). With 128 KiB, puts may
// use only 64 KiB (the rest is the engine's delete reserve), so the stream
// keeps hitting the cap, deletes free space, and compaction makes room.
func diskFullBytes(mode string, configured int64) int64 {
	if configured > 0 || mode != ModeDiskFull {
		return configured
	}
	return 128 << 10
}

// crashPoints are the engine's crash-point names (docs/SPEC.md section 8).
// crashpoints mode visits them in this order, one per iteration.
var crashPoints = []string{
	"before_write", "mid_write", "after_write", "after_fsync", "after_index",
	"batch_mid_write",
	"compact_mid_temp", "compact_after_temp_fsync", "compact_after_rename", "compact_after_dir_fsync",
	"recovery_mid_scan", "recovery_before_truncate", "recovery_after_truncate",
}

// killPlan is how one iteration kills, beyond the random kill of serve.
// Every field comes from the iteration's seeded RNG.
type killPlan struct {
	serveEnv      []string      // crash point armed in serve
	openEnv       []string      // crash point armed in one extra open() after the kill
	compactN      int           // kill jitter after the Nth compact request (0 = off)
	jitter        time.Duration // how long after that compact request
	recoveryKills []float64     // SIGKILL an open() at these fractions of a normal open()'s run time
}

// newKillPlan draws the mode's kill plan for iteration iter.
func newKillPlan(mode string, iter int, rng *rand.Rand) killPlan {
	var p killPlan
	switch mode {
	case ModeCompaction:
		p.compactN = 1 + rng.IntN(5)
		p.jitter = time.Duration(rng.IntN(3000)) * time.Microsecond
	case ModeRecoveryCrash:
		// Kill, restart, kill during recovery, again (F2): 1 to 3 open()s,
		// each SIGKILLed partway through, before the open that counts.
		for range 1 + rng.IntN(3) {
			p.recoveryKills = append(p.recoveryKills, rng.Float64())
		}
	case ModeCrashpoints:
		name := crashPoints[(iter-1)%len(crashPoints)]
		if strings.HasPrefix(name, "recovery_") {
			// mid_write leaves a torn tail, so open() has a tail to cut.
			p.serveEnv = crashEnv("mid_write", 1+rng.IntN(50))
			p.openEnv = crashEnv(name, crashAfter(name, rng))
		} else {
			p.serveEnv = crashEnv(name, crashAfter(name, rng))
		}
	}
	return p
}

// crashAfter picks NINELIVES_CRASH_AFTER: which hit of the point kills.
func crashAfter(name string, rng *rand.Rand) int {
	switch {
	case name == "recovery_before_truncate", name == "recovery_after_truncate":
		return 1 // an open() truncates at most once
	case strings.HasPrefix(name, "compact_"):
		return 1 + rng.IntN(3) // a few compactions per run
	case name == "batch_mid_write":
		return 1 + rng.IntN(10)
	case name == "recovery_mid_scan":
		return 1 + rng.IntN(20) // per record scanned; compaction keeps the file short
	}
	return 1 + rng.IntN(100) // per op
}

func crashEnv(name string, after int) []string {
	return []string{"NINELIVES_CRASH_AT=" + name, fmt.Sprintf("NINELIVES_CRASH_AFTER=%d", after)}
}

// String is the plan for oracle.log and the summary line.
func (p killPlan) String() string {
	var parts []string
	if p.serveEnv != nil {
		parts = append(parts, "serve:"+envPoint(p.serveEnv))
	}
	if p.openEnv != nil {
		parts = append(parts, "open:"+envPoint(p.openEnv))
	}
	if p.compactN > 0 {
		parts = append(parts, fmt.Sprintf("kill %dus after compact #%d", p.jitter.Microseconds(), p.compactN))
	}
	for _, f := range p.recoveryKills {
		parts = append(parts, fmt.Sprintf("open-kill@%.0f%%", f*100))
	}
	return strings.Join(parts, " ")
}

// envPoint shows a crash env as name/after.
func envPoint(env []string) string {
	name := strings.TrimPrefix(env[0], "NINELIVES_CRASH_AT=")
	return name + "/" + strings.TrimPrefix(env[1], "NINELIVES_CRASH_AFTER=")
}

// killRecoveries runs the plan's extra open()s after serve's kill: open()s
// SIGKILLed partway through (recovery-crash), or one open() with a recovery
// crash point armed (crashpoints). Each must die by SIGKILL or finish
// cleanly; the normal open, check, dump follow. openTime is how long the
// last full open() took: the seed picks the fraction, the machine's speed
// sets the actual delay, so the kill lands inside open()'s run time.
func killRecoveries(c config, o *oracle, p killPlan, openTime time.Duration) (string, error) {
	landed := 0
	for i, f := range p.recoveryKills {
		d := max(time.Duration(f*float64(openTime)), time.Microsecond)
		killed, err := openUntilKilled(c, nil, d)
		if err != nil {
			return "", err
		}
		o.note("RECOVERY-KILL %d delay_us=%d killed=%v", i+1, d.Microseconds(), killed)
		if killed {
			landed++
		}
	}
	if len(p.recoveryKills) > 0 {
		return fmt.Sprintf(" (%d/%d open()s killed before finishing)", landed, len(p.recoveryKills)), nil
	}
	if p.openEnv == nil {
		return "", nil
	}
	killed, err := openUntilKilled(c, p.openEnv, 0)
	if err != nil {
		return "", err
	}
	o.note("RECOVERY-CRASHPOINT %s killed=%v", envPoint(p.openEnv), killed)
	return map[bool]string{true: ", open hit", false: ", open missed"}[killed], nil
}
