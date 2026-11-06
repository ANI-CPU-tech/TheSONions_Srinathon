// serve protocol (docs/SPEC.md section 12): READY line, per-writer goroutines,
// stdout mutex, 4 MiB lines, batch blocks, ACK only after engine success,
// clean shutdown on stdin EOF. PR K2.
package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"sync"

	"ninelives/src/engine"
)

// serveLineBuf is the initial stdin buffer. A 1 MiB value is about 2 MiB of
// hex, and SPEC section 12 requires lines of at least 4 MiB; bufio.Reader
// also grows across reads, so longer lines still work.
const serveLineBuf = 4 << 20

// serveQueueCap is the per-writer queue depth. The reader blocks when a
// queue is full (backpressure), which preserves per-writer order.
const serveQueueCap = 256

// request is one parsed stdin request, already bound to its writer queue.
type request struct {
	writer int    // writer id from the @<writer> prefix, default 0
	opid   uint64 // from @<writer>:<opid>, else auto-incrementing
	op     string // put | del | get | batch | compact | stats
	key    []byte
	val    []byte
	ops    []engine.Op // batch payload
	err    error       // parse failure: worker answers ERR ... BADREQ
}

// emitter serialises stdout lines behind one mutex and flushes each line
// immediately (SPEC section 12: output is serialised and flushed per line).
type emitter struct {
	mu sync.Mutex
	w  *bufio.Writer
}

// emitf writes one complete output line.
func (e *emitter) emitf(format string, a ...any) {
	e.mu.Lock()
	defer e.mu.Unlock()
	fmt.Fprintf(e.w, format, a...)
	e.w.Flush()
}

// cmdServe opens the DB (running recovery), prints READY, then serves stdin
// until EOF: finish in-flight ops, Close the DB, exit 0 (SPEC section 12).
func cmdServe(path string, maxBytes int64) error {
	db, err := engine.Open(path, engine.Options{MaxBytes: maxBytes})
	if err != nil {
		return err
	}
	em := &emitter{w: bufio.NewWriter(os.Stdout)}
	st := db.Stats()
	em.emitf("READY keys=%d recovery_ms=%d\n", st.Keys, st.RecoveryTime.Milliseconds())

	// One goroutine and queue per writer id, created on first sight. A
	// single reader goroutine feeds the queues, so requests from one
	// writer keep their arrival order; different writers run concurrently
	// and the engine's write lock serialises the actual disk writes.
	var wg sync.WaitGroup
	queues := make(map[int]chan request)
	queue := func(w int) chan request {
		ch, ok := queues[w]
		if !ok {
			ch = make(chan request, serveQueueCap)
			queues[w] = ch
			wg.Add(1)
			go func() {
				defer wg.Done()
				serveWorker(db, ch, em)
			}()
		}
		return ch
	}

	nextID := uint64(1) // auto-incrementing op-id, touched only by the reader
	readErr := serveReader(os.Stdin, queue, &nextID)

	// stdin EOF: close every queue, wait for in-flight ops, then Close.
	for _, ch := range queues {
		close(ch)
	}
	wg.Wait()
	if readErr != nil {
		db.Close()
		return readErr
	}
	return db.Close() // every acked write is already fsynced
}

// serveReader reads stdin line by line and enqueues requests. It returns
// nil on EOF (clean shutdown) or the underlying read error. Lines of at
// least 4 MiB are accepted; a batch block is read contiguously here, in the
// reader, so the n op lines cannot interleave with another writer.
func serveReader(in io.Reader, queue func(int) chan request, nextID *uint64) error {
	br := bufio.NewReaderSize(in, serveLineBuf)
	for {
		line, err := readLine(br)
		if strings.TrimSpace(line) != "" {
			r := parseRequest(br, line, nextID)
			queue(r.writer) <- r // blocks when full: backpressure, order kept
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil // clean shutdown path
			}
			return err
		}
	}
}

// readLine reads one newline-terminated line without its terminator. The
// last line of a stream may arrive without one, together with io.EOF.
func readLine(br *bufio.Reader) (string, error) {
	line, err := br.ReadString('\n')
	line = strings.TrimSuffix(line, "\n")
	line = strings.TrimSuffix(line, "\r")
	return line, err
}

