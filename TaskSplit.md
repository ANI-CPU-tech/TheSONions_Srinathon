# TaskSplit: who owns what

> **Status: FROZEN.** Changes only through a `contract/*` PR approved by all three teammates.

**The one rule: every file has exactly one owner, and nobody edits a file they don't own.** The shared contracts ([docs/SPEC.md](docs/SPEC.md) and `src/engine/api.go`) are frozen up front, so all three tracks start in parallel from minute one and never touch the same file.

## 1. Team and roles

| Person | Role | Owns |
|---|---|---|
| **Anirudh** (TeamMate 1) | Storage engine **+ Integrator** | Write path, recovery, compaction, disk-full handling, crash points, ARCHITECTURE.md. As integrator: frozen contract files, PROGRESS.md, PROMPTS.md, prompts/planning-claude-ai.md, go.mod, .gitignore, .github/, merging PRs |
| **Khushi** (TeamMate 2) | CLI, serve and docs | All 8 CLI commands plus `compact`, the serve protocol, the build script, README.md, diagrams/ |
| **Likith** (TeamMate 3) | Torture tool and attack docs | Torture harness, oracle, all modes, fakeserve self-test, BREAKING.md, docs/SCENARIOS.md |

Anirudh holds both the engine role and the integrator role. When acting as integrator (merging, updating PROGRESS.md, contract PRs), Anirudh works on `main` or a `contract/*` branch, never mixing integrator files into an `anirudh/*` feature PR.

## 2. Ownership table

| Path | Owner | Notes |
|---|---|---|
| `MASTER_PROMPT.md` | Integrator (Anirudh) | Bootstrap source |
| `CLAUDE.md` | Integrator (Anirudh) | **FROZEN** |
| `TaskSplit.md` | Integrator (Anirudh) | **FROZEN** |
| `PROGRESS.md` | Integrator (Anirudh) | Edited only on `main`, after each merge |
| `progress/anirudh.md` | Anirudh | |
| `progress/khushi.md` | Khushi | |
| `progress/likith.md` | Likith | |
| `PROMPTS.md` | Integrator (Anirudh) | Index of the prompt logs |
| `prompts/planning-claude-ai.md` | Integrator (Anirudh) | Append-only |
| `prompts/anirudh.md` | Anirudh | Append-only |
| `prompts/khushi.md` | Khushi | Append-only |
| `prompts/likith.md` | Likith | Append-only |
| `README.md` | Khushi | |
| `ARCHITECTURE.md` | Anirudh | |
| `BREAKING.md` | Likith | Verified against Anirudh's code |
| `diagrams/**` | Khushi | Mermaid source + exported PNG |
| `docs/brief.pdf` | Integrator (Anirudh) | Organisers' brief |
| `docs/PROBLEM.md` | Integrator (Anirudh) | **FROZEN** |
| `docs/RULES.md` | Integrator (Anirudh) | **FROZEN** |
| `docs/SPEC.md` | Integrator (Anirudh) | **FROZEN** |
| `docs/SCENARIOS.md` | Likith | 32-row tracker |
| `go.mod` | Integrator (Anirudh) | **FROZEN**, never a `require` line |
| `.gitignore` | Integrator (Anirudh) | |
| `.github/**` | Integrator (Anirudh) | CODEOWNERS |
| `scripts/build.sh` | Khushi | |
| `scripts/check-ownership.sh`, `scripts/merge-prompts.sh` | Integrator (Anirudh) | Optional, later |
| `src/engine/api.go` | Integrator (Anirudh) | **FROZEN** contract |
| `src/engine/**` (everything else) | Anirudh | |
| `src/cmd/ninelives/**` | Khushi | |
| `torture/**` | Likith | Includes `torture/fakeserve/` |
| `tests/engine/**` | Anirudh | |
| `tests/cli/**` | Khushi | |

Frozen files: `CLAUDE.md`, `TaskSplit.md`, `docs/PROBLEM.md`, `docs/RULES.md`, `docs/SPEC.md`, `src/engine/api.go`, `go.mod`.

## 3. Branch and PR rules

