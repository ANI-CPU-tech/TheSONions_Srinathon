# Progress: Khushi
Last updated: 2026-11-06 15:04 IST   Branch: khushi   Current PR: K3 (all tasks on one `khushi` branch per human instruction)
## Done
- Setup: fast-forwarded `main` to `origin/main` (A1+A2+A3 merged), created branch `khushi` from it (commit 3000dfd).
- K1: token codec + `open`/`put`/`get`/`del`/`batch`/`dump`/`check`/`compact`, `--max-bytes`, exit codes 0/1/2/3 (commit b4c3b48). Verified by unit tests + manual exit-code matrix; pushed on human request.
- K2: serve protocol per SPEC section 12 (commit 3ee1191): READY, `[@writer:id]` prefixes, goroutine + buffered queue per writer, 4 MiB lines, contiguous batch blocks, stdout mutex, ACK only post-fsync, ERR codes, EOF → Close → exit 0. Verified T1–T10 incl. a 2 MiB line round-trip; found and fixed a partial-batch bug in `readBatchBlock` before commit. Pushed on human request (through 9ddd07d).
- K3: `tests/cli/cli_test.go` integration suite + hardened `scripts/build.sh` (commit 76d642a). Covers every command, the whole exit-code table, token round-trips, dump byte order, batch failure paths, check CORRUPT, serve (READY, auto/explicit ids, batch blocks, whole-batch rejection, BADREQ/DISKFULL/TOOLARGE, 2 MiB line, two-writer FIFO, EOF exit 0) and `TestBuildScriptCleanClone` (working tree copied to temp, `sh scripts/build.sh` there, binary executed — proves the clean-clone build). 15 tests, `-count=5` run green, no flakes.
- Error review on human request before K4: fresh uncached gofmt/vet/build/test all clean; `GOFLAGS=-race go test ./tests/cli/` (race-instrumented CLI included) passed with **zero data races** (58 s); all 15 tests + 7 subtests pass verbosely; 5 documented errors (1 real K2 batch bug, 3 test shadowing bugs, 1 cross-writer assertion, plus the K1 pre-commit flag-count issue) all reviewed, fixed and regression-covered.
- K4: five diagrams, Mermaid source + exported PNG (commit e617ce9): `architecture` (every component + global RWMutex), `file-layout` (byte offsets, payloads, commit rule), `write-path` (numbered 1-7 + CRASH comments), `recovery` (SPEC 6 loop, idempotent truncate), `compaction` (temp/fsync/rename/dir-fsync/swap, E4, triggers). Rendered with `npx @mermaid-js/mermaid-cli` (human approved; one-time Chromium install at ~/.cache/puppeteer) and each PNG visually inspected for syntax errors and SPEC agreement. `diagrams/README.md` documents the re-export command and the Miro caveat.
- Merged `origin/main` into `khushi` (commit df53fa6, pushed): A4 compaction, A5 crash points, A6 ARCHITECTURE, L1 torture all landed — `compact` now prints `OK` (measured 151703 → 29024 bytes, check OK) and TestCompact exercises the real path; ARCHITECTURE.md embeds `diagrams/architecture.png`, paths match.
- K5: full README (commit de798f2): build, tour, every command + exit codes, batch/B5, tokens, serve protocol, on-disk format with a hex dump whose 3 CRC32-C values were **recomputed with hash/crc32.Castagnoli** (054fdedf / c73c2871 / e3129ac9 — all match), recovery in plain English, write path, disk-full, compaction (measured), crash points (verified: NINELIVES_CRASH_AT=after_fsync really SIGKILLs, exit 137, write survives), torture quickstart from a real run (PASS 3/3 seed 12345), repo layout, honest platform notes. Embedded diagrams: file-layout, write-path, recovery.
- K5 acceptance test: `/tmp/opencode/readme_check.sh` re-executed every README block from clean state and diffed — **32/32 PASS** after fixing 2 real README issues found this way (recovery_ms variability now documented; usage-block elision shown after token error) plus my checker's own 2 bugs. Full suite re-run after merge: gofmt/vet/build/test all green (engine 7.6s, cli 2.1s, torture 1.5s).
- K5 pushed on human instruction (through 23c9e62).
- Post-L2/L3 integration (next task after K5): merged `origin/main` PR #8 (L2 oracle verifier + L3 modes) into `khushi`, re-captured the torture section — the old "verification lands in PR L2" banner is gone (the verifier now runs every iteration), L3 modes `large-values|large-batch|disk-full` are live, L4 modes exit 2 with an explicit message; fixed a wrong README claim (same seed reproduces **delays** byte-identically — verified twice with seed 12345: 86/249/92 ms both runs — while `sent`/`acked` vary with scheduling, per SPEC §13 rule 10). README self-test re-run: **32/32 PASS**; full suite green (engine 7.7s, cli 2.2s, torture 3.4s).
## In progress
- (none — ALL of K1–K5 complete and pushed; post-L2/L3 README refresh committed locally, awaiting human's push word)
## Next
1. On human instruction: push the README refresh and/or open the PR `khushi` → `main`.
2. Keep syncing with `main` before docs freeze (hour 20): L4 modes and Likith's BREAKING.md/SCENARIOS are still landing; README torture section may need one more refresh when L4 lands.
3. Cross-training prep (TaskSplit §6): crash-here answers — `progress/khushi.md` Notes section has the ACK/EOF/enqueue kill points.
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
- Diagram PNGs exported with mermaid-cli `-b white`; source and PNG committed together, re-export command documented in diagrams/README.md.
- Miro board (https://miro.com/app/board/uXjVERbFfok=/) could not be opened from this machine (needs a login session): diagram-vs-Miro sync is recorded as NOT VERIFIED, honestly; diagrams were instead checked against SPEC sections 3/4/6/7 line by line.
- **K5 honesty gaps (cannot be done from this machine, recorded per CLAUDE rule 10):** (a) the clean-clone test on a *second laptop with Wi-Fi off* was NOT performed — the equivalent local check is `TestBuildScriptCleanClone` (copies the tree to a temp dir and runs build.sh there, offline) plus the README's build block verified verbatim; (b) macOS not tested — README says so explicitly instead of claiming it. Both would need the human's hardware.
- README outputs are verbatim captures; the only normalised value in the self-test is `recovery_ms` (load-dependent), and the README itself documents that variability so a judge is not surprised.
## Bugs found by torture
- Not from torture (harness still a scaffold). Bugs found by own testing:
  1. K2 `readBatchBlock` returned valid ops + nil error when a line failed to parse → a malformed batch would have applied its valid subset (partial write). Fixed in 3ee1191 (return `firstErr` after the loop).
  2. K3 test bugs: three `if res := …` shadowing mistakes made assertions read the wrong result, and one assertion expected cross-writer response order (only per-writer order is guaranteed). Fixed in 76d642a; `-count=5` since green.
## Notes for crash-here prep
- serve ACK path: `Put` returns after `WriteAt`+`Sync` (SPEC §4 steps 4–5); `emitf("ACK …")` strictly after → printed ACK = on disk (torture L4 proves it).
- Kill between reader enqueue and worker execution: not applied, no ACK → oracle allows as in-flight.
- EOF shutdown: close(queues) → wg.Wait → db.Close; a kill in that window is an unacked op.
