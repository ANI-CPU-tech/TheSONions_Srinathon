# Progress: Khushi
Last updated: 2026-11-06 15:04 IST   Branch: khushi   Current PR: K3 (all tasks on one `khushi` branch per human instruction)
## Done
- Setup: fast-forwarded `main` to `origin/main` (A1+A2+A3 merged), created branch `khushi` from it (commit 3000dfd).
- K1: token codec + `open`/`put`/`get`/`del`/`batch`/`dump`/`check`/`compact`, `--max-bytes`, exit codes 0/1/2/3 (commit b4c3b48). Verified by unit tests + manual exit-code matrix; pushed on human request.
- K2: serve protocol per SPEC section 12 (commit 3ee1191): READY, `[@writer:id]` prefixes, goroutine + buffered queue per writer, 4 MiB lines, contiguous batch blocks, stdout mutex, ACK only post-fsync, ERR codes, EOF → Close → exit 0. Verified T1–T10 incl. a 2 MiB line round-trip; found and fixed a partial-batch bug in `readBatchBlock` before commit. Pushed on human request (through 9ddd07d).
- K3: `tests/cli/cli_test.go` integration suite + hardened `scripts/build.sh` (commit 76d642a). Covers every command, the whole exit-code table, token round-trips, dump byte order, batch failure paths, check CORRUPT, serve (READY, auto/explicit ids, batch blocks, whole-batch rejection, BADREQ/DISKFULL/TOOLARGE, 2 MiB line, two-writer FIFO, EOF exit 0) and `TestBuildScriptCleanClone` (working tree copied to temp, `sh scripts/build.sh` there, binary executed — proves the clean-clone build). 15 tests, `-count=5` run green, no flakes.
## In progress
- (none — paused after K3 for human review)
## Next
1. K4: diagrams — `architecture`, `file-layout`, `write-path`, `recovery`, `compaction` `.mmd` sources + PNG export via `npx @mermaid-js/mermaid-cli` (human approved this tool).
2. K5: full README (build, commands with real example output, tokens, serve, on-disk format with a real hex dump, recovery, B5 batch order, disk-full, torture quickstart, platform notes).
## Blockers / needs from others
- Anirudh: `engine.Compact()` still `ErrNotImplemented` (A4 pending) → CLI `compact` exits 1 (tested tolerantly: TestCompact accepts OK once A4 lands), serve `compact` op → `ERR … BADREQ`.
- Likith: torture still a scaffold; serve + CLI ready to be driven.
## Decisions (and why)
- **Single branch `khushi`** with `K<n>:`-style commits instead of TaskSplit §3's one-branch-per-PR: direct human instruction; recorded so the integrator can split later if wanted.
- `compact` prints `OK` / serve answers `ACK <opid> 0` (no record written → no seq). SPEC has no row for compact output.
- `check` failures print `CORRUPT: <reason>` on stdout even for non-corruption reasons (e.g. missing file); exit 1 either way — SPEC §11 lists that as check's output line.
- Malformed token / op / batch line = exit 2 or `ERR BADREQ` (nothing touched); engine rejections exit 1; `ErrDiskFull` exit 3. Serve's ERR uses BADREQ as the protocol's catch-all (only 4 codes exist).
- Batch file ignores blank lines and `#` comments; serve ignores blank/whitespace stdin lines (hand-typing friendliness, SPEC silent).
- Serve auto op-ids start at 1; malformed `@` prefix echoes opid 0. Auto ids are not consumed by explicit-prefix requests.
- Tests assert only what SPEC guarantees: exact lines for single-writer sessions; for two writers, per-writer order + distinct seqs + final state (cross-writer ordering and exact seq values are races — TestServePrefixes/TwoWriters were rewritten after flaky assertions failed, which was a test bug, not a CLI bug).
- `scripts/build.sh` does not force `GOPROXY=off`: with zero dependencies no network is contacted anyway, and forcing it would break a judge laptop whose Go is older than 1.27 (toolchain auto-download needs the proxy).
- No `git push` / GitHub actions unless the human says so (K1 and K2 pushes were explicit exceptions).
## Bugs found by torture
- Not from torture (harness still a scaffold). Bugs found by own testing:
  1. K2 `readBatchBlock` returned valid ops + nil error when a line failed to parse → a malformed batch would have applied its valid subset (partial write). Fixed in 3ee1191 (return `firstErr` after the loop).
  2. K3 test bugs: three `if res := …` shadowing mistakes made assertions read the wrong result, and one assertion expected cross-writer response order (only per-writer order is guaranteed). Fixed in 76d642a; `-count=5` since green.
## Notes for crash-here prep
- serve ACK path: `Put` returns after `WriteAt`+`Sync` (SPEC §4 steps 4–5); `emitf("ACK …")` strictly after → printed ACK = on disk (torture L4 proves it).
- Kill between reader enqueue and worker execution: not applied, no ACK → oracle allows as in-flight.
- EOF shutdown: close(queues) → wg.Wait → db.Close; a kill in that window is an unacked op.
