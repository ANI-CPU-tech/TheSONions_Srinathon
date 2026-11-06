# Rules

What we may and may not do. Breaking a rule in the "Disqualification" list ends the hackathon for us, so read this before adding any import or copying any snippet.

> **Status: FROZEN.** Changes only through a `contract/*` PR approved by all three teammates. If `docs/brief.pdf` contradicts this file, the PDF wins.

## Allowed

- Any language. We chose **Go, standard library only**.
- The full standard library: file I/O, threading, hashing, serialisation, fsync.
- A test framework (Go's `testing`).
- AI tools, as long as every prompt is logged (see [PROMPTS.md](../PROMPTS.md)).
- Reading docs, papers and blogs about real databases.

## Not allowed

- Any database, storage engine or persistence library: SQLite, LevelDB, RocksDB, LMDB, BerkeleyDB, Redis, H2, Derby, BoltDB, Badger and similar. **If it stores keys and values for you, it is banned.**
- Any ORM or key-value wrapper.
- Any library that implements WAL, journaling, B-trees or LSM trees.
- Copying source from an existing database project. Reading it is fine.
- Pre-written code from before the event. Everything is committed to a fresh repo after the start time.
- Sharing code with other teams. Discussing ideas is fine.

## Grey area (ask a judge first)

- Serialisation libraries such as protobuf or msgpack. We don't need them; we use `encoding/binary`.
- Memory-mapped file helpers, allowed only if they are in the standard library. We don't use mmap.

## Operational rules

- Everything runs offline. If the judge disconnects Wi-Fi and the system stops working, we score zero.
- Keep a Git repo with meaningful commits throughout. One giant commit at hour 23 is a red flag.
- The judge runs our code on our laptop, from our repo, following our README. If the README is wrong, the judge stops.
- During the 15-minute demo: Wi-Fi off, no AI, no internet. We may read our own code and BREAKING.md.

## Disqualification

- A banned library, even indirectly.
- Copied DB code.
- Code written before the event.
- Code shared between teams.
- AI or internet during the demo.
- A rigged torture tool.
- A falsified PROMPTS.md or commit history.

## Our own team rules (on top of the brief)

- **`go.mod` never contains a `require` line.** Standard library only, no exceptions. A reviewer rejects any PR that adds one.
- Banned names to watch for in imports: `sqlite`, `leveldb`, `rocksdb`, `lmdb`, `bolt`/`bbolt`, `badger`, `pebble`, `redis`, any `wal`/`journal`/`btree`/`lsm` package.
- Nothing we build may touch the network at runtime.
- The torture tool kills with SIGKILL at random millisecond delays and keeps its own oracle. Never rig it.
- Merge PRs with merge commits or rebase-merge, never squash, so the history judges read stays real.
- No AI tool is credited as an author or contributor anywhere: no `Co-Authored-By` trailers, no "Generated with ..." lines in commits or PRs. Prompts are still logged in `prompts/`, as the rules require.
