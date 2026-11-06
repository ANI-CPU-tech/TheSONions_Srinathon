// Command torture kills ninelives serve at random moments and checks the
// recovered database against its own oracle. See docs/SPEC.md section 13.
// Skeleton only: the harness lands in PR L1.
package main

import (
	"flag"
	"fmt"
	"os"
)

func main() {
	iterations := flag.Int("iterations", 500, "number of kill/restart iterations")
	seed := flag.Int64("seed", 0, "RNG seed (mandatory)")
	mode := flag.String("mode", "default", "default|large-values|large-batch|disk-full|recovery-crash|compaction|crashpoints")
	bin := flag.String("bin", "./bin/ninelives", "path to the ninelives binary")
	dir := flag.String("dir", "runs", "directory for databases and oracle logs")
	maxBytes := flag.Int64("max-bytes", 0, "max database file size passed to serve (0 = unlimited)")
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

	fmt.Fprintf(os.Stderr, "torture: not implemented (iterations=%d seed=%d mode=%s bin=%s dir=%s max-bytes=%d)\n",
		*iterations, *seed, *mode, *bin, *dir, *maxBytes)
	os.Exit(1)
}
