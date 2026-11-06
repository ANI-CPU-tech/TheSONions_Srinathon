package main

// Code-path model for the Code path panel. Each path lists, in code
// order, the steps a command goes through and the crash points between
// them, with the function that does each step. It is written by hand from
// the source, not traced live; a test checks that every function and
// crash point named here exists in the code.
//
// traceFor then picks what one real run went through, using only
// evidence: the exit code, the armed crash point, the output, and the file
// before and after.

import (
	"fmt"
	"slices"
	"strings"
)

// node is one step, or one crash point between steps.
type node struct {
	ID     string   `json:"id"`
	Crash  bool     `json:"crash,omitempty"`  // a crash point (ID is its name)
	Branch bool     `json:"branch,omitempty"` // runs only sometimes; lit by evidence
	Title  string   `json:"title"`
	Where  []string `json:"where"` // "file:Function", repo-relative
	Spec   string   `json:"spec,omitempty"`
	Note   string   `json:"note,omitempty"` // crash points: what is on disk and what open() does
}

type path struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Nodes []node `json:"nodes"`
}

const (
	cmdGo     = "src/cmd/ninelives/commands.go:"
	recoverGo = "src/engine/recover.go:"
	writeGo   = "src/engine/write.go:"
	compactGo = "src/engine/compact.go:"
	dbGo      = "src/engine/db.go:"
)

func stepNode(id, title, spec string, where ...string) node {
	return node{ID: id, Title: title, Spec: spec, Where: where}
}

func crashAt(id, note string, where ...string) node {
	return node{ID: id, Crash: true, Title: "crash point " + id, Note: note, Where: where}
}

func branch(n node) node { n.Branch = true; return n }

