// Header and record encoding/decoding with CRC32-C (docs/SPEC.md section 3).
// PR A1. Planned: 24-byte file header, 17-byte record header, PUT/DEL/BATCH
// payloads, CRC check that acts as the commit marker.
package engine
