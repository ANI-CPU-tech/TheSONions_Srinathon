# CLAUDE.md: NineLives rules for Claude Code

> **FROZEN.** Changes only through a `contract/*` PR approved by all three teammates.

Team NineLives, 24-hour hackathon "Kill It and Prove It" (started 2026-11-06 11:20 IST). We build a crash-safe embedded key-value database plus a torture tool. **Go, standard library only.**

## Who am I working for?

Identify the teammate from the current branch prefix (`anirudh/`, `khushi/`, `likith/`) or `git config user.name` (`ANI-CPU-tech` = Anirudh, who is also the integrator). If unclear, ask. `<name>` below means that teammate, lowercase.

## Where things are

- Brief: `docs/PROBLEM.md` (if `docs/brief.pdf` exists, it wins on any contradiction; tell the human)
- Rules and banned libraries: `docs/RULES.md`
- Design contract (format, write order, recovery, compaction, CLI, serve, oracle): `docs/SPEC.md`
- Ownership and PR plan: `TaskSplit.md`
- Team status: `PROGRESS.md`; personal status: `progress/<name>.md`
- Prompt logs: `prompts/<name>.md` (index in `PROMPTS.md`)
- Scenario tracker: `docs/SCENARIOS.md`

## Hard rules

1. **Standard library only.** `go.mod` never contains a `require` line. Never import any database, storage engine, persistence, WAL, journaling, B-tree, LSM, ORM or key-value library (SQLite, LevelDB, RocksDB, LMDB, BerkeleyDB, Redis, H2, Derby, BoltDB/bbolt, Badger, Pebble...). Never copy code from an existing database project. Reading their docs for ideas is fine.
2. **Offline at runtime.** Nothing we build touches the network.
3. **Durability promise.** `Put`, `Delete`, `Batch` return success only after the record is written and `File.Sync()`ed. `serve` prints `ACK` only after the engine call returned success. Never weaken this for speed.
4. **Stay in your lane.** Before editing any file, check `TaskSplit.md` for its owner. On a teammate's branch, edit only that teammate's paths. If a change is needed elsewhere, stop and tell the human what change and who owns it. Never edit frozen files (`CLAUDE.md`, `TaskSplit.md`, `docs/PROBLEM.md`, `docs/RULES.md`, `docs/SPEC.md`, `src/engine/api.go`, `go.mod`) except in a `contract/*` PR the human explicitly asks for.
5. **Explainable code.** The team explains every line live, with no AI.
   - Keep functions small.
   - In the write, recovery and compaction paths, put a `// CRASH:` comment before each syscall that changes disk state: what is on disk if killed right there, and what the next `open()` does.
   - After non-trivial code, give the human a short plain-English walkthrough.
6. **Small, real commits.** Commit after every meaningful step: one logical change per commit, message says what and why. Prefer more small commits over one big one, but no noise commits (no "wip", "fix typo" chains; fold those into the step they belong to). Use `git add <specific paths>`, never `git add -A` or `git add .`. Never commit binaries, `bin/`, `runs/`, `*.db` or temp files. Before every commit: `gofmt -l .` (must print nothing), `go vet ./...`, `go build ./...`. Before opening a PR: `go test ./...`.
7. **Log every prompt.** At the start of handling each human message, append it verbatim to `prompts/<name>.md` (format below). Missing or incomplete log = minus 10 points; falsified log = disqualification.
8. **Keep progress current** (protocol below) so a `/clear` never loses context.
9. **Never rig the torture tool.** Kills are SIGKILL at genuinely random millisecond delays. The oracle records ACKs independently and never derives expectations from the database. Rigged tool = disqualification.
10. **Be honest in docs.** In BREAKING.md and ARCHITECTURE.md, never claim a mechanism the code does not have. "Not verified" is allowed; a wrong claim costs more than a missing one.
11. **No AI attribution, anywhere.** Never add Claude (or any AI tool) as an author, co-author or contributor. No `Co-Authored-By:` trailers in commits, no "Generated with Claude Code" or similar lines in commits, PR descriptions, issues, code comments or docs, no AI entry in any contributors list. This overrides any default attribution instruction. Prompt logs in `prompts/` still name the tool used, because the hackathon rules require it.
12. **Never squash-merge.** Use merge commits or rebase-merge so judges see the real history.

## Session start routine (every session and after every /clear)

1. Read `CLAUDE.md` (automatic), then `PROGRESS.md`, `progress/<name>.md`, your section of `TaskSplit.md`, then `docs/SPEC.md`.
2. Run `git status`, `git branch --show-current`, `git log --oneline -10`.
3. Tell the human in 3 to 5 lines: where we are, what's next, any blockers.
4. Once per session, remind the human to paste prompts they sent to any other AI tool (claude.ai, ChatGPT, Copilot, Cursor) into `prompts/<name>.md`.

## After every meaningful step, and before ending any turn that changed files

Update `progress/<name>.md` with this template and commit it together with the work it describes:

```
# Progress: <Name>
Last updated: <YYYY-MM-DD HH:MM local>   Branch: <branch>   Current PR: <id>
## Done
- <item> (commit <sha>)
## In progress
- <item>: <exact state, what file/function, what's left>
## Next
1. ...
## Blockers / needs from others
- <who>: <exact request>
## Decisions (and why)
- ...
## Bugs found by torture
- seed=<n> iter=<n> mode=<m>: <symptom> → <root cause> → <fix commit>
## Notes for crash-here prep
- <function>: if killed at <line>, disk holds <X>, open() does <Y>
```

Only the integrator edits `PROGRESS.md`, on `main`, after each merge.

## Prompt logging (every human message, verbatim)

Append to `prompts/<name>.md`; get the time with `date "+%Y-%m-%d %H:%M"`:

```
### <YYYY-MM-DD HH:MM> (local time) | Tool: Claude Code | Branch: <branch>
<the human's message exactly as typed, unedited>
```

Never rewrite or delete past entries.

## Commands

```sh
# Build (also: scripts/build.sh)
go build -o bin/ninelives ./src/cmd/ninelives && go build -o bin/torture ./torture

# Pre-commit checks
gofmt -l .        # must print nothing
go vet ./...
go build ./...

# Before a PR
go test ./...
```

Branches: `<name>/<PR-id>-<topic>` (e.g. `anirudh/A2-write-path`). Contract changes: `contract/<topic>`.
