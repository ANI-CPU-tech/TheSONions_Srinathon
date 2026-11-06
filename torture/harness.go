// Iteration loop: start `ninelives serve` as a child, let the writers run,
// SIGKILL it after a random millisecond delay (from --seed + iteration),
// then run open, check, dump.
package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

const (
	maxDelayMS   = 400              // kill delay is 1..maxDelayMS ms after READY
	readyTimeout = 30 * time.Second // serve must print READY within this
	cliTimeout   = 60 * time.Second // open, check, dump must finish within this
	maxLine      = 8 << 20          // longest stdout line we accept from serve
)

// config is everything the flags set.
type config struct {
	seed       int64
	iterations int
	mode       string
	bin        string
	dir        string
	maxBytes   int64
	bug        string // injected fakeserve fault (L2 self-test), empty for the real binary
}

func (c config) runDir() string { return filepath.Join(c.dir, strconv.FormatInt(c.seed, 10)) }
func (c config) dbPath() string { return filepath.Join(c.runDir(), "db") }

// cliArgs is the argument list for one ninelives command on our database.
// maxBytes is the effective cap after the mode's disk-full default is applied.
func (c config) cliArgs(cmd string, maxBytes int64) []string {
	args := []string{cmd, c.dbPath()}
	if maxBytes > 0 {
		args = append(args, "--max-bytes", strconv.FormatInt(maxBytes, 10))
	}
	return args
}

// result is what one iteration saw, for the summary line.
type result struct {
	iter                      int
	delay                     time.Duration
	sent, acked, errs, flight int
	keys                      int
	recoveryMS                string
	openTime                  time.Duration     // wall time of the full open(), process start included
	note                      string            // mode detail: crash point, recovery kills, compaction kill
	state                     map[string]string // the dump after recovery
}

// runIteration runs one kill/recover cycle. Its random choices come only
// from (seed, iter), so a seed reproduces the op streams and the delay.
// openTime is the previous iteration's full open() time (recovery-crash
// kills land inside a window that long).
func runIteration(c config, o *oracle, iter int, openTime time.Duration) (result, error) {
	rng := rand.New(rand.NewPCG(uint64(c.seed), uint64(iter)*16))
	res := result{iter: iter, delay: time.Duration(1+rng.IntN(maxDelayMS)) * time.Millisecond}
	p := newKillPlan(c.mode, iter, rng)
	o.beginIteration(iter, res.delay)
	if plan := p.String(); plan != "" {
		o.note("PLAN %s", plan)
	}
	self, err := runAndKill(c, o, iter, res.delay, p)
	res.sent, res.acked, res.errs, res.flight = o.counts()
	res.note = p.String()
	if p.serveEnv != nil {
		res.note += map[bool]string{true: " hit", false: " missed"}[self]
	}
	if err != nil {
		return res, err
	}
	rec, err := killRecoveries(c, o, p, openTime)
	res.note += rec
	if err != nil {
		return res, err
	}
	res.state, res.recoveryMS, res.openTime, err = recoverAndDump(c)
	res.keys = len(res.state)
	return res, err
}

// runAndKill starts serve, runs the writers and SIGKILLs serve after delay,
// or earlier if the plan says so. self reports that serve died before our
// kill, which only an armed crash point may cause; wait checks it was SIGKILL.
func runAndKill(c config, o *oracle, iter int, delay time.Duration, p killPlan) (self bool, err error) {
	s, err := startServe(c, o, p.serveEnv)
	if err != nil {
		return false, err
	}
	var wg sync.WaitGroup
	for w := 1; w <= writers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s.runWriter(newGenerator(c.seed, iter, w, c.mode))
		}()
	}
	self = s.waitForKill(delay, p) // the writers are mid-stream: the kill lands during ops
	s.kill(self)
	wg.Wait()
	return self, s.wait()
}

// waitForKill blocks until it is time to kill serve: the random delay is
// up, or (compaction mode) jitter after the Nth compact was sent, so the
// kill tends to land inside or right after a compaction. It returns true
// if serve died first, which only an armed crash point should cause.
func (s *session) waitForKill(delay time.Duration, p killPlan) bool {
	deadline := time.After(delay)
	sent := 0
	for {
		select {
		case <-deadline:
			return false
		case <-s.router.done:
			return true
		case <-s.compactSent:
			if sent++; sent == p.compactN {
				time.Sleep(p.jitter)
				return false
			}
		}
	}
}

// session is one running serve process and the writers talking to it.
type session struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	pipeMu sync.Mutex // one request or whole batch block per write (SPEC 12)
	oracle *oracle
	router *router
	killed atomic.Bool
	stderr *os.File

	compactSent chan struct{} // one tick per compact request sent

	errMu sync.Mutex
	err   error // first protocol error a writer saw
}

