// Command fakeserve stands in for `ninelives` so the torture tool can be
// tested before, and independently of, the real engine. It has the same
// commands the tool uses (serve, open, check, dump) and speaks the serve
// protocol (docs/SPEC.md sections 10-12). Storage is a text log: one line
// per acked request, appended with one write and fsynced before the ACK.
// A line without its newline is a torn write and is dropped.
//
// This is the honest version (--bug=none). The deliberate bugs that prove
// the oracle catches them (drop-ack, partial-batch, garbage, stale) land in
// PR L2.
package main

import (
	"bufio"
	"bytes"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"slices"
	"strconv"
	"strings"
)

func main() {
	if len(os.Args) < 3 {
		fmt.Fprintln(os.Stderr, "usage: fakeserve serve|open|check|dump <path> [--max-bytes N]")
		os.Exit(2)
	}
	path := os.Args[2] // --max-bytes is accepted and ignored
	var err error
	switch os.Args[1] {
	case "serve":
		err = serve(path)
	case "open":
		err = open(path)
	case "check":
		err = check(path)
	case "dump":
		err = dump(path)
	default:
		fmt.Fprintln(os.Stderr, "fakeserve: unknown command", os.Args[1])
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "fakeserve:", err)
		os.Exit(1)
	}
}

// store is the state rebuilt from the log.
type store struct {
	kv       map[string]string // raw key -> raw value
	lines    int               // complete lines = last seq
	validEnd int64             // end of the last complete line
	size     int64             // file size; more than validEnd = torn tail
}

// load replays every complete line of the log at path.
func load(path string) (*store, error) {
	s := &store{kv: map[string]string{}}
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	s.size = int64(len(b))
	for {
		i := bytes.IndexByte(b[s.validEnd:], '\n')
		if i < 0 {
			return s, nil
		}
		if err := s.apply(string(b[s.validEnd : s.validEnd+int64(i)])); err != nil {
			return nil, fmt.Errorf("line %d: %v", s.lines+1, err)
		}
		s.lines++
		s.validEnd += int64(i) + 1
	}
}

// apply runs the ops of one log line in order: "put <k> <v>" and "del <k>".
func (s *store) apply(line string) error {
	f := strings.Fields(line)
	for len(f) > 0 {
		switch {
		case f[0] == "put" && len(f) >= 3:
			k, v, err := decode2(f[1], f[2])
			if err != nil {
				return err
			}
			s.kv[k] = v
			f = f[3:]
		case f[0] == "del" && len(f) >= 2:
			k, _, err := decode2(f[1], "x:")
			if err != nil {
				return err
			}
			delete(s.kv, k)
			f = f[2:]
		default:
			return fmt.Errorf("bad op %q", f[0])
		}
	}
	return nil
}

// open recovers: cut a torn tail, like the real engine's open().
func open(path string) error {
	s, err := recoverFile(path)
	if err != nil {
		return err
	}
	fmt.Printf("keys=%d recovery_ms=0\n", len(s.kv))
	return nil
}

func recoverFile(path string) (*store, error) {
	s, err := load(path)
	if err != nil || s.validEnd == s.size {
		return s, err
	}
	f, err := os.OpenFile(path, os.O_WRONLY, 0)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	if err := f.Truncate(s.validEnd); err != nil {
		return nil, err
	}
	return s, f.Sync()
}

// check is read-only, like the real one.
func check(path string) error {
	s, err := load(path)
	if err != nil {
		fmt.Println("CORRUPT:", err)
		os.Exit(1)
	}
	if s.validEnd != s.size {
		fmt.Printf("CORRUPT: torn tail at offset %d\n", s.validEnd)
		os.Exit(1)
	}
	fmt.Println("OK")
	return nil
}

func dump(path string) error {
	s, err := recoverFile(path)
	if err != nil {
		return err
	}
	keys := make([]string, 0, len(s.kv))
	for k := range s.kv {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	w := bufio.NewWriter(os.Stdout)
	for _, k := range keys {
		fmt.Fprintf(w, "%s %s\n", dumpToken(k), dumpToken(s.kv[k]))
	}
	return w.Flush()
}

// serve answers requests one at a time; each ACK follows an fsync.
func serve(path string) error {
	s, err := recoverFile(path)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	fmt.Printf("READY keys=%d recovery_ms=0\n", len(s.kv))
	in := bufio.NewScanner(os.Stdin)
	in.Buffer(make([]byte, 64<<10), 8<<20)
	seq, auto := s.lines, 0
	for in.Scan() {
		auto++
		opid, req := splitPrefix(in.Text(), auto)
		line, err := readRequest(req, in)
		if err != nil {
			fmt.Printf("ERR %s BADREQ %v\n", opid, err)
			continue
		}
		// One write, then fsync, then ACK: a kill before the fsync leaves a
		// complete line (applied, allowed) or a torn one (dropped).
		if _, err := f.WriteString(line + "\n"); err != nil {
			return err
		}
		if err := f.Sync(); err != nil {
			return err
		}
		seq++
		fmt.Printf("ACK %s %d\n", opid, seq)
	}
	return in.Err()
}

// splitPrefix strips an optional "@<writer>:<opid>" prefix.
func splitPrefix(line string, auto int) (opid, rest string) {
	if first, rest, ok := strings.Cut(line, " "); ok && strings.HasPrefix(first, "@") {
		if _, id, ok := strings.Cut(first, ":"); ok {
			return id, rest
		}
	}
	return strconv.Itoa(auto), line
}

// readRequest turns a put, del or batch request into one log line,
// reading a batch's op lines from in.
func readRequest(req string, in *bufio.Scanner) (string, error) {
	f := strings.Fields(req)
	if len(f) == 2 && f[0] == "batch" {
		n, err := strconv.Atoi(f[1])
		if err != nil || n < 0 {
			return "", fmt.Errorf("bad batch count %q", f[1])
		}
		ops := make([]string, 0, n)
		for range n {
			if !in.Scan() {
				return "", errors.New("batch cut short")
			}
			ops = append(ops, in.Text())
		}
		return strings.Join(ops, " "), nil
	}
	if (len(f) == 3 && f[0] == "put") || (len(f) == 2 && f[0] == "del") {
		return req, nil
	}
	return "", fmt.Errorf("unsupported request %q", req)
}

func decode2(a, b string) (string, string, error) {
	x, err := decodeToken(a)
	if err != nil {
		return "", "", err
	}
	y, err := decodeToken(b)
	return x, y, err
}

func decodeToken(tok string) (string, error) {
	if h, ok := strings.CutPrefix(tok, "x:"); ok {
		b, err := hex.DecodeString(h)
		return string(b), err
	}
	return tok, nil
}

// dumpToken prints raw text when every byte is printable ASCII other than
// space (SPEC section 10), else x:<hex>.
func dumpToken(s string) string {
	if s == "" || strings.HasPrefix(s, "x:") {
		return "x:" + hex.EncodeToString([]byte(s))
	}
	for i := 0; i < len(s); i++ {
		if s[i] <= ' ' || s[i] > '~' {
			return "x:" + hex.EncodeToString([]byte(s))
		}
	}
	return s
}
