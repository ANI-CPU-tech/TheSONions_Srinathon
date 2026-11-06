package main

// Watch mode: the page follows the demo database while you run
// ./bin/ninelives in your own terminal. nlui polls the file (it only
// reads it, never opens it for writing) and sends a snapshot whenever it
// changed. It cannot see the command, its exit code or an armed crash
// point, so the code path it shows is inferred from the change alone and
// says so.

import (
	"fmt"
	"net/http"
	"os"
	"time"
)

const watchEvery = 100 * time.Millisecond

// fileSig is what changes when any process writes, truncates, renames or
// deletes the database or its temp files.
type fileSig struct {
	exists     bool
	size, mod  int64
	inode      uint64
	tmpCompact bool
	tmpInit    bool
}

func sigOf(path string) fileSig {
	var s fileSig
	if fi, err := os.Stat(path); err == nil {
		s.exists, s.size, s.mod, s.inode = true, fi.Size(), fi.ModTime().UnixNano(), inodeOf(fi)
	}
	_, err := os.Stat(path + ".compact")
	s.tmpCompact = err == nil
	_, err = os.Stat(path + ".init")
	s.tmpInit = err == nil
	return s
}

// handleWatch streams a snapshot each time the demo database changes and
// then stays the same for one more poll, so a write caught halfway is not
// shown as a torn tail. It runs until the page stops reading or nlui quits.
func (a *app) handleWatch(w http.ResponseWriter, r *http.Request) {
	out := newStream(w)
	prev := takeSnapshot(a.db)
	out.send(event{Type: "snapshot", Snapshot: prev})
	last, seen := sigOf(a.db), sigOf(a.db)
	tick := time.NewTicker(watchEvery)
	defer tick.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-a.quit:
			return
		case <-tick.C:
		}
		now := sigOf(a.db)
		if now != seen { // still changing: wait until it holds still
			seen = now
			continue
		}
		if now == last {
			continue
		}
		last = now
		next := takeSnapshot(a.db)
		d := diffOf(prev, next)
		out.send(event{Type: "snapshot", Snapshot: next, Diff: &d, Trace: inferTrace(prev, next, d)})
		prev = next
	}
}

// inferTrace guesses the code path from what changed in the file. Every
// summary says it is inferred: watch mode never sees the command itself.
func inferTrace(a, b *snapshot, d diffView) *traceView {
	t := &traceView{Action: "terminal"}
	open := segment{Path: "open", Status: "done"}
	if d.Created {
		open.Lit = append(open.Lit, "create")
	}
	if d.TornCut > 0 {
		open.Lit = append(open.Lit, "recovery_before_truncate", "truncate", "recovery_after_truncate", "sync")
		open.Note = fmt.Sprintf("cut a torn tail of %d bytes", d.TornCut)
	}
	if len(d.TempGone) > 0 && len(d.TempNew) == 0 {
		open.Note = joinNote(open.Note, "deleted leftover "+d.TempGone[0])
	}
	switch {
	case d.Removed:
		t.Summary = "inferred: the database file was deleted"
		return t
	case b.Torn != nil && (a.Torn == nil || a.Torn.Off != b.Torn.Off):
		t.Segments = []segment{open, {Path: "write", Status: "killed", Stop: "mid_write",
			Note: "a partial record appeared at the end: the writer died during WriteAt, before Sync. The op was never acked; the next open() cuts it."}}
		t.Summary = "inferred: a process died while writing a record"
	case len(d.TempNew) > 0 && !d.Rewritten:
		t.Segments = []segment{open, {Path: "compact", Status: "killed",
			Note: d.TempNew[0] + " was left behind: compaction died before the rename. demo.db is still the old file; the next open() deletes the temp file."}}
		t.Summary = "inferred: a compaction died before its rename"
	case d.Rewritten:
		t.Segments = []segment{open, {Path: "compact", Status: "done", Note: fmt.Sprintf("the file was rewritten: %d -> %d bytes", d.SizeFrom, d.SizeTo)}}
		t.Summary = "inferred: compaction ran"
	case d.Added > 0:
		t.Segments = []segment{open, writeSeg(b.Records[len(b.Records)-d.Added:])}
		t.Summary = fmt.Sprintf("inferred: %d record(s) appended", d.Added)
	default:
		t.Segments = []segment{open}
		t.Summary = "inferred: open() ran (recovery)"
		if !d.Created && d.TornCut == 0 && len(d.TempGone) == 0 {
			t.Summary = "inferred: the file changed"
		}
	}
	t.Segments = append(t.Segments, segment{Path: "close", Status: "done"})
	return t
}

// writeSeg is the write path of the last appended record.
func writeSeg(added []recordView) segment {
	s := segment{Path: "write", Status: "done"}
	last := added[len(added)-1]
	if last.Type == "BATCH" {
		s.Lit = []string{"batch_mid_write"}
	}
	s.Note = fmt.Sprintf("last record: %s seq %d, %d bytes", last.Type, last.Seq, last.Size)
	return s
}

func joinNote(a, b string) string {
	if a == "" {
		return b
	}
	return a + "; " + b
}
