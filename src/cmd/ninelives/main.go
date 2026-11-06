// Command ninelives is the CLI for the NineLives database.
// See docs/SPEC.md sections 10-12 for the output and serve contract.
package main

import "os"

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

// commands maps each command name to its positional argument count. The
// database path is argument 0 for every command; --max-bytes is a flag and
// never counted (SPEC section 11).
var commands = map[string]int{
	"open": 1, "put": 3, "get": 2, "del": 2, "batch": 2,
	"dump": 1, "check": 1, "serve": 1, "compact": 1,
}

func main() {
	os.Exit(run(os.Args[1:]))
}
