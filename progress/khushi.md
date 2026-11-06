# Progress: Khushi
Last updated: 2026-11-06 13:17 IST   Branch: khushi   Current PR: K1 (code), all tasks on one `khushi` branch per human instruction
## Done
- Setup: fast-forwarded `main` to `origin/main` (A1+A2+A3 merged), created branch `khushi` from it (commit 3000dfd).
- K1: token codec + `open`/`put`/`get`/`del`/`batch`/`dump`/`check`/`compact`, `--max-bytes`, exit codes 0/1/2/3, `serve` stub (commit b4c3b48). Verified: `gofmt -l .` clean, `go vet ./...`, `go build ./...`, `go test ./...` green, plus a manual smoke run of every command and every exit code (usage=2, disk-full=3, corrupt check=1, happy path=0).
## In progress
- (none — paused after K1 for human review)
## Next
1. K2: `serve` protocol in `src/cmd/ninelives/serve.go` (SPEC section 12): READY line, per-writer goroutine queues, stdout mutex, 4 MiB lines, contiguous batch blocks, ACK only after engine success, EOF → Close → exit 0.
2. K3: `scripts/build.sh` + CLI integration tests in `tests/cli/`.
3. K4: diagrams (5 `.mmd` + PNG export via npx mermaid-cli).
4. K5: full README with a real hex dump of the on-disk format.
## Blockers / needs from others
- Anirudh: `engine.Compact()` still returns `ErrNotImplemented`, so `ninelives compact <path>` exits 1 with that message. CLI side is ready; it prints `OK` once A4 merges. `recovery_ms` for tiny files prints `0` (sub-millisecond) — fine, but 1M-key runs will show real numbers.
## Decisions (and why)
- **Single branch `khushi`** with `K1:`-style commit grouping instead of TaskSplit §3's one-branch-per-PR (`khushi/K1-...`): direct human instruction; deviation recorded here so the integrator can split later if wanted.
- `ninelives compact` prints `OK` (SPEC §11 has no row for compact; PROBLEM.md doesn't either) — documented here and in README (K5).
- `check` failures print `CORRUPT: <reason>` on stdout even when the reason is e.g. a missing file, because SPEC §11 lists that as check's output line; exit 1 either way.
- A malformed token or batch-file line is a usage error (exit 2): the DB has not been touched. Engine rejections (key too large, empty key) stay exit 1, ErrDiskFull exit 3.
- Batch file ignores blank lines and `#` comments; everything else must be `put k v` / `del k` (PROBLEM.md §6).
- No `git push` / GitHub actions until the human says so.
## Bugs found by torture
- (none yet — torture harness L1 not merged)
## Notes for crash-here prep
- (K2 onwards)
