# Testing NineLives by hand

The test commands, in the order to run them. Add new ones here as we go.

They are typed at the `nl>` prompt of nlshell, the test menu in `scripts/nlshell/`. Start it from the repo root:

```sh
go build -o bin/nlshell ./scripts/nlshell && ./bin/nlshell
```

Type each line and press Enter. Wait for `✓ done` before the next one; a red `✗` means something failed. Every command prints the shell commands it runs, so each step can also be typed without nlshell.

Ctrl-C stops a long run without leaving nlshell. Ctrl-D quits.

## 1. Build and the Go tests

```
/build
/test
/test-crashpoints
/test-recovery-1m
```

## 2. CLI by hand

```
/reset
/put a 1
/put b x:00ff
/get a
/get zz
/del a
/batch put c 3; put d 4; del c
/dump
/check
/compact
/disk-full
```

- `/get zz` should print `NOTFOUND`.
- `/dump` should print `b x:00ff` and `d 4`.
- `/check` should print `OK`.
- `/disk-full` should end with `exit=3`.

## 3. Serve, then a real kill (the judge's part)

```
/reset
/serve
```

nlshell switches to plain typing mode. Type these lines one at a time:

```
put a 1
batch 2
put b 2
del a
get b
!kill
```

Then, back at `nl>`:

```
/recover
```

Expected: `ACK` after each write, `VAL 3 2` for `get b`, then `serve ended: signal: killed`. `/recover` should print `OK` and `b 2`.

## 4. Crash points

```
/crash after_fsync
/crash mid_write
/crash batch_mid_write
/crash compact_after_rename
/crash recovery_before_truncate
/f4
```

Type `/crash ` (with the space) and the menu lists all 13 points. For each one, the final `check` should print `OK`.

## 5. Torture

```
/torture default 12345 500
/torture compaction
/torture recovery-crash
/torture large-values 999 100
/selftest
```

Each torture run should end with `PASS n/n`. With no seed, it picks a new random one each time. In `/selftest`, the four bug runs must say `FAILED` and the last must say `PASS 3/3`.

## 6. Optional, slower

```
/torture-all
/test-race
/clean-clone
```

`/torture-all` takes about 5 minutes.

## 7. The same tests in a window: NineLives Lab

`scripts/nlui` runs the same commands as nlshell. For each one it shows the exact command line and its output, what the command did to the database file, and the code path it took.

```sh
go build -o bin/nlui ./scripts/nlui && ./bin/nlui
```

It opens a Chromium app window. Without Chromium it uses the default browser, and with `--no-browser` it only prints the URL. It listens on 127.0.0.1 only and needs no network: the page is embedded in the binary. Every API call needs the random token in the printed URL, so open that exact URL. Ctrl-C in the terminal, or closing the window, stops it. The demo database is `$TMPDIR/nlui/demo.db`, separate from nlshell's.

| Tab | What to do | Expected |
|---|---|---|
| CLI | `CLI tour`, then `disk full` | every step ✓; disk full ends `exit 3 · expected` |
| Serve + kill | `start serve`; send `put a 1`, the `batch 2` chip, `get b`; then `SIGKILL`; then `recover` | ACK, ACK, `VAL 3 2`; `killed by SIGKILL (exit 137)`; recover ends `OK` and `b 2` |
| Crash lab | `run all 14` | `crash lab: 14/14 recovered` |
| Torture | `run torture` (default, 100); `oracle self-test` | `PASS 100/100`; the four bug runs show `exit 1 · expected`, the last one PASS |
| Build + tests | `build`, `test` | every step ✓ |

The Code path panel is a model of the source, written by hand. `go test ./scripts/nlui` checks that every function and crash point it names exists and sits where it says. Which steps lit up is worked out from the exit code, the output and the file before and after; it is not a live trace. Click a step chip to replay it; click a record or the torn tail to see its bytes.
