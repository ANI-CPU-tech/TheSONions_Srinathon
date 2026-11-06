// Output: one-line summary per iteration, final pass/fail count, and on the
// first failure what went wrong, the op counts and a repro command. The
// expected-vs-found report from the verifier lands in PR L2.
package main

import (
	"fmt"
	"io"
)

// summary is the one line printed after each passing iteration.
func summary(c config, r result) string {
	return fmt.Sprintf("iter %d/%d ok  delay=%dms sent=%d acked=%d err=%d inflight=%d keys=%d recovery_ms=%s",
		r.iter, c.iterations, r.delay.Milliseconds(), r.sent, r.acked, r.errs, r.flight, r.keys, r.recoveryMS)
}

// reportFailure prints the first failure and returns it.
func reportFailure(out io.Writer, c config, r result, err error) error {
	fmt.Fprintf(out, "FAIL iter %d/%d delay=%dms sent=%d acked=%d err=%d inflight=%d\n",
		r.iter, c.iterations, r.delay.Milliseconds(), r.sent, r.acked, r.errs, r.flight)
	fmt.Fprintf(out, "  %v\n", err)
	if vf, ok := err.(*verifyFailure); ok {
		fmt.Fprintf(out, "  key=%q found=%q expected=%v opids=%v\n", vf.key, vf.found, vf.expect, vf.opids)
	}
	fmt.Fprintf(out, "  oracle log: %s/oracle.log, serve stderr: %s/serve.stderr\n", c.runDir(), c.runDir())
	fmt.Fprintf(out, "  repro: %s\n", repro(c, r.iter))
	fmt.Fprintf(out, "FAILED after %d of %d iterations passed (seed %d, mode %s)\n", r.iter-1, c.iterations, c.seed, c.mode)
	return err
}

// reportSetup prints an error that stopped the run before any iteration.
func reportSetup(out io.Writer, err error) error {
	fmt.Fprintf(out, "FAIL setup: %v\n", err)
	return err
}

// repro is the command that replays the run up to the failing iteration.
// Each iteration starts from the state the previous one left, so the replay
// starts again from iteration 1.
func repro(c config, iter int) string {
	cmd := fmt.Sprintf("./bin/torture --seed %d --iterations %d --mode %s --bin %s --dir %s", c.seed, iter, c.mode, c.bin, c.dir)
	if c.maxBytes > 0 {
		cmd += fmt.Sprintf(" --max-bytes %d", c.maxBytes)
	}
	return cmd
}
