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
	"strings"
	"sync"
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

	mu     sync.Mutex
	cancel context.CancelFunc // stops the running job; nil when none runs
}

func newApp(root, dir, token string, port int) *app {
	return &app{root: root, dir: dir, db: filepath.Join(dir, "demo.db"), token: token, port: port,
		slot: make(chan struct{}, 1)}
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

func (a *app) release() { <-a.slot }

// shutdown stops whatever runs, so its stream ends and the server can stop.
func (a *app) shutdown() {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.cancel != nil {
		a.cancel()
	}
}

// binInfo says whether one of our binaries is built, and when.
type binInfo struct {
	Path  string `json:"path"`
	Built bool   `json:"built"`
	When  string `json:"when,omitempty"`
}

// handleState is the top bar: paths, built binaries, busy flag.
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
	})
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
