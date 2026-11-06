# Problem: "Kill It and Prove It"

The full hackathon brief for team NineLives. If `docs/brief.pdf` is added to the repo, the PDF is the source of truth and this file must be corrected to match it.

Related: rules in [RULES.md](RULES.md), our design contract in [SPEC.md](SPEC.md), scenario tracker in [SCENARIOS.md](SCENARIOS.md).

> **Status: FROZEN.** Changes only through a `contract/*` PR approved by all three teammates.

## 1. The challenge

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

## 2. What to build

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

## 3. Rules

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

## 4. The invariant (the rule that decides everything)

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

## 5. Scenarios we must survive (32 rows; ARCHITECTURE.md needs one row each)

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

## 6. Deliverable 1: engine and CLI

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

## 7. Deliverable 2: the torture tool

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

## 8. Deliverable 3: BREAKING.md (at most 2 pages)

1. **Crash points table:** 8 to 15 rows. Each gives the exact function or line, what is on disk if killed there, and what open() does.
2. **The ordering argument:** why our sequence of write, fsync and rename guarantees the invariant, naming the syscalls. If we fsync the directory after a rename, say so and why. If we don't, say why it is still safe.
3. **Known weaknesses:** at least 3, stated honestly.
4. **What we tried that failed:** abandoned designs and bugs the torture tool found. An empty section looks like hiding.
5. **If we had 24 more hours:** three concrete things, in priority order.

Every crash point is checked against the code. A wrong one costs more than a missing one.

## 9. Deliverable 4: PROMPTS.md (or a prompts/ folder)

- Every prompt sent to any AI tool, in chronological order, unedited, with the tool name and approximate time. Responses are not needed.
- **Earns credit:** prompts showing we found a bug ourselves and asked for help understanding it; prompts asking for an explanation, followed by evidence we verified it; prompts that push back on the AI.
- **Loses credit:** pasting the whole brief and submitting the output; an obviously incomplete log.
- No log at all costs 10 points.
- Code the team cannot explain is scored as if an AI wrote it unread.

## 10. Deliverable 5: ARCHITECTURE.md

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

## 11. Submission layout and deadline checklist

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

## 12. The 15-minute demo (Wi-Fi off, no AI, no internet)

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

## 13. Scoring (100 points)

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

## 14. Disqualification

- A banned library, even indirectly.
- Copied DB code.
- Code written before the event.
- Code shared between teams.
- AI or internet during the demo.
- A rigged torture tool.
- A falsified PROMPTS.md or commit history.

## 15. Suggested timeline and FAQ highlights

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