- Branch names: `<name>/<PR-id>-<topic>`, for example `anirudh/A2-write-path` or `likith/L1-harness`.
- One PR per row in section 4. Keep PRs small and merge them often.
- A PR may only touch paths owned by its author. The integrator rejects any PR that touches other paths. A `scripts/check-ownership.sh` owned by the integrator may automate this.
- Before opening a PR, rebase on `origin/main` and run `gofmt -l .`, `go vet ./...`, `go build ./...` and `go test ./...`.
- Merge with merge commits or rebase-merge, **never squash**, so judges see the real history.
- Contract changes go in a separate tiny PR, `contract/<topic>`, touching only frozen files. It needs approval from all three, merges first, and everyone rebases afterwards.
- Need something from another person's area? Open a GitHub issue or message them with the exact request. Never edit their files.
- Each teammate edits only their own `progress/<name>.md` and `prompts/<name>.md`. Only the integrator edits PROGRESS.md, directly on `main` after each merge.
- Commit often: one logical step per commit, message says what and why. No AI attribution lines in commits or PRs.

## 4. PR plan per person

Every PR lists: ID, title, owned files touched, acceptance criteria, depends-on, target hour (hours since start, 2026-11-06 11:20 IST).

### Anirudh: engine (`src/engine/**` except `api.go`, `tests/engine/**`, `ARCHITECTURE.md`)

| PR | Content | Files | Acceptance | Depends on | Target hour |
|---|---|---|---|---|---|
| A1 | Record and header codec with CRC32-C and unit tests: round-trip, torn-record detection, bad CRC | `src/engine/codec.go`, `tests/engine/codec_test.go` | Tests pass; format byte-for-byte matches SPEC section 3 | B0 | 2 |
| A2 | `Open` and recovery scan, index, `Put`/`Get`/`Delete`/`Batch` with the global RWMutex and exact write order, `Close`, `ForEachSorted`, `Stats`, `Check` | `src/engine/db.go`, `src/engine/recover.go`, `tests/engine/` | Write order exactly SPEC section 4; torn tail truncated; `// CRASH:` comments on every disk-changing syscall | A1 | 5 |
| A3 | Disk-full guard with DeleteReserve, write-error truncate and failed-state poisoning, plus tests for E1–E3 | `src/engine/`, `tests/engine/` | `ErrDiskFull` writes nothing; `ErrFailed` after unrecoverable write error; E1–E3 tests pass | A2 | 9 |
| A4 | Crash-safe compaction (temp file, fsync, rename, directory fsync, leftover cleanup, auto-trigger, `Compact()`), plus tests for E4 and E5 | `src/engine/compact.go`, `tests/engine/` | File shrinks after 1M puts + 1M deletes; E4, E5 tests pass | A2 | 13 |
| A5 | Crash-point hooks and a recovery benchmark: 1M keys, open under 2 s, measured and recorded in progress | `src/engine/crashpoint.go`, `tests/engine/` | All 13 SPEC crash-point names wired; benchmark result in `progress/anirudh.md` | A2 | 15 |
| A6 | ARCHITECTURE.md: diagram embed, 32-row proof table with file and function references, the three guarantees | `ARCHITECTURE.md` | Every row checked against code; matches docs/SCENARIOS.md | A4, A5 | 19 |

### Khushi: CLI and serve (`src/cmd/ninelives/**`, `tests/cli/**`, `README.md`, `diagrams/**`, `scripts/build.sh`)

| PR | Content | Files | Acceptance | Depends on | Target hour |
|---|---|---|---|---|---|
| K1 | Token codec, the `open`/`put`/`get`/`del`/`batch`/`dump`/`check`/`compact` commands, `--max-bytes`, the exit codes. Compiles against stubs and works once A2 lands | `src/cmd/ninelives/` | Output and exit codes exactly SPEC sections 10–11 | B0 | 4 |
| K2 | serve protocol: per-writer goroutines, stdout mutex, 4 MiB lines, batch blocks, READY/ACK/ERR, clean EOF shutdown | `src/cmd/ninelives/serve.go` | Protocol exactly SPEC section 12; ACK printed only after engine success | K1 | 6 |
| K3 | `scripts/build.sh` and CLI integration tests in `tests/cli` | `scripts/build.sh`, `tests/cli/` | Build from clean clone; tests cover every command | K1, A2 | 8 |
| K4 | Diagrams (Mermaid source plus exported PNG) matching SPEC and the Miro board | `diagrams/` | Shows every component, file layout, numbered write order, recovery flow, locks | A2 | 16 |
| K5 | Full README; clean-clone test on a second laptop, with Wi-Fi off, recorded in progress | `README.md` | Judge can follow it start to finish; format section matches a hex dump | K3, K4 | 21 |

