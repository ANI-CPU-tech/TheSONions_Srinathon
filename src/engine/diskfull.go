// Disk-full guard (docs/SPEC.md section 4, step 2): max_bytes check with
// DeleteReserve, one compaction retry, then ErrDiskFull. PR A3.
package engine
