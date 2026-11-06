package main

// Command catalogue: every button in the page is one action here, built
// into a job of argv steps. The scenarios are the same as nlshell's
// (scripts/nlshell/commands.go), as separate steps so the page can show
// the file between them. Every argument is checked here, before anything
// runs.

import (
	"errors"
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

const (
	nlBin      = "./bin/ninelives"
	tortureBin = "./bin/torture"
	maxBatch   = 20_000 // batch lines we accept; the engine itself stops at 10,000 ops
)

// modes are the torture modes (torture/modes.go allModes).
var modes = []string{"default", "large-values", "large-batch", "disk-full", "recovery-crash", "compaction", "crashpoints"}

// crashPoints are the engine's crash points (docs/SPEC.md section 8), in
// code order. A test checks this list against src/engine.
var crashPoints = []string{
	"before_write", "mid_write", "after_write", "after_fsync", "after_index", "batch_mid_write",
	"compact_mid_temp", "compact_after_temp_fsync", "compact_after_rename", "compact_after_dir_fsync",
	"recovery_mid_scan", "recovery_before_truncate", "recovery_after_truncate",
}

// action is one button. build gets the request's positional args.
type action struct {
	Name  string `json:"name"`
	Panel string `json:"panel"` // cli, crash, tests, torture
	Args  string `json:"args,omitempty"`
	Desc  string `json:"desc"`
	build func(a *app, args []string, maxBytes int64) (job, error)
}

// runRequest is the body of POST /api/run.
type runRequest struct {
	Action   string   `json:"action"`
	Args     []string `json:"args"`
	MaxBytes int64    `json:"maxBytes"`
}

var actions = []action{
	{Name: "reset", Panel: "cli", Desc: "delete the demo database and start fresh", build: simple("reset", (*app).resetStep)},
	{Name: "open", Panel: "cli", Desc: "open (runs recovery) and print the key count", build: cliAction("open", 0)},
	{Name: "put", Panel: "cli", Args: "<key> <value>", Desc: "store a value; x:<hex> for bytes", build: cliAction("put", 2)},
	{Name: "get", Panel: "cli", Args: "<key>", Desc: "print the value, or NOTFOUND", build: cliAction("get", 1)},
	{Name: "del", Panel: "cli", Args: "<key>", Desc: "delete a key (a missing key still writes a record)", build: cliAction("del", 1)},
	{Name: "batch", Panel: "cli", Args: "<op line>...", Desc: "apply put/del lines as one atomic record", build: batchAction},
	{Name: "dump", Panel: "cli", Desc: "every key and value, sorted", build: cliAction("dump", 0)},
	{Name: "check", Panel: "cli", Desc: "read-only integrity check: OK or CORRUPT", build: cliAction("check", 0)},
	{Name: "compact", Panel: "cli", Desc: "rewrite the live keys into a new file", build: cliAction("compact", 0)},
	{Name: "recover", Panel: "cli", Desc: "open, check, dump: what torture does after a kill", build: recoverAction},
	{Name: "disk-full", Panel: "cli", Desc: "a put over --max-bytes 30 must fail with exit code 3", build: diskFullAction},
	{Name: "cli-tour", Panel: "cli", Desc: "every CLI command once, on a fresh database", build: tourAction},

	{Name: "crash", Panel: "crash", Args: "<crash-point>", Desc: "SIGKILL at one crash point, then recover", build: crashAction},
	{Name: "f4", Panel: "crash", Desc: "cut the last byte off the file by hand, then recover (F4)", build: f4Action},

	{Name: "build", Panel: "tests", Desc: "scripts/build.sh, plus bin/fakeserve", build: buildAction},
	{Name: "test", Panel: "tests", Desc: "gofmt, go vet and every Go test", build: testAction},
	{Name: "test-race", Panel: "tests", Desc: "engine, torture and lab tests under the race detector", build: goTest("race detector", "-race", "./tests/engine/", "./torture/", "./scripts/nlui/")},
	{Name: "test-crashpoints", Panel: "tests", Desc: "real SIGKILL at all 13 engine crash points (Go test)", build: goTest("crash points", "./tests/engine", "-run", "TestCrashPoints", "-v")},
	{Name: "test-recovery-1m", Panel: "tests", Desc: "F3: open() of 1M keys must take under 2 s", build: goTest("1M-key recovery", "./tests/engine", "-run", "TestRecoveryMillionKeysUnder2s", "-v")},
	{Name: "bench-recovery", Panel: "tests", Desc: "benchmark 1M-key recovery", build: goTest("recovery benchmark", "./tests/engine", "-bench", "BenchmarkRecoveryMillionKeys", "-run", "^$")},
	{Name: "selftest", Panel: "tests", Desc: "fakeserve with 4 injected bugs: the oracle must catch each one", build: selftestAction},
	{Name: "clean-clone", Panel: "tests", Desc: "fresh clone, build, test, short torture run", build: cleanCloneAction},

	{Name: "torture", Panel: "torture", Args: "<mode> [seed] [iterations]", Desc: "kill serve at random moments and check every ACK survived", build: tortureAction},
	{Name: "torture-all", Panel: "torture", Desc: "every mode in a row, the seeds from PROGRESS.md (about 5 min)", build: tortureAllAction},
}

// buildJob checks req and turns it into a job.
func (a *app) buildJob(req runRequest) (job, error) {
	i := slices.IndexFunc(actions, func(x action) bool { return x.Name == req.Action })
	if i < 0 {
		return job{}, fmt.Errorf("unknown action %q", req.Action)
	}
	if req.MaxBytes < 0 || req.MaxBytes > 1<<40 {
		return job{}, errors.New("max bytes must be between 0 (no limit) and 2^40")
	}
	return actions[i].build(a, req.Args, req.MaxBytes)
}

// cli is one ./bin/ninelives command on the demo database.
func (a *app) cli(title, cmd string, maxBytes int64, args ...string) step {
	argv := append([]string{nlBin, cmd, a.db}, args...)
	if maxBytes > 0 && cmd != "check" { // check takes no cap
		argv = append(argv, "--max-bytes", strconv.FormatInt(maxBytes, 10))
	}
	return step{Title: title, Argv: argv, Trace: cmd}
}

// armed arms crash point p for s: the process SIGKILLs itself there.
// Exit 0 stays allowed so a point that is not reached shows as missed.
func armed(s step, p string) step {
	s.Env, s.Crash, s.OK = []string{"NINELIVES_CRASH_AT=" + p}, p, []int{0, 137}
	return s
}

func allow(s step, codes ...int) step {
	s.OK = codes
	return s
}

func (a *app) resetStep() step {
	targets := []string{a.db, a.db + ".compact", a.db + ".init", a.batchFile()}
	quoted := make([]string, len(targets))
	for i, t := range targets {
		quoted[i] = shellQuote(t)
	}
	return step{Title: "fresh database", Show: "rm -f " + strings.Join(quoted, " "), fn: func() (string, error) {
		for _, t := range targets {
			if err := os.Remove(t); err != nil && !os.IsNotExist(err) {
				return "", err
			}
		}
		return "fresh database: " + a.db, nil
	}}
}

func (a *app) batchFile() string { return filepath.Join(a.dir, "batch.txt") }

// batchFileStep writes the op lines to the batch file the CLI reads.
func (a *app) batchFileStep(ops []string) step {
	quoted := make([]string, len(ops))
	for i, o := range ops {
		quoted[i] = shellQuote(o)
	}
	return step{Title: "write the batch file", Show: fmt.Sprintf("printf '%%s\\n' %s > %s", strings.Join(quoted, " "), shellQuote(a.batchFile())),
		fn: func() (string, error) {
			text := strings.Join(ops, "\n") + "\n"
			return text, os.WriteFile(a.batchFile(), []byte(text), 0o644)
		}}
}

// truncateStep cuts n bytes off the end of the demo database, done in Go
// because macOS has no truncate command.
func (a *app) truncateStep(n int64) step {
	return step{Title: "cut the last byte off by hand", Show: fmt.Sprintf("truncate -s -%d %s", n, shellQuote(a.db)), fn: func() (string, error) {
		fi, err := os.Stat(a.db)
		if err != nil {
			return "", err
		}
		if fi.Size()-n < 24 {
			return "", fmt.Errorf("file is %d bytes; cutting %d would reach the header", fi.Size(), n)
		}
		return fmt.Sprintf("%d -> %d bytes", fi.Size(), fi.Size()-n), os.Truncate(a.db, fi.Size()-n)
	}}
}

// simple is an action of fixed steps that takes no arguments.
func simple(title string, steps ...func(*app) step) func(*app, []string, int64) (job, error) {
	return func(a *app, args []string, _ int64) (job, error) {
		if len(args) != 0 {
			return job{}, fmt.Errorf("%s takes no arguments", title)
		}
		j := job{Title: title, Snap: true}
		for _, s := range steps {
			j.Steps = append(j.Steps, s(a))
		}
		return j, nil
	}
}

// cliAction is one CLI command with exactly n user arguments.
func cliAction(cmd string, n int) func(*app, []string, int64) (job, error) {
	return func(a *app, args []string, maxBytes int64) (job, error) {
		if len(args) != n {
			return job{}, fmt.Errorf("%s takes %d argument(s), got %d", cmd, n, len(args))
		}
		for _, s := range args {
			if s == "" || strings.ContainsAny(s, "\n\r") {
				return job{}, fmt.Errorf("%s: empty argument or a line break in one (use x: for the empty value)", cmd)
			}
		}
		return job{Title: strings.TrimSpace(cmd + " " + strings.Join(args, " ")), Snap: true,
			Steps: []step{a.cli("", cmd, maxBytes, args...)}}, nil
	}
}

func batchAction(a *app, ops []string, maxBytes int64) (job, error) {
	if len(ops) == 0 || len(ops) > maxBatch {
		return job{}, fmt.Errorf("batch needs 1 to %d op lines", maxBatch)
	}
	for _, o := range ops {
		if strings.ContainsAny(o, "\n\r") {
			return job{}, errors.New("batch: one op per line")
		}
	}
	return job{Title: fmt.Sprintf("batch of %d op lines", len(ops)), Snap: true,
		Steps: []step{a.batchFileStep(ops), a.cli("apply them as one record", "batch", maxBytes, a.batchFile())}}, nil
}

func recoverAction(a *app, args []string, maxBytes int64) (job, error) {
	if len(args) != 0 {
		return job{}, errors.New("recover takes no arguments")
	}
	return job{Title: "recover", Snap: true, Steps: []step{
		a.cli("open runs recovery", "open", maxBytes),
		a.cli("check must print OK", "check", 0),
		a.cli("dump what survived", "dump", maxBytes),
	}}, nil
}

func diskFullAction(a *app, args []string, _ int64) (job, error) {
	if len(args) != 0 {
		return job{}, errors.New("disk-full takes no arguments")
	}
	return job{Title: "disk full", Snap: true, Steps: []step{
		allow(a.cli("a put that cannot fit under 30 bytes: exit 3, nothing written", "put", 30, "big", "1234567890"), 3),
	}}, nil
}

func tourAction(a *app, args []string, _ int64) (job, error) {
	if len(args) != 0 {
		return job{}, errors.New("cli-tour takes no arguments")
	}
	return job{Title: "CLI tour", Snap: true, Steps: []step{
		a.resetStep(),
		a.cli("create the file", "open", 0),
		a.cli("", "put", 0, "a", "1"),
		a.cli("bytes as x:<hex>", "put", 0, "b", "x:00ff"),
		a.cli("", "get", 0, "a"),
		a.cli("a missing key", "get", 0, "zz"),
		a.cli("", "del", 0, "a"),
		a.batchFileStep([]string{"put c 3", "put d 4", "del c"}),
		a.cli("last op on a key wins: c ends deleted", "batch", 0, a.batchFile()),
		a.cli("", "dump", 0),
		a.cli("", "check", 0),
		a.cli("drop dead records", "compact", 0),
		a.cli("same keys after compaction", "dump", 0),
	}}, nil
}

// crashAction is nlshell's crashScript as steps: a committed key, the
// command that reaches the point with the point armed, check before
// recovery, open, check, dump.
func crashAction(a *app, args []string, _ int64) (job, error) {
	if len(args) != 1 || !slices.Contains(crashPoints, args[0]) {
		return job{}, fmt.Errorf("crash needs one of: %s", strings.Join(crashPoints, ", "))
	}
	p := args[0]
	steps := []step{a.resetStep(), a.cli("a committed key that must survive", "put", 0, "base", "0")}
	switch {
	case p == "batch_mid_write":
		steps = append(steps, a.batchFileStep([]string{"put k1 v1", "put k2 v2"}),
			armed(a.cli("the batch, killed half-written", "batch", 0, a.batchFile()), p))
	case strings.HasPrefix(p, "compact_"):
		steps = append(steps, a.cli("", "put", 0, "k1", "v1"),
			a.cli("overwrite k1, so there is a dead record to drop", "put", 0, "k1", "v2"),
			armed(a.cli("compact, killed at "+p, "compact", 0), p))
	case strings.HasPrefix(p, "recovery_"):
		steps = append(steps, armed(a.cli("leave a torn tail: killed half-written", "put", 0, "torn", "x"), "mid_write"),
			armed(a.cli("recovery, killed at "+p, "open", 0), p))
	default:
		steps = append(steps, armed(a.cli("put k1, killed at "+p, "put", 0, "k1", "v1"), p))
	}
	steps = append(steps,
		allow(a.cli("check before recovery (CORRUPT is expected after a torn write or with a temp file left)", "check", 0), 0, 1),
		a.cli("open runs recovery", "open", 0),
		a.cli("check after recovery must print OK", "check", 0),
		a.cli("base must still be there", "dump", 0))
	return job{Title: "crash at " + p, Snap: true, Steps: steps}, nil
}

func f4Action(a *app, args []string, _ int64) (job, error) {
	if len(args) != 0 {
		return job{}, errors.New("f4 takes no arguments")
	}
	return job{Title: "F4: file cut by 1 byte", Snap: true, Steps: []step{
		a.resetStep(),
		a.cli("", "put", 0, "a", "1"), a.cli("", "put", 0, "b", "1"), a.cli("", "put", 0, "c", "1"),
		a.truncateStep(1),
		allow(a.cli("check before recovery: the last record is torn", "check", 0), 0, 1),
		a.cli("open truncates the torn record", "open", 0),
		a.cli("check after recovery must print OK", "check", 0),
		a.cli("a and b are there; c was the cut record", "dump", 0),
	}}, nil
}

func buildAction(_ *app, args []string, _ int64) (job, error) {
	if len(args) != 0 {
		return job{}, errors.New("build takes no arguments")
	}
	return job{Title: "build", Steps: []step{
		{Title: "the one build command", Argv: []string{"scripts/build.sh"}},
		{Title: "the fake server for the oracle self-test", Argv: []string{"go", "build", "-o", "bin/fakeserve", "./torture/fakeserve"}},
	}}, nil
}

func testAction(_ *app, args []string, _ int64) (job, error) {
	if len(args) != 0 {
		return job{}, errors.New("test takes no arguments")
	}
	return job{Title: "gofmt, vet, tests", Steps: []step{
		{Title: "gofmt: must list no file", Argv: []string{"gofmt", "-l", "."}, NoOutput: true},
		{Title: "go vet", Argv: []string{"go", "vet", "./..."}},
		{Title: "every Go test", Argv: []string{"go", "test", "./..."}},
	}}, nil
}

// goTest is one go test command.
func goTest(title string, args ...string) func(*app, []string, int64) (job, error) {
	return func(_ *app, extra []string, _ int64) (job, error) {
		if len(extra) != 0 {
			return job{}, fmt.Errorf("%s takes no arguments", title)
		}
		return job{Title: title, Steps: []step{{Title: title, Argv: append([]string{"go", "test"}, args...)}}}, nil
	}
}

func selftestAction(_ *app, args []string, _ int64) (job, error) {
	if len(args) != 0 {
		return job{}, errors.New("selftest takes no arguments")
	}
	steps := []step{{Title: "build the fake server", Argv: []string{"go", "build", "-o", "bin/fakeserve", "./torture/fakeserve"}}}
	for _, bug := range []string{"drop-ack", "partial-batch", "garbage", "stale"} {
		steps = append(steps, allow(step{Title: "--bug " + bug + ": the oracle must catch it (exit 1)",
			Argv: []string{tortureBin, "--seed", "11", "--iterations", "5", "--bin", "./bin/fakeserve", "--bug", bug, "--dir", "runs/selftest"}}, 1))
	}
	steps = append(steps, step{Title: "--bug none: an honest server must pass",
		Argv: []string{tortureBin, "--seed", "12", "--iterations", "3", "--bin", "./bin/fakeserve", "--bug", "none", "--dir", "runs/selftest"}})
	return job{Title: "oracle self-test", Steps: steps, Torture: true}, nil
}

func cleanCloneAction(a *app, args []string, _ int64) (job, error) {
	if len(args) != 0 {
		return job{}, errors.New("clean-clone takes no arguments")
	}
	clone, runs := filepath.Join(a.dir, "clone"), filepath.Join(a.dir, "clone-runs")
	return job{Title: "clean clone", Torture: true, Steps: []step{
		{Title: "remove the last clone", Show: "rm -rf " + shellQuote(clone) + " " + shellQuote(runs), fn: func() (string, error) {
			return "", errors.Join(os.RemoveAll(clone), os.RemoveAll(runs))
		}},
		{Title: "clone the committed state (local, no network)", Argv: []string{"git", "clone", "-q", ".", clone}},
		{Title: "the README build command", Argv: []string{"scripts/build.sh"}, Dir: clone},
		{Title: "every Go test", Argv: []string{"go", "test", "./..."}, Dir: clone},
		{Title: "a short torture run", Argv: []string{tortureBin, "--iterations", "50", "--seed", "1", "--dir", runs}, Dir: clone},
	}}, nil
}

// tortureAction: <mode> [seed] [iterations]. No seed: a new random one,
// like a judge would pick.
func tortureAction(_ *app, args []string, maxBytes int64) (job, error) {
	if len(args) < 1 || len(args) > 3 || !slices.Contains(modes, args[0]) {
		return job{}, fmt.Errorf("torture needs a mode (%s), then an optional seed and iteration count", strings.Join(modes, ", "))
	}
	seed, iters := strconv.Itoa(1+rand.IntN(999_999)), "100"
	if len(args) > 1 && args[1] != "" {
		if _, err := strconv.ParseInt(args[1], 10, 64); err != nil {
			return job{}, fmt.Errorf("seed %q is not an integer", args[1])
		}
		seed = args[1]
	}
	if len(args) > 2 && args[2] != "" {
		n, err := strconv.Atoi(args[2])
		if err != nil || n < 1 || n > 100_000 {
			return job{}, fmt.Errorf("iterations %q: want 1 to 100000", args[2])
		}
		iters = args[2]
	}
	argv := []string{tortureBin, "--seed", seed, "--iterations", iters, "--mode", args[0]}
	if maxBytes > 0 {
		argv = append(argv, "--max-bytes", strconv.FormatInt(maxBytes, 10))
	}
	title := fmt.Sprintf("torture %s, seed %s, %s iterations", args[0], seed, iters)
	return job{Title: title, Torture: true, Steps: []step{{Title: title, Argv: argv}}}, nil
}

func tortureAllAction(_ *app, args []string, _ int64) (job, error) {
	if len(args) != 0 {
		return job{}, errors.New("torture-all takes no arguments")
	}
	runs := []struct {
		mode, seed, iters string
	}{
		{"default", "12345", "500"}, {"large-values", "999", "100"}, {"large-batch", "31337", "100"},
		{"disk-full", "2024", "100"}, {"recovery-crash", "42", "100"}, {"compaction", "7", "100"},
		{"crashpoints", "1306", "130"},
	}
	j := job{Title: "every torture mode", Torture: true}
	for _, r := range runs {
		j.Steps = append(j.Steps, step{Title: fmt.Sprintf("%s, seed %s", r.mode, r.seed),
			Argv: []string{tortureBin, "--iterations", r.iters, "--seed", r.seed, "--mode", r.mode}})
	}
	return j, nil
}
