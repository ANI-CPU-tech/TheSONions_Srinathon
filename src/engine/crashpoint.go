// Crash-point hooks (docs/SPEC.md section 8). They supplement random kills
// for precise tests and demo answers; they never replace them.
package engine

import (
	"os"
	"strconv"
	"sync/atomic"
	"syscall"
)

// Read once at start-up. With NINELIVES_CRASH_AT unset, crashAt is "" and
// every hook is a single string compare that never matches.
var (
	crashAt    = os.Getenv("NINELIVES_CRASH_AT")
	crashAfter = crashAfterEnv()
	crashHits  atomic.Int64
)

// crashAfterEnv parses NINELIVES_CRASH_AFTER (default 1, minimum 1).
func crashAfterEnv() int64 {
	n, err := strconv.ParseInt(os.Getenv("NINELIVES_CRASH_AFTER"), 10, 64)
	if err != nil || n < 1 {
		return 1
	}
	return n
}

// crashpoint SIGKILLs the process when NINELIVES_CRASH_AT equals name, on
// the Nth hit (N from NINELIVES_CRASH_AFTER).
func crashpoint(name string) {
	if crashHit(name) {
		die()
	}
}

// crashHit reports whether this hit of name is the one to die on. Callers
// that must do something first (write half a record) use it, then die().
func crashHit(name string) bool {
	return name == crashAt && crashHits.Add(1) == crashAfter
}

// die sends SIGKILL to this process: a real kill, no deferred functions, no
// flushing, exactly like the torture tool's kill. Never os.Exit.
func die() {
	syscall.Kill(os.Getpid(), syscall.SIGKILL)
	select {} // SIGKILL cannot be caught; wait for it to land
}
