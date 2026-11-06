# NineLives: Master Prompt for Claude Code

## How to use this file (for the humans)

1. Wait until the hackathon has officially started. The rules require every file to be committed to a fresh repo after the start time.
2. Create an empty Git repo, copy this file into its root as `MASTER_PROMPT.md`, and copy the organisers' PDF to `docs/brief.pdf`.
3. Open Claude Code in the repo root and send exactly this message:

   > Read MASTER_PROMPT.md completely and carry out the "Bootstrap task" section. Ask me the questions it tells you to ask before writing files.

4. After bootstrap, push `main`, protect it (PRs required), and turn off squash merging (use "Create a merge commit" or "Rebase and merge"). Judges read the commit history, and squash merges destroy it.
5. Each teammate clones the repo and runs Claude Code on their own branch. `CLAUDE.md` is loaded automatically in every session, so `/clear` never loses the rules. `PROGRESS.md` and `progress/<name>.md` hold the state.

Everything below this line is the prompt.

---

# PROMPT FOR CLAUDE CODE

You are the engineering assistant for team NineLives in a 24-hour hackathon called **"Kill It and Prove It"**. You are working in the root of our Git repository. The language is **Go, standard library only**.

Read this whole document before doing anything. It contains:

1. Hard rules for you
2. The full hackathon brief
3. Our design contract (the spec everyone codes against)
4. The repo layout and file ownership
5. Exactly which files to create and what goes in each
6. How to write TaskSplit.md so three people never conflict
7. The progress and prompt-logging protocols
8. The bootstrap task you must perform now

If `docs/brief.pdf` exists, it is the source of truth. If anything in this prompt contradicts it, the PDF wins: tell me about the contradiction.

---

## 1. Hard rules for you (Claude Code)

These apply in every session, forever. Copy them into `CLAUDE.md`.

1. **Standard library only.** `go.mod` must never contain a `require` line. Never import any database, storage engine, persistence, WAL, journaling, B-tree, LSM, ORM or key-value library. Banned examples: SQLite, LevelDB, RocksDB, LMDB, BerkeleyDB, Redis, H2, Derby, BoltDB/bbolt, Badger, Pebble. Never copy code from an existing database project. Reading their docs to learn ideas is fine.
2. **Offline at runtime.** Nothing we build may touch the network. The judge turns Wi-Fi off.
3. **The durability promise.** `Put`, `Delete` and `Batch` return success only after the record has been written and `fsync`ed (`File.Sync()`). `serve` prints `ACK` only after the engine call returned success. Never weaken this for speed.
4. **Stay in your lane.** Before editing any file, check `TaskSplit.md` for who owns it. On a teammate's branch, edit only that teammate's owned paths. If a change is needed elsewhere, stop and tell the human what change is needed and who owns it. Never edit frozen contract files except in a `contract/*` PR the human explicitly asks for.
5. **Explainable code.** The team must explain every line live with no AI. So:
   - Keep functions small.
   - In the write path, recovery path and compaction path, put a short `// CRASH:` comment before each syscall that changes disk state. The comment says what is on disk if the process is killed right there and what the next `open()` does.
   - After writing non-trivial code, give the human a short plain-English walkthrough.
6. **Small, real commits.** Commit after every meaningful step, with a message that says what and why. Use `git add <specific paths>`, never `git add -A`. Never commit binaries, `bin/`, `runs/`, `*.db` or temp files. Before every commit run `gofmt -l .`, `go vet ./...` and `go build ./...`. Run `go test ./...` before opening a PR.
7. **Log every prompt.** At the start of handling each human message, append it verbatim to `prompts/<name>.md` (format in section 7). This is mandatory: a missing or incomplete log costs 10 points, and a falsified one disqualifies us.
8. **Keep progress current.** Follow the progress protocol (section 7) so a `/clear` never loses context.
9. **Never rig the torture tool.** Kills must be SIGKILL at genuinely random millisecond delays. The oracle must record ACKs independently and never derive expectations from the database. A rigged tool means disqualification.
10. **Be honest in docs.** In BREAKING.md and ARCHITECTURE.md, never claim a mechanism the code does not have. "Not verified" is an allowed answer, and a wrong claim costs more than a missing one.

---

## 2. The hackathon brief (full context)

Copy this section, nearly verbatim but well formatted, into `docs/PROBLEM.md`. Put the allowed and not-allowed parts into `docs/RULES.md` as well.

### 2.1 The challenge

Build a key-value database from scratch that survives being killed at any moment, and prove it to a judge in 15 minutes.

There is no UI, no API server and no AI feature. The whole problem is correctness under failure. A database that works only when nothing goes wrong scores zero; one that stays correct after 500 kills wins. Judges measure whether we understand what our code does when the process dies between two instructions.

| Item | Detail |
|---|---|
| Duration | 24 hours, continuous |
| Team size | 2 to 4 |
| Language | Any (we chose Go) |
| Provided | The brief only. No starter code, datasets, servers or cloud credits |
| Hardware | Our own laptops. Judging happens on our laptop |
| Internet | Allowed for docs and AI tools. Not allowed for downloading database libraries |
| Judging | One 15-minute live session per team at hour 24 |

### 2.2 What to build

A single-process, embedded key-value database. It runs as one program, stores everything in one file on disk, and exposes these operations (all mandatory):

| Operation | Behaviour | Durable? |
|---|---|---|
| `put(key, value)` | Store the value under the key, overwriting any existing value | Yes. Once put returns success, the value survives a crash |
| `get(key)` | Return the current value or "not found" | n/a |
| `delete(key)` | Remove the key. Deleting a missing key is not an error | Yes. Once delete returns success, the key stays gone after a crash |
| `batch([ops])` | Apply a list of puts and deletes atomically: all or none | Yes. After a crash a batch is fully on disk or fully absent |
| `open(path)` | Open or create the file and recover from any previous crash | Must finish in under 2 seconds with 1,000,000 keys |
| `close()` | Flush and close cleanly | Yes |

**Data limits:**
- Keys are byte strings of 1 to 1,024 bytes.
- Values are byte strings of 0 to 1 MB.
- The database must handle at least 1,000,000 keys.
- A batch can hold up to 10,000 operations.

**Concurrency:**
- At least two threads must be able to call put, delete and batch at the same time without corrupting the file or breaking atomicity.
- `get` may run concurrently with writes. It must return the value from before or after a batch, never a half-applied batch.
- A single global lock is fine. Fancy concurrency earns nothing.

**Storage:**
- Exactly one authoritative data file. A lock file or a compaction temp file is allowed.
- We choose the on-disk format.
- The file must not grow forever. After 1,000,000 puts followed by 1,000,000 deletes, compaction must be able to shrink it, and compaction must itself be crash-safe.

**Disk full:**
- `open(path, max_bytes)` takes a configurable maximum file size.
- At the limit, writes fail cleanly with a clear error.
- The database stays fully readable and recoverable. Running out of space never corrupts existing data.