var paths = []path{
	{ID: "parse", Title: "CLI: arguments", Nodes: []node{
		stepNode("parse", "Parse arguments and decode key/value tokens; a bad one is a usage error (exit 2) before the file is opened", "SPEC §10, §11",
			cmdGo+"parseFlags", "src/cmd/ninelives/token.go:decodeToken"),
	}},
	{ID: "serveparse", Title: "serve: request line", Nodes: []node{
		stepNode("serveparse", "Parse the request line; a malformed one gets ERR BADREQ and never reaches the engine", "SPEC §12",
			"src/cmd/ninelives/serve.go:parseRequest"),
	}},
	{ID: "open", Title: "Open: recovery", Nodes: []node{
		stepNode("leftovers", "Delete a leftover demo.db.compact or demo.db.init: neither is ever authoritative", "SPEC §6.1", recoverGo+"removeLeftovers"),
		branch(stepNode("create", "No file yet: header to demo.db.init, fsync, rename to demo.db, fsync the directory", "SPEC §5", "src/engine/create.go:createFile")),
		stepNode("header", "Read the 24-byte header: magic NLV1, version, CRC", "SPEC §6.3", recoverGo+"readHeader"),
		stepNode("scan", "Scan from offset 24: 17-byte header, length in range, CRC over the payload; apply each record to the index", "SPEC §6.4",
			recoverGo+"scanLog", recoverGo+"applyRecord"),
		crashAt("recovery_mid_scan", "Recovery has only read so far: the file is unchanged. The next open() scans again from offset 24.", recoverGo+"recoverLog"),
		branch(crashAt("recovery_before_truncate", "The torn tail is still on disk and nothing from it was applied. The next open() stops at the same offset and cuts again: recovery is idempotent.", recoverGo+"truncateTail")),
		branch(stepNode("truncate", "Torn tail found: Truncate(validEnd) cuts it off", "SPEC §6.5", recoverGo+"truncateTail")),
		branch(crashAt("recovery_after_truncate", "Cut, but maybe not durable yet. The next open() sees a clean end, or the same torn tail and cuts it again.", recoverGo+"truncateTail")),
		branch(stepNode("sync", "Sync() makes the cut durable", "SPEC §6.5", recoverGo+"truncateTail")),
		stepNode("ready", "size = validEnd, nextSeq = max(SeqBase, highest seq + 1): ready", "SPEC §6.6", recoverGo+"recoverLog"),
	}},
	{ID: "write", Title: "Write path: put, del, batch", Nodes: []node{
		stepNode("lock", "Take the write lock (one global sync.RWMutex)", "SPEC §4", writeGo+"Put", writeGo+"Delete", writeGo+"Batch"),
		stepNode("validate", "Validate: key 1 to 1024 bytes, value up to 1 MiB, batch up to 10,000 ops and 64 MiB", "SPEC §4.1",
			writeGo+"checkKey", writeGo+"checkValue", writeGo+"checkBatch"),
		stepNode("encode", "Encode the whole record in one buffer, seq = nextSeq: CRC | Length | Type | Seq | payload (no disk touched, so before the guard, which needs its length)", "SPEC §4.3",
			"src/engine/codec.go:EncodePut", "src/engine/codec.go:EncodeDel", "src/engine/codec.go:EncodeBatch"),
		stepNode("guard", "Disk-full guard: size + record must fit under max_bytes, minus a 64 KiB reserve unless it is a delete; if not, compact once and check again", "SPEC §4.2",
			"src/engine/diskfull.go:checkRoom", "src/engine/diskfull.go:fits"),
		crashAt("before_write", "No byte of the record is on disk. open() ends at the old size; the op was never acked and is absent.", writeGo+"commit"),
		crashAt("mid_write", "Half the record is written, then SIGKILL: a torn record. open() finds it short or with a bad CRC and cuts it off; the op is absent.", writeGo+"commit"),
		branch(crashAt("batch_mid_write", "Half of the batch record is written. One record, one CRC: recovery drops the whole batch, never a part of it.", writeGo+"commit")),
		stepNode("writeat", "WriteAt(record, size): the whole record in a single call", "SPEC §4.4", writeGo+"commit"),
		crashAt("after_write", "The record is in the page cache, which a process kill does not lose. open() applies it if it is complete with a good CRC (allowed: in flight, never acked), else cuts it off.", writeGo+"commit"),
		stepNode("fsync", "Sync(): the record is on disk", "SPEC §4.5", writeGo+"commit"),
		crashAt("after_fsync", "Durable but not yet acked. open() applies it: allowed, the op was in flight.", writeGo+"commit"),
		stepNode("index", "Apply it to the index (the same applyRecord recovery uses), size += length, nextSeq++", "SPEC §4.6",
			recoverGo+"applyRecord", writeGo+"commit"),
		crashAt("after_index", "Indexed but not returned, so never acked. The record is durable: open() applies it.", writeGo+"commit"),
		branch(stepNode("autocompact", "Auto-compaction: file at least 4 MiB and more than half dead", "SPEC §7", compactGo+"maybeCompact")),
		stepNode("ack", "Unlock and return seq: the CLI prints OK seq=N, serve prints ACK", "SPEC §4.7", writeGo+"commit"),
	}},
	{ID: "read", Title: "Read: get", Nodes: []node{
		stepNode("rlock", "Check the key, then take the read lock: a reader never sees half a batch", "SPEC §4", dbGo+"Get"),
		stepNode("lookup", "Look the key up in the in-memory index: value offset, length, seq", "SPEC §1", dbGo+"Get"),
		stepNode("readat", "ReadAt(offset): read only the value from the file", "SPEC §4", dbGo+"readValue"),
		stepNode("value", "Print the value token, or NOTFOUND (serve: VAL or NOTFOUND)", "SPEC §10", cmdGo+"cmdGet"),
	}},
	{ID: "dump", Title: "Dump", Nodes: []node{
		stepNode("sort", "Take the read lock and sort the keys by raw bytes", "SPEC §11", dbGo+"ForEachSorted"),
		stepNode("readall", "ReadAt each value from the file", "SPEC §4", dbGo+"readValue"),
		stepNode("lines", "Print one key and value token per line", "SPEC §10", cmdGo+"cmdDump"),
	}},
	{ID: "compact", Title: "Compaction", Nodes: []node{
		stepNode("clock", "Take the write lock", "SPEC §7", compactGo+"Compact"),
		stepNode("temp", "Create demo.db.compact (O_TRUNC) with a header whose SeqBase = nextSeq", "SPEC §7.1", compactGo+"writeCompactFile", compactGo+"copyLive"),
		stepNode("copy", "Copy each live key as one PUT with its original seq; over max_bytes: stop, delete the temp file (E4)", "SPEC §7.2, §7.3", compactGo+"copyLive"),
		crashAt("compact_mid_temp", "Half the live keys are in the temp file. demo.db is untouched and authoritative; open() deletes the temp file (C3).", compactGo+"copyLive"),
		stepNode("tempsync", "fsync the temp file", "SPEC §7.4", compactGo+"writeCompactFile"),
		crashAt("compact_after_temp_fsync", "A complete, fsynced copy sits next to the old file, but demo.db is still the old file. open() deletes the copy and keeps the old file (C3).", compactGo+"compactLocked"),
		stepNode("rename", "rename(demo.db.compact, demo.db): atomic, so exactly one of the two is demo.db", "SPEC §7.5", compactGo+"compactLocked"),
		crashAt("compact_after_rename", "demo.db is the new file, complete because it was fsynced before the rename (C4, C5). open() uses it.", compactGo+"compactLocked"),
		stepNode("swap", "Switch the file handle and the index to the new file; close the old one", "SPEC §7.7", compactGo+"compactLocked"),
		stepNode("dirsync", "fsync the directory, so the rename also survives power loss", "SPEC §7.6", "src/engine/fsutil.go:syncDir"),
		crashAt("compact_after_dir_fsync", "Compaction is complete and durable. open() uses the new file.", compactGo+"compactLocked"),
		stepNode("done", "Print OK (serve: ACK with seq 0)", "SPEC §11", cmdGo+"cmdCompact"),
	}},
	{ID: "check", Title: "Check (read-only)", Nodes: []node{
		stepNode("temps", "A leftover demo.db.compact or demo.db.init is a problem", "SPEC §11", recoverGo+"Check"),
		stepNode("cheader", "Check the header", "SPEC §11", recoverGo+"readHeader"),
		stepNode("cscan", "Check every record: length, CRC, a well-formed payload", "SPEC §11", recoverGo+"scanLog"),
		stepNode("tail", "The scan must end exactly at the end of the file: no torn tail", "SPEC §11", recoverGo+"Check"),
		stepNode("verdict", "Print OK (exit 0) or CORRUPT: <reason> (exit 1)", "SPEC §11", cmdGo+"cmdCheck"),
	}},
	{ID: "stats", Title: "Open: result", Nodes: []node{
		stepNode("keys", "Print keys=<n> recovery_ms=<ms>", "SPEC §11", cmdGo+"cmdOpen"),
	}},
	{ID: "close", Title: "Close", Nodes: []node{
		stepNode("close", "Close the file: every acked write is already fsynced, nothing to flush", "SPEC §9", dbGo+"Close"),
	}},
}

