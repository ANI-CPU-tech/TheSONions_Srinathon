package main

import (
	"context"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// sample is a valid argument list for every action.
var sample = map[string][]string{
	"put": {"a", "1"}, "get": {"a"}, "del": {"a"}, "batch": {"put c 3", "del c"},
	"crash": {"mid_write"}, "torture": {"default", "12", "3"},
}

func TestEveryActionBuilds(t *testing.T) {
	a := testApp(t)
	for _, x := range actions {
		j, err := a.buildJob(runRequest{Action: x.Name, Args: sample[x.Name]})
		if err != nil {
			t.Errorf("%s: %v", x.Name, err)
			continue
		}
		if x.Desc == "" || x.Panel == "" || len(j.Steps) == 0 || j.Title == "" {
			t.Errorf("%s: missing description, panel, steps or title", x.Name)
		}
		for _, s := range j.Steps {
			if s.fn == nil && (len(s.Argv) == 0 || s.Argv[0] == "sh" || s.Argv[0] == "bash") {
				t.Errorf("%s: step %q runs %v; steps must run a program, never a shell", x.Name, s.Title, s.Argv)
			}
			if s.fn != nil && s.Show == "" {
				t.Errorf("%s: in-process step %q shows no command", x.Name, s.Title)
			}
		}
	}
}

func TestBadRequestsAreRefused(t *testing.T) {
	a := testApp(t)
	for _, req := range []runRequest{
		{Action: "nosuch"},
		{Action: "put", Args: []string{"a"}},
		{Action: "put", Args: []string{"a", ""}},
		{Action: "put", Args: []string{"a", "1\nput b 2"}},
		{Action: "get", Args: []string{"a", "b"}},
		{Action: "dump", Args: []string{"x"}},
		{Action: "batch"},
		{Action: "batch", Args: []string{"put a 1\nput b 2"}},
		{Action: "crash", Args: []string{"nowhere"}},
		{Action: "crash"},
		{Action: "torture", Args: []string{"fast"}},
		{Action: "torture", Args: []string{"default", "12x"}},
		{Action: "torture", Args: []string{"default", "1", "0"}},
		{Action: "torture", Args: []string{"default", "1", "100001"}},
		{Action: "torture", Args: []string{"default", "1", "2", "3"}},
		{Action: "open", MaxBytes: -1},
	} {
		if _, err := a.buildJob(req); err == nil {
			t.Errorf("%+v was accepted", req)
		}
	}
}

func TestTortureArgs(t *testing.T) {
	a := testApp(t)
	j, _ := a.buildJob(runRequest{Action: "torture", Args: []string{"disk-full", "777", "9"}, MaxBytes: 4096})
	want := "./bin/torture --seed 777 --iterations 9 --mode disk-full --max-bytes 4096"
	if got := strings.Join(j.Steps[0].Argv, " "); got != want || !j.Torture {
		t.Errorf("argv %q, want %q", got, want)
	}
	j, _ = a.buildJob(runRequest{Action: "torture", Args: []string{"compaction"}})
	if argv := j.Steps[0].Argv; argv[2] == "" || argv[4] != "100" {
		t.Errorf("defaults: %v", argv)
	}
}

// The crash-point list must be exactly the points the engine has.
func TestCrashPointsMatchEngine(t *testing.T) {
	re := regexp.MustCompile(`crash(?:point|Hit)\("([a-z_]+)"\)`)
	files, _ := filepath.Glob("../../src/engine/*.go")
	var found []string
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range re.FindAllStringSubmatch(string(b), -1) {
			if !slices.Contains(found, m[1]) {
				found = append(found, m[1])
			}
		}
	}
	got, want := slices.Sorted(slices.Values(crashPoints)), slices.Sorted(slices.Values(found))
	if !slices.Equal(got, want) {
		t.Errorf("crashPoints %v\nengine has %v", got, want)
	}
}

func lastSnapshot(evs []event) *snapshot {
	for i := len(evs) - 1; i >= 0; i-- {
		if evs[i].Snapshot != nil {
			return evs[i].Snapshot
		}
	}
	return nil
}