// startServe starts serve and waits for its READY line.
// env arms a crash point in serve (crashpoints mode); nil otherwise.
func startServe(c config, o *oracle, env []string) (*session, error) {
	stderr, err := os.OpenFile(filepath.Join(c.runDir(), "serve.stderr"), os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o644)
	if err != nil {
		return nil, err
	}
	maxBytes := diskFullBytes(c.mode, c.maxBytes)
	args := c.cliArgs("serve", maxBytes)
	if c.bug != "" {
		// put --bug right after the binary, before the command, so Go's flag
		// parsing in fakeserve sees it before the first positional argument.
		args = append([]string{"--bug", c.bug}, args...)
	}
	cmd := exec.Command(c.bin, args...)
	cmd.Env = append(os.Environ(), env...)
	cmd.Stderr = stderr
	stdin, err := cmd.StdinPipe()
	if err != nil {
		stderr.Close()
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		stderr.Close()
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		stderr.Close()
		return nil, fmt.Errorf("start serve: %w", err)
	}
	s := &session{cmd: cmd, stdin: stdin, oracle: o, router: newRouter(), stderr: stderr,
		compactSent: make(chan struct{}, 64)}
	sc := bufio.NewScanner(stdout)
	sc.Buffer(make([]byte, 64<<10), maxLine)
	if err := s.awaitReady(sc); err != nil {
		cmd.Process.Kill()
		cmd.Wait()
		stderr.Close()
		return nil, err
	}
	go s.router.run(sc)
	return s, nil
}

// awaitReady reads serve's first line, which must start with "READY ".
func (s *session) awaitReady(sc *bufio.Scanner) error {
	ready := make(chan error, 1)
	go func() {
		switch {
		case !sc.Scan():
			ready <- errors.New("serve exited before printing READY (see serve.stderr)")
		case !strings.HasPrefix(sc.Text(), "READY "):
			ready <- fmt.Errorf("serve's first line is %q, want READY", sc.Text())
		default:
			ready <- nil
		}
	}()
	select {
	case err := <-ready:
		return err
	case <-time.After(readyTimeout):
		return fmt.Errorf("serve printed no READY within %v", readyTimeout)
	}
}

// runWriter sends this writer's ops one at a time, waiting for each reply
// (synchronous writer, SPEC rule 2), until the kill.
func (s *session) runWriter(g *generator) {
	for !s.killed.Load() {
		req := g.next()
		reply := s.router.expect(req.opid)
		if reply == nil {
			return // serve's stdout is closed
		}
		s.oracle.sent(req) // logged before the first byte reaches serve
		if err := s.send(req.wire()); err != nil {
			return // serve is gone; the op stays in flight
		}
		if req.compact {
			select {
			case s.compactSent <- struct{}{}:
			default: // nobody counting
			}
		}
		line, ok := <-reply
		if !ok {
			return // killed before the reply: the op stays in flight
		}
		if err := s.oracle.reply(req.opid, line); err != nil {
			s.setErr(err)
			return
		}
	}
}

// send writes one whole request under the pipe mutex.
func (s *session) send(b []byte) error {
	s.pipeMu.Lock()
	defer s.pipeMu.Unlock()
	_, err := s.stdin.Write(b)
	return err
}

func (s *session) setErr(err error) {
	s.errMu.Lock()
	defer s.errMu.Unlock()
	if s.err == nil {
		s.err = err
	}
}

// kill sends SIGKILL: no SIGTERM, no stdin close, no chance to clean up.
// self means serve already died (an armed crash point SIGKILLed it).
func (s *session) kill(self bool) {
	s.killed.Store(true)
	s.oracle.killed(self)
	s.cmd.Process.Signal(syscall.SIGKILL) // error means it already exited; wait reports that
}

// wait reaps serve and fails unless our SIGKILL is what ended it and every
// line it printed answered an op we sent.
func (s *session) wait() error {
	<-s.router.done
	err := s.cmd.Wait()
	s.stdin.Close()
	s.stderr.Close()
	var exit *exec.ExitError
	if !errors.As(err, &exit) {
		return fmt.Errorf("serve exited by itself before the kill (err %v, see serve.stderr)", err)
	}
	if ws, ok := exit.Sys().(syscall.WaitStatus); !ok || !ws.Signaled() || ws.Signal() != syscall.SIGKILL {
		return fmt.Errorf("serve died by itself before the kill: %v (see serve.stderr)", err)
	}
	if stray := s.router.strays(); len(stray) > 0 {
		return fmt.Errorf("serve printed lines that answer no op in flight: %q", stray)
	}
	s.errMu.Lock()
	defer s.errMu.Unlock()
	return s.err
}

// router hands each line serve prints to the writer waiting for that op id.
// Each writer has at most one op in flight, so a map of channels is enough.
type router struct {
	mu      sync.Mutex
	waiting map[uint64]chan string // nil once stdout is closed
	stray   []string
	done    chan struct{} // closed when stdout hits EOF
}

func newRouter() *router {
	return &router{waiting: map[uint64]chan string{}, done: make(chan struct{})}
}

// expect registers op opid and returns the channel its reply will arrive
// on, or nil if serve's stdout is already closed.
func (r *router) expect(opid uint64) chan string {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.waiting == nil {
		return nil
	}
	ch := make(chan string, 1)
	r.waiting[opid] = ch
	return ch
}

