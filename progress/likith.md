# Progress: Likith
Last updated: 2026-11-06 15:30 IST   Branch: likith   Current PR: L2
## Done
- L1 torture harness already in tree (seeded op stream, synchronous writers, pipe mutex, oracle.log, random-delay SIGKILL, open/check/dump runner, fakeserve self-test); tests pass (commit on `main`).
- L2 verifier implementing SPEC section 13 oracle rules: allowed values per key (rule 4), untouched keys equal base (rule 5), batch atomicity (rule 6), unknown values (rule 7); recovery health (rule 8) checked by the harness. Returns a `verifyFailure` with expected vs found, key and op ids.
- L2 wired into `torture run()` with base-state carried across iterations (rule 9); `reportFailure` prints expected/found/opids plus the repro command.
- L2 `torture/fakeserve --bug=none|drop-ack|partial-batch|garbage|stale`: `--bug=none` passes; every injected bug is caught by the verifier (verified via `TestFakeserveBugsCaught` and end-to-end binary runs).
- Tests: `torture/verify_test.go` (unit cases for the verifier) and `torture/harness_test.go` (`TestFakeserveBugsCaught`).
## In progress
- (none)
## Next
1. L3 modes: `large-values`, `large-batch`, `disk-full` (asserts ERR ops absent and E5 recovery), on branch `likith`.
2. L4 modes: `recovery-crash`, `compaction`, `crashpoints`.
3. L5: `BREAKING.md` and `docs/SCENARIOS.md` statuses.
## Blockers / needs from others
- L3 depends on A3 (disk-full, Anirudh) for real-engine runs; fakeserve self-test works now.
- L4 depends on A4 (compaction) and A5 (crashpoints).
## Decisions (and why)
- Single branch `likith` holds all of Likith's tasks (L2–L5), per instruction; one logical commit per task.
- Verifier is deterministic from (seed, iter); fakeserve bugs use a deterministic trigger so the self-test is reproducible.
## Bugs found by torture
- (none yet)
## Notes for crash-here prep
- (none yet)
