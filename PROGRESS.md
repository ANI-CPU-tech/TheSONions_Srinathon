# PROGRESS: team dashboard

Owner: integrator (Anirudh). Edited only on `main`, after each merge. Personal detail lives in `progress/<name>.md`.

- **Hackathon start:** 2026-11-06 11:20 IST. Deadline: 2026-11-07 11:20 IST.
- **Last updated:** 2026-11-06 12:30 IST (hour 2).
- **Current phase:** B0 bootstrap + project scaffold done (empty files per responsibility in every track). All three tracks may branch from `main` now.

## PR status

| PR | Owner | Branch | Status | Merged at |
|---|---|---|---|---|
| B0 | Anirudh (integrator) | `main` (direct) | Done: 13 commits pushed to `main` | 2026-11-06 12:16 |
| A1 | Anirudh | `anirudh/A1-codec` | Not started | |
| A2 | Anirudh | `anirudh/A2-write-path` | Not started | |
| A3 | Anirudh | `anirudh/A3-disk-full` | Not started | |
| A4 | Anirudh | `anirudh/A4-compaction` | Not started | |
| A5 | Anirudh | `anirudh/A5-crashpoints-bench` | Not started | |
| A6 | Anirudh | `anirudh/A6-architecture` | Not started | |
| K1 | Khushi | `khushi/K1-cli` | Not started | |
| K2 | Khushi | `khushi/K2-serve` | Not started | |
| K3 | Khushi | `khushi/K3-build-tests` | Not started | |
| K4 | Khushi | `khushi/K4-diagrams` | Not started | |
| K5 | Khushi | `khushi/K5-readme` | Not started | |
| L1 | Likith | `likith/L1-harness` | Not started | |
| L2 | Likith | `likith/L2-verifier-fakeserve` | Not started | |
| L3 | Likith | `likith/L3-modes` | Not started | |
| L4 | Likith | `likith/L4-crash-modes` | Not started | |
| L5 | Likith | `likith/L5-breaking` | Not started | |
| B1 | Anirudh (integrator) | `main` | Not started | |

## Scenario coverage

0 / 32 verified (see `docs/SCENARIOS.md`). A: 0/7, B: 0/6, C: 0/6, D: 0/4, E: 0/5, F: 0/4.

## Open risks

- `docs/brief.pdf` is not in the repo yet; `docs/PROBLEM.md` was written from the master prompt. Add the PDF and diff it against PROBLEM.md.
- CODEOWNERS still has placeholders `@KHUSHI_GH` and `@LIKITH_GH`.
- 2 s recovery at 1M keys is unmeasured until A5; measure early.
- Torture tool must be real-SIGKILL at random delays; fakeserve self-test (L2) is the only check until A2 lands.

## Decisions log

| Date | Decision | Why |
|---|---|---|
| 2026-11-06 | Append-only log + in-memory hash index, Go stdlib only | Brief FAQ says it is enough for full marks; simplest to explain live |
| 2026-11-06 | One record per op/batch, CRC32-C is the commit marker | A batch is atomic because it has one CRC |
| 2026-11-06 | Compaction via temp file + fsync + rename + dir fsync | Atomic swap on Linux/macOS; old file stays authoritative until rename |
| 2026-11-06 | B0 committed straight to `main`, as the integrator asked | Everyone branches from it; later work goes through PRs |
| 2026-11-06 | No AI attribution in commits or PRs (CLAUDE.md rule 11) | Integrator's instruction; prompts are still logged |
| 2026-11-06 | Many small meaningful commits, never squash-merge | Judges read the history |

## Next integration checkpoint

**Hour 6 (2026-11-06 17:20 IST):** first real torture run with A2 + K2 + L1.
