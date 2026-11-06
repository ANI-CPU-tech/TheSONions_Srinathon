// Command nlui is NineLives Lab: a window that runs the real test and demo
// commands (the same ones nlshell offers) and shows, for each one, the exact
// command line, its output, what it did to the database file and which code
// path it took.
//
// Build and run from the repo root:
//
//	go build -o bin/nlui ./scripts/nlui && ./bin/nlui
//
// Standard library only. It listens on 127.0.0.1 and nowhere else, and the
// page, script and styles are embedded in the binary, so it works with
// Wi-Fi off. Every request to /api/ must carry a per-launch random token.
package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"
)

func main() {
	port := flag.Int("port", 0, "TCP port on 127.0.0.1 (0 = any free port)")
	dir := flag.String("dir", filepath.Join(os.TempDir(), "nlui"), "directory for the demo database")
	noBrowser := flag.Bool("no-browser", false, "only print the URL, do not open a window")
	flag.Parse()
	if *port < 0 || *port > 65535 || flag.NArg() > 0 {
		flag.Usage()
		os.Exit(2)
	}
	if err := run(*port, *dir, *noBrowser); err != nil {
		fmt.Fprintln(os.Stderr, "nlui:", err)
		os.Exit(1)
	}
}

// run starts the server and blocks until Ctrl-C, SIGTERM, or the window we
// opened is closed.
func run(port int, dir string, noBrowser bool) error {
	root, err := repoRoot()
	if err != nil {
		return err
	}
	// Commands run from the repo root, like nlshell and the README.
	if err := os.Chdir(root); err != nil {
		return err
	}
	if dir, err = filepath.Abs(dir); err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	token, err := newToken()
	if err != nil {
		return err
	}
	// Loopback only, by construction: there is no flag for the address.
	ln, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
	if err != nil {
		return err
	}
	a := newApp(root, dir, token, ln.Addr().(*net.TCPAddr).Port)
	srv := &http.Server{Handler: a.routes(), ReadHeaderTimeout: 10 * time.Second}
	served := make(chan error, 1)
	go func() { served <- srv.Serve(ln) }()

	url := fmt.Sprintf("http://127.0.0.1:%d/#t=%s", a.port, token)
	fmt.Printf("NineLives Lab: %s\nDemo database: %s\nCtrl-C to quit.\n", url, a.db)
	closed := make(chan struct{})
	if !noBrowser {
		openWindow(url, dir, closed)
	}

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	select {
	case <-stop:
	case <-closed:
		fmt.Println("window closed")
	case err := <-served:
		return err
	}
	a.shutdown() // stop the running job and serve first, so their streams end
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return srv.Shutdown(ctx)
}

// repoRoot walks up from the working directory to the go.mod of module
// ninelives, so nlui can be started from anywhere inside the repo.
func repoRoot() (string, error) {
	start, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for dir := start; ; dir = filepath.Dir(dir) {
		if b, err := os.ReadFile(filepath.Join(dir, "go.mod")); err == nil && modulePath(string(b)) == "ninelives" {
			return dir, nil
		}
		if filepath.Dir(dir) == dir {
			return "", fmt.Errorf("not inside the NineLives repo: no go.mod of module ninelives at or above %s", start)
		}
	}
}

// modulePath returns the module path declared in a go.mod file, or "".
func modulePath(gomod string) string {
	for _, line := range strings.Split(gomod, "\n") {
		if rest, ok := strings.CutPrefix(strings.TrimSpace(line), "module "); ok {
			return strings.Trim(strings.TrimSpace(rest), `"`)
		}
	}
	return ""
}

// newToken returns 128 random bits as hex. Only the page we open learns it.
func newToken() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", errors.New("no randomness for the session token: " + err.Error())
	}
	return hex.EncodeToString(b), nil
}

// openWindow shows url in a Chromium app window (no tabs, no address bar)
// with its own profile in dir, else in the default browser, else it only
// prints the URL. closed is closed when the Chromium window we started
// exits, so closing the window also stops nlui.
func openWindow(url, dir string, closed chan<- struct{}) {
	for _, name := range []string{"chromium", "google-chrome", "google-chrome-stable", "chromium-browser"} {
		path, err := exec.LookPath(name)
		if err != nil {
			continue
		}
		cmd := exec.Command(path, "--app="+url, "--user-data-dir="+filepath.Join(dir, "browser"),
			"--no-first-run", "--no-default-browser-check")
		if err := cmd.Start(); err != nil {
			continue
		}
		started := time.Now()
		go func() {
			cmd.Wait()
			// A launcher that hands the window to another process exits at
			// once; only a window that was really open counts as closed.
			if time.Since(started) > 3*time.Second {
				close(closed)
			}
		}()
		return
	}
	opener := "xdg-open"
	if runtime.GOOS == "darwin" {
		opener = "open"
	}
	if err := exec.Command(opener, url).Start(); err != nil {
		fmt.Println("open the URL above in a browser")
	}
}
