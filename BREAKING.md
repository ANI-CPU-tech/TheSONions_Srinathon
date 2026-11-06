# BREAKING: how NineLives can die, and why it doesn't lose data

Owner: **Likith** (PR L5), finished by Anirudh after the torture track was
handed over. At most 2 pages. Every row was re-checked line by line against
`src/engine/` on 2026-11-06; a wrong crash point costs more than a missing one.
Rows 2-13 are the SPEC section 8 crash points: `NINELIVES_CRASH_AT=<name>`
SIGKILLs the process there, and `torture --mode crashpoints` fires each one.

## 1. Crash points

| # | Function / line | On disk if killed here | What the next `open()` does |
|---|-----------------|------------------------|-----------------------------|
| 1 | `createFile` before `os.Rename` (`create.go:35`) | `<path>.init` complete (header fsynced), `<path>` does not exist | Deletes `.init` and creates the file again; nothing was ever acked |
| 2 | `commit` `crashpoint("before_write")` (`write.go:74`) | No byte of the record is on disk | Ends the scan at the old size; the op was never acked, simply absent |
| 3 | `commit` mid-write (`write.go:75`, `mid_write`/`batch_mid_write`) | A partial record prefix (torn record) | Scans, stops at the torn record, truncates it as a torn tail; not acked |
| 4 | `commit` `crashpoint("after_write")` (`write.go:86`) | Full record in the page cache, not fsynced | Applies it if complete with a good CRC (in-flight, allowed) or truncates the torn tail |
| 5 | `commit` `crashpoint("after_fsync")` (`write.go:92`) | Record durable, not yet indexed or acked | Applies it (durable, in-flight — allowed) |
| 6 | `commit` `crashpoint("after_index")` (`write.go:97`) | Record indexed, not yet returned (never acked) | Applies it; it is durable |
| 7 | `recoverLog` `crashpoint("recovery_mid_scan")` (`recover.go:92`) | Read-only during the scan; nothing changed | Rescans from scratch on the next `open()` |
| 8 | `truncateTail` `crashpoint("recovery_before_truncate")` (`recover.go:113`) | Torn tail still on disk; nothing applied | Scans, stops at the same offset, truncates again (idempotent) |
| 9 | `truncateTail` `crashpoint("recovery_after_truncate")` (`recover.go:117`) | Cut made but maybe not synced | Sees a clean end or the same torn tail; rescans and retries |
| 10 | `copyLive` `crashHit("compact_mid_temp")` (`compact.go:106`) | `<path>.compact` partial (half the live keys); `<path>` untouched | Deletes `.compact`, uses the old authoritative file (C3) |
| 11 | `compactLocked` `crashpoint("compact_after_temp_fsync")` (`compact.go:49`) | `<path>.compact` complete and fsynced; `<path>` untouched | Deletes `.compact`, uses the old file (C3) |
| 12 | `compactLocked` `crashpoint("compact_after_rename")` (`compact.go:55`) | `<path>` is the new complete file (renamed, fsynced before rename) | Reads the new file; the rename is visible after a process kill |
| 13 | `compactLocked` `crashpoint("compact_after_dir_fsync")` (`compact.go:68`) | Rename durable, old handle closed, directory fsynced. No temp file is left: the rename moved it | Reads the new file; nothing to clean up |
| 14 | `fail`, before or after its `Truncate(db.size)` (`diskfull.go:53`), after a failed `WriteAt`/`Sync` | A partial or unsynced record past `db.size`, never acked. (The `failed` flag that blocks writes is in memory only and dies with the process) | Applies the record if it is complete with a good CRC (in flight, allowed), else truncates it as a torn tail |

## 2. The ordering argument

Every write does exactly, under the write lock: validate, encode one record
in memory (CRC computed last), disk-full guard, `WriteAt` the whole record at
the end of the file in one call, `Sync` (fsync), update the in-memory index,
maybe auto-compact, return `seq`; `serve` prints `ACK` only after that return
(SPEC section 4, `write.go` `commit`).
The CRC is the commit marker: a record exists only when its full length is
present and its CRC matches, so a batch is atomic (one record, one CRC).

The index is updated only after the fsync returns, and `seq` is returned only
after the index update, so an op is acked only once it is durable and indexed.
A kill before the fsync leaves nothing, a torn prefix (recovery truncates it)
or a complete record in the page cache, which a process kill does not lose
(recovery applies it: the op was in flight, never acked, so either outcome is
allowed). A kill after the fsync leaves a durable record that recovery
applies. We only ever append at the end of the last committed record, so no
kill can damage an acked record; if a write fails, `fail` truncates back
before anything else is appended.

