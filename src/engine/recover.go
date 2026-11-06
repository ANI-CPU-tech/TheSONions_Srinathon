// Recovery run by Open (docs/SPEC.md section 6): remove leftover temp files,
// validate header, scan records, truncate torn tail + sync, rebuild index.
// PR A2. Must stay under 2 s at 1M keys (benchmark in PR A5).
package engine