// run delivers lines until EOF, then closes every waiting channel.
func (r *router) run(sc *bufio.Scanner) {
	for sc.Scan() {
		r.deliver(sc.Text())
	}
	if err := sc.Err(); err != nil {
		r.mu.Lock()
		r.stray = append(r.stray, "read error: "+err.Error())
		r.mu.Unlock()
	}
	r.mu.Lock()
	for _, ch := range r.waiting {
		close(ch)
	}
	r.waiting = nil
	r.mu.Unlock()
	close(r.done)
}

// deliver routes one line by its op id, the second field.
func (r *router) deliver(line string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	f := strings.Fields(line)
	if len(f) >= 2 {
		if opid, err := strconv.ParseUint(f[1], 10, 64); err == nil {
			if ch, ok := r.waiting[opid]; ok {
				delete(r.waiting, opid)
				ch <- line
				return
			}
		}
	}
	r.stray = append(r.stray, line)
}

func (r *router) strays() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.stray
}

// recoverAndDump runs open, then check, then dump (brief section 7, step 5)
// and returns the dumped state, open's recovery time and open's wall time.
func recoverAndDump(c config) (map[string]string, string, time.Duration, error) {
	start := time.Now()
	out, err := runCLI(c, "open")
	openTime := time.Since(start)
	if err != nil {
		return nil, "", openTime, err
	}
	recMS := "?"
	for _, f := range strings.Fields(out) {
		if v, ok := strings.CutPrefix(f, "recovery_ms="); ok {
			recMS = v
		}
	}
	if out, err = runCLI(c, "check"); err != nil {
		return nil, recMS, openTime, err
	}
	if strings.TrimSpace(out) != "OK" {
		return nil, recMS, openTime, fmt.Errorf("check printed %q, want OK", strings.TrimSpace(out))
	}
	if out, err = runCLI(c, "dump"); err != nil {
		return nil, recMS, openTime, err
	}
	state, err := parseDump(out)
	return state, recMS, openTime, err
}

// openUntilKilled runs open with env added and SIGKILLs it after delay
// (delay 0: never; it may die from an armed crash point). killed reports
// that SIGKILL ended it; a clean exit 0 means open finished first. Any other
// end, or running past cliTimeout, is an error.
func openUntilKilled(c config, env []string, delay time.Duration) (killed bool, err error) {
	cmd := exec.Command(c.bin, c.cliArgs("open", diskFullBytes(c.mode, c.maxBytes))...)
	cmd.Env = append(os.Environ(), env...)
	var out strings.Builder
	cmd.Stdout, cmd.Stderr = &out, &out
	if err := cmd.Start(); err != nil {
		return false, err
	}
	var timedOut atomic.Bool
	if delay > 0 {
		defer time.AfterFunc(delay, func() { cmd.Process.Signal(syscall.SIGKILL) }).Stop()
	}
	defer time.AfterFunc(cliTimeout, func() { timedOut.Store(true); cmd.Process.Signal(syscall.SIGKILL) }).Stop()
	err = cmd.Wait()
	if err == nil {
		return false, nil
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) && !timedOut.Load() {
		if ws, ok := exit.Sys().(syscall.WaitStatus); ok && ws.Signaled() && ws.Signal() == syscall.SIGKILL {
			return true, nil
		}
	}
	return false, fmt.Errorf("open (env %v) did not exit 0 or die by SIGKILL: %v (timed out: %v)\n%s", env, err, timedOut.Load(), out.String())
}

// runCLI runs one ninelives command and returns its stdout. A non-zero exit
// is an error that includes what the command printed.
func runCLI(c config, cmd string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), cliTimeout)
	defer cancel()
	var stdout, stderr strings.Builder
	x := exec.CommandContext(ctx, c.bin, c.cliArgs(cmd, diskFullBytes(c.mode, c.maxBytes))...)
	x.Stdout, x.Stderr = &stdout, &stderr
	if err := x.Run(); err != nil {
		return stdout.String(), fmt.Errorf("%s: %v\nstdout: %s\nstderr: %s", cmd, err, stdout.String(), stderr.String())
	}
	return stdout.String(), nil
}

// parseDump turns "<key-token> <value-token>" lines into a map of raw bytes.
func parseDump(out string) (map[string]string, error) {
	state := map[string]string{}
	if out == "" {
		return state, nil // empty database
	}
	for i, line := range strings.Split(strings.TrimSuffix(out, "\n"), "\n") {
		f := strings.Fields(line)
		if len(f) != 2 {
			return nil, fmt.Errorf("dump line %d: %q, want <key> <value>", i+1, line)
		}
		k, err := decodeToken(f[0])
		if err != nil {
			return nil, fmt.Errorf("dump line %d: %v", i+1, err)
		}
		v, err := decodeToken(f[1])
		if err != nil {
			return nil, fmt.Errorf("dump line %d: %v", i+1, err)
		}
		if _, dup := state[string(k)]; dup {
			return nil, fmt.Errorf("dump line %d: key %q appears twice", i+1, k)
		}
		state[string(k)] = string(v)
	}
	return state, nil
}
