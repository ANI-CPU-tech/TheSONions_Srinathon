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
