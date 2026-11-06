// Independent oracle: runs/<seed>/oracle.log records each op when SENT and
// its ACK seq or ERR when received (docs/SPEC.md section 13, rule 3). It is
// built only from what the tool sent and what serve replied, never from the
// database.
package main

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

type status int

const (
	inFlight status = iota // sent, no reply (the kill came first)
	acked                  // ACK received: must be durable
	refused                // ERR received: must never apply
)

// record is one op the tool sent in the current iteration.
type record struct {
	req    request
	status status
	seq    uint64 // from the ACK
	code   string // from the ERR
}

// oracle holds this iteration's records and appends every event to the log.
// Writers call it concurrently, so every method takes mu.
type oracle struct {
	mu    sync.Mutex
	start time.Time // timestamps are monotonic nanoseconds since start
	f     *os.File
	w     *bufio.Writer
	recs  map[uint64]*record
	order []*record // in send order
}

func openOracle(path string) (*oracle, error) {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return nil, err
	}
	return &oracle{start: time.Now(), f: f, w: bufio.NewWriter(f)}, nil
}

// now is the monotonic timestamp for a log line.
func (o *oracle) now() int64 { return time.Since(o.start).Nanoseconds() }

// beginIteration forgets the previous iteration's records.
func (o *oracle) beginIteration(iter int, delay time.Duration) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.recs = map[uint64]*record{}
	o.order = nil
	fmt.Fprintf(o.w, "ITER %d delay_ms=%d\n", iter, delay.Milliseconds())
}

// sent records r as in flight. Called before r is written to the pipe.
func (o *oracle) sent(r request) {
	o.mu.Lock()
	defer o.mu.Unlock()
	rec := &record{req: r}
	o.recs[r.opid] = rec
	o.order = append(o.order, rec)
	fmt.Fprintf(o.w, "SENT %d w%d %d %s\n", o.now(), r.writer, r.opid, r.text())
}

// reply records serve's answer to op opid: "ACK <opid> <seq>" or
// "ERR <opid> <CODE> <message>". Anything else is a protocol error.
func (o *oracle) reply(opid uint64, line string) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	rec := o.recs[opid]
	f := strings.Fields(line)
	switch {
	case rec == nil || rec.status != inFlight:
		return fmt.Errorf("reply %q for op %d, which is not in flight", line, opid)
	case len(f) == 3 && f[0] == "ACK":
		seq, err := strconv.ParseUint(f[2], 10, 64)
		if err != nil {
			return fmt.Errorf("bad seq in %q", line)
		}
		rec.status, rec.seq = acked, seq
		fmt.Fprintf(o.w, "ACK %d %d %d\n", o.now(), opid, seq)
	case len(f) >= 3 && f[0] == "ERR":
		rec.status, rec.code = refused, f[2]
		fmt.Fprintf(o.w, "ERR %d %d %s\n", o.now(), opid, f[2])
	default:
		return fmt.Errorf("unexpected reply %q to op %d", line, opid)
	}
	return nil
}

// killed logs the moment the tool sent SIGKILL.
func (o *oracle) killed() {
	o.mu.Lock()
	defer o.mu.Unlock()
	fmt.Fprintf(o.w, "KILL %d\n", o.now())
}

// counts returns how many ops were sent, acked, refused and left in flight.
func (o *oracle) counts() (sent, ack, errs, flight int) {
	o.mu.Lock()
	defer o.mu.Unlock()
	for _, r := range o.order {
		switch r.status {
		case acked:
			ack++
		case refused:
			errs++
		default:
			flight++
		}
	}
	return len(o.order), ack, errs, flight
}

// note appends a free-form line, such as an iteration result.
func (o *oracle) note(format string, args ...any) {
	o.mu.Lock()
	defer o.mu.Unlock()
	fmt.Fprintf(o.w, format+"\n", args...)
}

// flush pushes buffered lines to the file. The tool itself is never killed,
// so flushing once per iteration loses nothing.
func (o *oracle) flush() error {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.w.Flush()
}

func (o *oracle) close() error {
	if err := o.flush(); err != nil {
		o.f.Close()
		return err
	}
	return o.f.Close()
}
