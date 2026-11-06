package main

// The Serve panel: one `ninelives serve` on the demo database, with the
// page's typing as its stdin. "Kill" sends a real SIGKILL; "Close stdin"
// is the clean shutdown. A tracker works out which op id serve gives each
// request, so every reply can be matched to its request, and so the
// requests still waiting at a kill can be listed as in flight.

import (
	"context"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

const snapEvery = 250 * time.Millisecond // at most this often while serve runs

// serveReq is one request typed into serve.
type serveReq struct {
	Opid   uint64 `json:"opid"`
	Kind   string `json:"kind"` // put, del, get, batch, compact, stats, or what was typed
	Text   string `json:"text"`
	Status string `json:"status"` // waiting, ack, val, notfound, stats, err, inflight
	Reply  string `json:"reply,omitempty"`
}

// tracker follows serve's input rules (serveReader, parseRequest and
// readBatchBlock in src/cmd/ninelives/serve.go) to know each op id.
type tracker struct {
	nextID  uint64 // serve's auto-incrementing op id, from 1
	owed    int    // op lines still owed to the open batch block
	block   *serveReq
	waiting []*serveReq
}

func newTracker() *tracker { return &tracker{nextID: 1} }

// line takes one input line and returns the request it starts, or nil for
// a blank line or a line inside a batch block.
func (t *tracker) line(line string) *serveReq {
	line = strings.TrimSuffix(line, "\r")
	if t.owed > 0 { // readBatchBlock takes the next n lines, blank or not
		t.owed--
		t.block.Text += "\n" + line
		return nil
	}
	if strings.TrimSpace(line) == "" {
		return nil // serveReader skips blank lines
	}
	r := &serveReq{Text: line, Status: "waiting"}
	t.waiting = append(t.waiting, r)
	rest, explicit := line, false
	if strings.HasPrefix(rest, "@") {
		sp := strings.IndexByte(rest, ' ')
		if sp < 0 {
			return r // no operation: ERR 0 BADREQ
		}
		id, exp, ok := prefixID(rest[:sp])
		if !ok {
			return r // malformed prefix: ERR 0 BADREQ, no id used up
		}
		rest, explicit, r.Opid = rest[sp+1:], exp, id
	}
	if !explicit {
		r.Opid, t.nextID = t.nextID, t.nextID+1
	}
	f := strings.Fields(rest)
	if len(f) > 0 {
		r.Kind = f[0]
	}
	if len(f) == 2 && f[0] == "batch" {
		if n, err := strconv.Atoi(f[1]); err == nil && n >= 0 {
			t.owed, t.block = n, r
		}
	}
	return r
}

// prefixID mirrors parsePrefix: "@w" (auto id) or "@w:id".
func prefixID(pfx string) (id uint64, explicit, ok bool) {
	parts := strings.Split(pfx[1:], ":")
	if len(parts) > 2 {
		return 0, false, false
	}
	if w, err := strconv.Atoi(parts[0]); err != nil || w < 0 {
		return 0, false, false
	}
	if len(parts) == 1 {
		return 0, false, true
	}
	id, err := strconv.ParseUint(parts[1], 10, 64)
	return id, true, err == nil
}

// answer matches one reply line to the oldest waiting request with its op
// id, or returns nil (READY, or a line that answers nothing we sent).
func (t *tracker) answer(line string) *serveReq {
	f := strings.Fields(line)
	if len(f) < 2 {
		return nil
	}
	opid, err := strconv.ParseUint(f[1], 10, 64)
	if err != nil {
		return nil
	}
	for i, r := range t.waiting {
		if r.Opid == opid {
			r.Status, r.Reply = strings.ToLower(f[0]), line
			t.waiting = append(t.waiting[:i], t.waiting[i+1:]...)
			return r
		}
	}
	return nil
}

// inFlight marks and returns every request that got no reply.
func (t *tracker) inFlight() []*serveReq {
	out := t.waiting
	for _, r := range out {
		r.Status = "inflight"
	}
	t.waiting = nil
	return out
}

// serveSession is the one running serve.
type serveSession struct {
	cmd *exec.Cmd
	out *stream

	wmu   sync.Mutex // one write to stdin at a time, so a batch block stays whole
	stdin io.WriteCloser

	mu    sync.Mutex // guards tr, inode, dirty; never held while writing stdin
	tr    *tracker
	inode uint64
	dirty bool
}

// handleServeStart runs serve until it exits, streaming everything.
func (a *app) handleServeStart(w http.ResponseWriter, r *http.Request) {
	var req struct {
		MaxBytes int64 `json:"maxBytes"`
	}
	if err := readJSON(w, r, 1<<10, &req); err != nil || req.MaxBytes < 0 || req.MaxBytes > 1<<40 {
		writeErr(w, http.StatusBadRequest, "want {\"maxBytes\": 0 or more}")
		return
	}
	if !a.acquire() {
		writeErr(w, http.StatusConflict, "busy: another job or the serve session is running")
		return
	}
	defer a.release()
	s := step{Title: "serve on the demo database", Argv: []string{nlBin, "serve", a.db}}
	if req.MaxBytes > 0 {
		s.Argv = append(s.Argv, "--max-bytes", strconv.FormatInt(req.MaxBytes, 10))
	}
	out := newStream(w)
	before := takeSnapshot(a.db)
	out.send(event{Type: "step", Step: 1, Title: s.Title, Cmd: s.display(a.root)})
	out.send(event{Type: "snapshot", Snapshot: before})
	ss := &serveSession{out: out, tr: newTracker(), inode: before.Inode}
	ex := a.runServe(r.Context(), s, ss, before)
	out.send(event{Type: "exit", Step: 1, Exit: &ex})
	ss.mu.Lock()
	flight := ss.tr.inFlight()
	ss.mu.Unlock()
	if len(flight) > 0 {
		out.send(event{Type: "inflight", Reqs: flight, Trace: killTrace(flight)})
	}
	out.send(event{Type: "snapshot", Snapshot: takeSnapshot(a.db)})
	out.send(event{Type: "done", OK: ex.Signal == "" && ex.Code == 0, Text: "serve ended"})
}

// runServe starts serve, registers the session and waits for serve to
// exit. Closing the page closes serve's stdin (clean exit), then kills it.
func (a *app) runServe(ctx context.Context, s step, ss *serveSession, before *snapshot) exitInfo {
	cmd := exec.Command(s.Argv[0], s.Argv[1:]...)
	cmd.Dir, cmd.Env = a.root, os.Environ()
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return exitInfo{Code: 127, Err: err.Error()}
	}
	outW := &lines{emit: func(l string) { a.serveLine(ss, l, before) }}
	errW := &lines{emit: func(l string) { ss.out.send(event{Type: "out", Step: 1, Stream: "stderr", Text: l}) }}
	cmd.Stdout, cmd.Stderr, cmd.WaitDelay = outW, errW, stopGrace
	start := time.Now()
	if err := cmd.Start(); err != nil {
		return exitInfo{Code: 127, Err: err.Error() + " (build first?)"}
	}
	ss.cmd, ss.stdin = cmd, stdin
	a.setServe(ss)
	defer a.setServe(nil)
	exited := make(chan struct{})
	go ss.snapshots(a.db, exited)
	go ss.closeOnLeave(ctx, exited)
	cmd.Wait()
	close(exited)
	outW.end()
	errW.end()
	ex := exitOf(cmd.ProcessState)
	ex.Ms, ex.OK = time.Since(start).Milliseconds(), ex.Code == 0 || ex.Signal == "SIGKILL"
	return ex
}

