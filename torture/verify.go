// Verifier: compares the recovered dump against the oracle using the rules in
// docs/SPEC.md section 13 (allowed values, batch atomicity, unknown values,
// recovery health). PR L2. The oracle is built only from what the tool sent
// and what serve replied, never from the database.
package main

import (
	"fmt"
	"strings"
)

// effect is what one operation does to one key. present==false means ABSENT.
type effect struct {
	present bool
	value   string
}

func (e effect) String() string {
	if !e.present {
		return "ABSENT"
	}
	return e.value
}

// singleOpEffect returns the effect of op o on key k, or nil if o does not touch k.
func singleOpEffect(o op, k string) *effect {
	if string(o.key) != k {
		return nil
	}
	if o.del {
		return &effect{present: false}
	}
	return &effect{present: true, value: string(o.value)}
}

// batchEffect returns the effect of one batch on key k, applying its ops in
// order, or nil if the batch does not touch k.
func batchEffect(ops []op, k string) *effect {
	var cur *effect
	touched := false
	for _, o := range ops {
		if string(o.key) != k {
			continue
		}
		touched = true
		cur = singleOpEffect(o, k)
	}
	if !touched {
		return nil
	}
	return cur
}

// opEffect returns the effect of request r on key k, or nil if r does not touch k.
func opEffect(r request, k string) *effect {
	if r.batch {
		return batchEffect(r.ops, k)
	}
	if len(r.ops) == 0 {
		return nil
	}
	return singleOpEffect(r.ops[0], k)
}

// requestKeys returns the set of keys r mentions.
func requestKeys(r request) map[string]bool {
	ks := map[string]bool{}
	for _, o := range r.ops {
		ks[string(o.key)] = true
	}
	return ks
}

// effRef records one op's effect on a key, with the op id for reports.
type effRef struct {
	seq  uint64
	opid uint64
	eff  *effect
}

// verifyFailure describes one mismatch between the dump and the oracle.
type verifyFailure struct {
	key    string
	found  string   // dump value, or "<absent>"
	expect []string // allowed values
	opids  []uint64 // op ids involved
	msg    string
}

func (e *verifyFailure) Error() string { return e.msg }

func newFailure(key, found string, expect []string, opids []uint64, format string, args ...any) *verifyFailure {
	return &verifyFailure{key: key, found: found, expect: expect, opids: opids, msg: fmt.Sprintf(format, args...)}
}

// tagOf returns the value tag that traces one request back to a single op.
func tagOf(r request) string { return fmt.Sprintf("w%do%d:", r.writer, r.opid) }