// needBinary skips a test that runs the real CLI when it is not built.
func needBinary(t *testing.T) {
	t.Helper()
	if _, err := os.Stat("../../bin/ninelives"); err != nil {
		t.Skip("bin/ninelives not built (run scripts/build.sh)")
	}
}

// runAction runs one action for real and returns its events.
func runAction(t *testing.T, a *app, name string, args ...string) ([]event, bool) {
	t.Helper()
	j, err := a.buildJob(runRequest{Action: name, Args: args})
	if err != nil {
		t.Fatal(err)
	}
	return runTestJob(t, a, context.Background(), j)
}

func TestCLIActionsForReal(t *testing.T) {
	needBinary(t)
	a := testApp(t)
	for _, c := range []struct {
		name string
		args []string
		out  string
	}{
		{"put", []string{"a", "1"}, "OK seq=1\n"},
		{"get", []string{"a"}, "1\n"},
		{"batch", []string{"put b x:00ff", "del a"}, "put b x:00ff\ndel a\nOK seq=2\n"},
		{"dump", nil, "b x:00ff\n"},
		{"check", nil, "OK\n"},
		{"compact", nil, "OK\n"},
		{"disk-full", nil, ""},
	} {
		evs, ok := runAction(t, a, c.name, c.args...)
		if !ok || outText(evs, "stdout") != c.out {
			t.Errorf("%s: ok=%v stdout %q, want %q\n%s", c.name, ok, outText(evs, "stdout"), c.out, outText(evs, "stderr"))
		}
	}
	evs, _ := runAction(t, a, "dump")
	if s := lastSnapshot(evs); s == nil || s.Keys != 1 || len(s.Records) != 1 {
		t.Errorf("snapshot after compaction: %+v", s)
	}
}

// Every crash point, and F4, end to end: the armed step dies by SIGKILL
// and the database recovers to OK with the committed key intact.
func TestCrashScenariosForReal(t *testing.T) {
	needBinary(t)
	a := testApp(t)
	for _, p := range append(slices.Clone(crashPoints), "f4") {
		var evs []event
		var ok bool
		if p == "f4" {
			evs, ok = runAction(t, a, "f4")
		} else {
			evs, ok = runAction(t, a, "crash", p)
		}
		if !ok {
			t.Errorf("%s: job failed\n%s%s", p, outText(evs, "stdout"), outText(evs, "stderr"))
			continue
		}
		killed := false
		for _, ex := range exits(evs) {
			killed = killed || ex.Signal == "SIGKILL"
		}
		if p != "f4" && !killed {
			t.Errorf("%s: no step died by SIGKILL", p)
		}
		// The job is OK, so the last check printed OK; the dump after it
		// must hold the committed keys.
		want := "OK\nbase 0\n"
		if p == "f4" {
			want = "OK\na 1\nb 1\n"
		}
		if out := outText(evs, "stdout"); !strings.Contains(out, want) {
			t.Errorf("%s: recovery did not end in %q:\n%s", p, want, out)
		}
	}
}

func TestRunEndpoint(t *testing.T) {
	needBinary(t)
	a := testApp(t)
	h := a.routes()
	post := func(body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "/api/run", strings.NewReader(body))
		r.Host = "127.0.0.1:7000"
		r.Header.Set("X-NL-Token", testToken)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	w := post(`{"action":"put","args":["k","v"]}`)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"text":"OK seq=1"`) {
		t.Errorf("put: %d %s", w.Code, w.Body.String())
	}
	if w := post(`{"action":"put","args":["k"]}`); w.Code != 400 {
		t.Errorf("bad args: %d", w.Code)
	}
	if w := post(`{"action":"put","args":["k","v"],"extra":1}`); w.Code != 400 {
		t.Errorf("unknown field: %d", w.Code)
	}
	a.acquire()
	if w := post(`{"action":"dump"}`); w.Code != 409 {
		t.Errorf("while busy: %d", w.Code)
	}
	a.release()
}