// serveLine handles one line serve printed: show it, match it to its
// request, and trace what that request went through.
func (a *app) serveLine(ss *serveSession, line string, before *snapshot) {
	ss.out.send(event{Type: "out", Step: 1, Stream: "stdout", Text: line})
	if strings.HasPrefix(line, "READY ") {
		after := takeSnapshot(a.db)
		t := &traceView{Action: "serve", Segments: []segment{{Path: "open", Status: "done"}}}
		t.evidence("open", nil, before, after)
		t.Summary = "serve opened the database (recovery ran) and printed READY"
		ss.out.send(event{Type: "trace", Trace: t})
		ss.out.send(event{Type: "snapshot", Snapshot: after})
		return
	}
	ss.mu.Lock()
	r := ss.tr.answer(line)
	rewritten := false
	if r != nil && r.Status == "ack" {
		ss.dirty = true
		if fi, err := os.Stat(a.db); err == nil {
			ino := inodeOf(fi)
			rewritten, ss.inode = ino != ss.inode, ino
		}
	}
	ss.mu.Unlock()
	if r != nil {
		ss.out.send(event{Type: "reply", Req: r, Trace: replyTrace(r, rewritten)})
	}
}

// snapshots sends the file view at most every snapEvery while writes
// happen, so a pasted burst does not flood the page.
func (ss *serveSession) snapshots(db string, exited <-chan struct{}) {
	tick := time.NewTicker(snapEvery)
	defer tick.Stop()
	for {
		select {
		case <-exited:
			return
		case <-tick.C:
			ss.mu.Lock()
			dirty := ss.dirty
			ss.dirty = false
			ss.mu.Unlock()
			if dirty {
				ss.out.send(event{Type: "snapshot", Snapshot: takeSnapshot(db)})
			}
		}
	}
}

// closeOnLeave ends serve when the page goes away: stdin EOF first (a
// clean shutdown), SIGKILL if it is still running after stopGrace.
func (ss *serveSession) closeOnLeave(ctx context.Context, exited <-chan struct{}) {
	select {
	case <-exited:
		return
	case <-ctx.Done():
	}
	ss.closeStdin()
	select {
	case <-exited:
	case <-time.After(stopGrace):
		ss.cmd.Process.Signal(syscall.SIGKILL)
	}
}

