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
}

func (c config) runDir() string { return filepath.Join(c.dir, strconv.FormatInt(c.seed, 10)) }
func (c config) dbPath() string { return filepath.Join(c.runDir(), "db") }

// cliArgs is the argument list for one ninelives command on our database.
func (c config) cliArgs(cmd string) []string {
	args := []string{cmd, c.dbPath()}
	if c.maxBytes > 0 {
		args = append(args, "--max-bytes", strconv.FormatInt(c.maxBytes, 10))
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
	state                     map[string]string // the dump after recovery
}

// runIteration runs one kill/recover cycle. Its random choices come only
// from (seed, iter), so a seed reproduces the op streams and the delay.
func runIteration(c config, o *oracle, iter int) (result, error) {
	rng := rand.New(rand.NewPCG(uint64(c.seed), uint64(iter)*16))
	res := result{iter: iter, delay: time.Duration(1+rng.IntN(maxDelayMS)) * time.Millisecond}
	o.beginIteration(iter, res.delay)
	err := runAndKill(c, o, iter, res.delay)
	res.sent, res.acked, res.errs, res.flight = o.counts()
	if err != nil {
		return res, err
	}
	res.state, res.recoveryMS, err = recoverAndDump(c)
	res.keys = len(res.state)
	return res, err
}

// runAndKill starts serve, runs the writers and SIGKILLs serve after delay.
func runAndKill(c config, o *oracle, iter int, delay time.Duration) error {
	s, err := startServe(c, o)
	if err != nil {
		return err
	}
	var wg sync.WaitGroup
	for w := 1; w <= writers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s.runWriter(newGenerator(c.seed, iter, w))
		}()
	}
	time.Sleep(delay) // the writers are mid-stream: the kill lands during ops
	s.kill()
	wg.Wait()
	return s.wait()
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

	errMu sync.Mutex
	err   error // first protocol error a writer saw
}

// startServe starts serve and waits for its READY line.
func startServe(c config, o *oracle) (*session, error) {
	stderr, err := os.OpenFile(filepath.Join(c.runDir(), "serve.stderr"), os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o644)
	if err != nil {
		return nil, err
	}
	cmd := exec.Command(c.bin, c.cliArgs("serve")...)
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
	s := &session{cmd: cmd, stdin: stdin, oracle: o, router: newRouter(), stderr: stderr}
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
func (s *session) kill() {
	s.killed.Store(true)
	s.oracle.killed()
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
// and returns the dumped state and open's recovery time.
func recoverAndDump(c config) (map[string]string, string, error) {
	out, err := runCLI(c, "open")
	if err != nil {
		return nil, "", err
	}
	recMS := "?"
	for _, f := range strings.Fields(out) {
		if v, ok := strings.CutPrefix(f, "recovery_ms="); ok {
			recMS = v
		}
	}
	if out, err = runCLI(c, "check"); err != nil {
		return nil, recMS, err
	}
	if strings.TrimSpace(out) != "OK" {
		return nil, recMS, fmt.Errorf("check printed %q, want OK", strings.TrimSpace(out))
	}
	if out, err = runCLI(c, "dump"); err != nil {
		return nil, recMS, err
	}
	state, err := parseDump(out)
	return state, recMS, err
}

// runCLI runs one ninelives command and returns its stdout. A non-zero exit
// is an error that includes what the command printed.
func runCLI(c config, cmd string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), cliTimeout)
	defer cancel()
	var stdout, stderr strings.Builder
	x := exec.CommandContext(ctx, c.bin, c.cliArgs(cmd)...)
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
