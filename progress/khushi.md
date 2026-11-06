# Progress: Khushi
Last updated: 2026-11-06 14:49 IST   Branch: khushi   Current PR: K2 (all tasks on one `khushi` branch per human instruction)
## Done
- Setup: fast-forwarded `main` to `origin/main` (A1+A2+A3 merged), created branch `khushi` from it (commit 3000dfd).
- K1: token codec + `open`/`put`/`get`/`del`/`batch`/`dump`/`check`/`compact`, `--max-bytes`, exit codes 0/1/2/3, `serve` stub (commit b4c3b48). Verified: gofmt/vet/build/test green + manual smoke of every command and exit code. Pushed to `origin/khushi` on human request (commits b4c3b48, 5e7169f, f62fdf3).
- K2: serve protocol per SPEC section 12 (commit 3ee1191): READY line, `[@writer:id]` prefixes with auto writer 0 and auto-incrementing op-id, one goroutine + buffered queue per writer id, single reader, 4 MiB line buffer, contiguous batch blocks, stdout mutex with per-line flush, ACK only after the engine call returns (post-fsync), ERR codes DISKFULL/TOOLARGE/FAILED/BADREQ, EOF → drain queues → Close → exit 0. Verified (T1–T10): basic session, hand-typed no-prefix ops, batch + empty batch, malformed batch line rejected whole (bug found, see below), ERR codes all four, 2 MiB input line → 2 MiB output line byte-exact, two writers (interleaved execution, per-writer order preserved, correct final dump), EOF mid-batch block → ERR + exit 0, blank lines ignored, check OK after serve, durability after EOF.
## In progress
- (none — paused after K2 for human review)
## Next
1. K3: `scripts/build.sh` hardening + CLI/serve integration tests in `tests/cli/` covering every command.
2. K4: diagrams (5 `.mmd` + PNG export via `npx @mermaid-js/mermaid-cli`).
3. K5: full README with a real hex dump of the on-disk format.
## Blockers / needs from others
- Anirudh: `engine.Compact()` still returns `ErrNotImplemented` (A4 pending) → CLI `compact` exits 1 and serve answers `ERR <id> BADREQ ninelives: not implemented` for the `compact` op. Both sides are ready; they flip to `OK` / `ACK <id> 0` when A4 merges. (BADREQ is the spec's catch-all: section 12 defines only DISKFULL|BADREQ|TOOLARGE|FAILED.)
- Likith: torture still a scaffold; serve is ready to be driven (L1 spawns `./bin/ninelives serve`).
## Decisions (and why)
- **Single branch `khushi`** with `K1:`/`K2:`-style commits instead of TaskSplit §3's one-branch-per-PR: direct human instruction; recorded so the integrator can split later if wanted.
- `ninelives compact` prints `OK`; serve `compact` answers `ACK <opid> 0` (no record is written, so there is no seq). SPEC has no row for compact output — documented here for README (K5).
- `check` failures print `CORRUPT: <reason>` on stdout even for e.g. a missing file (SPEC §11 lists that as check's output line); exit 1 either way.
- Malformed token/batch line/batch-file line = usage error (exit 2) or serve `ERR BADREQ`: nothing touched. Engine rejections stay exit 1, `ErrDiskFull` exit 3.
- Batch file ignores blank lines and `#` comments (PROBLEM.md §6 format); serve ignores blank/whitespace-only stdin lines (SPEC silent — friendly to hand-typing).
- Serve auto op-ids start at 1; a malformed `@` prefix echoes opid 0 (the id is unknowable). Explicit `@w` (no `:id`) form accepted as a superset of `@w:id`, opid auto-increments.
- Batch block: always consume exactly n lines even on a parse error, so the stream never desyncs; n > 10000 drained then rejected as TOOLARGE (bounded memory).
- No `git push` / GitHub actions until the human says so (K1 push was an explicit exception).
## Bugs found by torture
- Not from torture (harness still a scaffold). Found by K2 testing before commit: `readBatchBlock` returned valid ops with `nil` error even when a line failed to parse, so `ERR BADREQ` was skipped and the valid subset of a malformed batch was applied — a partial-batch write. Fix: return `firstErr` after the loop (commit 3ee1191). Symptom: `GARBAGE LINE` in a `batch 2` block still printed `ACK 1 1` and `dump` showed the `put` from line 1.
## Notes for crash-here prep
- serve ACK path: `Put` returns only after `file.WriteAt` + `file.Sync` (SPEC §4 steps 4–5); `emitf("ACK ...")` runs strictly after → a printed ACK means the record is on disk even if killed 1 ms later (torture L4 will prove it).
- Kill between reader enqueue and worker execution: request not yet applied, no ACK printed → oracle allows it as in-flight.
- EOF shutdown: close(queues) → wg.Wait (in-flight finish) → db.Close; kill during that window is just an unacked op.
