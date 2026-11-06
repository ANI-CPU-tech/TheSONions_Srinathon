# Progress: Anirudh
Last updated: 2026-11-06 12:36 IST   Branch: anirudh/A1-codec   Current PR: A1
## Done
- B0 bootstrap committed to `main` as integrator (commits a0ca0d9..7eaa156)
- Project scaffold on `main`: empty files per responsibility in src/engine, src/cmd/ninelives, torture, tests, diagrams
- A1: file header codec, `EncodeFileHeader`/`DecodeFileHeader` (commit 2fff0d1)
- A1: record encoders `EncodePut`/`EncodeDel`/`EncodeBatch`, `DecodeRecordHeader`, `VerifyRecord`, `ErrTornRecord` (commit 3f2cf4c)
- A1: payload decoder `WalkPayload`, validates whole payload before any callback (commit 88f6bb2)
- A1 tests in `tests/engine/codec_test.go`: byte layouts, round trips, every torn prefix, every byte flip, malformed payloads. `go test ./...` green.
## In progress
- (none) A1 is committed locally on `anirudh/A1-codec`, not pushed, no PR opened yet.
## Next
1. Push `anirudh/A1-codec` and open the A1 PR when ready.
2. A2: `Open`/recovery scan, index, `Put`/`Get`/`Delete`/`Batch`, `Close`, `ForEachSorted`, `Stats`, `Check`.
## Blockers / needs from others
- (none)
## Decisions (and why)
- Codec functions are exported: tests live in `tests/engine/` (package `engine_test`), which can only see exported names.
- `MaxRecordPayload = MaxBatchPayload + 4 + MaxBatchOps*7`: SPEC says "MaxBatchPayload + overhead"; overhead is the batch count plus per-op framing. So A2 must count key + value bytes against `MaxBatchPayload`.
- Two error kinds: `ErrTornRecord` (short, Length out of range, or CRC mismatch: recovery truncates here) vs `ErrCorrupt` (CRC good but payload malformed: a writer bug, never a torn write).
- `WalkPayload` passes value offset/length, not value bytes, so the index stores `{offset, length, seq}` with no copies (SPEC section 6).
- `WalkPayload` validates first, then calls back, so a bad batch never half-applies to the index.
- Encoders do not validate sizes; write path step 1 (A2) does that before encoding.
## Bugs found by torture
- (none yet)
## Notes for crash-here prep
- codec.go has no syscalls, so no crash points. Its job in a crash: any prefix of a record a kill leaves behind fails `DecodeRecordHeader` (under 17 bytes) or `VerifyRecord` (short payload or CRC mismatch), so recovery sees `ErrTornRecord` and truncates there.
