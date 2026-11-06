# BREAKING: how NineLives can die, and why it doesn't lose data

Owner: **Likith** (PR L5). At most 2 pages. Every claim is checked against
`src/engine/`; a wrong crash point costs more than a missing one.

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
| 13 | `compactLocked` `crashpoint("compact_after_dir_fsync")` (`compact.go:68`) | Rename durable, old handle closed, directory fsynced | New file is authoritative; temp files cleaned on next `open()` |
| 14 | `fail` write-error path (`diskfull.go:49`) | Partial/unsynced record past `db.size`, or DB poisoned (`failed=true`) | Truncates the torn tail and keeps serving; if poisoned, returns `ErrFailed` until reopened |

## 2. The ordering argument

Every write does exactly: validate, disk-full guard, `WriteAt` one complete
record, `Sync`, then update the in-memory index and return `seq` (SPEC section 4).
The CRC is the commit marker: a record exists only when its full length is
present and its CRC matches, so a batch is atomic (one record, one CRC).

The index is updated only after the fsync returns, and `seq` is returned only
after the index update, so an op is acked only once it is durable and indexed.
A kill before the fsync leaves either nothing or a torn tail, both of which
recovery truncates; a kill after the fsync leaves a durable record that
recovery applies (in-flight, allowed).

For create and compaction, the new content is written and fsynced to a temp
file (`<path>.init` or `<path>.compact`), then `os.Rename` swaps it over
`<path>`, and `syncDir` fsyncs the parent directory so the rename survives
power loss, not just a process kill. Until the rename the old file is
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
- `syncDir` uses plain `fsync` on the directory, which on macOS does not
  survive power loss without `F_FULLFSYNC` (process kills are fine).
- Compaction temporarily needs about 2x disk space.
- A single batch payload is capped at 64 MiB (`MaxBatchPayload`).
- Torture seed reproducibility covers the op stream and kill delay, but not
  the exact instruction hit, because OS scheduling varies.

## 4. What we tried that failed

No abandoned designs yet. The torture tool has found no confirmed engine
bugs in the fakeserve self-test (PR L2): `--bug=none` passes and every
injected bug (`drop-ack`, `partial-batch`, `garbage`, `stale`) is caught.
This section will be filled with seed, mode, symptom, root cause and fix
commit once L3/L4 real-engine torture runs are possible; they are currently
blocked on the working `ninelives serve` binary (Khushi K1/K2) and
Anirudh's disk-full/compaction/crashpoints work (A3/A4/A5).

## 5. If we had 24 more hours

1. Run L3 (`large-values`, `large-batch`, `disk-full`) and L4
   (`recovery-crash`, `compaction`, `crashpoints`) at 100 iterations each
   against the real engine and record green modes/seeds in `docs/SCENARIOS.md`.
2. Measure and record the 1M-key recovery benchmark against the 2 s limit
   (Anirudh A5) and cross-check it with torture mode `F3`.
3. Run the cross-training crash-here walkthroughs (hour 12 and hour 20) so
   every member can explain the recovery path and answer crash-here questions.