// send writes text (one or more lines) to serve's stdin in one write, so
// a batch block arrives whole. Each request is recorded first, so its
// reply always finds it. The tracker lock is not held during the write:
// serve may block on stdout, which needs that lock to drain.
func (ss *serveSession) send(text string) error {
	text = strings.TrimSuffix(text, "\n")
	ss.wmu.Lock()
	defer ss.wmu.Unlock()
	var reqs []*serveReq
	ss.mu.Lock()
	for _, l := range strings.Split(text, "\n") {
		if r := ss.tr.line(l); r != nil {
			reqs = append(reqs, r)
		}
	}
	ss.mu.Unlock()
	for _, r := range reqs {
		ss.out.send(event{Type: "sent", Req: r})
	}
	_, err := io.WriteString(ss.stdin, text+"\n")
	return err
}

func (ss *serveSession) closeStdin() {
	ss.wmu.Lock()
	defer ss.wmu.Unlock()
	ss.stdin.Close()
}

func (a *app) setServe(ss *serveSession) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.serve = ss
}

func (a *app) getServe() *serveSession {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.serve
}

// handleServeSend writes {"text": "..."} to serve's stdin.
func (a *app) handleServeSend(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Text string `json:"text"`
	}
	if err := readJSON(w, r, 8<<20, &req); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	ss := a.getServe()
	if ss == nil {
		writeErr(w, http.StatusConflict, "serve is not running")
		return
	}
	if err := ss.send(req.Text); err != nil {
		writeErr(w, http.StatusConflict, "serve's stdin is closed: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// handleServeSignal is "kill" (SIGKILL, no cleanup at all) or "eof"
// (close stdin: serve finishes in-flight ops, closes the DB, exits 0).
func (a *app) handleServeSignal(w http.ResponseWriter, r *http.Request) {
	ss := a.getServe()
	if ss == nil {
		writeErr(w, http.StatusConflict, "serve is not running")
		return
	}
	switch r.PathValue("how") {
	case "kill":
		ss.cmd.Process.Signal(syscall.SIGKILL)
	case "eof":
		ss.closeStdin()
	default:
		writeErr(w, http.StatusNotFound, "want /api/serve/kill or /api/serve/eof")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// replyTrace is the path one answered request went through.
func replyTrace(r *serveReq, rewritten bool) *traceView {
	t := &traceView{Action: "serve " + r.Kind, Segments: []segment{{Path: "serveparse", Status: "done"}}}
	code := ""
	if f := strings.Fields(r.Reply); r.Status == "err" && len(f) > 2 {
		code = f[2]
	}
	if code == "BADREQ" {
		t.Segments[0].Stop, t.Segments[0].Status, t.Summary = "serveparse", "refused", "ERR BADREQ: refused before the engine"
		return t
	}
	var s segment
	switch r.Kind {
	case "put", "del", "batch":
		s = segment{Path: "write", Status: "done"}
		if r.Kind == "batch" {
			s.Lit = []string{"batch_mid_write"}
		}
		if rewritten {
			s.Lit, s.Note = append(s.Lit, "autocompact"), "the file was rewritten: compaction ran"
		}
	case "get":
		s = segment{Path: "read", Status: "done"}
		if r.Status == "notfound" {
			s.Skip, s.Note = []string{"readat"}, "the key is not in the index: NOTFOUND"
		}
	case "compact":
		s = segment{Path: "compact", Status: "done"}
	default: // stats
		t.Summary = r.Reply
		return t
	}
	switch code {
	case "DISKFULL":
		s.Stop, s.Status, s.Note = "guard", "refused", "ErrDiskFull: nothing was written"
		if r.Kind == "compact" {
			s.Stop, s.Note = "copy", "E4: the compacted file would not fit; old file untouched"
		}
	case "TOOLARGE":
		s.Stop, s.Status, s.Note = "validate", "refused", "too large: nothing was written"
	case "FAILED":
		s.Status, s.Note = "failed", "the database is in the failed state: reopen required"
	}
	t.Segments = append(t.Segments, s)
	t.Summary = r.Reply
	return t
}

// killTrace says what is known about requests in flight at a kill.
func killTrace(flight []*serveReq) *traceView {
	t := &traceView{Action: "kill", Summary: strconv.Itoa(len(flight)) + " request(s) in flight at the SIGKILL: each may or may not survive"}
	t.Segments = []segment{{Path: "write", Status: "killed",
		Note: "SIGKILL landed while these ops were in flight; where exactly is unknown. open() keeps a record only if it is complete with a good CRC, so each one is fully there or fully absent."}}
	return t
}

func inodeOf(fi os.FileInfo) uint64 {
	if st, ok := fi.Sys().(*syscall.Stat_t); ok {
		return uint64(st.Ino)
	}
	return 0
}