// segment is one path as one run went through it.
type segment struct {
	Path   string   `json:"path"`
	Lit    []string `json:"lit,omitempty"`  // branch nodes that ran
	Skip   []string `json:"skip,omitempty"` // steps that did not run
	Stop   string   `json:"stop,omitempty"` // where it stopped; "" = it ran to the end
	Status string   `json:"status"`         // done, killed, refused, failed, corrupt, missed, cancelled
	Note   string   `json:"note,omitempty"`
}

type traceView struct {
	Action   string    `json:"action"`
	Segments []segment `json:"segments"`
	Summary  string    `json:"summary"`
}

// pathsFor is the order of paths a CLI command goes through.
func pathsFor(action string) []string {
	switch action {
	case "check":
		return []string{"parse", "check"}
	case "open":
		return []string{"parse", "open", "stats", "close"}
	case "put", "del", "batch":
		return []string{"parse", "open", "write", "close"}
	case "get":
		return []string{"parse", "open", "read", "close"}
	}
	return []string{"parse", "open", action, "close"} // dump, compact
}

// opPath is the path holding the command's own work.
func opPath(action string) string {
	switch action {
	case "put", "del", "batch":
		return "write"
	case "get":
		return "read"
	case "open":
		return "open"
	}
	return action
}

// traceFor works out what one CLI run went through. out is its last
// output lines (stdout and stderr).
func traceFor(action, crash string, ex exitInfo, out []string, before, after *snapshot) *traceView {
	t := &traceView{Action: action}
	for _, p := range pathsFor(action) {
		t.Segments = append(t.Segments, segment{Path: p, Status: "done"})
	}
	t.evidence(action, out, before, after)
	text := strings.Join(out, "\n")
	switch {
	case crash != "" && ex.Signal == "SIGKILL":
		t.killedAt(crash)
	case crash != "" && ex.Code == 0:
		if s := t.seg(pathOf(crash)); s != nil {
			s.Status = "missed"
		}
		t.Summary = "crash point " + crash + " was armed but not reached; the command ran to the end"
	case ex.Signal == "SIGKILL":
		s := t.seg(opPath(action))
		s.Status, s.Note = "killed", "killed from outside (no crash point armed): where exactly is unknown"
		t.cutAfter(opPath(action))
	case ex.Signal != "":
		t.seg(opPath(action)).Status, t.Summary = "cancelled", "stopped by "+ex.Signal
	case ex.Code == 2:
		t.Segments = []segment{{Path: "parse", Stop: "parse", Status: "refused", Note: lastLine(out, "usage error")}}
		t.Summary = "usage error: refused before the file was opened"
	case ex.Code == 3:
		t.diskFull(action)
	case ex.Code != 0:
		t.failed(action, text, out)
	}
	if t.Summary == "" {
		t.Summary = summarize(t)
	}
	return t
}

