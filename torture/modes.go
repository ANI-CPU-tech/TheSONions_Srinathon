// Modes: large-values, large-batch, disk-full (PR L3); recovery-crash,
// compaction, crashpoints (PR L4).
//
// Each mode tweaks the op stream and/or the serve config produced by the
// generator and run loop. The dump-vs-oracle verification (verify.go) is
// mode-agnostic: refused ops never apply, so a stream run under a disk-full
// cap or with oversized values/batches is still checked against the same
// allowed-value rules (docs/SPEC.md section 13).
package main

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

// implemented reports whether a mode's stream and verification are wired up.
// Real-engine green runs for L3/L4 additionally depend on Anirudh's
// disk-full/compaction/crashpoints work (A3, A4, A5) and Khushi's serve
// implementation (K2), which are not yet available.
func implemented(mode string) bool {
	switch mode {
	case ModeDefault, ModeLargeValues, ModeLargeBatch, ModeDiskFull:
		return true
	}
	return false
}

// maxBatchOps returns the max ops per batch for the mode.
func maxBatchOps(mode string) int {
	if mode == ModeLargeBatch {
		return 100
	}
	return maxBatch
}

// maxValueFill returns the random bytes appended after the value tag for
// the mode (default stays small; large-values uses much larger values).
func maxValueFill(mode string) int {
	if mode == ModeLargeValues {
		return 64 << 10 // 64 KiB values
	}
	return maxFill
}

// validMode reports whether m is a known mode name.
func validMode(m string) bool {
	for _, mode := range []string{
		ModeDefault, ModeLargeValues, ModeLargeBatch, ModeDiskFull,
		ModeRecoveryCrash, ModeCompaction, ModeCrashpoints,
	} {
		if m == mode {
			return true
		}
	}
	return false
}

// diskFullBytes returns the --max-bytes to pass to serve for disk-full mode,
// or the configured value (0 = unlimited) for other modes.
func diskFullBytes(mode string, configured int64) int64 {
	if mode != ModeDiskFull {
		return configured
	}
	if configured > 0 {
		return configured
	}
	return 4 << 20 // 4 MiB default cap so disk-full is exercised
}
