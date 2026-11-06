package main

import (
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// Every function the model names must exist where it says, so the Code
// path panel can never point at code that is not there.
func TestModelNamesRealFunctions(t *testing.T) {
	src := map[string]string{}
	for _, p := range paths {
		ids := map[string]bool{}
		for _, n := range p.Nodes {
			if ids[n.ID] {
				t.Errorf("path %s: node id %s twice", p.ID, n.ID)
			}
			ids[n.ID] = true
			if len(n.Where) == 0 || n.Title == "" || (n.Crash && n.Note == "") {
				t.Errorf("path %s node %s: missing where, title or crash note", p.ID, n.ID)
			}
			for _, w := range n.Where {
				file, fn, ok := strings.Cut(w, ":")
				if !ok {
					t.Errorf("%s: %q is not file:Function", n.ID, w)
					continue
				}
				if _, seen := src[file]; !seen {
					b, err := os.ReadFile("../../" + file)
					if err != nil {
						t.Errorf("%s: %v", n.ID, err)
					}
					src[file] = string(b)
				}
				if !regexp.MustCompile(`func (\([^)]*\) )?` + fn + `\(`).MatchString(src[file]) {
					t.Errorf("%s: no func %s in %s", n.ID, fn, file)
				}
			}
		}
	}
}

// Each engine crash point sits exactly once in the model, inside the
// function that really calls it.
func TestModelCrashPoints(t *testing.T) {
	seen := map[string]int{}
	for _, p := range paths {
		for _, n := range p.Nodes {
			if !n.Crash {
				continue
			}
			seen[n.ID]++
			if !slices.Contains(crashPoints, n.ID) {
				t.Errorf("model has crash point %s, the engine does not", n.ID)
			}
			file, fn, _ := strings.Cut(n.Where[0], ":")
			b, _ := os.ReadFile("../../" + file)
			body := funcBody(string(b), fn)
			if !strings.Contains(body, `"`+n.ID+`"`) {
				t.Errorf("crash point %s: not called inside %s in %s", n.ID, fn, file)
			}
		}
	}
	for _, p := range crashPoints {
		if seen[p] != 1 {
			t.Errorf("crash point %s appears %d times in the model", p, seen[p])
		}
	}
}

// funcBody is the text of func fn, up to the next top-level func.
func funcBody(src, fn string) string {
	loc := regexp.MustCompile(`(?m)^func (\([^)]*\) )?` + fn + `\(`).FindStringIndex(src)
	if loc == nil {
		return ""
	}
	rest := src[loc[1]:]
	if end := strings.Index(rest, "\nfunc "); end >= 0 {
		rest = rest[:end]
	}
	return rest
}

func snap(exists bool) *snapshot { return &snapshot{Exists: exists} }

func TestTraceCases(t *testing.T) {
	torn := &snapshot{Exists: true, Torn: &tornView{Off: 100, Bytes: 9}}
	cases := []struct {
		name, action, crash string
		ex                  exitInfo
		out                 []string
		before              *snapshot
		want                string // path:status:stop of each segment
	}{
		{"put ok, new file", "put", "", exitInfo{}, []string{"OK seq=1"}, snap(false),
			"parse:done: open:done: write:done: close:done:"},
		{"usage error", "put", "", exitInfo{Code: 2}, []string{"ninelives put: token \"x:A\" is not lowercase hex", "usage:"}, snap(true),
			"parse:refused:parse"},
		{"disk full", "put", "", exitInfo{Code: 3}, nil, snap(true),
			"parse:done: open:done: write:refused:guard close:done:"},
		{"compaction E4", "compact", "", exitInfo{Code: 3}, nil, snap(true),
			"parse:done: open:done: compact:refused:copy close:done:"},
		{"too large", "put", "", exitInfo{Code: 1}, []string{"ninelives put: ninelives: value larger than 1 MiB"}, snap(true),
			"parse:done: open:done: write:refused:validate close:done:"},
		{"killed mid write", "put", "mid_write", exitInfo{Code: 137, Signal: "SIGKILL"}, nil, snap(true),
			"parse:done: open:done: write:killed:mid_write"},
		{"killed in recovery", "open", "recovery_after_truncate", exitInfo{Code: 137, Signal: "SIGKILL"}, nil, torn,
			"parse:done: open:killed:recovery_after_truncate"},
		{"compaction killed", "compact", "compact_after_rename", exitInfo{Code: 137, Signal: "SIGKILL"}, nil, snap(true),
			"parse:done: open:done: compact:killed:compact_after_rename"},
		{"armed, not reached", "put", "compact_mid_temp", exitInfo{}, nil, snap(true),
			"parse:done: open:done: write:done: close:done:"},
		{"check corrupt", "check", "", exitInfo{Code: 1}, []string{"CORRUPT: ninelives: corrupt file: torn tail at offset 60 (file is 61 bytes)"}, torn,
			"parse:done: check:corrupt:tail"},
		{"check temp file", "check", "", exitInfo{Code: 1}, []string{"CORRUPT: ninelives: corrupt file: leftover temp file /x.compact"}, snap(true),
			"parse:done: check:corrupt:temps"},
		{"bad header", "open", "", exitInfo{Code: 1}, []string{"ninelives open: ninelives: not a NineLives file (bad header): bad magic"}, snap(true),
			"parse:done: open:failed:header"},
	}
	for _, c := range cases {
		tr := traceFor(c.action, c.crash, c.ex, c.out, c.before, c.before)
		var got []string
		for _, s := range tr.Segments {
			got = append(got, s.Path+":"+s.Status+":"+s.Stop)
		}
		if strings.Join(got, " ") != c.want {
			t.Errorf("%s:\n got %s\nwant %s", c.name, strings.Join(got, " "), c.want)
		}
		if tr.Summary == "" {
			t.Errorf("%s: no summary", c.name)
		}
	}
}

func TestTraceEvidence(t *testing.T) {
	torn := &snapshot{Exists: true, Torn: &tornView{Off: 100, Bytes: 9}, TempFiles: []string{"demo.db.compact"}}
	tr := traceFor("open", "", exitInfo{}, []string{"keys=1 recovery_ms=0"}, torn, snap(true))
	o := tr.seg("open")
	if !slices.Contains(o.Lit, "truncate") || !slices.Contains(o.Lit, "sync") || !strings.Contains(o.Note, "9 bytes") || !strings.Contains(o.Note, "demo.db.compact") {
		t.Errorf("open evidence %+v", o)
	}
	if slices.Contains(o.Lit, "create") {
		t.Error("create lit for an existing file")
	}
	tr = traceFor("get", "", exitInfo{}, []string{"NOTFOUND"}, snap(true), snap(true))
	if r := tr.seg("read"); !slices.Contains(r.Skip, "readat") {
		t.Errorf("NOTFOUND still reads: %+v", r)
	}
	tr = traceFor("batch", "batch_mid_write", exitInfo{Code: 137, Signal: "SIGKILL"}, nil, snap(true), snap(true))
	if w := tr.seg("write"); w.Stop != "batch_mid_write" || !slices.Contains(w.Lit, "batch_mid_write") {
		t.Errorf("batch kill %+v", w)
	}
}

// For real: the armed step of every crash scenario stops at its point.
func TestCrashTracesForReal(t *testing.T) {
	needBinary(t)
	a := testApp(t)
	for _, p := range crashPoints {
		evs, _ := runAction(t, a, "crash", p)
		found := false
		for _, ev := range evs {
			if ev.Type == "trace" {
				for _, s := range ev.Trace.Segments {
					found = found || (s.Stop == p && s.Status == "killed")
				}
			}
		}
		if !found {
			t.Errorf("%s: no trace stops there", p)
		}
	}
}