// evidence lights the branches the file shows were taken.
func (t *traceView) evidence(action string, out []string, before, after *snapshot) {
	if o := t.seg("open"); o != nil {
		if !before.Exists {
			o.Lit = append(o.Lit, "create")
		}
		if before.Torn != nil {
			o.Lit = append(o.Lit, "recovery_before_truncate", "truncate", "recovery_after_truncate", "sync")
			o.Note = fmt.Sprintf("cut a torn tail of %d bytes at offset %d", before.Torn.Bytes, before.Torn.Off)
		}
		if len(before.TempFiles) > 0 {
			o.Note = strings.TrimPrefix(o.Note+"; deleted "+strings.Join(before.TempFiles, ", "), "; ")
		}
	}
	if w := t.seg("write"); w != nil {
		if action == "batch" {
			w.Lit = append(w.Lit, "batch_mid_write")
		}
		if diffOf(before, after).Rewritten {
			w.Lit, w.Note = append(w.Lit, "autocompact"), "the file was rewritten: compaction ran"
		}
	}
	if r := t.seg("read"); r != nil && slices.Contains(out, "NOTFOUND") {
		r.Skip, r.Note = []string{"readat"}, "the key is not in the index: NOTFOUND"
	}
}

// killedAt marks the segment holding crash point p as killed there and
// drops the segments after it.
func (t *traceView) killedAt(p string) {
	s := t.seg(pathOf(p))
	if s == nil {
		t.Summary = "killed at " + p + ", which this command does not reach"
		return
	}
	s.Stop, s.Status = p, "killed"
	if n := findNode(s.Path, p); n != nil && n.Branch && !slices.Contains(s.Lit, p) {
		s.Lit = append(s.Lit, p)
	}
	t.cutAfter(s.Path)
	t.Summary = "SIGKILL at " + p
}

