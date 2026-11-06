# Progress: Likith
Last updated: 2026-11-06 16:10 IST   Branch: likith   Current PR: L3 (L2 done; L5 draft done)
## Done
- L1 torture harness already in tree (seeded op stream, synchronous writers, pipe mutex, oracle.log, random-delay SIGKILL, open/check/dump runner, fakeserve self-test); tests pass (commit on `main`).
- L2 verifier implementing SPEC section 13 oracle rules: allowed values per key (rule 4, incl. in-flight ABSENT effects), untouched keys equal base (rule 5), batch atomicity (rule 6), unknown values (rule 7); recovery health (rule 8) by the harness. Returns `verifyFailure` with expected vs found, key and op ids.
- L2 wired into `torture run()` with base-state carried across iterations (rule 9); `reportFailure` prints expected/found/opids plus repro command.
- L2 `torture/fakeserve --bug=none|drop-ack|partial-batch|garbage|stale`: `--bug=none` passes; every injected bug caught (verified via `TestFakeserveBugsCaught` and end-to-end binary runs). COMMITTED as 2a77ff1.
- L3 modes `large-values`, `large-batch`, `disk-full` wired in: `torture/modes.go` (mode constants, `maxBatchOps`, `maxValueFill`, `diskFullBytes`, `implemented`, `validMode`), `writers.go` generator made mode-aware, `main.go` mode dispatch (valid/implemented gates), `harness.go` passes mode/maxBytes to generators and serve. All three modes pass end-to-end against fakeserve (`--bug=none`).
- Verifier regression test `TestVerifyInFlightDeleteAbsent` (rule 4: in-flight delete of an ACKed key may leave it absent), caught by large-batch runs.
- BREAKING.md drafted (L5): 14 crash-point rows, ordering argument, 8 weaknesses, all checked against `src/engine/` (create.go, write.go, recover.go, compact.go, diskfull.go, db.go, codec.go, fsutil.go).
## In progress
- (none)
## Next
1. Run full `gofmt -l`, `go vet`, `go build`, `go test ./...`; commit L3 + BREAKING.md on branch `likith`.
2. L4 modes `recovery-crash`, `compaction`, `crashpoints` — stream not wired; `implemented()` returns false and `main` exits with a clear message until A4/A5 (Anirudh) and K2 (Khushi) land.
3. docs/SCENARIOS.md statuses — leave TODO until real-engine reproducible runs exist (rule: never mark PASS without a run).
## Blockers / needs from others
- L3/L4 real-engine green runs (100 iterations per mode) are BLOCKED: no working `ninelives serve` binary (Khushi K1/K2 serve is a stub) and Anirudh's disk-full/compaction/crashpoints (A3/A4/A5) not yet merged. Per TaskSplit rule 6 I am not editing `src/cmd/` or `src/engine/`.
- docs/SCENARIOS.md PASS statuses need reproducible real-engine runs (blocked by the same).
## Decisions (and why)
- Single branch `likith` holds all of Likith's tasks (L2–L5), per instruction; one logical commit per task.
- Verifier allows dump absence when an in-flight op has effect ABSENT (rule 4) — this was a real bug found by large-batch mode.
- BREAKING.md written from code now so every crash point is checked against the implementation; final L5 sign-off awaits real-engine runs.
## Bugs found by torture
- Verifier false positive: key absent after an ACKed put + in-flight delete was wrongly rejected; fixed in verify.go (rule 4 ABSENT effect). Found via large-batch mode against fakeserve.
## Notes for crash-here prep
- Crash points 1–14 documented in BREAKING.md §1, each mapped to `src/engine/` lines and on-disk state.
