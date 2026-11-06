package main

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// runTestJob runs j and returns its events in order.
func runTestJob(t *testing.T, a *app, ctx context.Context, j job) ([]event, bool) {
	t.Helper()
	w := httptest.NewRecorder()
	ok := a.runJob(ctx, j, newStream(w))
	return parseEvents(t, w.Body.String()), ok
}

func parseEvents(t *testing.T, body string) []event {
	t.Helper()
	var evs []event
	sc := bufio.NewScanner(strings.NewReader(body))
	for sc.Scan() {
		var ev event
		if err := json.Unmarshal(sc.Bytes(), &ev); err != nil {
			t.Fatalf("bad NDJSON line %q: %v", sc.Text(), err)
		}
		evs = append(evs, ev)
	}
	return evs
}

func outText(evs []event, stream string) string {
	var b strings.Builder
	for _, ev := range evs {
		if ev.Type == "out" && ev.Stream == stream {
			b.WriteString(ev.Text + "\n")
		}
	}
	return b.String()
}

func exits(evs []event) []exitInfo {
	var out []exitInfo
	for _, ev := range evs {
		if ev.Type == "exit" {
			out = append(out, *ev.Exit)
		}
	}
	return out
}

func TestJobStreamsAndStops(t *testing.T) {
	a := testApp(t)
	j := job{Title: "t", Steps: []step{
		{Title: "both streams", Argv: []string{"sh", "-c", "echo out1; echo err1 >&2; printf 'no newline'"}},
		{Title: "allowed failure", Argv: []string{"sh", "-c", "exit 3"}, OK: []int{0, 3}},
		{Title: "real failure", Argv: []string{"sh", "-c", "exit 4"}},
		{Title: "never runs", Argv: []string{"sh", "-c", "echo SHOULD-NOT-RUN"}},
	}}
	evs, ok := runTestJob(t, a, context.Background(), j)
	if ok {
		t.Error("job with a failing step reported OK")
	}
	if got := outText(evs, "stdout"); got != "out1\nno newline\n" {
		t.Errorf("stdout %q", got)
	}
	if got := outText(evs, "stderr"); got != "err1\n" {
		t.Errorf("stderr %q", got)
	}
	ex := exits(evs)
	if len(ex) != 3 || !ex[0].OK || ex[1].Code != 3 || !ex[1].OK || ex[2].Code != 4 || ex[2].OK {
		t.Errorf("exits %+v", ex)
	}
	if last := evs[len(evs)-1]; last.Type != "done" || last.OK {
		t.Errorf("last event %+v", last)
	}
}

func TestJobSignalAndNoOutput(t *testing.T) {
	a := testApp(t)
	evs, _ := runTestJob(t, a, context.Background(), job{Steps: []step{
		{Argv: []string{"sh", "-c", "kill -9 $$"}, OK: []int{137}},
		{Argv: []string{"sh", "-c", "echo unformatted.go"}, NoOutput: true},
	}})
	ex := exits(evs)
	if len(ex) != 2 || ex[0].Code != 137 || ex[0].Signal != "SIGKILL" || !ex[0].OK {
		t.Errorf("SIGKILL step %+v", ex)
	}
	if ex[1].OK || ex[1].Err == "" {
		t.Errorf("NoOutput step that printed was OK: %+v", ex[1])
	}
}

// Cancel must stop the whole process group, grandchildren included.
func TestJobCancelKillsGroup(t *testing.T) {
	a := testApp(t)
	pidFile := t.TempDir() + "/pid"
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(300*time.Millisecond, cancel)
	start := time.Now()
	evs, ok := runTestJob(t, a, ctx, job{Steps: []step{
		{Argv: []string{"sh", "-c", "sleep 30 & echo $! > " + pidFile + "; wait"}},
	}})
	if ok || time.Since(start) > 5*time.Second {
		t.Fatalf("cancel did not stop the job (ok=%v, %v)", ok, time.Since(start))
	}
	if ex := exits(evs); len(ex) != 1 || ex[0].Err != "cancelled" {
		t.Errorf("exit %+v", ex)
	}
	b, _ := os.ReadFile(pidFile)
	pid, err := strconv.Atoi(strings.TrimSpace(string(b)))
	if err != nil {
		t.Fatalf("no grandchild pid: %q", b)
	}
	time.Sleep(100 * time.Millisecond)
	if syscall.Kill(pid, 0) == nil {
		syscall.Kill(pid, syscall.SIGKILL)
		t.Error("grandchild sleep still running after cancel")
	}
}

func TestJobInProcessStep(t *testing.T) {
	a := testApp(t)
	evs, ok := runTestJob(t, a, context.Background(), job{Steps: []step{
		{Show: "echo hi", fn: func() (string, error) { return "hi\nthere\n", nil }},
		{Show: "false", fn: func() (string, error) { return "", os.ErrPermission }},
	}})
	if ok || outText(evs, "stdout") != "hi\nthere\n" {
		t.Errorf("ok=%v out=%q", ok, outText(evs, "stdout"))
	}
	if ex := exits(evs); len(ex) != 2 || ex[1].OK || ex[1].Err == "" {
		t.Errorf("exits %+v", ex)
	}
	if evs[1].Type != "step" || evs[1].Cmd != "echo hi" {
		t.Errorf("step event %+v", evs[1])
	}
}

func TestLinesCutsLongLines(t *testing.T) {
	var got []string
	w := &lines{emit: func(s string) { got = append(got, s) }}
	w.Write([]byte(strings.Repeat("a", maxLine+10) + "\nb\r\n"))
	w.Write([]byte("par"))
	w.Write([]byte("tial"))
	w.end()
	if len(got) != 3 || !strings.HasSuffix(got[0], "(line cut at 64 KiB)") || len(got[0]) < maxLine || got[1] != "b" || got[2] != "partial" {
		t.Errorf("lines: %d %q %q", len(got), got[1:], got[0][len(got[0])-30:])
	}
}

// display must be a line sh reads back as exactly the same argv.
func TestDisplayRoundTrips(t *testing.T) {
	s := step{Env: []string{"NINELIVES_CRASH_AT=mid_write"}, Argv: []string{"printf", "%s|", "plain", "it's $HOME; rm", "x:00ff", ""}}
	line := s.display("/repo")
	want := "NINELIVES_CRASH_AT=mid_write printf '%s|' plain 'it'\\''s $HOME; rm' x:00ff ''"
	if line != want {
		t.Errorf("display:\n got %s\nwant %s", line, want)
	}
	out, err := exec.Command("sh", "-c", strings.TrimPrefix(line, "NINELIVES_CRASH_AT=mid_write ")).Output()
	if err != nil || string(out) != "plain|it's $HOME; rm|x:00ff||" {
		t.Errorf("sh read it back as %q (%v)", out, err)
	}
}