func (t *traceView) diskFull(action string) {
	if action == "compact" {
		s := t.seg("compact")
		s.Stop, s.Status, s.Note = "copy", "refused", "E4: the compacted file would not fit under max_bytes; temp file removed, old file untouched"
		t.Summary = "compaction refused: disk full (exit 3)"
		return
	}
	if s := t.seg("write"); s != nil {
		s.Stop, s.Status, s.Note = "guard", "refused", "ErrDiskFull: the record does not fit; nothing was written"
	}
	t.Summary = "refused by the disk-full guard (exit 3): nothing written"
}

// failed reads exit code 1 from the error text the engine and CLI print.
func (t *traceView) failed(action, text string, out []string) {
	has := func(subs ...string) bool {
		for _, s := range subs {
			if strings.Contains(text, s) {
				return true
			}
		}
		return false
	}
	switch {
	case action == "check":
		s := t.seg("check")
		s.Status, s.Note = "corrupt", lastLine(out, "CORRUPT")
		switch {
		case has("leftover temp file"):
			s.Stop = "temps"
		case has("not a NineLives file"):
			s.Stop = "cheader"
		case has("torn tail"):
			s.Stop = "tail"
		default:
			s.Stop = "cscan"
		}
		t.Summary = "check found a problem (exit 1)"
	case has("key larger than", "empty key", "value larger than", "batch exceeds", "batch op kind"):
		s := t.seg("write")
		s.Stop, s.Status, s.Note = "validate", "refused", lastLine(out, "")
		t.Summary = "refused by validation (exit 1): nothing written"
	case has("not a NineLives file"):
		t.stopFailed("open", "header", lastLine(out, ""))
	case has("corrupt file"):
		t.stopFailed("open", "scan", lastLine(out, ""))
	default:
		t.stopFailed(opPath(action), "", lastLine(out, "exit 1"))
	}
}

func (t *traceView) stopFailed(p, at, note string) {
	if s := t.seg(p); s != nil {
		s.Stop, s.Status, s.Note = at, "failed", note
		t.cutAfter(p)
	}
	t.Summary = "failed (exit 1)"
}

// cutAfter drops the segments after path p: a killed or failed open()
// never reaches them.
func (t *traceView) cutAfter(p string) {
	for i, s := range t.Segments {
		if s.Path == p {
			t.Segments = t.Segments[:i+1]
			return
		}
	}
}

func (t *traceView) seg(p string) *segment {
	for i := range t.Segments {
		if t.Segments[i].Path == p {
			return &t.Segments[i]
		}
	}
	return nil
}

// summarize is the one-line result of a run that ended normally.
func summarize(t *traceView) string {
	for _, s := range t.Segments {
		if s.Note != "" {
			return t.Action + ": " + s.Note
		}
	}
	return t.Action + ": ran to the end"
}

// pathOf is the path that holds crash point p.
func pathOf(p string) string {
	for _, pa := range paths {
		if findNode(pa.ID, p) != nil {
			return pa.ID
		}
	}
	return ""
}

func findNode(pathID, id string) *node {
	for i := range paths {
		if paths[i].ID != pathID {
			continue
		}
		for j := range paths[i].Nodes {
			if paths[i].Nodes[j].ID == id {
				return &paths[i].Nodes[j]
			}
		}
	}
	return nil
}

// lastLine is the last non-empty line of out, or def.
func lastLine(out []string, def string) string {
	for i := len(out) - 1; i >= 0; i-- {
		if strings.TrimSpace(out[i]) != "" && !strings.HasPrefix(out[i], "usage:") && !strings.HasPrefix(out[i], "  ninelives ") {
			return out[i]
		}
	}
	return def
}
