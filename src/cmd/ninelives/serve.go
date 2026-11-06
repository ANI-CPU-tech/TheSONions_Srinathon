// serve protocol (docs/SPEC.md section 12): READY line, per-writer goroutines,
// stdout mutex, 4 MiB lines, batch blocks, ACK only after engine success,
// clean shutdown on stdin EOF. PR K2.
package main
