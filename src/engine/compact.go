// Compaction (docs/SPEC.md section 7): live keys to <path>.compact, fsync,
// rename over <path>, fsync directory, swap handles. Auto-trigger by dead-byte
// ratio. PR A4.
package engine
