package main

import (
	"context"
	"crypto/subtle"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

//go:embed web
var webFS embed.FS

// app is the whole server state. One job or one serve session runs at a
// time: the engine has no file lock, so two processes must never write the
// demo database at once.
type app struct {
	root  string // repo root; every command runs here
	dir   string // demo directory
	db    string // demo database: <dir>/demo.db
	token string // per-launch secret every /api/ request must send
	port  int

	slot chan struct{} // capacity 1: holds the running job or serve session
	quit chan struct{} // closed at shutdown: ends the watch streams
	stop sync.Once
	// freed is when the slot was last released (unix ns). Watch mode
	// ignores file changes nlui itself made: while the slot is held and
	// just after.
	freed atomic.Int64

	mu     sync.Mutex
	cancel context.CancelFunc // stops the running job; nil when none runs
	serve  *serveSession      // the running serve; nil when none runs
}

func newApp(root, dir, token string, port int) *app {
	return &app{root: root, dir: dir, db: filepath.Join(dir, "demo.db"), token: token, port: port,
		slot: make(chan struct{}, 1), quit: make(chan struct{})}
}

// routes is every endpoint, behind the guard.
func (a *app) routes() http.Handler {
	static, err := fs.Sub(webFS, "web")
	if err != nil {
		panic(err) // the embed pattern above guarantees the directory
	}
	mux := http.NewServeMux()
	mux.Handle("GET /", http.FileServerFS(static))
	mux.HandleFunc("GET /api/state", a.handleState)
	mux.HandleFunc("GET /api/bytes", a.handleBytes)
	mux.HandleFunc("GET /api/model", a.handleModel)
	mux.HandleFunc("GET /api/watch", a.handleWatch)
	mux.HandleFunc("POST /api/run", a.handleRun)
	mux.HandleFunc("POST /api/cancel", a.handleCancel)
	mux.HandleFunc("POST /api/serve/start", a.handleServeStart)
	mux.HandleFunc("POST /api/serve/send", a.handleServeSend)
	mux.HandleFunc("POST /api/serve/{how}", a.handleServeSignal)
	return a.guard(mux)
}

// guard rejects any request that did not come from the page we opened:
//   - Host must be our loopback address, which defeats DNS rebinding (a
//     web page whose own host name resolves to 127.0.0.1);
//   - Origin, when the browser sends one, must be ours, which defeats
//     cross-site form posts;
//   - every /api/ call must carry the token, which other pages cannot read.
//
// Static files need no token: they hold no secret.
func (a *app) guard(next http.Handler) http.Handler {
	hosts := []string{fmt.Sprintf("127.0.0.1:%d", a.port), fmt.Sprintf("localhost:%d", a.port)}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case !oneOf(r.Host, hosts):
			http.Error(w, "forbidden: wrong Host", http.StatusForbidden)
			return
		case r.Header.Get("Origin") != "" && !oneOf(r.Header.Get("Origin"), []string{"http://" + hosts[0], "http://" + hosts[1]}):
			http.Error(w, "forbidden: wrong Origin", http.StatusForbidden)
			return
		case strings.HasPrefix(r.URL.Path, "/api/") &&
			subtle.ConstantTimeCompare([]byte(r.Header.Get("X-NL-Token")), []byte(a.token)) != 1:
			http.Error(w, "forbidden: missing or wrong token", http.StatusForbidden)
			return
		}
		h := w.Header()
		h.Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data:; connect-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'none'")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}

func oneOf(s string, list []string) bool {
	for _, x := range list {
		if s == x {
			return true
		}
	}
	return false
}

// acquire takes the one slot, or reports what holds it.
func (a *app) acquire() bool {
	select {
	case a.slot <- struct{}{}:
		return true
	default:
		return false
	}
}

func (a *app) release() {
	a.freed.Store(time.Now().UnixNano())
	<-a.slot
}

// ownWrite reports whether the file may have been changed by nlui's own
// job or serve session: one runs now, or one ended within quiet.
func (a *app) ownWrite(quiet time.Duration) bool {
	return len(a.slot) > 0 || time.Since(time.Unix(0, a.freed.Load())) < quiet
}

// shutdown stops whatever runs, so its stream ends and the server can
// stop: the job is cancelled, serve gets stdin EOF (a clean exit).
func (a *app) shutdown() {
	a.stop.Do(func() { close(a.quit) })
	a.cancelJob()
	if ss := a.getServe(); ss != nil {
		ss.closeStdin()
	}
}

// cancelJob stops the running job, if any, and says whether there was one.
func (a *app) cancelJob() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.cancel == nil {
		return false
	}
	a.cancel()
	return true
}

func (a *app) setCancel(c context.CancelFunc) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.cancel = c
}

// handleRun builds the requested job, takes the slot and streams the job
// to the page. Closing the page (or /api/cancel) stops it.
func (a *app) handleRun(w http.ResponseWriter, r *http.Request) {
	var req runRequest
	if err := readJSON(w, r, 8<<20, &req); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	j, err := a.buildJob(req)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if !a.acquire() {
		writeErr(w, http.StatusConflict, "busy: another job or the serve session is running")
		return
	}
	defer a.release()
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	a.setCancel(cancel)
	defer a.setCancel(nil)
	a.runJob(ctx, j, newStream(w))
}

func (a *app) handleCancel(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]bool{"cancelled": a.cancelJob()})
}

// handleModel is what the page needs to draw its forms.
func (a *app) handleModel(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"actions": actions, "modes": modes, "crashPoints": crashPoints, "paths": paths})
}

// binInfo says whether one of our binaries is built, and when.
type binInfo struct {
	Path  string `json:"path"`
	Built bool   `json:"built"`
	When  string `json:"when,omitempty"`
}

// handleState is the top bar (paths, built binaries, busy flag) and the
// Disk panel (a read-only snapshot of the demo database).
func (a *app) handleState(w http.ResponseWriter, r *http.Request) {
	var bins []binInfo
	for _, b := range []string{"bin/ninelives", "bin/torture", "bin/fakeserve"} {
		bi := binInfo{Path: "./" + b}
		if fi, err := os.Stat(filepath.Join(a.root, b)); err == nil && fi.Mode().IsRegular() {
			bi.Built, bi.When = true, fi.ModTime().Format(time.DateTime)
		}
		bins = append(bins, bi)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"root": a.root, "dir": a.dir, "db": a.db, "bins": bins, "busy": len(a.slot) > 0,
		"snapshot": takeSnapshot(a.db),
	})
}

// handleBytes serves ?off=&len= bytes of the demo database as hex, for the
// record inspector.
func (a *app) handleBytes(w http.ResponseWriter, r *http.Request) {
	off, err1 := strconv.ParseInt(r.URL.Query().Get("off"), 10, 64)
	n, err2 := strconv.ParseInt(r.URL.Query().Get("len"), 10, 64)
	if err1 != nil || err2 != nil {
		writeErr(w, http.StatusBadRequest, "off and len must be integers")
		return
	}
	h, err := readHex(a.db, off, n)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"off": off, "hex": h})
}

// writeJSON sends v as the whole response.
func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}

// writeErr sends {"error": msg}.
func writeErr(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}

// readJSON decodes a request body of at most limit bytes into v. Unknown
// fields are an error, so a typo in the page fails loudly.
func readJSON(w http.ResponseWriter, r *http.Request, limit int64, v any) error {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, limit))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			return fmt.Errorf("request body over %d bytes", limit)
		}
		return fmt.Errorf("bad request body: %v", err)
	}
	return nil
}
