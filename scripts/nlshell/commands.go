package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// command is one entry in the menu. script returns the shell text to run,
// which nlshell prints before running it, so every step can also be typed
// by hand (the demo is without tools).
type command struct {
	name     string
	args     string // usage hint, shown in the menu
	desc     string
	minArgs  int
	complete func(argIndex int) []string // nil: no argument completion
	script   func(args []string) string  // nil: handled in main (serve, help, exit)
}

const nl = "./bin/ninelives"

// demoDir holds the scratch database the CLI commands work on.
var demoDir = filepath.Join(os.TempDir(), "nl")
var db = filepath.Join(demoDir, "demo.db")

var modes = []string{"default", "large-values", "large-batch", "disk-full", "recovery-crash", "compaction", "crashpoints"}

// crashPoints are the engine's crash-point names (docs/SPEC.md section 8).
var crashPoints = []string{
	"before_write", "mid_write", "after_write", "after_fsync", "after_index", "batch_mid_write",
	"compact_mid_temp", "compact_after_temp_fsync", "compact_after_rename", "compact_after_dir_fsync",
	"recovery_mid_scan", "recovery_before_truncate", "recovery_after_truncate",
}

func first(opts []string) func(int) []string {
	return func(i int) []string {
		if i == 0 {
			return opts
		}
		return nil
	}
}

func fixed(s string) func([]string) string { return func([]string) string { return s } }

var commands = []command{
	{name: "build", desc: "build bin/ninelives, bin/torture and bin/fakeserve",
		script: fixed("scripts/build.sh && go build -o bin/fakeserve ./torture/fakeserve")},
	{name: "test", desc: "gofmt, go vet and every Go test",
		script: fixed(`test -z "$(gofmt -l .)" || { gofmt -l .; echo "gofmt: the files above need formatting"; exit 1; }; go vet ./... && go test ./...`)},
	{name: "test-race", desc: "engine and torture tests under the race detector",
		script: fixed("go test -race ./tests/engine/ ./torture/")},
	{name: "test-crashpoints", desc: "real SIGKILL at all 13 engine crash points (Go test)",
		script: fixed("go test ./tests/engine -run TestCrashPoints -v")},
	{name: "test-recovery-1m", desc: "F3: open() of 1M keys must take under 2 s",
		script: fixed("go test ./tests/engine -run TestRecoveryMillionKeysUnder2s -v")},
	{name: "bench-recovery", desc: "benchmark 1M-key recovery",
		script: fixed("go test ./tests/engine -bench BenchmarkRecoveryMillionKeys -run '^$'")},

	{name: "reset", desc: "delete the demo database and start fresh",
		script: fixed(fmt.Sprintf("mkdir -p %s && rm -f %s %s.compact %s.init && echo 'fresh database: %s'", q(demoDir), q(db), q(db), q(db), db))},
	{name: "open", desc: "open (runs recovery) and print key count", script: cli("open")},
	{name: "put", args: "<key> <value>", desc: "put a key; x:<hex> for binary", minArgs: 2, script: cli("put")},
	{name: "get", args: "<key>", desc: "get a key", minArgs: 1, script: cli("get")},
	{name: "del", args: "<key>", desc: "delete a key", minArgs: 1, script: cli("del")},
	{name: "batch", args: "<op>; <op>; ...", desc: "atomic batch, e.g. /batch put c 3; put d 4; del c", minArgs: 1, script: batchScript},
	{name: "dump", desc: "print every key, sorted", script: cli("dump")},
	{name: "check", desc: "read-only integrity check (OK or CORRUPT)", script: cli("check")},
	{name: "compact", desc: "rewrite live keys into a new file", script: cli("compact")},
	{name: "recover", desc: "open, then check, then dump (what torture does after a kill)",
		script: fixed(recoverScript)},
	{name: "serve", desc: "interactive serve: type requests, !kill sends a real SIGKILL"},

	{name: "cli-tour", desc: "every CLI command once, on a fresh database", script: fixed(cliTour)},
	{name: "disk-full", desc: "a put over --max-bytes 30 must fail with exit code 3",
		script: fixed(fmt.Sprintf("%s put %s big 1234567890 --max-bytes 30; echo \"exit=$?  (3 = disk full)\"", nl, q(db)))},
	{name: "crash", args: "<crash-point>", desc: "SIGKILL the CLI at a crash point, then recover", minArgs: 1,
		complete: first(crashPoints), script: crashScript},
	{name: "f4", desc: "cut the last byte off the file by hand, then recover", script: fixed(f4Script)},

	{name: "torture", args: "<mode> [seed] [iterations]", desc: "run the torture tool (new random seed if none given)", minArgs: 1,
		complete: first(modes), script: tortureScript},
	{name: "torture-all", desc: "every mode, in a row (stops at the first failure)", script: fixed(tortureAll)},
	{name: "selftest", desc: "fakeserve with 4 injected bugs: the oracle must catch each one", script: fixed(selftest)},
	{name: "clean-clone", desc: "fresh clone, build, test, short torture run", script: fixed(cleanClone)},

	{name: "help", desc: "list every command"},
	{name: "clear", desc: "clear the screen", script: fixed("clear")},
	{name: "exit", desc: "leave nlshell (also Ctrl-D)"},
}

// lookup finds a command by name, with or without the leading slash.
func lookup(name string) *command {
	name = strings.TrimPrefix(name, "/")
	for i := range commands {
		if commands[i].name == name {
			return &commands[i]
		}
	}
	return nil
}

