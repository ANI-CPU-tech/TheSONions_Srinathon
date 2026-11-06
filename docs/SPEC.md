# SPEC: NineLives design contract

> **Status: FROZEN** (since bootstrap B0, 2026-11-06).
>
> Every teammate codes against this file. It changes only through the contract-change process below. If `docs/brief.pdf` contradicts it, the PDF wins and the contradiction is raised with the whole team.

## Contract-change process

1. Open a branch `contract/<topic>` that touches **only** frozen files (`docs/SPEC.md`, `src/engine/api.go`, and any other file marked frozen in [TaskSplit.md](../TaskSplit.md)).
2. Keep the PR tiny and explain why the change is needed and who it affects.
3. All three teammates (Anirudh, Khushi, Likith) approve it.
4. It merges before any feature PR that depends on it. Everyone then rebases on `origin/main`.

Never change the contract inside a feature PR.

## 1. Architecture in one paragraph

NineLives is an append-only log with an in-memory hash index. The log file is the database. Every put, delete or batch appends exactly one record and fsyncs it before returning. The index maps each key to the file offset and length of its latest value and is rebuilt on every `open()` by scanning the log. Compaction rewrites only live keys into a temp file, fsyncs it, atomically renames it over the original, and fsyncs the directory.

## 2. Constants (`src/engine/api.go`)

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

## 3. File layout (all integers little-endian)

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

## 4. Write path (the exact order; ARCHITECTURE.md numbers these steps)

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

## 5. Creating a new file

1. Write the header to `<path>.init`.
2. fsync it.
3. `rename(<path>.init, <path>)`.
4. fsync the directory.

So `<path>` either does not exist or has a complete header.

## 6. Recovery (`Open`)

1. Remove the leftover `<path>.compact` and `<path>.init` if present. They are never authoritative.
2. If `<path>` does not exist, create it (section 5).
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

## 7. Compaction (`Compact`)

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

## 8. Crash-point hooks (for precise testing and demo answers)

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

## 9. Engine API (Go): `src/engine/api.go` is FROZEN after bootstrap

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

## 10. Token encoding (CLI, serve, dump, batch files)

- A key or value token is either raw text (no whitespace, not starting with `x:`) or `x:<lowercase hex>`.
- The empty value is `x:`.
- `dump` prints raw text when every byte is printable ASCII other than space; otherwise it prints `x:hex`.
- The torture tool always sends `x:hex`.

## 11. CLI output contract

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

## 12. serve protocol

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

## 13. Torture oracle rules (Likith implements these exactly; BREAKING.md explains them)

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
