// Command fakeserve speaks the serve protocol with deliberate bugs
// (--bug=none|drop-ack|partial-batch|garbage|stale) to prove the torture
// oracle catches each one. Skeleton only: lands in PR L2.
package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Fprintln(os.Stderr, "fakeserve: not implemented")
	os.Exit(1)
}
