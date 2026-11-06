package main

// Job runner: a job is a list of steps run in order, each one process (or
// one small in-process action). Output streams to the page as NDJSON, one
// event per line. Processes run from an argv list, never through a shell,
// so no key or value typed in the page can become shell syntax.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"slices"
	"strings"
	"sync"
	"syscall"
	"time"
)

const (
	maxLine   = 64 << 10        // longest output line passed to the page
	tailLines = 40              // output lines kept per step for the trace
	stopGrace = 2 * time.Second // SIGINT, then SIGKILL after this
)

// step is one thing a job does.
type step struct {
	Title    string   // what the step is for, shown above the command
	Argv     []string // the process to run (relative paths are from Dir)
	Env      []string // added to the environment, e.g. NINELIVES_CRASH_AT=mid_write
	Dir      string   // working directory; "" = repo root
	OK       []int    // exit codes that let the job go on; nil = only 0
	Trace    string   // engine action for the code-path view ("put", "open", ...); "" = none
	Crash    string   // crash point armed in this step, if any
	NoOutput bool     // any output is a failure (gofmt -l)
	Show     string   // the shell line shown for an in-process step
	fn       func() (string, error)
}

// job is the unit /api/run executes; it stops at the first step whose exit
// code is not in that step's OK list.
type job struct {
	Title   string
	Steps   []step
	Snap    bool // snapshot the demo database before and after each step
	Torture bool // parse torture output lines
}

// event is one NDJSON line of a job's stream.
type event struct {
	Type     string      `json:"type"` // job, step, out, exit, snapshot, trace, torture, done
	Step     int         `json:"step,omitempty"`
	Count    int         `json:"count,omitempty"`
	Title    string      `json:"title,omitempty"`
	Cmd      string      `json:"cmd,omitempty"`
	Stream   string      `json:"stream,omitempty"`
	Text     string      `json:"text,omitempty"`
	OK       bool        `json:"ok,omitempty"`
	Exit     *exitInfo   `json:"exit,omitempty"`
	Snapshot *snapshot   `json:"snapshot,omitempty"`
	Diff     *diffView   `json:"diff,omitempty"`
	Trace    *traceView  `json:"trace,omitempty"`
	Req      *serveReq   `json:"req,omitempty"`
	Reqs     []*serveReq `json:"reqs,omitempty"`
}

// exitInfo is how one step ended.
type exitInfo struct {
	Code   int    `json:"code"` // like a shell's $?: 128+signal when killed
	Signal string `json:"signal,omitempty"`
	OK     bool   `json:"ok"`
	Ms     int64  `json:"ms"`
	Err    string `json:"err,omitempty"` // could not start, cancelled, unexpected output
}

// stream writes events to one HTTP response, one flushed line each.
// Several goroutines (stdout, stderr) send, so it locks.
type stream struct {
	mu  sync.Mutex
	enc *json.Encoder
	rc  *http.ResponseController
}

func newStream(w http.ResponseWriter) *stream {
	w.Header().Set("Content-Type", "application/x-ndjson")
	return &stream{enc: json.NewEncoder(w), rc: http.NewResponseController(w)}
}

// send writes one event. A gone client is not an error here: the request
// context is cancelled too, which stops the job.
func (s *stream) send(ev event) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.enc.Encode(ev) == nil {
		s.rc.Flush()
	}
}

// runJob runs every step and reports whether all of them ended OK.
func (a *app) runJob(ctx context.Context, j job, out *stream) bool {
	out.send(event{Type: "job", Title: j.Title, Count: len(j.Steps)})
	var before *snapshot
	if j.Snap {
		before = takeSnapshot(a.db)
		out.send(event{Type: "snapshot", Snapshot: before})
	}
	for i, s := range j.Steps {
		n := i + 1
		out.send(event{Type: "step", Step: n, Title: s.Title, Cmd: s.display(a.root)})
		ex, tail := a.runStep(ctx, s, func(stream, line string) {
			out.send(event{Type: "out", Step: n, Stream: stream, Text: line})
		})
		out.send(event{Type: "exit", Step: n, Exit: &ex})
		if j.Snap {
			after := takeSnapshot(a.db)
			d := diffOf(before, after)
			out.send(event{Type: "snapshot", Step: n, Snapshot: after, Diff: &d})
			if s.Trace != "" {
				out.send(event{Type: "trace", Step: n, Trace: traceFor(s.Trace, s.Crash, ex, tail, before, after)})
			}
			before = after
		}
		if !ex.OK {
			out.send(event{Type: "done", Text: fmt.Sprintf("stopped at step %d of %d", n, len(j.Steps))})
			return false
		}
	}
	out.send(event{Type: "done", OK: true, Text: "all steps OK"})
	return true
}

// runStep runs one step, sends its output lines to emit, and returns how
// it ended plus its last output lines.
func (a *app) runStep(ctx context.Context, s step, emit func(stream, line string)) (exitInfo, []string) {
	var mu sync.Mutex
	var tail []string
	printed := false
	keep := func(stream string) func(string) {
		return func(line string) {
			mu.Lock()
			tail, printed = append(tail[max(0, len(tail)-tailLines+1):], line), true
			mu.Unlock()
			emit(stream, line)
		}
	}
	start := time.Now()
	var ex exitInfo
	if s.fn != nil {
		ex = runFn(s.fn, keep("stdout"))
	} else {
		ex = a.runProc(ctx, s, keep("stdout"), keep("stderr"))
	}
	ex.Ms = time.Since(start).Milliseconds()
	ex.OK = slices.Contains(okCodes(s), ex.Code) && ex.Err == ""
	if s.NoOutput && printed && ex.Code == 0 {
		ex.OK, ex.Err = false, "printed output; nothing was expected"
	}
	if ctx.Err() != nil {
		ex.OK, ex.Err = false, "cancelled"
	}
	return ex, tail
}

