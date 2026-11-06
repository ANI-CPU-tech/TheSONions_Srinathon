# Progress: Anirudh
Last updated: 2026-11-07 00:35 IST   Branch: main   Current PR: none (X3 merged as #12)
## Done
- Miro board (team board uXjVERbFfok=): seven new 3600x1800 frames below the old ones, drawn from the code as it is now: 08 Architecture (as built), 09 System Map, 10 CLI and serve, 11 Torture (as built), 12 nlshell, 13 nlui, 14 What changed (first plan vs as built, with links). Old frames 06 and 07 left untouched; they still show the first plan (16-byte header, LSN = offset, one mutex).
- docs/TESTING.md: the hand-test commands in order (nlshell), with expected output; pushed to main (commit 28bbf8a)
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
- A3 merged to `main` (PR #3, merge 3000dfd)
- A4: crash-safe `Compact` (temp file, fsync, rename, swap, dir fsync), E4 abort (commit 91cf387)
- A4: disk-full guard compacts once and rechecks, E5 (commit 9b3d149)
- A4: auto-compaction trigger after writes; 1M puts + 1M deletes shrink to header only (commit bbb4264)
- A4 tests in `tests/engine/compact_test.go` + E5 in `diskfull_test.go`. `go test ./...` and `-race -short` green.
- A4 merged to `main` (PR #4, merge 563a98f)
- A5: `crashpoint.go` real SIGKILL hook, all 13 SPEC names wired (commit a7be212)
- A5: child-process test kills at every crash point and verifies recovery (commit bd0f7df)
- A5: 1M-key recovery test (< 2 s) and benchmark (commit 8b6c9ec)
- A5 merged to `main` (PR #5, merge 1bd0dbe)
- A6: crash test case after_write/2 (kill after batch WriteAt, before Sync) for row B3 (commit b8ea7f9)
- A6: crash tests pin the in-flight outcome: absent before/during WriteAt, present after fsync (commit 1e3362f)
- A6: ARCHITECTURE.md: Mermaid component + recovery diagrams, byte layout, numbered write order, 32-row proof table, three guarantees (commit 4607f79)
- L1 (taken over from Likith, 2026-11-06 14:23): seeded op stream + tagged values (c4f7e3c), oracle.log (1fd217e), kill/recover loop with open/check/dump and summaries (fc13361), honest fakeserve (9ef96fc), tests (05f7ae4). 8 iterations vs fakeserve: PASS, 9k-42k ops per iteration, 0-1 ops in flight at each kill.
- L4 + L3 fixes (232cc6c, tests 0f7de56): recovery-crash, compaction, crashpoints modes; large-values to 1 MiB, large-batch to 10,000, disk-full cap 128 KiB; ERR other than DISKFULL fails the run.
- L5: BREAKING.md rechecked against code (b0e36f3) and section 4 filled; SCENARIOS.md 30 PASS / 2 NOT VERIFIED; ARCHITECTURE.md Verified-by filled with modes + seeds (90d6bd3).
- Real-disk torture, all PASS: default 12345 x500, large-values 999, large-batch 31337, disk-full 2024 (3048 DISKFULL), recovery-crash 42 (99/188 open kills landed), compaction 7 (x100 each), crashpoints 1306 x130 (91 crash points fired).
- Integration on local `main` (not pushed): PR #9 synced, B1 `scripts/merge-prompts.sh` + PROMPTS.md (54 prompts), PROGRESS.md rewritten, fresh-clone deadline checklist, judge-style torture on new seeds all PASS (bd37135..a4ff291).
- X1: `scripts/nlshell` interactive test menu (b13d7ef, tests ef85496). Checked on a real pty via `script`: suggestions, Tab, arrows, /crash, /serve + !kill, /recover.
## In progress
- X3 watch mode (`scripts/nlui/watch.go`): GET /api/watch polls demo.db read-only every 100 ms and streams changes made from a terminal, with an inferred code path. Done and checked in headless Chromium with real terminal commands (put, batch, mid_write crash, open, compact); docs/TESTING.md updated. Next: push and PR.
- X2 NineLives Lab (`scripts/nlui`, plan approved 22:35): guarded loopback server, read-only DB decoder (engine codec, cross-checked against engine Stats), argv-only job runner (own process group, SIGINT then SIGKILL on cancel), the command catalogue + /api/run (all 13 crash points and F4 pass end to end in the tests), the code-path model + evidence-based trace (tests check every named function and crash point against the source) and the interactive serve session (SIGKILL, stdin EOF; op ids tracked with serve's own rules, checked against the real serve), the torture output parser and the web page (CLI, Serve + kill, Crash lab, Torture, Build + tests; checked in headless Chromium: put, all 14 crash scenarios, serve put/batch/get/SIGKILL/recover, torture 8 iterations, self-test, Stop, light and dark) done; docs/TESTING.md section 7 written. Next: Khushi wires it into build.sh + README (see Blockers), then PR.
## Next
1. Push `main` (7 commits ahead), then `anirudh/X1-test-shell`, PR, merge.
2. Before the deadline: `sh scripts/merge-prompts.sh`, commit, push.
3. Team: settle the three planning prompts timed before 11:20; add docs/brief.pdf; CODEOWNERS logins.
4. Demo prep: crash-here answers, TTL design extension.
## Blockers / needs from others
- Khushi (X2 handoff; her files, I must not edit them): to make nlui part of the judged demo,
  1. `scripts/build.sh`: after `go build -o bin/torture ./torture` add `go build -o bin/nlui ./scripts/nlui`; after the torture `test -x` line add `test -x bin/nlui || { echo "build.sh: bin/nlui was not built" >&2; exit 1; }`; change the last echo to `echo "built bin/ninelives, bin/torture and bin/nlui"`. Nothing reads that echo text (grep checked).
  2. `README.md`: a short "Visual lab (optional)" section: `./bin/nlui` opens a window; 127.0.0.1 only, works offline; every panel prints the exact command so it can be retyped. In Platform notes, change "The runtime never touches the network." to "The database and the torture tool never touch the network; nlui listens on 127.0.0.1 only."
  3. Side finding: the torture flag table in README still says recovery-crash, compaction and crashpoints "land in PR L4 and currently exit 2". They are implemented and pass.
  Risk to state in the PR: with build.sh building nlui, an nlui compile error fails the judged build. nlui is stdlib only and `go test ./scripts/nlui` runs in `go test ./...`.
- Torture owner (Likith per TaskSplit): `repro()` in `torture/report.go` drops `--bug`, so a failing fakeserve self-test run prints a repro that runs fakeserve without the bug. Found while testing nlui; not fixed.
- Khushi (K4): `diagrams/architecture.png`, embedded at the top of ARCHITECTURE.md, does not exist yet. Until it lands the image link is broken; the Mermaid in ARCHITECTURE.md is complete on its own.
- Team: TaskSplit.md still lists Likith as owner of `torture/**`, BREAKING.md, docs/SCENARIOS.md. TaskSplit.md is frozen; the ownership change needs a `contract/*` PR approved by all three.
## Decisions (and why)
- Watch mode infers the code path from the file change only (it never sees the command). Every summary says "inferred". A change is sent only after the file holds still for one poll, so a write caught halfway is not shown as a torn tail. Changes while nlui's own job or serve holds the slot (and 300 ms after) are skipped: that run shows them itself.
- nlui's Code path panel is a hand-written model of the source (trace.go), not a live trace: the engine has no tracing hooks and adding them now would touch the judged write path. The UI says so. Tests check that every function and crash point it names exists, and that each crash point is called inside the function it names. Which steps a run reached comes only from evidence: exit code, armed crash point, output, file before and after.
- nlui (X2) is Go stdlib `net/http` + `embed`, bound to 127.0.0.1 only (no address flag), opened in a Chromium `--app` window. Chosen over Electron: no npm download, no `require`, builds offline. Every /api call needs a per-launch random token; Host and Origin are checked (DNS rebinding, cross-site posts).
- nlshell lives in `scripts/` (integrator-owned, not part of the database) and uses `stty` for raw mode: x/term would break the stdlib-only rule.
- nlshell prints every shell command it runs: the demo is without tools, so it must teach the commands, not hide them.
- Torture: kill delay 1-400 ms, timed from READY, so kills land while both writers are mid-stream (and inside fsyncs), never "after N ops".
- Torture: the run dir `runs/<seed>/` is wiped at start and the DB carries over between iterations, so `--seed S --iterations N` replays everything up to iteration N.
- Torture: op ids are iter*10,000,000 + n*2 + (writer-1): unique for the whole run and independent of scheduling, so tags are reproducible too.
- Torture: a run fails if serve ends any way other than our SIGKILL, or prints a line that answers no op in flight.
- fakeserve built in L1 (honest only) because K2 serve is not ready and the harness needs an end-to-end test; L2 adds the bugs.
- Codec functions are exported: tests live in `tests/engine/` (package `engine_test`), which can only see exported names.
- `MaxRecordPayload = MaxBatchPayload + 4 + MaxBatchOps*7`; `checkBatch` counts key + value bytes against `MaxBatchPayload`.
- Two error kinds: `ErrTornRecord` (recovery truncates there) vs `ErrCorrupt` (CRC good, payload malformed: writer bug). `Open` refuses `ErrCorrupt` instead of silently dropping records.
- The write path applies its own record with `applyRecord`, the same function recovery uses, so live writes and recovery always build the same index.
- Dead bytes = records area minus live bytes. A put's cost is its whole record; a put inside a batch costs its op bytes (7 + key + value). Batch headers and DEL records always count as dead (compaction drops them).
- After a WriteAt/Sync error, `fail` truncates back to `db.size` and fsyncs, so the write never happened and the DB stays usable. Only if that fails does the DB enter `ErrFailed` until reopen (never append after unknown bytes).
- Disk-full guard runs inside `commit`, after encoding, before WriteAt: encoding touches no disk, and the guard needs the exact record length.
- ENOSPC and EFBIG from the OS map to `ErrDiskFull`, so a real full disk behaves like the max_bytes cap.
- Guard: if a record does not fit and there are dead bytes, compact once and recheck (E5). DEL records are dead bytes, so deletes alone can never exhaust the reserve.
- Compaction keeps the temp file's handle and makes it `db.f`, so nothing can fail between the rename and the handle swap. After the rename the DB always switches to the new file.
- Directory fsync failure after the compaction rename sets the failed state: the rename may not be durable, so stop taking writes.
- Compacted records keep each key's original seq; header SeqBase = nextSeq so seqs never go backwards even after trailing deletes are dropped.
- E4: compacted file is checked against MaxBytes (no reserve) while writing; over it, abort with ErrDiskFull, remove the temp file, old file untouched. A put inside a batch costs 7 framing bytes but 23 as a standalone PUT, so compaction can grow a file of big batches.
- Crash-point env vars are read once at start-up (package vars), so an unarmed hook is one string compare. The hit counter is atomic.
- `mid_write` / `batch_mid_write` write the first half of the record with WriteAt, then die. `batch_mid_write` only counts batch records.
- `recovery_mid_scan` fires before applying each record, so NINELIVES_CRASH_AFTER=N dies before record N is applied.
- `compact_mid_temp` flushes the first half of the live keys to `<path>.compact`, then dies (needs at least one live key).
- `recovery_after_truncate` sits after Truncate, before its Sync.
- The 1M benchmark builds the file from encoded PUT records instead of calling Put 1M times: identical bytes, no 1M fsyncs.
- Auto-compaction errors are dropped: the write that triggered it is already durable. No back-off yet (shortcut comment in `maybeCompact`).
- Empty batch: writes nothing, returns seq 0. Delete of a missing key still writes a DEL record (each acked delete has its own seq).
- Validation runs inside the write lock, matching SPEC section 4 step 1 literally.
- `ForEachSorted` holds the read lock while calling fn; fn must not call back into the DB.
- New file SeqBase = 1, so the first record gets seq 1.
- ARCHITECTURE.md cites file + function, never line numbers, so rows stay true when code moves.
- Proof table says "not verified" wherever only a test without a kill, or no test, exists (C6, D2, D4, header-only F4). A wrong claim costs more than a missing one.
- Known edge, not fixed: a header-only file (24 bytes) cut to 23 by hand fails Open with ErrNotNineLives (F4 row says so).
## Measurements
- F3 recovery, 1M keys as 1M separate PUT records (11-byte keys, 100-byte values, 127 MB): open 376 ms on tmpfs, 416 ms on NVMe (`/home`). Warm page cache in both (file just written); cold cache not measured (needs root to drop caches). Benchmark: about 408 ms/op, 2.0M allocs/op.
- 1M puts + 1M deletes as 10,000-op batches, then Compact: about 2 s total; file 23 MB after the puts, 24 bytes (header only) at the end.
- Recovery, 1M keys (~11-byte keys, ~17-byte values, written as 100 batches of 10,000, file 35 MB): open = 359 ms on this laptop. Throwaway test, not committed; the real benchmark (individual puts) is A5.
## Bugs found by torture
- (none yet)
## Notes for crash-here prep
- Crash points, in code order: write.go `commit`: before_write, mid_write / batch_mid_write, after_write, after_fsync, after_index. recover.go: recovery_mid_scan (in `recoverLog`), recovery_before_truncate and recovery_after_truncate (in `truncateTail`). compact.go: compact_mid_temp (in `copyLive`), compact_after_temp_fsync, compact_after_rename, compact_after_dir_fsync (in `compactLocked`). `tests/engine/crashpoint_test.go` kills a real child at each one.
- `commit` (write.go): killed before WriteAt: nothing on disk, op absent (never acked). Killed during/after WriteAt, before Sync: record or a prefix is in the page cache; open() applies it if complete with a good CRC (in flight, allowed), else truncates it. Killed after Sync, before ACK: record durable, open() applies it (in flight, allowed).
- `fail` (diskfull.go): killed before or after the Truncate: the file may end in a partial record past the last committed size. It was never acked; open() truncates it as a torn tail (or applies it if complete: allowed, it was in flight).
- `writeCompactFile` (compact.go): killed while writing or before fsync of `<path>.compact`: old `<path>` untouched and authoritative; open() deletes the temp file (C3).
- `compactLocked` (compact.go): killed before rename: same as above, even though the temp file is complete (C3). Killed after rename, before dir fsync: `<path>` is the new file, complete because it was fsynced before the rename (C4, C5). Rename is atomic, so exactly one of old/new is `<path>`.
- `checkRoom` (diskfull.go): no disk change itself; it may run a compaction (see above). ErrDiskFull is returned before WriteAt, so nothing was written.
- `truncateTail` (recover.go): killed before or after Truncate: next open() scans, stops at the same offset, truncates again (idempotent).
- `createFile` (create.go): killed before rename: only `<path>.init`, open() deletes it and recreates. Killed after rename: `<path>` has a complete fsynced header.
- `removeLeftovers` (recover.go): killed mid-remove: temp file still there or gone; next open() removes it again. Temp files are never authoritative.
- codec.go has no syscalls. Any prefix of a record fails `DecodeRecordHeader` or `VerifyRecord`, so recovery treats it as torn.
