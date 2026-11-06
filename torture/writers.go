// Writers: a seeded op stream (puts, deletes, batches) with unique tagged
// values w<writer>o<opid>:, sent by synchronous writers through one pipe
// mutex (docs/SPEC.md section 13, rules 1 and 2).
package main

import (
	"encoding/binary"
	"fmt"
	"math/rand/v2"
	"strings"
)

const (
	writers  = 2  // concurrent synchronous writers, ids 1..writers
	keySpace = 64 // small on purpose: both writers keep hitting the same keys
	maxFill  = 64 // random bytes after the tag in each value
	maxBatch = 10 // ops per batch in the default mode (at least 2)

	// opidStride separates op ids of different iterations, so every op of
	// the whole run has a unique id and every value a unique tag.
	opidStride = 10_000_000
)

// op is one put or delete.
type op struct {
	del        bool
	key, value []byte
}

// request is one serve request: a single op, a batch of ops, or a compact
// (no ops: it changes no key).
type request struct {
	writer  int
	opid    uint64
	batch   bool
	compact bool
	ops     []op
}

// line is the protocol text of o, without the newline.
func (o op) line() string {
	if o.del {
		return "del " + encodeToken(o.key)
	}
	return "put " + encodeToken(o.key) + " " + encodeToken(o.value)
}

// wire is the exact bytes sent to serve: one line, or a whole batch block,
// so it can go down the pipe in a single write.
func (r request) wire() []byte {
	var b strings.Builder
	if r.compact {
		fmt.Fprintf(&b, "@%d:%d compact\n", r.writer, r.opid)
	} else if r.batch {
		fmt.Fprintf(&b, "@%d:%d batch %d\n", r.writer, r.opid, len(r.ops))
		for _, o := range r.ops {
			b.WriteString(o.line() + "\n")
		}
	} else {
		fmt.Fprintf(&b, "@%d:%d %s\n", r.writer, r.opid, r.ops[0].line())
	}
	return []byte(b.String())
}

// text is r on one line, for oracle.log.
func (r request) text() string {
	if r.compact {
		return "compact"
	}
	if !r.batch {
		return r.ops[0].line()
	}
	parts := []string{fmt.Sprintf("batch %d", len(r.ops))}
	for _, o := range r.ops {
		parts = append(parts, o.line())
	}
	return strings.Join(parts, " ; ")
}

// generator makes one writer's op stream for one iteration. It depends only
// on seed, iteration and writer id, never on timing (SPEC rule 10). The mode
// tweaks value sizes and batch sizes (PR L3).
type generator struct {
	rng    *rand.Rand
	writer int
	base   uint64 // op id of this writer's first op in this iteration
	n      uint64 // ops made so far
	mode   string
}

func newGenerator(seed int64, iter, writer int, mode string) *generator {
	return &generator{
		rng:    rand.New(rand.NewPCG(uint64(seed), uint64(iter)*16+uint64(writer))),
		writer: writer,
		base:   uint64(iter)*opidStride + uint64(writer-1),
		mode:   mode,
	}
}

// next returns the next request: in the compaction and crashpoints modes
// first a compactPercent chance of a compact, then 50% put, 20% delete,
// 30% batch.
func (g *generator) next() request {
	opid := g.base + g.n*writers // writers interleave, ids never collide
	g.n++
	r := request{writer: g.writer, opid: opid}
	tag := fmt.Sprintf("w%do%d:", g.writer, opid)
	if g.rng.IntN(100) < compactPercent(g.mode) {
		r.compact = true
		return r
	}
	switch roll := g.rng.IntN(10); {
	case roll < 5:
		r.ops = []op{g.put(tag)}
	case roll < 7:
		r.ops = []op{g.del()}
	default:
		r.batch = true
		r.ops = make([]op, batchLen(g.mode, g.rng))
		for i := range r.ops {
			if g.rng.IntN(4) == 0 {
				r.ops[i] = g.del()
			} else {
				r.ops[i] = g.put(tag)
			}
		}
	}
	return r
}

func (g *generator) key() []byte { return fmt.Appendf(nil, "k%03d", g.rng.IntN(keySpace)) }

func (g *generator) del() op { return op{del: true, key: g.key()} }

// put makes a value that starts with tag, so it traces back to one op.
func (g *generator) put(tag string) op {
	n := fillLen(g.mode, len(tag), g.rng)
	v := append(make([]byte, 0, len(tag)+n), tag...)
	for ; n >= 8; n -= 8 { // 8 random bytes per call: 1 MiB values stay cheap
		v = binary.LittleEndian.AppendUint64(v, g.rng.Uint64())
	}
	for ; n > 0; n-- {
		v = append(v, byte(g.rng.Uint64()))
	}
	return op{key: g.key(), value: v}
}
