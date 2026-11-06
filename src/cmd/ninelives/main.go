// Command ninelives is the CLI for the NineLives database.
// See docs/SPEC.md sections 10-12 for the output and serve contract.
// Skeleton only: commands land in PRs K1 and K2.
package main

import (
	"fmt"
	"os"
)

const usage = `usage:
  ninelives open    <path> [--max-bytes N]
  ninelives put     <path> <key> <value>
  ninelives get     <path> <key>
  ninelives del     <path> <key>
  ninelives batch   <path> <file>
  ninelives dump    <path>
  ninelives check   <path>
  ninelives serve   <path>
  ninelives compact <path>`

var commands = map[string]bool{
	"open": true, "put": true, "get": true, "del": true, "batch": true,
	"dump": true, "check": true, "serve": true, "compact": true,
}

func main() {
	if len(os.Args) < 2 || !commands[os.Args[1]] {
		fmt.Fprintln(os.Stderr, usage)
		os.Exit(2) // usage error
	}
	fmt.Fprintf(os.Stderr, "ninelives %s: not implemented\n", os.Args[1])
	os.Exit(1)
}
