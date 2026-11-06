// Command torture kills ninelives serve at random moments and checks the
// recovered database against its own oracle. See docs/SPEC.md section 13.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

func main() {
	c := config{}
	flag.IntVar(&c.iterations, "iterations", 500, "number of kill/restart iterations")
	flag.Int64Var(&c.seed, "seed", 0, "RNG seed (mandatory)")
	flag.StringVar(&c.mode, "mode", "default", "default|large-values|large-batch|disk-full|recovery-crash|compaction|crashpoints")
	flag.StringVar(&c.bin, "bin", "./bin/ninelives", "path to the ninelives binary")
	flag.StringVar(&c.dir, "dir", "runs", "directory for databases and oracle logs")
	flag.Int64Var(&c.maxBytes, "max-bytes", 0, "max database file size passed to serve (0 = unlimited)")
	flag.StringVar(&c.bug, "bug", "", "fakeserve injected fault: none|drop-ack|partial-batch|garbage|stale (L2 self-test)")
	flag.Parse()

	seedSet := false
	flag.Visit(func(f *flag.Flag) {
		if f.Name == "seed" {
			seedSet = true
		}
	})
	if !seedSet {
		fmt.Fprintln(os.Stderr, "torture: --seed is mandatory")
		flag.Usage()
		os.Exit(2)
	}
	if c.iterations < 1 {
		fmt.Fprintln(os.Stderr, "torture: --iterations must be at least 1")
		os.Exit(2)
	}
	if !validMode(c.mode) {
		fmt.Fprintf(os.Stderr, "torture: unknown mode %q\n", c.mode)
		flag.Usage()
		os.Exit(2)
	}
	if !implemented(c.mode) {
		fmt.Fprintf(os.Stderr, "torture: mode %q is not wired up yet (PRs L3/L4, needs A4/A5 and K2)\n", c.mode)
		os.Exit(2)
	}
	if err := run(c, os.Stdout); err != nil {
		os.Exit(1)
	}
}

// run does every iteration in order and stops at the first failure. Each
// iteration starts from the database the previous one left, so a fresh run
// directory makes the whole run reproducible from the seed.
func run(c config, out io.Writer) error {
	if err := os.RemoveAll(c.runDir()); err != nil {
		return reportSetup(out, err)
	}
	if err := os.MkdirAll(c.runDir(), 0o755); err != nil {
		return reportSetup(out, err)
	}
	o, err := openOracle(filepath.Join(c.runDir(), "oracle.log"))
	if err != nil {
		return reportSetup(out, err)
	}
	defer o.close()
	o.note("# torture seed=%d mode=%s iterations=%d bin=%s", c.seed, c.mode, c.iterations, c.bin)
	fmt.Fprintf(out, "torture: seed=%d mode=%s iterations=%d, oracle log %s\n",
		c.seed, c.mode, c.iterations, filepath.Join(c.runDir(), "oracle.log"))
	// base is the previously verified dump; iteration 1 starts from nil (SPEC rule 9).
	base := map[string]string{}
	for iter := 1; iter <= c.iterations; iter++ {
		res, err := runIteration(c, o, iter)
		if err != nil {
			o.note("FAIL %d %v", iter, err)
			o.flush()
			return reportFailure(out, c, res, err)
		}
		if err := verify(res.state, o, base); err != nil {
			o.note("FAIL %d %v", iter, err)
			o.flush()
			return reportFailure(out, c, res, err)
		}
		o.note("PASS %d keys=%d", iter, res.keys)
		if err := o.flush(); err != nil {
			return reportSetup(out, err)
		}
		fmt.Fprintln(out, summary(c, res))
		base = res.state // the verified dump becomes the base for the next iteration
	}
	fmt.Fprintf(out, "PASS %d/%d iterations (seed %d, mode %s)\n", c.iterations, c.iterations, c.seed, c.mode)
	return nil
}