For create and compaction, the new content is written and fsynced to a temp
file (`<path>.init` or `<path>.compact`), then `os.Rename` swaps it over
`<path>`, and `syncDir` fsyncs the parent directory. A process kill never
undoes a rename; the directory fsync is there so power loss cannot either
(designed for, not tested). Until the rename the old file is
untouched and authoritative; after it the new file is complete because it was
fsynced first. Neither temp file is ever authoritative: `Open` deletes both
before it reads `<path>`.

## 3. Known weaknesses

Honest limitations, stated plainly:

- The index lives in RAM and is rebuilt on every `open()`; the process
  itself never loses acknowledged data, but the index is not on disk.
- Recovery is O(file size) until compaction reclaims dead bytes.
- A CRC32-C false accept has odds of about 1 in 2^32 per record.
- Mid-file corruption (bit rot) is not handled: recovery treats any CRC
  mismatch as a torn tail and truncates everything after it.
- One `fsync` per op is slow by design; it is the durability guarantee.
- Power loss is not tested on any platform. On macOS it also depends on
  Go's `File.Sync` issuing `F_FULLFSYNC` for files and the directory, which
  we have not verified. Process kills, which the brief tests, are covered.
- Compaction temporarily needs about 2x disk space: `--max-bytes` caps each
  file, and during compaction the old file and `<path>.compact` both exist.
- A failing auto-compaction (for example over the cap, E4) is retried after
  every write, with no back-off (marked `shortcut:` in `maybeCompact`).
- Damage from outside is not repaired: a file cut by hand loses its last
  record even if it was acked (F4), and a header-only file cut to 23 bytes
  fails `open()` with `ErrNotNineLives`.
- A single batch payload is capped at 64 MiB (`MaxBatchPayload`).
- Torture seed reproducibility covers the op streams and kill delays, but
  not the exact instruction hit, because OS scheduling varies. In
  `recovery-crash` the seed picks a fraction of the last `open()`'s run time,
  so the absolute delay also depends on machine speed.

## 4. What we tried that failed

- **Poison the DB on any failed write** (first A3 design): abandoned. A
  short write from ENOSPC would have made the DB refuse all writes until a
  restart. Now `fail` truncates back to the last committed size and fsyncs;
  only if that fails does the DB stop writing (commit 05ee30c).
- **Torture modes that passed without testing anything.** On the real
  engine, `disk-full` showed `err=0` on every run: its 4 MiB cap was never
  reached, because compaction kept 64 small keys far below it. `large-values`
  used 64 KiB values and `large-batch` 100 ops, not the brief's 1 MB and
  10,000. Fixed: 128 KiB cap (seed 2024 now gets 3,048 DISKFULL refusals in
  100 iterations), values up to exactly 1 MiB, batches of exactly 10,000.
- **Fixed recovery-kill delays** (0-20 ms) never landed: `open()` of our
  files takes a few ms, so in the first trial none of the kills we looked at
  (3 iterations) landed. Now the seed picks a fraction
  of the last `open()`'s run time: 99 of 188 kills land in seed 42's run.
- **Crash points that could never fire:** `recovery_*_truncate` armed with
  `NINELIVES_CRASH_AFTER` 2-3, but an `open()` truncates at most once.
- **Verifier false positive** (Likith, found by `large-batch` on
  fakeserve): a key whose last ACKed op was a put, with a delete in flight,
  was wrongly failed when absent. Rule 4 now allows the in-flight ABSENT.
- **Serve applied part of a malformed batch** (Khushi, own tests, before
  torture): `readBatchBlock` returned the valid ops of a batch with a bad
  line. Fixed in 3ee1191: the whole batch is refused.
- **Engine bugs found by torture: none so far.** Green on real disk:
  `default` seed 12345 (500 iterations), `large-values` 999, `large-batch`
  31337, `disk-full` 2024, `recovery-crash` 42, `compaction` 7 (100 each),
  `crashpoints` 1306 (130: each of the 13 points 10 times, 91 fired).

## 5. If we had 24 more hours

1. Tell a torn tail from mid-file corruption: after a bad CRC, scan on for
   a later valid record, and if one exists refuse to open (report it)
   instead of truncating acked data.
2. Test power loss for real: run the torture loop in a VM that is hard-reset
   instead of SIGKILLing a process, or replay writes with dm-log-writes.
3. Back off a failing auto-compaction, and count both files against
   `--max-bytes` while compaction runs.