// q quotes s for sh.
func q(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

// cli runs one ninelives command on the demo database with the user's args.
func cli(cmd string) func([]string) string {
	return func(args []string) string {
		s := fmt.Sprintf("mkdir -p %s && %s %s %s", q(demoDir), nl, cmd, q(db))
		for _, a := range args {
			s += " " + q(a)
		}
		return s
	}
}

// batchScript writes the ";"-separated ops to a batch file and runs it.
func batchScript(args []string) string {
	file := filepath.Join(demoDir, "batch.txt")
	s := "printf '%s\\n'"
	for _, o := range strings.Split(strings.Join(args, " "), ";") {
		if o = strings.TrimSpace(o); o != "" {
			s += " " + q(o)
		}
	}
	return fmt.Sprintf("mkdir -p %s && %s > %s && cat %s && %s batch %s %s", q(demoDir), s, q(file), q(file), nl, q(db), q(file))
}

var recoverScript = fmt.Sprintf("%[1]s open %[2]s && %[1]s check %[2]s && %[1]s dump %[2]s", nl, q(db))

var cliTour = fmt.Sprintf(`set -x
mkdir -p %[3]s; rm -f %[2]s
%[1]s open %[2]s
%[1]s put %[2]s a 1
%[1]s put %[2]s b x:00ff
%[1]s get %[2]s a
%[1]s get %[2]s zz
%[1]s del %[2]s a
printf 'put c 3\nput d 4\ndel c\n' > %[3]s/b.txt
%[1]s batch %[2]s %[3]s/b.txt
%[1]s dump %[2]s
%[1]s check %[2]s
%[1]s compact %[2]s
%[1]s dump %[2]s`, nl, q(db), q(demoDir))

// crashScript arms one crash point in the CLI command that reaches it: a
// put for the write path, a batch for batch_mid_write, compact for the
// compaction points, open for recovery (after a mid_write left a torn
// tail). Then it shows check before and after recovery.
func crashScript(args []string) string {
	p := args[0]
	env := "NINELIVES_CRASH_AT=" + q(p) + " "
	var kill string
	switch {
	case p == "batch_mid_write":
		kill = fmt.Sprintf("printf 'put k1 v1\\nput k2 v2\\n' > %[1]s/b.txt\n%[2]s%[3]s batch %[4]s %[1]s/b.txt", q(demoDir), env, nl, q(db))
	case strings.HasPrefix(p, "compact_"):
		kill = fmt.Sprintf("%[1]s put %[2]s k1 v1; %[1]s put %[2]s k1 v2\n%[3]s%[1]s compact %[2]s", nl, q(db), env)
	case strings.HasPrefix(p, "recovery_"):
		kill = fmt.Sprintf("NINELIVES_CRASH_AT=mid_write %[1]s put %[2]s torn x  # leave a torn tail\n%[3]s%[1]s open %[2]s", nl, q(db), env)
	default:
		kill = fmt.Sprintf("%[1]s%[2]s put %[3]s k1 v1", env, nl, q(db))
	}
	return fmt.Sprintf(`set -x
mkdir -p %[1]s; rm -f %[2]s %[2]s.compact; %[3]s put %[2]s base 0
%[4]s
: "exit=$?  (137 = killed by SIGKILL)"
%[3]s check %[2]s
%[3]s open %[2]s
%[3]s check %[2]s
%[3]s dump %[2]s`, q(demoDir), q(db), nl, kill)
}

var f4Script = fmt.Sprintf(`set -x
mkdir -p %[3]s; rm -f %[2]s
%[1]s put %[2]s a 1; %[1]s put %[2]s b 1; %[1]s put %[2]s c 1
truncate -s -1 %[2]s
%[1]s check %[2]s
%[1]s open %[2]s
%[1]s check %[2]s
%[1]s dump %[2]s`, nl, q(db), q(demoDir))

// tortureScript: /torture <mode> [seed] [iterations]. Without a seed it
// picks a new one from the clock, like a judge would.
func tortureScript(args []string) string {
	seed := strconv.FormatInt(time.Now().UnixNano()%1_000_000, 10)
	iters := "100"
	if len(args) > 1 {
		seed = args[1]
	}
	if len(args) > 2 {
		iters = args[2]
	}
	return fmt.Sprintf("./bin/torture --seed %s --iterations %s --mode %s", q(seed), q(iters), q(args[0]))
}

var tortureAll = `./bin/torture --iterations 500 --seed 12345 &&
./bin/torture --iterations 100 --seed 999 --mode large-values &&
./bin/torture --iterations 100 --seed 31337 --mode large-batch &&
./bin/torture --iterations 100 --seed 2024 --mode disk-full &&
./bin/torture --iterations 100 --seed 42 --mode recovery-crash &&
./bin/torture --iterations 100 --seed 7 --mode compaction &&
./bin/torture --iterations 130 --seed 1306 --mode crashpoints`

var selftest = `test -x bin/fakeserve || go build -o bin/fakeserve ./torture/fakeserve
for b in drop-ack partial-batch garbage stale; do
  echo "== --bug $b (must FAIL)"; ./bin/torture --seed 11 --iterations 5 --bin ./bin/fakeserve --bug $b --dir runs/selftest | tail -1
done
echo "== --bug none (must PASS)"; ./bin/torture --seed 12 --iterations 3 --bin ./bin/fakeserve --bug none --dir runs/selftest | tail -1`

var cleanClone = fmt.Sprintf(`set -x
rm -rf %[1]s/clone && git clone -q . %[1]s/clone && cd %[1]s/clone && scripts/build.sh && go test ./... && ./bin/torture --iterations 50 --seed 1 --dir %[1]s/clone-runs`, q(demoDir))