// inStrings reports whether s is in the list.
func inStrings(s string, list []string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// uniqueOpids returns the distinct op ids in refs.
func uniqueOpids(refs []effRef) []uint64 {
	seen := map[uint64]bool{}
	var out []uint64
	for _, r := range refs {
		if !seen[r.opid] {
			seen[r.opid] = true
			out = append(out, r.opid)
		}
	}
	return out
}

// lastAcked returns the effect of the ACKed op touching k with the highest
// seq, or the base value/effect if no ACKed op touches k.
func lastAcked(k string, ackedByKey map[string][]effRef, base map[string]string) *effect {
	best := ackedByKey[k]
	bi := -1
	for i, ref := range best {
		if bi < 0 || ref.seq > best[bi].seq {
			bi = i
		}
	}
	if bi >= 0 {
		return best[bi].eff
	}
	if v, ok := base[k]; ok {
		return &effect{present: true, value: v}
	}
	return &effect{present: false}
}

// lastSeq returns the seq of the ACKed op touching k with the highest seq,
// or 0 if none.
func lastSeq(k string, ackedByKey map[string][]effRef) uint64 {
	best := ackedByKey[k]
	bi := -1
	for i, ref := range best {
		if bi < 0 || ref.seq > best[bi].seq {
			bi = i
		}
	}
	if bi < 0 {
		return 0
	}
	return best[bi].seq
}

// verify compares the recovered dump against the oracle (docs/SPEC.md
// section 13, rules 4-7). base is the previously verified dump (nil for
// iteration 1). Recovery health (rule 8) is checked by the harness before
// this is called.
func verify(state map[string]string, o *oracle, base map[string]string) error {
	o.mu.Lock()
	defer o.mu.Unlock()

	touched := map[string]bool{}           // keys any sent op mentioned this iteration
	ackedByKey := map[string][]effRef{}    // ACKed effects per key
	inFlightByKey := map[string][]effRef{} // in-flight effects per key

	for _, rec := range o.order {
		r := rec.req
		ks := requestKeys(r)
		for k := range ks {
			touched[k] = true
		}
		if rec.status == acked {
			for k := range ks {
				if eff := opEffect(r, k); eff != nil {
					ackedByKey[k] = append(ackedByKey[k], effRef{seq: rec.seq, opid: r.opid, eff: eff})
				}
			}
		} else if rec.status == inFlight {
			for k := range ks {
				if eff := opEffect(r, k); eff != nil {
					inFlightByKey[k] = append(inFlightByKey[k], effRef{opid: r.opid, eff: eff})
				}
			}
		}
		// refused ops never apply and contribute nothing.
	}

	// Rule 4: allowed values per touched key.
	for k := range touched {
		last := lastAcked(k, ackedByKey, base)
		allowed := []string{}
		if last.present {
			allowed = append(allowed, last.value)
		}
		for _, ref := range inFlightByKey[k] {
			if ref.eff.present {
				allowed = append(allowed, ref.eff.value)
			}
		}
		opids := append(uniqueOpids(ackedByKey[k]), uniqueOpids(inFlightByKey[k])...)

		found, ok := state[k]
		if ok {
			if !inStrings(found, allowed) {
				return newFailure(k, found, allowed, opids,
					"key %q: value %q is not in allowed values %v", k, found, allowed)
			}
		} else {
			// The key is absent in the dump. That is fine when lastAcked is
			// absent, or when an in-flight op touching k has effect ABSENT
			// (a delete, or a batch whose last op on k deletes it): such an
			// in-flight op may or may not have been applied before the kill
			// (SPEC rule 4). Otherwise the last ACKed value must be present.
			if last.present {
				inflightAbsent := false
				for _, ref := range inFlightByKey[k] {
					if !ref.eff.present {
						inflightAbsent = true
						break
					}
				}
				if !inflightAbsent {
					return newFailure(k, "<absent>", allowed, opids,
						"key %q: expected %q (last ACKed seq=%d), found absent", k, last.value, lastSeq(k, ackedByKey))
				}
			}
		}
	}

	// Rule 5: untouched keys must equal the base state.
	for k, found := range state {
		if touched[k] {
			continue
		}
		if baseVal, hasBase := base[k]; hasBase {
			if found != baseVal {
				return newFailure(k, found, []string{baseVal}, nil,
					"untouched key %q: expected %q, found %q", k, baseVal, found)
			}
		} else {
			return newFailure(k, found, nil, nil,
				"untouched key %q appeared with no base value", k)
		}
	}
	for k, baseVal := range base {
		if touched[k] {
			continue
		}
		if _, ok := state[k]; !ok {
			return newFailure(k, "<absent>", []string{baseVal}, nil,
				"untouched key %q disappeared (expected %q)", k, baseVal)
		}
	}

	// Rule 6: batch atomicity for in-flight batches. Best effort: a key also
	// written by another op is left to rule 4 (no false positives).
	for _, rec := range o.order {
		if rec.status != inFlight || !rec.req.batch {
			continue
		}
		r := rec.req
		ks := requestKeys(r)
		showsB := false
		for k := range ks {
			if v, ok := state[k]; ok && strings.HasPrefix(v, tagOf(r)) {
				showsB = true
				break
			}
		}
		if !showsB {
			continue
		}
		for k := range ks {
			other := false
			for _, rec2 := range o.order {
				if rec2 == rec {
					continue
				}
				if rec2.status != acked && rec2.status != inFlight {
					continue
				}
				if requestKeys(rec2.req)[k] {
					other = true
					break
				}
			}
			if other {
				continue
			}
			be := batchEffect(r.ops, k)
			if be == nil {
				continue
			}
			want := "<absent>"
			if be.present {
				want = be.value
			}
			found, ok := state[k]
			if !ok {
				if be.present {
					return newFailure(k, "<absent>", []string{want}, []uint64{r.opid},
						"in-flight batch %d applied on %q but key is absent", r.opid, k)
				}
				continue
			}
			if found != want {
				return newFailure(k, found, []string{want}, []uint64{r.opid},
					"in-flight batch %d applied on %q: expected %q, found %q", r.opid, k, want, found)
			}
		}
	}

	// Rule 7: unknown values are subsumed by rule 4, because allowed only
	// contains effects of ops that touched the key, each carrying its own
	// tag; anything else is rejected above.
	return nil
}