### 2.3 Rules

**Allowed:**
- Any language (we use Go).
- The full standard library: file I/O, threading, hashing, serialisation, fsync.
- A test framework (Go's `testing`).
- AI tools, as long as every prompt is logged.
- Reading docs, papers and blogs about real databases.

**Not allowed:**
- Any database, storage engine or persistence library: SQLite, LevelDB, RocksDB, LMDB, BerkeleyDB, Redis, H2, Derby, BoltDB, Badger and similar. If it stores keys and values for you, it is banned.
- Any ORM or key-value wrapper.
- Any library that implements WAL, journaling, B-trees or LSM trees.
- Copying source from an existing database project. Reading it is fine.
- Pre-written code from before the event. Everything is committed to a fresh repo after the start time.
- Sharing code with other teams. Discussing ideas is fine.

**Grey area (ask a judge):**
- Serialisation libraries such as protobuf or msgpack. We don't need them; we use `encoding/binary`.
- Memory-mapped file helpers, which are allowed only if they are in the standard library. We don't use mmap.

**Operational rules:**
- Everything runs offline. If the judge disconnects Wi-Fi and the system stops working, we score zero.
- Keep a Git repo with meaningful commits throughout. One giant commit at hour 23 is a red flag.
- The judge runs our code on our laptop, from our repo, following our README. If the README is wrong, the judge stops.

### 2.4 The invariant (the rule that decides everything)

After a crash at any point, and after `open()` has completed recovery:

1. Every operation that returned success before the crash is present.
2. Every operation that had not returned success is either fully present or fully absent.
3. A batch counts as one operation for rules 1 and 2. It is never partially present.
4. No key ever holds a value that was never written to it: no garbage, no torn values, no values from another key.
5. `open()` never fails on a file produced by our own database, however it was killed.

**"Returned success"** is a promise. If the process dies one microsecond after the return, the data must be there. Returning success before the data reaches disk breaks the promise.

**"Crash"** means `kill -9` (TerminateProcess on Windows) with no warning or cleanup. It can land on any instruction, including inside `write()`, inside `fsync()`, inside a rename, or inside recovery during the next `open()`. The OS and disk are honest: once fsync returns, the bytes are on disk. Recovery must survive a crash during recovery.

**Not required:**
- Operations that never returned success may be lost.
- Surviving an OS crash or power loss. If our design survives power loss too, we can say so in the docs.

### 2.5 Scenarios we must survive (32 rows; ARCHITECTURE.md needs one row each)

**Group A: crash during a single write**

| ID | Scenario | Expected after restart |
|---|---|---|
| A1 | Kill before any bytes of a put reach the file | Key absent, or holds its previous value |
| A2 | Kill after some bytes of the record are written (torn write) | Key absent or previous value. Torn bytes are ignored, never returned |
| A3 | Kill after the record is fully written but before fsync | Either outcome is fine (no success was returned). Nothing else is corrupted |
| A4 | Kill after fsync returns but before put returns | Key present with the new value |
| A5 | Kill immediately after put returns success | Key present with the new value. No exceptions |
| A6 | Kill during a delete | Key still present with its old value, or absent. Never corrupted |
| A7 | Kill during a put of a 1 MB value | Same rules as A1 to A5 |

**Group B: crash during a batch**

| ID | Scenario | Expected after restart |
|---|---|---|
| B1 | Kill after 3 of 10 operations are written | None of the 10 visible |
| B2 | Kill after all 10 are written but the commit marker is not | None visible |
| B3 | Kill after the commit marker is written but before fsync | All or none, never some |
| B4 | Kill after the batch returns success | All 10 visible |
| B5 | A batch that puts key X and deletes key X | Ordering defined in the README and honoured after a crash |
| B6 | A batch of 10,000 operations killed halfway | None visible. Recovery still under 2 s |

**Group C: crash during recovery and maintenance**

| ID | Scenario | Expected after restart |
|---|---|---|
| C1 | Kill during open() while it replays the log | Next open() succeeds and reaches the same correct state |
| C2 | Kill during open() while it truncates or repairs a torn tail | Next open() succeeds |
| C3 | Kill during compaction, before the new file replaces the old | Old file still authoritative, all data intact |
| C4 | Kill during compaction, in the middle of the rename or swap | Exactly one of old or new is authoritative, both complete, nothing lost |
| C5 | Kill after the swap, before temp files are cleaned up | Data intact. Leftover temp files cleaned up or ignored on next open() |
| C6 | Kill during close() | Same as a kill at any other time |

**Group D: concurrency**

| ID | Scenario | Expected |
|---|---|---|
| D1 | Two writers putting different keys, killed at random | Every acknowledged put from either writer is present |
| D2 | Two writers putting the same key, killed at random | The key holds a value one of the writers actually wrote, never a mix |
| D3 | One writer in a long batch, another doing single puts | Batch all-or-nothing; acknowledged single puts present |
| D4 | A reader calling get during a batch on the same keys (no crash) | Reader sees the state before or after the batch, never in between |

**Group E: disk full**

| ID | Scenario | Expected |
|---|---|---|
| E1 | Cap reached during a single put | put returns an error, the database is still readable, previous data intact |
| E2 | Cap reached halfway through a batch | Batch returns an error, none of its operations visible |
| E3 | Cap reached, then process killed | Restart succeeds, all previously acknowledged data intact |
| E4 | Cap reached during compaction | Compaction fails or completes. Either way no data is lost and the database opens |
| E5 | Deletes free space, compaction runs, writes work again | Usable again without manual intervention |

**Group F: repeated abuse**

| ID | Scenario | Expected |
|---|---|---|
| F1 | 500 consecutive random kills and restarts | Invariant holds after every one |
| F2 | Kill, restart, kill during recovery, restart, kill during recovery again | Invariant holds; recovery is idempotent |
| F3 | 1,000,000 keys written, process killed, open() called | Recovery under 2 s, all acknowledged keys present |
| F4 | Database file truncated by 1 byte at the end by hand | open() succeeds; at most the last unacknowledged record is lost |

**Group G: not tested (don't spend time on these)**
- Bit rot in the middle of the file. Mention how we would detect it.
- Network access, multiple files or sharding.
- Multi-batch transactions, isolation levels, MVCC.
- Performance beyond the 2 s recovery limit. Slow writes are fine; lost writes are not.

### 2.6 Deliverable 1: engine and CLI

The judge drives the database through a CLI with exactly these commands, run from the repo root (our binary is `./bin/ninelives`):

```
./bin/ninelives open  <path> [--max-bytes N]   # open or create, run recovery, print key count and recovery time
./bin/ninelives put   <path> <key> <value>
./bin/ninelives get   <path> <key>             # prints the value, or NOTFOUND
./bin/ninelives del   <path> <key>
./bin/ninelives batch <path> <file>            # one op per line: "put k v" or "del k"
./bin/ninelives dump  <path>                   # every key and value, sorted by key
./bin/ninelives check <path>                   # verify file integrity, print OK or a reason
./bin/ninelives serve <path>                   # long-running: ops from stdin, ACK after each durable success
```

The judge kills `serve`, and treats every `ACK <op-id>` as a promise.

**Repo requirements:**
- `README.md` covers how to build, how to run each command, a one-page on-disk format with a diagram, and the recovery algorithm in plain English.
- One documented build command works on a clean machine with only the Go toolchain installed.
- The format description is precise enough that a judge could write a parser from it. Judges spot-check it against a hex dump.

### 2.7 Deliverable 2: the torture tool

This is half the problem. A weak engine with a brilliant torture tool beats a decent engine with no tool. It is also the tie-break.

**It must:**
1. Start the database in `serve` mode as a child process.
2. Send random puts, deletes and batches with random keys and values, from at least two concurrent writers.
3. Record every ACKed operation in its own log file. This is the oracle.
4. SIGKILL the child at a genuinely random moment, using a random delay in milliseconds, never "after N operations".
5. Restart with `open`, then run `check`, then `dump`.
6. Compare the dump against the oracle. Every ACKed operation must be reflected, every batch must be all-or-nothing, and no unknown values may appear.
7. Loop. The default is 500 iterations, with a one-line summary per iteration and a final pass/fail count.
8. On the first failure, stop and print what was expected versus what was found, with the operation IDs involved.

**Required modes:** large values (1 MB), large batches (10,000 ops), kills during recovery, kills during compaction, and disk-full (small `--max-bytes`, errors must be clean).

**How the judge runs it** (ours is `./bin/torture`):
```
./bin/torture --iterations 500 --seed 12345
./bin/torture --iterations 100 --seed 999 --mode large-values
./bin/torture --iterations 100 --seed 7 --mode compaction
./bin/torture --iterations 100 --seed 42 --mode recovery-crash
```
`--seed` is mandatory. The judge uses a seed we have never tried. A tool that cannot reproduce a failure from a seed is worth less.

**A weak tool loses points when it:**
- kills only between operations;
- uses SIGTERM or graceful shutdown;
- has no independent oracle;
- checks only that open succeeds;
- has no concurrent writers;
- doesn't test batches.

### 2.8 Deliverable 3: BREAKING.md (at most 2 pages)

1. **Crash points table:** 8 to 15 rows. Each gives the exact function or line, what is on disk if killed there, and what open() does.
2. **The ordering argument:** why our sequence of write, fsync and rename guarantees the invariant, naming the syscalls. If we fsync the directory after a rename, say so and why. If we don't, say why it is still safe.
3. **Known weaknesses:** at least 3, stated honestly.
4. **What we tried that failed:** abandoned designs and bugs the torture tool found. An empty section looks like hiding.
5. **If we had 24 more hours:** three concrete things, in priority order.

Every crash point is checked against the code. A wrong one costs more than a missing one.

### 2.9 Deliverable 4: PROMPTS.md (or a prompts/ folder)

- Every prompt sent to any AI tool, in chronological order, unedited, with the tool name and approximate time. Responses are not needed.
- **Earns credit:** prompts showing we found a bug ourselves and asked for help understanding it; prompts asking for an explanation, followed by evidence we verified it; prompts that push back on the AI.
- **Loses credit:** pasting the whole brief and submitting the output; an obviously incomplete log.
- No log at all costs 10 points.
- Code the team cannot explain is scored as if an AI wrote it unread.

### 2.10 Deliverable 5: ARCHITECTURE.md

**Part 1, the diagram.** It must show:
- every component: client API, write path, in-memory index, log writer, commit-marker logic, recovery routine, compaction routine, disk-full guard;
- the file layout: header, record format with field sizes in bytes, commit markers, checksums, and where the index lives if persisted;
- the exact write order as numbered steps;
- the recovery path as a flow;
- the locks.

A diagram with only "Database" and "File" boxes scores zero.

**Part 2, the proof table.** One row per scenario A1 to F4 (32 rows), with columns: Scenario | Disk state at the moment of the kill | Mechanism that handles it | Where in the code (file and function) | Verified by (torture mode and seed, a manual test with steps, or "not verified").

**Part 3, the three guarantees.** One paragraph each on durability, atomicity and recoverability, each naming the syscalls and the ordering it depends on. "We use a WAL so it is safe" is not an argument.

The judge spot-checks three random rows against the code. The diagram is on screen for the whole demo; if the code doesn't match it, the diagram is wrong.

### 2.11 Submission layout and deadline checklist

```
README.md  ARCHITECTURE.md  BREAKING.md  PROMPTS.md
diagrams/  src/  torture/  tests/  .git/
```

- [ ] One build command works on a clean machine
- [ ] All 8 CLI commands work
- [ ] `./bin/torture --iterations 500 --seed <any>` runs to completion
- [ ] BREAKING.md has all 5 sections
- [ ] PROMPTS.md exists
- [ ] ARCHITECTURE.md has the diagram, all 32 rows and the 3 guarantees
- [ ] Final commit pushed before the deadline
- [ ] Laptop charged; Wi-Fi can be turned off

### 2.12 The 15-minute demo (Wi-Fi off, no AI, no internet)

| Minutes | What happens |
|---|---|
| 0–1 | Build from a clean clone using the README command. It must work the first time |
| 1–4 | `torture --iterations 200 --seed <judge's>`. Zero failures |
| 4–6 | Compaction and recovery-crash modes with the judge's seeds |
| 6–8 | The judge starts `serve`, sends a batch by hand, and kills it 5 to 10 times manually, then runs check and dump |
| 8–12 | Crash-here questions: the judge points at 3 lines in the write or recovery path and asks what is on disk and what the next open() does. The answer must be precise; "I think it's fine" scores zero |
| 12–15 | Design extension: "Add an expiry time to every key; expired keys are invisible and purged at compaction. What changes, what breaks?" Cover format, recovery and at least one subtle problem, such as the clock going backwards or an expired key overwritten in a batch |

**Rules:**
- Any member may answer, but the judge may pick who.
- Every member must be able to explain the recovery path.
- We may read our own code and BREAKING.md.
- If the build or the torture tool itself crashes, those points are lost.
- Correctly diagnosing our own bug recovers some points.

### 2.13 Scoring (100 points)

| Area | Points |
|---|---|
| Survives the judge's kill runs (minus 10 per violation, 0 on build failure) | 30 |
| Torture tool: independent oracle 4, kills during ops 4, concurrent writers and batches 4, modes and seeds 3 | 15 |
| Architecture: diagram matches code 4, proof table accurate on 3 spot-checks 4, guarantee arguments 2 (minus 2 per wrong spot-checked row) | 10 |
| Crash-here questions (7 + 7 + 6) | 20 |
| BREAKING.md: crash points 7, ordering 4, weaknesses and failures 4 (minus 2 per wrong crash point) | 15 |
| Design extension | 10 |

**Penalties:**
- Recovery over 2 s at 1M keys: minus 5
- File doesn't shrink: minus 5
- Disk-full corrupts data: minus 10
- No PROMPTS.md: minus 10
- Work done before the event: disqualified

**Tie-break:** the stronger torture tool wins.

### 2.14 Disqualification

- A banned library, even indirectly.
- Copied DB code.
- Code written before the event.
- Code shared between teams.
- AI or internet during the demo.
- A rigged torture tool.
- A falsified PROMPTS.md or commit history.

### 2.15 Suggested timeline and FAQ highlights

| Hours | Work |
|---|---|
| 0–2 | Design on paper, decide the write order, split engine and torture work |
| 2–6 | Minimal engine and minimal torture tool |
| 6–10 | Torture, fix, add concurrency, large values, large batches |
| 10–14 | Crash-safe compaction; 2 s recovery at 1M keys |
| 14–17 | Disk full; recovery-crash mode; 500 iterations per mode |
| 17–20 | BREAKING.md, testing every crash point |
| 20–22 | Clean-clone README test; finalise PROMPTS.md |
| 22–24 | Rehearse crash-here answers; sleep |

**FAQ highlights:**
- An append-only log with an in-memory hash index is enough for full marks.
- fsync matters. Run the torture tool on a real disk, not a RAM disk. The judge may ask where we call fsync.
- The torture tool may be written in another language; ours is Go.
- Docker is not required and adds demo risk.
- If some scenarios fail, document them honestly.
- The 2 s limit is strict, so measure early.

---

## 3. Our design contract (write this into docs/SPEC.md and freeze it)

This is the contract all three teammates code against. Once bootstrap is merged, it changes only through a `contract/*` PR approved by all three.

### 3.1 Architecture in one paragraph

NineLives is an append-only log with an in-memory hash index. The log file is the database. Every put, delete or batch appends exactly one record and fsyncs it before returning. The index maps each key to the file offset and length of its latest value and is rebuilt on every `open()` by scanning the log. Compaction rewrites only live keys into a temp file, fsyncs it, atomically renames it over the original, and fsyncs the directory.

### 3.2 Constants (`src/engine/api.go`)

```
FileMagic         = "NLV1"
FormatVersion     = 1
FileHeaderSize    = 24
RecordHeaderSize  = 17
MaxKeySize        = 1024
MaxValueSize      = 1 << 20          // 1 MiB
MaxBatchOps       = 10000
MaxBatchPayload   = 64 << 20         // 64 MiB total payload per batch (documented limit)
DeleteReserve     = 64 << 10         // bytes kept free for deletes under max_bytes
CompactMinFile    = 4 << 20          // auto-compaction only for files of at least 4 MiB
CompactDeadRatio  = 0.5              // auto-compact when dead bytes > 50% of file
```

### 3.3 File layout (all integers little-endian)

**File header (24 bytes)**, written only at creation and by compaction:

| Offset | Size | Field |
|---|---|---|
| 0 | 4 | Magic `NLV1` |
| 4 | 2 | Format version (1) |
| 6 | 2 | Flags (0) |
| 8 | 8 | SeqBase: the next sequence number at the time this file was created |
| 16 | 4 | Reserved (0) |
| 20 | 4 | CRC32-C (Castagnoli) of bytes 0–19 |

**Record**, repeated until end of file:

| Offset | Size | Field |
|---|---|---|
| 0 | 4 | CRC32-C over bytes 4 to the end of the payload (Length, Type, Seq, Payload) |
| 4 | 4 | Length: payload length in bytes |
| 8 | 1 | Type: 0x01 PUT, 0x02 DEL, 0x03 BATCH |
| 9 | 8 | Seq: a monotonic sequence number, unique per record |
| 17 | Length | Payload |

**Payloads:**
- PUT: `key_len u16 | key | val_len u32 | value`
- DEL: `key_len u16 | key`
- BATCH: `count u32 | count × (op u8 [0x01 put / 0x02 del] | key_len u16 | key | [val_len u32 | value if put])`

**Rules:**
- The CRC is the commit marker. A record "exists" only if its full Length bytes are present and its CRC matches. A batch is one record with one CRC, which is why it is atomic.
- Operations inside a batch apply in listed order: the last operation on a key wins. So `put X; del X` leaves X deleted (B5).
- Seq is not a byte offset. Offsets change at compaction; Seq never goes backwards. On open: `nextSeq = max(header.SeqBase, maxRecordSeq + 1)`.

### 3.4 Write path (the exact order; ARCHITECTURE.md numbers these steps)

Under the global write lock (`sync.RWMutex`, write side):

1. Validate sizes. Errors: `ErrKeyTooLarge`, `ErrEmptyKey`, `ErrValueTooLarge`, `ErrBatchTooLarge`. An empty batch is a successful no-op that writes nothing.
2. **Disk-full guard:**
   - For put or batch: `size + recordLen <= max_bytes - DeleteReserve`.
   - For delete: `size + recordLen <= max_bytes`.
   - If the check fails and there are dead bytes, run compaction once and recheck. If it still fails, return `ErrDiskFull`. Nothing has been written.
   - `max_bytes == 0` means unlimited.
3. Encode the whole record into one buffer and assign `seq = nextSeq`.
4. `file.WriteAt(buf, size)` in a single call.
5. `file.Sync()`.
6. Update the in-memory index and dead-byte accounting, then `size += len(buf)` and `nextSeq++`.
7. Release the lock and return `seq`. The caller (serve) then prints `ACK <op-id> <seq>`.

**Failure rule (important):** if step 4 or step 5 returns an error, the file may now hold a partial record. Try `file.Truncate(size)` followed by `file.Sync()`. If that fails too, the DB enters a failed state: every later write returns `ErrFailed` until the DB is reopened. Never append after unknown bytes, because recovery would stop at the garbage and drop every acknowledged record after it.

`get` takes the read lock, looks up the index, and calls `file.ReadAt` for the value. It never sees a half-applied batch, because the index is updated for the whole batch inside one write-lock section.

### 3.5 Creating a new file

1. Write the header to `<path>.init`.
2. fsync it.
3. `rename(<path>.init, <path>)`.
4. fsync the directory.

So `<path>` either does not exist or has a complete header.

### 3.6 Recovery (`Open`)

1. Remove the leftover `<path>.compact` and `<path>.init` if present. They are never authoritative.
2. If `<path>` does not exist, create it (section 3.5).
3. Read and validate the header (magic, version, CRC). An invalid header returns `ErrNotNineLives`; this can't happen for our own files because the header is fsynced before the rename.
4. Scan from offset 24. At each offset:
   - If fewer than 17 bytes remain, the tail is torn.
   - If `Length > MaxBatchPayload + overhead` or the record runs past end of file, the tail is torn.
   - Read the payload and compute the CRC. A mismatch means a torn tail.
   - Otherwise apply the record to the index (a batch applies all its ops at once) and advance.
5. If `validEnd < fileSize`, call `file.Truncate(validEnd)` and then `file.Sync()`. This is idempotent: a kill during it is simply redone by the next open.
6. Set `size = validEnd` and compute `nextSeq` and dead bytes. The DB is ready.

**Performance:**
- Use a `bufio.Reader` of at least 1 MiB and avoid per-record allocations where possible.
- Values are never copied into the index; the index stores `{offset, length, seq}`.
- Target well under 1 s for 1M small keys. Benchmark it.

### 3.7 Compaction (`Compact`)

Under the write lock:

1. Create `<path>.compact` with `O_TRUNC` and write a header with `SeqBase = nextSeq`.
2. For each live key, read the value from the old file and append a PUT record with the key's original seq, building the new index as you go.
3. If the temp file would exceed `max_bytes`, abort, remove the temp file and return `ErrDiskFull` (E4). The old file is untouched.
4. fsync the temp file.
5. `os.Rename(<path>.compact, <path>)`. This is atomic on Linux and macOS.
6. fsync the parent directory.
7. Swap file handles to the new file, install the new index, and reset dead bytes.

**Crash outcomes:**
- Kill before step 5: the old file is authoritative and open() deletes the temp file (C3).
- Kill after step 5: the new file is complete, because step 4 fsynced it (C4, C5).

**Triggers:**
- Automatic after a successful write when `fileSize >= CompactMinFile` and `deadBytes > CompactDeadRatio × fileSize`.
- From the disk-full guard.
- Explicitly through `serve`'s `compact` op or the extra CLI command `ninelives compact <path>`.

### 3.8 Crash-point hooks (for precise testing and demo answers)

`src/engine/crashpoint.go` provides `crashpoint(name string)`. When the environment variable `NINELIVES_CRASH_AT` equals `name`, the process SIGKILLs itself on the Nth hit, where N comes from `NINELIVES_CRASH_AFTER` (default 1). This is a real kill, not `os.Exit`.

Names:
- `before_write`
- `mid_write` (write half the buffer, then die)
- `after_write`
- `after_fsync`
- `after_index`
- `batch_mid_write`
- `recovery_mid_scan`
- `recovery_before_truncate`
- `recovery_after_truncate`
- `compact_mid_temp`
- `compact_after_temp_fsync`
- `compact_after_rename`
- `compact_after_dir_fsync`

These supplement random kills; they never replace them.

### 3.9 Engine API (Go): `src/engine/api.go` is FROZEN after bootstrap

```go
package engine

type OpKind uint8
const ( OpPut OpKind = 1; OpDel OpKind = 2 )

type Op struct { Kind OpKind; Key, Value []byte }
type Options struct { MaxBytes int64 } // 0 = unlimited
type Stats struct { Keys int; FileBytes, DeadBytes int64; RecoveryTime time.Duration; NextSeq uint64 }

var (
    ErrDiskFull, ErrKeyTooLarge, ErrEmptyKey, ErrValueTooLarge, ErrBatchTooLarge,
    ErrFailed, ErrClosed, ErrNotNineLives, ErrCorrupt, ErrNotImplemented error
)

type Store interface {
    Put(key, value []byte) (seq uint64, err error)
    Get(key []byte) (value []byte, found bool, err error)
    Delete(key []byte) (seq uint64, err error)
    Batch(ops []Op) (seq uint64, err error)
    Compact() error
    ForEachSorted(fn func(key, value []byte) error) error // for dump
    Stats() Stats
    Close() error
}

// Implemented in db.go (owned by Anirudh):
// func Open(path string, opts Options) (*DB, error)
// func Check(path string) error   // read-only integrity verification, nil = OK
// var _ Store = (*DB)(nil)
```

### 3.10 Token encoding (CLI, serve, dump, batch files)

- A key or value token is either raw text (no whitespace, not starting with `x:`) or `x:<lowercase hex>`.
- The empty value is `x:`.
- `dump` prints raw text when every byte is printable ASCII other than space; otherwise it prints `x:hex`.
- The torture tool always sends `x:hex`.

### 3.11 CLI output contract

| Command | Output | Exit code |
|---|---|---|
| `open` | `keys=<n> recovery_ms=<ms>` | |
| `get` | the value token or `NOTFOUND` | |
| `put`, `del`, `batch` | `OK seq=<n>` | |
| `dump` | `<key-token> <value-token>` lines, sorted by raw key bytes | |
| `check` | `OK` | 0 |
| `check` (problem) | `CORRUPT: <reason>` | 1 |
| any error | | 1 |
| usage error | | 2 |
| disk full | | 3 |

`check` is read-only. It validates the header, every record's CRC, a clean tail, and that no temp files are present.

All commands accept `--max-bytes N`.

### 3.12 serve protocol

On start, serve opens the DB (running recovery) and prints `READY keys=<n> recovery_ms=<ms>`.

**Input** is one request per line on stdin. The optional prefix `@<writer>:<opid>` defaults to writer 0 and an auto-incrementing op-id, so a judge can type `put a 1` by hand.

```
[@w:id] put <key> <value>
[@w:id] del <key>
[@w:id] get <key>
[@w:id] batch <n>          followed by exactly n lines: "put <k> <v>" or "del <k>"
[@w:id] compact
[@w:id] stats
```

**Output** is one line per request on stdout, flushed immediately and serialised with a mutex:

```
ACK <opid> <seq>                 (put/del/batch/compact succeeded and is durable)
VAL <opid> <value-token>         (get hit)
NOTFOUND <opid>                  (get miss)
STATS <opid> keys=.. file_bytes=.. dead_bytes=..
ERR <opid> <CODE> <message>      CODE ∈ DISKFULL | BADREQ | TOOLARGE | FAILED
```

**Rules:**
- `ERR` guarantees nothing was written.
- Each writer id gets its own goroutine and queue: one writer's ops run in order, different writers run concurrently, and the engine lock serialises the actual writes.
- The line reader must accept lines of at least 4 MiB, since a 1 MiB value is about 2 MiB of hex.
- A multi-line batch block must arrive contiguously. The torture tool writes each request, or a whole batch block, to the pipe under one mutex.
- On stdin EOF, finish in-flight ops, `Close()` the DB, and exit 0.

### 3.13 Torture oracle rules (Likith implements these exactly; BREAKING.md explains them)

1. **Unique values.** Every value the tool writes starts with the tag `w<writer>o<opid>:`, so any value in a dump can be traced to exactly one op.
2. **Synchronous writers.** Each writer waits for a response before sending its next request, so at most one op per writer is in flight at the kill.
3. **The log.** The tool logs, to `runs/<seed>/oracle.log`, each op when SENT (with a monotonic timestamp) and its ACK seq or ERR when received.
4. **Allowed values per key.** For each key k touched this iteration:
   - `lastAcked(k)` is the effect on k of the ACKed op touching k with the highest seq. If there is none, it is the base value from the previous verified state.
   - `allowed(k) = {lastAcked(k)} ∪ {effect on k of each in-flight op touching k}`.
   - The effect of a delete is ABSENT. The effect of a batch on k is the result of applying its ops to k in order.
   - Ops that returned `ERR` never apply.
5. **Untouched keys** must equal the base state.
6. **Batch atomicity for an in-flight batch B.**
   - If any key shows B's unique value, B was applied. Then every key in B must show B's effect, or the effect of an op that could be ordered after B: an op from the other writer not acked before B was sent, or the other writer's in-flight op.
   - If no key shows B's value, every key must be explainable without B.
   - Delete-only batches are hard to observe. Document this.
7. **Unknown values.** A value with no matching tag, or a tag whose op never touched that key, is a failure.
8. **Recovery health.** `open` must exit 0 and `check` must print `OK`. Otherwise the iteration fails.
9. **New base.** After a pass, the verified dump becomes the base for the next iteration.
10. **Reproducibility.** The op stream, value sizes and kill delays derive only from `--seed` and the iteration number. Document honestly that OS scheduling means a seed reproduces the stream and the delay, not the exact instruction hit.

---

## 4. Repo layout and ownership

```
ninelives/
├── MASTER_PROMPT.md            # this file (integrator)
├── CLAUDE.md                   # auto-loaded rules for Claude Code (integrator, frozen)
├── TaskSplit.md                # who owns what (integrator, frozen)
├── PROGRESS.md                 # team-level status (integrator only)
├── progress/
│   ├── anirudh.md  khushi.md  likith.md     # each person edits only their own
├── PROMPTS.md                  # index explaining the prompts/ folder (integrator)
├── prompts/
│   ├── planning-claude-ai.md   # prompts sent to Claude on claude.ai before coding (integrator)
│   ├── anirudh.md  khushi.md  likith.md     # each person's prompts, append-only
├── README.md                   # Khushi
├── ARCHITECTURE.md             # Anirudh
├── BREAKING.md                 # Likith
├── diagrams/                   # Khushi (Mermaid source + exported PNG)
├── docs/
│   ├── brief.pdf               # organisers' brief (integrator)
│   ├── PROBLEM.md  RULES.md  SPEC.md        # integrator, frozen
│   └── SCENARIOS.md            # 32-row tracker (Likith)
├── go.mod                      # module ninelives, no dependencies (integrator, frozen)
├── .gitignore  .github/CODEOWNERS           # integrator
├── scripts/build.sh            # Khushi
├── src/
│   ├── engine/                 # Anirudh (api.go is frozen contract, integrator-owned)
│   └── cmd/ninelives/          # Khushi
├── torture/                    # Likith (main package; includes torture/fakeserve/)
└── tests/
    ├── engine/                 # Anirudh
    └── cli/                    # Khushi
```

**Build:**
```
go build -o bin/ninelives ./src/cmd/ninelives && go build -o bin/torture ./torture
```
This is also wrapped in `scripts/build.sh`. Binaries go in `bin/`, because a root-level file named `torture` would clash with the `torture/` directory.

`.gitignore` covers `bin/`, `runs/`, `*.db`, `*.db.compact`, `*.db.init` and `*.test`.

---

## 5. Files to create during bootstrap, and what goes in each

1. **`CLAUDE.md`**: short and dense, because it loads into every session. It contains:
   - the hard rules from section 1;
   - a pointer list (brief → `docs/PROBLEM.md`, rules → `docs/RULES.md`, contract → `docs/SPEC.md`, ownership → `TaskSplit.md`);
   - the session-start and session-end routines from section 7;
   - the build, vet and test commands;
   - "Identify which teammate you are working for from the current branch prefix (`anirudh/`, `khushi/`, `likith/`) or `git config user.name`; if unclear, ask."
2. **`docs/PROBLEM.md`**: section 2 of this prompt, complete and well formatted.
3. **`docs/RULES.md`**: allowed, not allowed, grey area, operational rules, disqualification list, plus our own rule of no `require` in go.mod.
4. **`docs/SPEC.md`**: section 3 of this prompt, complete. Mark it FROZEN and describe the contract-change process.
5. **`docs/SCENARIOS.md`**: all 32 scenarios as a table with columns ID | Scenario | Expected | Mechanism (fill later) | Code location (fill later) | Torture mode/seed | Status (TODO / PASS / FAIL / NOT VERIFIED).
6. **`TaskSplit.md`**: see section 6.
7. **`PROGRESS.md`**: a team dashboard with:
   - the hackathon start time and the current hour;
   - a PR status table (PR id, owner, branch, status, merged at);
   - a scenario coverage summary;
   - open risks;
   - decisions log (date, decision, why);
   - next integration checkpoint.
8. **`progress/<name>.md`** for each teammate, using the template in section 7.
9. **`PROMPTS.md`**: explains that the log lives in `prompts/`, one append-only file per member plus the planning session, each chronological, unedited, with tool name and local time. It notes that the final submission merges them chronologically (`scripts/merge-prompts.sh` may do this later) and that prompts to other tools (claude.ai, ChatGPT, Copilot, Cursor) must also be pasted in.
10. **`prompts/planning-claude-ai.md`**: seeded with the exact text in section 9.
11. **`prompts/<name>.md`**: three empty logs with a header, plus the integrator's own file if the integrator is a fourth person.
12. **`README.md` skeleton**: headings for Build, Commands (all 8 + compact), Token encoding, serve protocol, On-disk format (one page + diagram), Recovery algorithm (plain English), Batch ordering (B5), Disk-full behaviour, Platform notes (Linux/macOS tested; Windows unsupported because of rename-over-open-file and directory fsync), Running the torture tool. Each heading gets a `TODO(Khushi)` line.
13. **`ARCHITECTURE.md` skeleton**:
    - Part 1: a placeholder for `diagrams/architecture.png` plus a Mermaid block to be filled. Note the team Miro board as the visual reference: https://miro.com/app/board/uXjVERbFfok=/
    - Part 2: the full 32-row proof table with the scenario IDs filled and other cells `TODO`.
    - Part 3: three headings.
14. **`BREAKING.md` skeleton**: the five required sections. In section 1, add a candidate list of crash points to verify against the real code, clearly marked "candidates, verify before keeping":
    - create before rename
    - put before write
    - put mid write
    - put after write before fsync
    - put after fsync before index update
    - after index update before ACK is printed
    - batch mid write
    - recovery mid scan
    - recovery after truncate before fsync
    - compaction during temp write
    - compaction after temp fsync before rename
    - compaction after rename before dir fsync
    - compaction after dir fsync before handle swap
    - close during fsync
    - write error followed by kill

    In section 3, add candidate weaknesses, also to verify:
    - the index lives in RAM;
    - recovery is O(file size) until compaction;
    - a CRC32-C false accept has 1 in 2^32 odds;
    - mid-file corruption truncates everything after it (bit rot is not handled);
    - one fsync per op is slow;
    - no Windows support;
    - plain fsync on macOS does not survive power loss (no F_FULLFSYNC);
    - compaction temporarily needs 2× disk;
    - a batch payload is capped at 64 MiB;
    - seed reproducibility is limited by OS scheduling.
15. **`diagrams/README.md`**: what diagrams will live here.
16. **`.github/CODEOWNERS`**: path ownership matching TaskSplit.md, with placeholders `@ANIRUDH_GH`, `@KHUSHI_GH`, `@LIKITH_GH`, `@INTEGRATOR_GH` for GitHub usernames.
17. **Go skeleton** (compiles, no logic):
    - `go.mod` with the installed Go version from `go version`;
    - `src/engine/api.go` (full contract from section 3.9 and the constants);
    - `src/engine/db.go` (a `DB` struct with every method returning `ErrNotImplemented`);
    - `src/engine/crashpoint.go` (a stub);
    - `src/cmd/ninelives/main.go` (command dispatch printing `not implemented`, exit 1);
    - `torture/main.go` (flag parsing for `--iterations --seed --mode --bin --dir --max-bytes`, failing if `--seed` is missing);
    - `scripts/build.sh`.

    `go build ./...` and `go vet ./...` must pass.

Do not implement engine, CLI or torture logic during bootstrap. That work belongs to the teammates' branches.

---

## 6. How TaskSplit.md must be written (zero merge conflicts)

Create `TaskSplit.md` with the sections below. The rule of the whole document: **every file has exactly one owner, and nobody edits a file they don't own.** Shared contracts are frozen up front so everyone can work in parallel from minute one.

### 6.1 Team and roles

- **Anirudh (TeamMate 1): Storage engine.** Owns the write path, recovery, compaction, disk-full handling and crash points. Also owns ARCHITECTURE.md.
- **Khushi (TeamMate 2): CLI, serve and docs.** Owns all 8 CLI commands plus `compact`, the serve protocol, the build script, README.md and diagrams/.
- **Likith (TeamMate 3): Torture tool and attack docs.** Owns the torture harness, the oracle, all modes, the fakeserve self-test, BREAKING.md and docs/SCENARIOS.md.
- **Integrator:** the person running this bootstrap. Ask the human for their name.
  - If the integrator is one of the three, they hold both roles.
  - The integrator owns the frozen contract files, PROGRESS.md, PROMPTS.md, prompts/planning-claude-ai.md, go.mod, .gitignore and .github/, and merges PRs.

### 6.2 Ownership table

Copy the ownership from section 4 into a table with columns Path | Owner | Notes. Mark the frozen files explicitly:
- CLAUDE.md
- TaskSplit.md
- docs/PROBLEM.md
- docs/RULES.md
- docs/SPEC.md
- src/engine/api.go
- go.mod

### 6.3 Branch and PR rules

- Branch names: `<name>/<PR-id>-<topic>`, for example `anirudh/A2-write-path` or `likith/L1-harness`.
- One PR per row in 6.4. Keep PRs small and merge them often.
- A PR may only touch paths owned by its author. The integrator rejects any PR that touches other paths. A `scripts/check-ownership.sh` owned by the integrator may automate this.
- Before opening a PR, rebase on `origin/main` and run `gofmt -l .`, `go vet ./...`, `go build ./...` and `go test ./...`.
- Merge with merge commits or rebase-merge, **never squash**, so judges see the real history.
- Contract changes go in a separate tiny PR, `contract/<topic>`, touching only frozen files. It needs approval from all three, merges first, and everyone rebases afterwards.
- Need something from another person's area? Open a GitHub issue or message them with the exact request. Never edit their files.
- Each teammate edits only their own `progress/<name>.md` and `prompts/<name>.md`. Only the integrator edits PROGRESS.md, directly on main after each merge.

### 6.4 PR plan per person

Each PR has an ID, a title, owned files touched, acceptance criteria, depends-on, and a target hour.

**Anirudh: engine (`src/engine/**`, `tests/engine/**`, ARCHITECTURE.md)**

| PR | Content | Target hour |
|---|---|---|
| A1 | Record and header codec with CRC32-C and unit tests: round-trip, torn-record detection, bad CRC | 2 |
| A2 | `Open` and recovery scan, index, `Put`/`Get`/`Delete`/`Batch` with the global RWMutex and exact write order, `Close`, `ForEachSorted`, `Stats`, `Check` | 5 |
| A3 | Disk-full guard with DeleteReserve, write-error truncate and failed-state poisoning, plus tests for E1–E3 | 9 |
| A4 | Crash-safe compaction (temp file, fsync, rename, directory fsync, leftover cleanup, auto-trigger, `Compact()`), plus tests for E4 and E5 | 13 |
| A5 | Crash-point hooks and a recovery benchmark: 1M keys, open under 2 s, measured and recorded in progress | 15 |
| A6 | ARCHITECTURE.md: diagram embed, 32-row proof table with file and function references, the three guarantees | 19 |

**Khushi: CLI and serve (`src/cmd/ninelives/**`, `tests/cli/**`, README.md, diagrams/**, scripts/build.sh)**

| PR | Content | Target hour |
|---|---|---|
| K1 | Token codec, the `open`/`put`/`get`/`del`/`batch`/`dump`/`check`/`compact` commands, `--max-bytes`, the exit codes. Compiles against stubs and works once A2 lands | 4 |
| K2 | serve protocol: per-writer goroutines, stdout mutex, 4 MiB lines, batch blocks, READY/ACK/ERR, clean EOF shutdown | 6 |
| K3 | `scripts/build.sh` and CLI integration tests in `tests/cli` | 8 |
| K4 | Diagrams (Mermaid source plus exported PNG) matching SPEC and the Miro board | 16 |
| K5 | Full README; clean-clone test on a second laptop, with Wi-Fi off, recorded in progress | 21 |

**Likith: torture (`torture/**`, BREAKING.md, docs/SCENARIOS.md)**

| PR | Content | Target hour |
|---|---|---|
| L1 | Harness: flags, seeded RNG per iteration, spawn serve, 2 synchronous writers, pipe mutex, oracle.log, random-delay SIGKILL, open/check/dump runner, one-line summaries | 4 |
| L2 | Verifier implementing the oracle rules in SPEC section 3.13; failure report with expected vs found, op ids and a repro command. `torture/fakeserve` with `--bug=none\|drop-ack\|partial-batch\|garbage\|stale` proves the oracle catches each bug. Self-test before the real engine is ready | 7 |
| L3 | Modes `large-values`, `large-batch`, `disk-full` (asserts ERR ops absent and E5 recovery) | 11 |
| L4 | Modes `recovery-crash` (kill `open` repeatedly at random delays) and `compaction` (frequent compact ops, kills biased right after them), and `crashpoints` (iterates every `NINELIVES_CRASH_AT` name) | 15 |
| L5 | BREAKING.md (verified against Anirudh's code) and docs/SCENARIOS.md statuses with modes and seeds | 19 |

**Integrator**

| PR | Content | Target hour |
|---|---|---|
| B0 | Bootstrap (this task). Everyone branches only after B0 merges | 0–1 |
| — | Merge PRs and update PROGRESS.md at checkpoints | ongoing |
| B1 | Final assembly of PROMPTS.md in chronological order and the deadline checklist | 22 |

### 6.5 Dependency and merge timeline

Show B0, then the parallel tracks, with these integration checkpoints:

| Hour | Checkpoint |
|---|---|
| 6 | First real torture run: A2 + K2 + L1 |
| 10 | Concurrency, large values and batches green |
| 14 | Compaction + 1M-key recovery green |
| 17 | 500 iterations in every mode green |
| 20 | Docs freeze |
| 22 | Clean-clone rehearsal |

Note why the tracks don't block each other:
- Khushi codes against the frozen `api.go`.
- Likith codes against the frozen serve protocol and tests against `fakeserve` until the real engine lands.

### 6.6 Cross-training (required by the demo rules)

Every member must explain the recovery path and answer crash-here questions. Schedule:
- At hour 12 and hour 20, each person walks the other two through their code for 15 minutes.
- Likith writes BREAKING.md by reading Anirudh's code, which forces a second person to understand the engine.
- At hours 22–24, each person answers three crash-here questions from a teammate, plus the TTL design-extension question.

### 6.7 Demo roles

- **Khushi:** the build and README steps.
- **Likith:** the torture runs.
- **Anirudh:** first answerer for crash-here questions. Everyone must still be able to answer.

---

## 7. Progress and prompt-logging protocols (put these in CLAUDE.md)

### Session start routine (every session and after every /clear)

1. Read `CLAUDE.md` (automatic), then `PROGRESS.md`, then `progress/<name>.md`, then your section of `TaskSplit.md`, then `docs/SPEC.md`.
2. Run `git status`, `git branch --show-current` and `git log --oneline -10`.
3. Tell the human in 3 to 5 lines: where we are, what's next, and any blockers.

### After every meaningful step, and before ending any turn that changed files

Update `progress/<name>.md` using this template:

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

Commit progress updates together with the work they describe.

### Prompt logging (every human message, verbatim)

Append to `prompts/<name>.md`:

```
### <YYYY-MM-DD HH:MM> (local time) | Tool: Claude Code | Branch: <branch>
<the human's message exactly as typed, unedited>
```

Get the timestamp with `date "+%Y-%m-%d %H:%M"`. Never rewrite or delete past entries. Remind the human once per session to paste prompts they send to any other AI tool into the same file.

---

## 8. Bootstrap task (do this now)

1. **Ask me, in one message, before writing anything:**
   - (a) your name, since you are the integrator; and whether you are also Anirudh, Khushi or Likith;
   - (b) the official hackathon start time, which goes into PROGRESS.md;
   - (c) the GitHub usernames for CODEOWNERS (placeholders are fine);
   - (d) confirmation that the hackathon has started.

   If I say it hasn't started, stop and do nothing.
2. Check `go version` and `git status`. Run `git init` if needed.
3. Create every file in section 5 with the content described. `docs/PROBLEM.md`, `docs/SPEC.md` and `docs/SCENARIOS.md` must be complete, not summaries of summaries.
4. Create the Go skeleton. Make `go build ./...` and `go vet ./...` pass, and run `gofmt`.
5. Commit in several meaningful commits, for example:
   - `docs: add hackathon brief and rules context`
   - `docs: add NineLives design contract (SPEC)`
   - `docs: add task split and ownership`
   - `chore: add Go skeleton with frozen engine API`
   - `docs: add deliverable skeletons and progress/prompt logs`
6. Log my bootstrap message in `prompts/<integrator>.md` (or the planning file, if I'm one of the three) and fill `PROGRESS.md`.
7. Finish with a short summary:
   - the files created;
   - the commands for me to push and protect `main`, and to disable squash merges;
   - the exact first message each teammate should send Claude Code on their own branch: *"Read CLAUDE.md, PROGRESS.md and my section of TaskSplit.md, then start my first PR."*

---

## 9. Seed content for `prompts/planning-claude-ai.md`

Write this file exactly as below. These prompts were sent to Claude on claude.ai during planning, before the repo existed.

```
# Planning session: Claude (claude.ai), 2026-11-06, local time IST
Prompts only, verbatim. Responses are not included, per the rules.

### 2026-11-06 10:36 | Tool: Claude (claude.ai) | (attached: the organisers' brief PDF)
This is the project we are supposed to work on. I want u to take in all the context of the project

### 2026-11-06 10:44 | Tool: Claude (claude.ai)
It mentions three possible approaches:

1. Write-Ahead Log (WAL).
2. Append-only log with an index.
3. Copy-on-write pages.

which approach do u suggest? which is most Optimal here?

### 2026-11-06 11:17 | Tool: Claude (claude.ai)
Come up with a name for our project make sure it dosent sound AI generated

### 2026-11-06 11:23 | Tool: Claude (claude.ai) | (sent twice; the first attempt was interrupted)
I wud like to go with NineLives

https://miro.com/app/board/uXjVERbFfok=/?share_link_id=817666301661

connect to my miro board make appropriate architecture diagrams that explains the entire workflow how it works and everything show everything properly let the ratio of the architecture diagram be rectangular.

also add all the context of the project into miro too , show how the 2nd approach is better compared to other show the stark difference.

### 2026-11-06 11:36 | Tool: Claude (claude.ai)
What tech stack do u suggest us to use??? Please pick the most appropriate tech stack and tell me why u picked it

### 2026-11-06 11:52 | Tool: Claude (claude.ai)
Gimme like a master prompt that i can put into claude code let the prompt have the context of the pdf , its requirements , whats allowed and not allowed. For the language i wud pick Go.

create the neccessary MD files as mentioned in the pdf and also create the necessary MD files that include the context of the pdf what we are supposed to do and what not and everything. also tell it to keep a progress.md file that stores the progress of the project so that when i do slash clear i dont loose any context.

also create a TaskSplit.md that splits the task between my 3 teammates. that is Anirudh-TeamMate 1, Khushi TeamMate 2,Likith TeamMate 3 , make it such a way that they shud create their our PR's and work inside those PR branches and make it so that one persons tasks or PR dont conflict with others, and dont lead to merge conflicts so tell claude code to make the task-split.md in such a way.
```

Add any later prompts I send to claude.ai to this file.

END OF PROMPT
