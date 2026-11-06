# NineLives

A crash-safe embedded key-value database in Go (standard library only), plus a torture tool that SIGKILLs it at random moments and proves no acknowledged write is ever lost. Built for the "Kill It and Prove It" hackathon.

## Build

TODO(Khushi): one command that works on a clean machine with only Go installed.

```sh
scripts/build.sh
# or: go build -o bin/ninelives ./src/cmd/ninelives && go build -o bin/torture ./torture
```

## Commands

TODO(Khushi): usage and example output for each command: `open`, `put`, `get`, `del`, `batch`, `dump`, `check`, `serve`, plus `compact`. Exit codes.

## Token encoding

TODO(Khushi): raw text vs `x:<hex>`, the empty value `x:`, how `dump` chooses.

## serve protocol

TODO(Khushi): READY line, request format with `@<writer>:<opid>`, ACK / VAL / NOTFOUND / STATS / ERR lines.

## On-disk format

TODO(Khushi): one page with a diagram: header (24 bytes) and record layout with byte offsets, precise enough to write a parser and check against a hex dump.

## Recovery algorithm

TODO(Khushi): plain English, step by step.

## Batch ordering (B5)

TODO(Khushi): ops in a batch apply in listed order; `put X; del X` leaves X deleted.

## Disk-full behaviour

TODO(Khushi): `--max-bytes`, DeleteReserve, compaction retry, error code and exit code 3.

## Platform notes

TODO(Khushi): tested on Linux and macOS. Windows unsupported because of rename-over-open-file and directory fsync.

## Running the torture tool

TODO(Khushi): judge commands, modes, what a pass and a failure look like, where `runs/` logs go.
