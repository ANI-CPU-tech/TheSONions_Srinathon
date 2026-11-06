package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Lines that exercise every op-id rule of serve's parser.
var trackerLines = []string{
	"put a 1",            // auto id 1
	"@3:77 put b 2",      // explicit 77, the counter stays
	"@x put c 3",         // bad prefix: id 0, the counter stays
	"@1:2:3 put c 3",     // bad prefix
	"@4",                 // no operation: id 0
	"",                   // blank: skipped
	"batch 2",            // auto id 2, owes 2 lines
	"put d 4",            // block line
	"",                   // block line (blank still counts)
	"@2 get d",           // writer only: auto id 3
	"batch x",            // auto 4, bad count: no block
	"put e 5",            // auto 5, a request again
	"frob",               // auto 6, unknown op
	"batch 1",            // auto 7, owes 1
	"nonsense line here", // block line
	"stats",              // auto 8
}

func TestTrackerIDs(t *testing.T) {
	tr := newTracker()
	var got []string
	for _, l := range trackerLines {
		if r := tr.line(l); r != nil {
			got = append(got, fmt.Sprintf("%d:%s", r.Opid, r.Kind))
		}
	}
	want := "1:put 77:put 0: 0: 0: 2:batch 3:get 4:batch 5:put 6:frob 7:batch 8:stats"
	if strings.Join(got, " ") != want {
		t.Errorf("ids\n got %s\nwant %s", strings.Join(got, " "), want)
	}
}

// The tracker must agree with the real serve on every reply.
func TestTrackerMatchesRealServe(t *testing.T) {
	needBinary(t)
	cmd := exec.Command("../../bin/ninelives", "serve", filepath.Join(t.TempDir(), "db"))
	cmd.Stdin = strings.NewReader(strings.Join(trackerLines, "\n") + "\n")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("serve: %v\n%s", err, out)
	}
	tr := newTracker()
	for _, l := range trackerLines {
		tr.line(l)
	}
	replies := strings.Split(strings.TrimSpace(string(out)), "\n")[1:] // after READY
	for _, l := range replies {
		if tr.answer(l) == nil {
			t.Errorf("reply %q matches no tracked request", l)
		}
	}
	if len(tr.waiting) != 0 {
		for _, r := range tr.waiting {
			t.Errorf("request %q (id %d) got no reply; serve said:\n%s", r.Text, r.Opid, out)
		}
	}
}

// liveServer runs the app on a real loopback port, as the page sees it.
func liveServer(t *testing.T) (*app, string) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	a := newApp("../..", t.TempDir(), testToken, ln.Addr().(*net.TCPAddr).Port)
	srv := httptest.NewUnstartedServer(a.routes())
	srv.Listener = ln
	srv.Start()
	t.Cleanup(srv.Close)
	return a, srv.URL
}

func apiPost(t *testing.T, url, body string) *http.Response {
	t.Helper()
	req, _ := http.NewRequest("POST", url, strings.NewReader(body))
	req.Header.Set("X-NL-Token", testToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

// The judge's part, through the API: serve, a put and a batch by hand,
// a get, then SIGKILL; then recovery keeps what was acked.
func TestServeSessionForReal(t *testing.T) {
	needBinary(t)
	a, url := liveServer(t)
	resp := apiPost(t, url+"/api/serve/start", `{"maxBytes":0}`)
	events := make(chan event, 1000)
	go func() {
		defer close(events)
		sc := bufio.NewScanner(resp.Body)
		sc.Buffer(nil, 64<<20)
		for sc.Scan() {
			var ev event
			json.Unmarshal(sc.Bytes(), &ev)
			events <- ev
		}
	}()
	waitFor := func(match func(event) bool) event {
		t.Helper()
		timeout := time.After(10 * time.Second)
		for {
			select {
			case ev, ok := <-events:
				if !ok {
					t.Fatal("stream ended early")
				}
				if match(ev) {
					return ev
				}
			case <-timeout:
				t.Fatal("timed out")
			}
		}
	}
	waitFor(func(ev event) bool { return ev.Type == "trace" && ev.Trace.Action == "serve" })
	for _, text := range []string{"put a 1", "batch 2\nput b 2\ndel a", "get b"} {
		if r := apiPost(t, url+"/api/serve/send", fmt.Sprintf(`{"text":%q}`, text)); r.StatusCode != 200 {
			b, _ := io.ReadAll(r.Body)
			t.Fatalf("send %q: %d %s", text, r.StatusCode, b)
		}
	}
	val := waitFor(func(ev event) bool { return ev.Type == "reply" && ev.Req.Kind == "get" })
	if val.Req.Reply != "VAL 3 2" || val.Trace.Segments[1].Path != "read" {
		t.Errorf("get reply %+v trace %+v", val.Req, val.Trace)
	}
	if r := apiPost(t, url+"/api/run", `{"action":"dump"}`); r.StatusCode != 409 {
		t.Errorf("a job ran while serve holds the database: %d", r.StatusCode)
	}
	apiPost(t, url+"/api/serve/kill", "")
	ex := waitFor(func(ev event) bool { return ev.Type == "exit" })
	if ex.Exit.Signal != "SIGKILL" || ex.Exit.Code != 137 {
		t.Errorf("exit %+v", ex.Exit)
	}
	waitFor(func(ev event) bool { return ev.Type == "done" })
	evs, ok := runAction(t, a, "recover")
	if !ok || !strings.HasSuffix(outText(evs, "stdout"), "OK\nb 2\n") {
		t.Errorf("recover: ok=%v\n%s", ok, outText(evs, "stdout"))
	}
}

func TestServeSendWithoutServe(t *testing.T) {
	_, url := liveServer(t)
	if r := apiPost(t, url+"/api/serve/send", `{"text":"put a 1"}`); r.StatusCode != 409 {
		t.Errorf("send with no serve: %d", r.StatusCode)
	}
	if r := apiPost(t, url+"/api/serve/kill", ""); r.StatusCode != 409 {
		t.Errorf("kill with no serve: %d", r.StatusCode)
	}
}
