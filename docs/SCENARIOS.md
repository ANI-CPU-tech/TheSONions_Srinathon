# Scenario tracker (A1 to F4)

All 32 scenarios from the brief ([PROBLEM.md](PROBLEM.md) section 5). Owner: **Likith**. Fill "Mechanism" and "Code location" only after checking the real code; ARCHITECTURE.md Part 2 must agree with this file.

Status values: `TODO` (not looked at), `PASS` (verified by the listed torture mode/seed or manual test), `FAIL` (reproduced failure; link the bug in `progress/`), `NOT VERIFIED` (mechanism exists but no test proves it). Never mark `PASS` without a reproducible run.

| ID | Scenario | Expected | Mechanism | Code location | Torture mode/seed | Status |
|---|---|---|---|---|---|---|
| A1 | Kill before any bytes of a put reach the file | Key absent, or holds its previous value | TODO | TODO | TODO | TODO |
| A2 | Kill after some bytes of the record are written (torn write) | Key absent or previous value. Torn bytes are ignored, never returned | TODO | TODO | TODO | TODO |
| A3 | Kill after the record is fully written but before fsync | Either outcome is fine (no success was returned). Nothing else is corrupted | TODO | TODO | TODO | TODO |
| A4 | Kill after fsync returns but before put returns | Key present with the new value | TODO | TODO | TODO | TODO |
| A5 | Kill immediately after put returns success | Key present with the new value. No exceptions | TODO | TODO | TODO | TODO |
| A6 | Kill during a delete | Key still present with its old value, or absent. Never corrupted | TODO | TODO | TODO | TODO |
| A7 | Kill during a put of a 1 MB value | Same rules as A1 to A5 | TODO | TODO | TODO | TODO |
| B1 | Kill after 3 of 10 operations are written | None of the 10 visible | TODO | TODO | TODO | TODO |
| B2 | Kill after all 10 are written but the commit marker is not | None visible | TODO | TODO | TODO | TODO |
| B3 | Kill after the commit marker is written but before fsync | All or none, never some | TODO | TODO | TODO | TODO |
| B4 | Kill after the batch returns success | All 10 visible | TODO | TODO | TODO | TODO |
| B5 | A batch that puts key X and deletes key X | Ordering defined in the README and honoured after a crash | TODO | TODO | TODO | TODO |
| B6 | A batch of 10,000 operations killed halfway | None visible. Recovery still under 2 s | TODO | TODO | TODO | TODO |
| C1 | Kill during open() while it replays the log | Next open() succeeds and reaches the same correct state | TODO | TODO | TODO | TODO |
| C2 | Kill during open() while it truncates or repairs a torn tail | Next open() succeeds | TODO | TODO | TODO | TODO |
| C3 | Kill during compaction, before the new file replaces the old | Old file still authoritative, all data intact | TODO | TODO | TODO | TODO |
| C4 | Kill during compaction, in the middle of the rename or swap | Exactly one of old or new is authoritative, both complete, nothing lost | TODO | TODO | TODO | TODO |
| C5 | Kill after the swap, before temp files are cleaned up | Data intact. Leftover temp files cleaned up or ignored on next open() | TODO | TODO | TODO | TODO |
| C6 | Kill during close() | Same as a kill at any other time | TODO | TODO | TODO | TODO |
| D1 | Two writers putting different keys, killed at random | Every acknowledged put from either writer is present | TODO | TODO | TODO | TODO |
| D2 | Two writers putting the same key, killed at random | The key holds a value one of the writers actually wrote, never a mix | TODO | TODO | TODO | TODO |
| D3 | One writer in a long batch, another doing single puts | Batch all-or-nothing; acknowledged single puts present | TODO | TODO | TODO | TODO |
| D4 | A reader calling get during a batch on the same keys (no crash) | Reader sees the state before or after the batch, never in between | TODO | TODO | TODO | TODO |
| E1 | Cap reached during a single put | put returns an error, the database is still readable, previous data intact | TODO | TODO | TODO | TODO |
| E2 | Cap reached halfway through a batch | Batch returns an error, none of its operations visible | TODO | TODO | TODO | TODO |
| E3 | Cap reached, then process killed | Restart succeeds, all previously acknowledged data intact | TODO | TODO | TODO | TODO |
| E4 | Cap reached during compaction | Compaction fails or completes. Either way no data is lost and the database opens | TODO | TODO | TODO | TODO |
| E5 | Deletes free space, compaction runs, writes work again | Usable again without manual intervention | TODO | TODO | TODO | TODO |
| F1 | 500 consecutive random kills and restarts | Invariant holds after every one | TODO | TODO | TODO | TODO |
| F2 | Kill, restart, kill during recovery, restart, kill during recovery again | Invariant holds; recovery is idempotent | TODO | TODO | TODO | TODO |
| F3 | 1,000,000 keys written, process killed, open() called | Recovery under 2 s, all acknowledged keys present | TODO | TODO | TODO | TODO |
| F4 | Database file truncated by 1 byte at the end by hand | open() succeeds; at most the last unacknowledged record is lost | TODO | TODO | TODO | TODO |

## Group G (not tested by the judge)

- Bit rot in the middle of the file: TODO, describe how CRC32-C per record would detect it and what we do (per SPEC the recovery scan treats any CRC mismatch as a torn tail, so mid-file rot would truncate everything after it; see BREAKING.md weaknesses).
- Network access, multiple files, sharding: out of scope.
- Multi-batch transactions, isolation levels, MVCC: out of scope.
- Performance beyond the 2 s recovery limit: out of scope.