// parseRequest parses "@<writer>[:<opid>] <op ...>" (the prefix is optional
// and defaults to writer 0 with an auto-incrementing op-id, SPEC section
// 12). A malformed request becomes a BADREQ error; parse failures never
// touch the database. batch reads its n op lines from br right here, so the
// block stays contiguous on the wire.
func parseRequest(br *bufio.Reader, line string, nextID *uint64) request {
	r := request{writer: 0, opid: 0}

	rest := line
	if strings.HasPrefix(rest, "@") {
		sp := strings.IndexByte(rest, ' ')
		if sp < 0 {
			r.err = usagef("bad request: @writer:id prefix with no operation")
			return r
		}
		writer, opid, explicit, err := parsePrefix(rest[:sp])
		if err != nil {
			r.err = err
			return r
		}
		r.writer = writer
		if explicit {
			r.opid = opid
		} else {
			r.opid = *nextID
			*nextID++
		}
		rest = strings.TrimSpace(rest[sp+1:])
	} else {
		r.opid = *nextID
		*nextID++
	}

	fields := strings.Fields(rest)
	if len(fields) == 0 {
		r.err = usagef("bad request: missing operation")
		return r
	}
	switch fields[0] {
	case "put":
		if len(fields) != 3 {
			r.err = usagef("bad request: want \"put <key> <value>\"")
			return r
		}
		r.op = "put"
		r.key, r.err = decodeServeToken(fields[1], "key")
		if r.err == nil {
			r.val, r.err = decodeServeToken(fields[2], "value")
		}
	case "del", "get":
		if len(fields) != 2 {
			r.err = usagef("bad request: want \"%s <key>\"", fields[0])
			return r
		}
		r.op = fields[0]
		r.key, r.err = decodeServeToken(fields[1], "key")
	case "compact", "stats":
		if len(fields) != 1 {
			r.err = usagef("bad request: %s takes no arguments", fields[0])
			return r
		}
		r.op = fields[0]
	case "batch":
		if len(fields) != 2 {
			r.err = usagef("bad request: want \"batch <n>\" followed by <n> op lines")
			return r
		}
		n, err := strconv.Atoi(fields[1])
		if err != nil || n < 0 {
			r.err = usagef("bad request: batch count must be a non-negative integer, got %q", fields[1])
			return r
		}
		r.op = "batch"
		r.ops, r.err = readBatchBlock(br, n)
	default:
		r.err = usagef("bad request: unknown operation %q", fields[0])
	}
	return r
}

// parsePrefix parses "@<writer>:<opid>" or the shorter "@<writer>" (opid
// then auto-increments). explicit reports whether the opid came from the
// prefix. A malformed prefix yields opid 0 and a usage error.
func parsePrefix(pfx string) (writer int, opid uint64, explicit bool, err error) {
	if !strings.HasPrefix(pfx, "@") {
		return 0, 0, false, usagef("bad request: prefix %q must start with @", pfx)
	}
	parts := strings.Split(pfx[1:], ":")
	if len(parts) > 2 {
		return 0, 0, false, usagef("bad request: malformed prefix %q", pfx)
	}
	w, e := strconv.Atoi(parts[0])
	if e != nil || w < 0 {
		return 0, 0, false, usagef("bad request: writer id in %q must be a non-negative integer", pfx)
	}
	if len(parts) == 1 {
		return w, 0, false, nil
	}
	id, e := strconv.ParseUint(parts[1], 10, 64)
	if e != nil {
		return 0, 0, false, usagef("bad request: op id in %q must be a non-negative integer", pfx)
	}
	return w, id, true, nil
}

// readBatchBlock reads exactly n op lines ("put <k> <v>" / "del <k>") that
// follow a "batch <n>" line (SPEC section 12). It always consumes n lines
// so the stream stays in sync; the first malformed line is reported and the
// whole request becomes one BADREQ. n above MaxBatchOps is drained but not
// stored, so the engine's ErrBatchTooLarge (TOOLARGE) does the rejecting
// without unbounded memory.
func readBatchBlock(br *bufio.Reader, n int) ([]engine.Op, error) {
	if n > engine.MaxBatchOps {
		// Drain without storing so the stream stays in sync; the engine's
		// ErrBatchTooLarge (TOOLARGE) rejects without unbounded memory.
		for i := 0; i < n; i++ {
			_, err := readLine(br)
			if err != nil { // EOF: nothing left to drain either way
				break
			}
		}
		return nil, engine.ErrBatchTooLarge
	}
	ops := make([]engine.Op, 0, n)
	var firstErr error
	for i := 0; i < n; i++ {
		line, err := readLine(br)
		eof := errors.Is(err, io.EOF)
		if err != nil && !eof {
			return nil, usagef("bad request: reading batch line %d: %v", i+1, err)
		}
		if line != "" {
			op, perr := parseBatchLine(line)
			if perr != nil {
				if firstErr == nil {
					firstErr = usagef("bad request: batch line %d: %v", i+1, perr)
				}
			} else {
				ops = append(ops, op)
			}
		} else if firstErr == nil {
			firstErr = usagef("bad request: unexpected EOF inside batch block")
		}
		if eof {
			// The stream ended. Only a complete block can succeed: this
			// was the last needed line, delivered without a newline.
			if i == n-1 && firstErr == nil {
				break
			}
			return nil, firstErr
		}
	}
	if firstErr != nil {
		return nil, firstErr // one bad line rejects the whole batch
	}
	return ops, nil
}

