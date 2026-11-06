package main

import (
	"bufio"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"ninelives/src/engine"
)

func segs(t *traceView) string {
	var out []string
	for _, s := range t.Segments {
		out = append(out, s.Path+":"+s.Status+":"+s.Stop)
	}
	return strings.Join(out, " ")
}

func TestInferTrace(t *testing.T) {
	path := filepath.Join(t.TempDir(), "demo.db")
	none := takeSnapshot(path)
	db, _ := engine.Open(path, engine.Options{})
	db.Put([]byte("k"), []byte("v1"))
	one := takeSnapshot(path)
	db.Batch([]engine.Op{{Kind: engine.OpPut, Key: []byte("k"), Value: []byte("v2")}})
	two := takeSnapshot(path)
	db.Compact()
	db.Close()
	comp := takeSnapshot(path)
	rec := engine.EncodePut(9, []byte("x"), []byte("y"))
	f, _ := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0)
	f.Write(rec[:6])
	f.Close()
	torn := takeSnapshot(path)
	db, _ = engine.Open(path, engine.Options{})
	db.Close()
	cut := takeSnapshot(path)
	os.WriteFile(path+".compact", nil, 0o644)
	temp := takeSnapshot(path)

	cases := []struct {
		name string
		a, b *snapshot
		want string
	}{
		{"create + put", none, one, "open:done: write:done: close:done:"},
		{"batch", one, two, "open:done: write:done: close:done:"},
		{"compaction", two, comp, "open:done: compact:done: close:done:"},
		{"torn write", comp, torn, "open:done: write:killed:mid_write close:done:"},
		{"recovery cut", torn, cut, "open:done: close:done:"},
		{"compaction died", cut, temp, "open:done: compact:killed: close:done:"},
	}
	for _, c := range cases {
		tr := inferTrace(c.a, c.b, diffOf(c.a, c.b))
		if segs(tr) != c.want || !strings.HasPrefix(tr.Summary, "inferred") {
			t.Errorf("%s: %s (%s), want %s", c.name, segs(tr), tr.Summary, c.want)
		}
	}
	if tr := inferTrace(torn, cut, diffOf(torn, cut)); !strings.Contains(tr.Segments[0].Note, "torn tail of 6 bytes") {
		t.Errorf("recovery note %q", tr.Segments[0].Note)
	}
	if tr := inferTrace(none, one, diffOf(none, one)); tr.Segments[0].Lit[0] != "create" {
		t.Errorf("create not lit: %+v", tr.Segments[0])
	}
}

// For real: the page's watch stream follows writes made by another
// process-like writer, and shutdown ends the stream.
func TestWatchStream(t *testing.T) {
	a, url := liveServer(t)
	req, _ := http.NewRequest("GET", url+"/api/watch", nil)
	req.Header.Set("X-NL-Token", testToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	events := make(chan event, 100)
	go func() {
		defer close(events)
		sc := bufio.NewScanner(resp.Body)
		sc.Buffer(nil, 16<<20)
		for sc.Scan() {
			var ev event
			json.Unmarshal(sc.Bytes(), &ev)
			events <- ev
		}
	}()
	next := func() event {
		t.Helper()
		select {
		case ev, ok := <-events:
			if !ok {
				t.Fatal("stream ended")
			}
			return ev
		case <-time.After(5 * time.Second):
			t.Fatal("no event")
		}
		return event{}
	}
	if ev := next(); ev.Snapshot == nil || ev.Snapshot.Exists {
		t.Fatalf("first event %+v", ev)
	}
	db, _ := engine.Open(a.db, engine.Options{})
	db.Put([]byte("a"), []byte("1"))
	db.Close()
	if ev := next(); ev.Trace == nil || !strings.Contains(ev.Trace.Summary, "appended") || ev.Snapshot.Keys != 1 {
		t.Fatalf("after put: %+v", ev.Trace)
	}
	rec := engine.EncodePut(2, []byte("b"), []byte("2"))
	f, _ := os.OpenFile(a.db, os.O_WRONLY|os.O_APPEND, 0)
	f.Write(rec[:5])
	f.Close()
	if ev := next(); ev.Trace == nil || ev.Snapshot.Torn == nil || ev.Trace.Segments[1].Stop != "mid_write" {
		t.Fatalf("after torn write: %+v", ev.Trace)
	}
	a.shutdown()
	select {
	case _, ok := <-events:
		for ok {
			_, ok = <-events
		}
	case <-time.After(3 * time.Second):
		t.Fatal("shutdown did not end the watch stream")
	}
}
