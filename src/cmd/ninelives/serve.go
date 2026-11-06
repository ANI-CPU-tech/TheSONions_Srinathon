// serve protocol (docs/SPEC.md section 12): READY line, per-writer goroutines,
// stdout mutex, 4 MiB lines, batch blocks, ACK only after engine success,
// clean shutdown on stdin EOF. PR K2.
package main

import "errors"

// errServeNotImplemented stands in for the serve protocol until PR K2.
var errServeNotImplemented = errors.New("serve is not implemented yet (PR K2)")

// cmdServe runs the long-lived serve command. K1 ships the CLI only: the
// protocol lands in PR K2, and until then serve exits 1 with "not
// implemented" (SPEC section 11: any error is exit code 1).
func cmdServe(path string, maxBytes int64) error {
	return errServeNotImplemented
}