// parseBatchLine parses one "put <key> <value>" or "del <key>" op line.
func parseBatchLine(line string) (engine.Op, error) {
	fields := strings.Fields(strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r"))
	var zero engine.Op
	if len(fields) == 0 {
		return zero, usagef("empty op line")
	}
	switch {
	case fields[0] == "put" && len(fields) == 3:
		key, err := decodeToken(fields[1])
		if err != nil {
			return zero, err
		}
		val, err := decodeToken(fields[2])
		if err != nil {
			return zero, err
		}
		return engine.Op{Kind: engine.OpPut, Key: key, Value: val}, nil
	case fields[0] == "del" && len(fields) == 2:
		key, err := decodeToken(fields[1])
		if err != nil {
			return zero, err
		}
		return engine.Op{Kind: engine.OpDel, Key: key}, nil
	default:
		return zero, usagef("want \"put <key> <value>\" or \"del <key>\", got %s", snippet(line))
	}
}

// decodeServeToken decodes one token for a serve request, tagged BADREQ.
// The token grammar is identical to the CLI's (SPEC section 10); only the
// ERR code label differs, so the usage-error text is enough.
func decodeServeToken(tok, what string) ([]byte, error) {
	b, err := decodeToken(tok)
	if err != nil {
		return nil, usagef("bad request: bad %s token: %v", what, err)
	}
	return b, nil
}

// serveWorker owns one writer's queue: it runs that writer's requests in
// arrival order and prints exactly one response line for each, under the
// shared stdout mutex. ACK is printed only after the engine call returned
// success, i.e. after the record is fsynced (SPEC section 12).
func serveWorker(db *engine.DB, ch <-chan request, em *emitter) {
	for r := range ch {
		r.serve(db, em)
	}
}

// serve executes one request and emits its single response line.
func (r request) serve(db *engine.DB, em *emitter) {
	if r.err != nil {
		em.emitf("ERR %d BADREQ %v\n", r.opid, r.err)
		return
	}
	switch r.op {
	case "get":
		val, found, err := db.Get(r.key)
		if err != nil {
			em.emitf("ERR %d %s %v\n", r.opid, errCode(err), err)
			return
		}
		if !found {
			em.emitf("NOTFOUND %d\n", r.opid)
			return
		}
		em.emitf("VAL %d %s\n", r.opid, encodeToken(val))
		return
	case "stats":
		s := db.Stats()
		em.emitf("STATS %d keys=%d file_bytes=%d dead_bytes=%d\n",
			r.opid, s.Keys, s.FileBytes, s.DeadBytes)
		return
	}
	var seq uint64
	var err error
	switch r.op {
	case "put":
		seq, err = db.Put(r.key, r.val)
	case "del":
		seq, err = db.Delete(r.key)
	case "batch":
		seq, err = db.Batch(r.ops)
	case "compact":
		err = db.Compact() // no record is written: ACK carries seq 0
	default:
		err = usagef("bad request: unknown operation %q", r.op)
	}
	if err != nil {
		em.emitf("ERR %d %s %v\n", r.opid, errCode(err), err)
		return
	}
	em.emitf("ACK %d %d\n", r.opid, seq)
}

// errCode maps an engine or parse error to the four ERR codes of SPEC
// section 12: DISKFULL, TOOLARGE and FAILED are exact; everything else
// (bad tokens, unknown ops, argument errors, unexpected engine states) is
// BADREQ, the only remaining code the protocol defines.
func errCode(err error) string {
	switch {
	case errors.Is(err, engine.ErrDiskFull):
		return "DISKFULL"
	case errors.Is(err, engine.ErrFailed):
		return "FAILED"
	case errors.Is(err, engine.ErrKeyTooLarge),
		errors.Is(err, engine.ErrValueTooLarge),
		errors.Is(err, engine.ErrBatchTooLarge):
		return "TOOLARGE"
	default:
		return "BADREQ"
	}
}