func okCodes(s step) []int {
	if s.OK == nil {
		return []int{0}
	}
	return s.OK
}

// runFn runs an in-process step; its text is shown as output.
func runFn(fn func() (string, error), out func(string)) exitInfo {
	text, err := fn()
	for _, l := range strings.Split(strings.TrimSuffix(text, "\n"), "\n") {
		if l != "" {
			out(l)
		}
	}
	if err != nil {
		return exitInfo{Code: 1, Err: err.Error()}
	}
	return exitInfo{}
}

// runProc runs one process in its own process group, so a cancel reaches
// its children too (torture's serve), and waits for it.
func (a *app) runProc(ctx context.Context, s step, stdout, stderr func(string)) exitInfo {
	cmd := exec.Command(s.Argv[0], s.Argv[1:]...)
	cmd.Dir = a.root
	if s.Dir != "" {
		cmd.Dir = s.Dir
	}
	cmd.Env = append(os.Environ(), s.Env...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	outW, errW := &lines{emit: stdout}, &lines{emit: stderr}
	cmd.Stdout, cmd.Stderr = outW, errW
	cmd.WaitDelay = stopGrace // a grandchild holding our pipe cannot hang the job
	if err := cmd.Start(); err != nil {
		return exitInfo{Code: 127, Err: err.Error()}
	}
	exited := make(chan struct{})
	go stopGroup(ctx, cmd.Process.Pid, exited)
	err := cmd.Wait()
	close(exited)
	outW.end()
	errW.end()
	ex := exitOf(cmd.ProcessState)
	if errors.Is(err, exec.ErrWaitDelay) {
		ex.Err = "a child process kept the output open after exit"
	}
	return ex
}

// stopGroup waits for the context to end, then asks the whole process
// group to stop (SIGINT, like Ctrl-C) and kills it if it is still there
// after stopGrace. It returns as soon as the process has exited.
func stopGroup(ctx context.Context, pid int, exited <-chan struct{}) {
	select {
	case <-exited:
		return
	case <-ctx.Done():
	}
	syscall.Kill(-pid, syscall.SIGINT)
	select {
	case <-exited:
	case <-time.After(stopGrace):
		syscall.Kill(-pid, syscall.SIGKILL)
	}
}

// exitOf turns a wait status into a shell-style exit code.
func exitOf(ps *os.ProcessState) exitInfo {
	ws, ok := ps.Sys().(syscall.WaitStatus)
	if !ok {
		return exitInfo{Code: ps.ExitCode()}
	}
	if ws.Signaled() {
		name := ws.Signal().String()
		switch ws.Signal() {
		case syscall.SIGKILL:
			name = "SIGKILL"
		case syscall.SIGINT:
			name = "SIGINT"
		case syscall.SIGTERM:
			name = "SIGTERM"
		}
		return exitInfo{Code: 128 + int(ws.Signal()), Signal: name}
	}
	return exitInfo{Code: ws.ExitStatus()}
}

// lines is an io.Writer that emits whole lines. A line longer than maxLine
// is cut, so one huge line cannot flood the page.
type lines struct {
	buf  []byte
	cut  bool
	emit func(string)
}

func (w *lines) Write(p []byte) (int, error) {
	n := len(p)
	for len(p) > 0 {
		i := bytes.IndexByte(p, '\n')
		if i < 0 {
			w.add(p)
			break
		}
		w.add(p[:i])
		w.flush()
		p = p[i+1:]
	}
	return n, nil
}

func (w *lines) add(p []byte) {
	if room := maxLine - len(w.buf); len(p) > room {
		p, w.cut = p[:room], true
	}
	w.buf = append(w.buf, p...)
}

func (w *lines) flush() {
	s := strings.TrimSuffix(string(w.buf), "\r")
	if w.cut {
		s += " …(line cut at 64 KiB)"
	}
	w.emit(s)
	w.buf, w.cut = w.buf[:0], false
}

// end emits a last line that had no newline.
func (w *lines) end() {
	if len(w.buf) > 0 || w.cut {
		w.flush()
	}
}

// display is the step as one shell line a person can retype: env, argv,
// quoted only where needed. The step never runs through a shell.
func (s step) display(root string) string {
	if s.fn != nil {
		return s.Show
	}
	var parts []string
	if s.Dir != "" && s.Dir != root {
		parts = append(parts, "cd", shellQuote(s.Dir), "&&")
	}
	for _, e := range s.Env {
		name, val, _ := strings.Cut(e, "=")
		parts = append(parts, name+"="+shellQuote(val))
	}
	for _, a := range s.Argv {
		parts = append(parts, shellQuote(a))
	}
	return strings.Join(parts, " ")
}

var plainWord = regexp.MustCompile(`^[A-Za-z0-9_@%+=:,./-]+$`)

// shellQuote quotes s for sh when it is not a plain word.
func shellQuote(s string) string {
	if plainWord.MatchString(s) {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
