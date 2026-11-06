# Progress: Anirudh
Last updated: 2026-11-06 12:56 IST   Branch: anirudh/A3-disk-full   Current PR: A3
## Done
- B0 bootstrap committed to `main` as integrator (commits a0ca0d9..7eaa156)
- Project scaffold on `main`: empty files per responsibility in src/engine, src/cmd/ninelives, torture, tests, diagrams
- A1 codec merged to `main` (PR #1, merge d4de803): header/record encode, torn + bad-CRC detection, `WalkPayload`
- A2: atomic new-file creation `createFile` + fs helpers (commit c749b65)
- A2: in-memory index with live-byte accounting (commit 4fbeac5)
- A2: `Open` + recovery scan, torn-tail truncate, `Get`/`ForEachSorted`/`Stats`/`Close` (commit 2d72961)
- A2: `Put`/`Delete`/`Batch` with SPEC section 4 order and `// CRASH:` comments (commit a6a4b07)
- A2: read-only `Check` (commit 5595cab)
- A2 tests: `tests/engine/recover_test.go`, `write_test.go`, `check_test.go`. `go test ./...` and `go test -race ./tests/engine/` green.
- A2 merged to `main` (PR #2, merge 8680f22)
- A3: max_bytes guard with DeleteReserve in `checkRoom` (commit 7b0fb87)
- A3: failure rule in `fail`: truncate + fsync after a failed write, poison only if that fails; ENOSPC/EFBIG reported as ErrDiskFull (commit 05ee30c)
- A3 tests in `tests/engine/diskfull_test.go`: E1, E2, E3, delete reserve, real partial write via RLIMIT_FSIZE. Green, also under -race.
## In progress
- (none) A3 committed locally on `anirudh/A3-disk-full`, not pushed, no PR yet.
## Next
1. Push `anirudh/A3-disk-full`, open the A3 PR, merge.
2. As integrator on `main`: update PROGRESS.md (A1, A2 merged; A3 status).
3. A4: compaction (temp file, fsync, rename, dir fsync), auto-trigger, and the "compact once, recheck" retry in `checkRoom`; E4, E5 tests.
## Blockers / needs from others
- (none)
## Decisions (and why)
- Codec functions are exported: tests live in `tests/engine/` (package `engine_test`), which can only see exported names.
- `MaxRecordPayload = MaxBatchPayload + 4 + MaxBatchOps*7`; `checkBatch` counts key + value bytes against `MaxBatchPayload`.
- Two error kinds: `ErrTornRecord` (recovery truncates there) vs `ErrCorrupt` (CRC good, payload malformed: writer bug). `Open` refuses `ErrCorrupt` instead of silently dropping records.
- The write path applies its own record with `applyRecord`, the same function recovery uses, so live writes and recovery always build the same index.
- Dead bytes = records area minus live bytes. A put's cost is its whole record; a put inside a batch costs its op bytes (7 + key + value). Batch headers and DEL records always count as dead (compaction drops them).
- After a WriteAt/Sync error, `fail` truncates back to `db.size` and fsyncs, so the write never happened and the DB stays usable. Only if that fails does the DB enter `ErrFailed` until reopen (never append after unknown bytes).
- Disk-full guard runs inside `commit`, after encoding, before WriteAt: encoding touches no disk, and the guard needs the exact record length.
- ENOSPC and EFBIG from the OS map to `ErrDiskFull`, so a real full disk behaves like the max_bytes cap.
- The compaction retry in the guard waits for A4 (needs compaction). Until then a full DB with dead bytes still returns `ErrDiskFull`; E5 is not met yet.
- Empty batch: writes nothing, returns seq 0. Delete of a missing key still writes a DEL record (each acked delete has its own seq).
- Validation runs inside the write lock, matching SPEC section 4 step 1 literally.
- `ForEachSorted` holds the read lock while calling fn; fn must not call back into the DB.
- New file SeqBase = 1, so the first record gets seq 1.
## Measurements
- Recovery, 1M keys (~11-byte keys, ~17-byte values, written as 100 batches of 10,000, file 35 MB): open = 359 ms on this laptop. Throwaway test, not committed; the real benchmark (individual puts) is A5.
## Bugs found by torture
- (none yet)
## Notes for crash-here prep
- `commit` (write.go): killed before WriteAt: nothing on disk, op absent (never acked). Killed during/after WriteAt, before Sync: record or a prefix is in the page cache; open() applies it if complete with a good CRC (in flight, allowed), else truncates it. Killed after Sync, before ACK: record durable, open() applies it (in flight, allowed).
- `fail` (diskfull.go): killed before or after the Truncate: the file may end in a partial record past the last committed size. It was never acked; open() truncates it as a torn tail (or applies it if complete: allowed, it was in flight).
- `checkRoom` (diskfull.go): no disk change. ErrDiskFull is returned before WriteAt, so nothing was written.
- `truncateTail` (recover.go): killed before or after Truncate: next open() scans, stops at the same offset, truncates again (idempotent).
- `createFile` (create.go): killed before rename: only `<path>.init`, open() deletes it and recreates. Killed after rename: `<path>` has a complete fsynced header.
- `removeLeftovers` (recover.go): killed mid-remove: temp file still there or gone; next open() removes it again. Temp files are never authoritative.
- codec.go has no syscalls. Any prefix of a record fails `DecodeRecordHeader` or `VerifyRecord`, so recovery treats it as torn.
