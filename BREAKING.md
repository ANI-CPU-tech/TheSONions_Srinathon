# BREAKING: how NineLives can die, and why it doesn't lose data

Owner: **Likith** (PR L5). At most 2 pages. Every claim is checked against Anirudh's code; a wrong crash point costs more than a missing one.

## 1. Crash points

8 to 15 rows. Each: exact function or line, what is on disk if killed there, what `open()` does.

| # | Function / line | On disk if killed here | What the next open() does |
|---|---|---|---|
| | TODO | TODO | TODO |

**Candidates, verify against the real code before keeping:**
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

## 2. The ordering argument

TODO(Likith): why our sequence of write, fsync and rename guarantees the invariant, naming the syscalls. Say whether we fsync the directory after a rename, and why.

## 3. Known weaknesses

At least 3, stated honestly.

**Candidates, verify before keeping:**
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

## 4. What we tried that failed

TODO(Likith): abandoned designs and bugs the torture tool found (seed, mode, symptom, root cause, fix commit). An empty section looks like hiding.

## 5. If we had 24 more hours

TODO(Likith): three concrete things, in priority order.
