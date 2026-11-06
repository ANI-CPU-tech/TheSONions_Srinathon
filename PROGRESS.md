# PROGRESS: team dashboard

Owner: integrator (Anirudh). Edited only on `main`, after each merge. Personal detail lives in `progress/<name>.md`.

- **Hackathon start:** 2026-11-06 11:20 IST. Deadline: 2026-11-07 11:20 IST.
- **Last updated:** 2026-11-06 17:10 IST (hour 6).
- **Current phase:** every deliverable is built, merged and green. Left: demo prep (crash-here questions, design extension), the second-laptop clean-clone test, and a last `scripts/merge-prompts.sh` run before the deadline.

## PR status

| PR | Owner | Branch | Status | Merged at |
|---|---|---|---|---|
| B0 | Anirudh (integrator) | `main` (direct) | Done: bootstrap + scaffold | 2026-11-06 12:16 |
| A1 | Anirudh | `anirudh/A1-codec` | Merged (#1) | 12:41 |
| A2 | Anirudh | `anirudh/A2-write-path` | Merged (#2) | 12:53 |
| A3 | Anirudh | `anirudh/A3-disk-full` | Merged (#3) | 13:01 |
| A4 | Anirudh | `anirudh/A4-compaction` | Merged (#4) | 13:17 |
| A5 | Anirudh | `anirudh/A5-crashpoints-bench` | Merged (#5) | 13:28 |
| A6 | Anirudh | `anirudh/A6-architecture` | Merged inside #6 (L1 was branched from it) | 14:38 |
| K1-K5 | Khushi | `khushi` (one branch for all five, per her instruction) | Merged (#7) | 15:47 |
| L1 | Anirudh (took over from Likith) | `anirudh/L1-harness` | Merged (#6) | 14:38 |
| L2, L3, L5 draft | Likith | `likith` | Merged (#8) | 15:26 |
| L4, L3 fixes, L5 | Anirudh (took over) | `anirudh/L4-L5-modes-docs` | Merged (#9) | 16:21 |
| B1 | Anirudh (integrator) | `main` | Done: `scripts/merge-prompts.sh`, 53 prompts merged into PROMPTS.md; rerun before the deadline | local, not pushed |

## Scenario coverage

30 / 32 PASS (see `docs/SCENARIOS.md`). A: 7/7, B: 6/6, C: 5/6, D: 3/4, E: 5/5, F: 4/4. NOT VERIFIED: C6 (kill inside `close()`) and D4 (`get` during a batch): the mechanism exists, no test proves it.

## Deadline checklist (run on a fresh clone of `main`, 2026-11-06)

- [x] One build command works on a clean machine: `scripts/build.sh` in a fresh clone (this laptop). Second laptop with Wi-Fi off: not done yet (Khushi, K5).
- [x] All 8 CLI commands work (plus `serve`), exit codes 0/2/3 checked by hand; `tests/cli` covers the full table.
- [x] `./bin/torture --iterations 500 --seed <any>` runs to completion: see "Judge-style runs" below.
- [x] BREAKING.md has all 5 sections, every crash point rechecked against the code.
- [x] PROMPTS.md exists, with the merged chronological list.
- [x] ARCHITECTURE.md has the diagram, all 32 rows and the 3 guarantees.
- [x] Layout: README.md, ARCHITECTURE.md, BREAKING.md, PROMPTS.md, diagrams/, src/, torture/, tests/ at the root.
- [x] `go.mod` has no `require` line; no non-stdlib import anywhere (`go list` checked).
- [ ] Final commit pushed before the deadline.
- [ ] Laptop charged; Wi-Fi can be turned off.

## Judge-style runs (fresh clone, seeds never used before, NVMe)

All PASS, `./bin/torture --seed <seed> --iterations <n> --mode <mode>` from a fresh clone of local `main` (bd37135); `go test ./...` green in the same clone.

| Mode | Seed | Iterations | Result |
|---|---|---|---|
| default | 80421 | 500 | PASS (112 s) |
| large-values | 5150 | 100 | PASS (51 s) |
| compaction | 777 | 100 | PASS (7 s) |
| recovery-crash | 4711 | 100 | PASS (21 s) |
| large-batch | 2718 | 100 | PASS (27 s) |
| disk-full | 1618 | 100 | PASS (21 s), 6,610 clean DISKFULL refusals |
| crashpoints | 9001 | 130 | PASS (7 s) |

Earlier runs on the same code: default 12345 x500, large-values 999, large-batch 31337, disk-full 2024, recovery-crash 42, compaction 7 (x100 each), crashpoints 1306 x130: all PASS.

## Open risks

- **Planning prompts before the start time.** `prompts/planning-claude-ai.md` has three prompts timed 10:36, 10:44 and 11:17, before the 11:20 start; the first attached the brief. The brief says work done before the event disqualifies. The team must confirm what happened; the log must not be edited to hide it.
- `docs/brief.pdf` is still not in the repo; `docs/PROBLEM.md` was written from the master prompt. Add the PDF and diff it against PROBLEM.md.
- `.github/CODEOWNERS` still has placeholders `@KHUSHI_GH` and `@LIKITH_GH` (real GitHub logins needed).
- TaskSplit.md still names Likith as owner of `torture/**`, BREAKING.md and docs/SCENARIOS.md, though Anirudh did L1, L4 and L5. TaskSplit.md is frozen: a `contract/*` PR approved by all three would fix it.
- Not tested: power loss (any platform), macOS, recovery with a cold page cache (1M keys: 416 ms warm).
- Not prepared yet: the TTL design-extension answer (10 points) and the crash-here walkthroughs (TaskSplit section 6).

## Decisions log

| Date | Decision | Why |
|---|---|---|
| 2026-11-06 | Append-only log + in-memory hash index, Go stdlib only | Brief FAQ says it is enough for full marks; simplest to explain live |
| 2026-11-06 | One record per op/batch, CRC32-C is the commit marker | A batch is atomic because it has one CRC |
| 2026-11-06 | Compaction via temp file + fsync + rename + dir fsync | Atomic swap on Linux/macOS; old file stays authoritative until rename |
| 2026-11-06 | B0 committed straight to `main`, as the integrator asked | Everyone branches from it; later work goes through PRs |
| 2026-11-06 | No AI attribution in commits or PRs (CLAUDE.md rule 11) | Integrator's instruction; prompts are still logged |
| 2026-11-06 | Many small meaningful commits, never squash-merge | Judges read the history |
| 2026-11-06 | A failed write truncates back and fsyncs; the DB stops writing only if that fails | Poisoning on every error would make ENOSPC fatal until restart |
| 2026-11-06 | Anirudh took over the torture track (L1, L4, L5) | Likith's AI tooling stopped working; Likith's L2/L3 were merged as they were |
| 2026-11-06 | Khushi's K1-K5 on one branch, one merge (#7) | Her instruction; commits stay granular inside it |
| 2026-11-06 | Torture modes sized to the brief: 1 MiB values, 10,000-op batches, a disk-full cap that is really hit | The first versions passed without testing what the brief asks |

## Next integration checkpoint

**Before the deadline (2026-11-07, by 11:00 IST):** rerun `scripts/merge-prompts.sh`, rerun the judge-style torture commands on a fresh clone, push `main`.