### Likith: torture (`torture/**`, `BREAKING.md`, `docs/SCENARIOS.md`)

| PR | Content | Files | Acceptance | Depends on | Target hour |
|---|---|---|---|---|---|
| L1 | Harness: flags, seeded RNG per iteration, spawn serve, 2 synchronous writers, pipe mutex, oracle.log, random-delay SIGKILL, open/check/dump runner, one-line summaries | `torture/` | `--seed` mandatory; SIGKILL at random ms delay; oracle.log written independently | B0 | 4 |
| L2 | Verifier implementing the oracle rules in SPEC section 13; failure report with expected vs found, op ids and a repro command. `torture/fakeserve` with `--bug=none\|drop-ack\|partial-batch\|garbage\|stale` proves the oracle catches each bug. Self-test before the real engine is ready | `torture/`, `torture/fakeserve/` | Every fakeserve bug is caught; `--bug=none` passes | L1 | 7 |
| L3 | Modes `large-values`, `large-batch`, `disk-full` (asserts ERR ops absent and E5 recovery) | `torture/` | 100 iterations per mode green on real engine | L2, A3 | 11 |
| L4 | Modes `recovery-crash` (kill `open` repeatedly at random delays) and `compaction` (frequent compact ops, kills biased right after them), and `crashpoints` (iterates every `NINELIVES_CRASH_AT` name) | `torture/` | 100 iterations per mode green | L3, A4, A5 | 15 |
| L5 | BREAKING.md (verified against Anirudh's code) and docs/SCENARIOS.md statuses with modes and seeds | `BREAKING.md`, `docs/SCENARIOS.md` | All 5 sections; every crash point checked against code | L4, A6 | 19 |

### Integrator (Anirudh)

| PR | Content | Target hour |
|---|---|---|
| B0 | Bootstrap (this task). Everyone branches only after B0 is on `main` | 0–1 |
| — | Merge PRs and update PROGRESS.md at checkpoints | ongoing |
| B1 | Final assembly of PROMPTS.md in chronological order and the deadline checklist | 22 |

## 5. Dependency and merge timeline

```
B0 (bootstrap, on main)
 ├── Anirudh: A1 → A2 → A3 → A4 → A5 → A6
 ├── Khushi:  K1 → K2 → K3 → K4 → K5
 └── Likith:  L1 → L2 (fakeserve) → L3 → L4 → L5
```

| Hour | Checkpoint |
|---|---|
| 6 | First real torture run: A2 + K2 + L1 |
| 10 | Concurrency, large values and batches green |
| 14 | Compaction + 1M-key recovery green |
| 17 | 500 iterations in every mode green |
| 20 | Docs freeze |
| 22 | Clean-clone rehearsal |

Why the tracks don't block each other:
- Khushi codes against the frozen `src/engine/api.go`; the stub `DB` compiles from minute one.
- Likith codes against the frozen serve protocol (SPEC section 12) and tests against `torture/fakeserve` until the real engine lands.
- Anirudh's engine has no dependency on CLI or torture code; unit tests live in `tests/engine/`.

## 6. Cross-training (required by the demo rules)

Every member must explain the recovery path and answer crash-here questions.
- At hour 12 and hour 20, each person walks the other two through their code for 15 minutes.
- Likith writes BREAKING.md by reading Anirudh's code, which forces a second person to understand the engine.
- At hours 22–24, each person answers three crash-here questions from a teammate, plus the TTL design-extension question.

## 7. Demo roles

- **Khushi:** the build and README steps.
- **Likith:** the torture runs.
- **Anirudh:** first answerer for crash-here questions. Everyone must still be able to answer.
