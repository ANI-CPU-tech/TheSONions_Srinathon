package main

import (
	"testing"
)

func rec(writer int, opid uint64, batch bool, ops []op, status status, seq uint64) *record {
	return &record{req: request{writer: writer, opid: opid, batch: batch, ops: ops}, status: status, seq: seq}
}

func putOp(key, value string) op { return op{key: []byte(key), value: []byte(value)} }
func delOp(key string) op        { return op{del: true, key: []byte(key)} }

func newOracle(recs ...*record) *oracle {
	o := &oracle{recs: map[uint64]*record{}, order: []*record{}}
	for _, r := range recs {
		o.recs[r.req.opid] = r
		o.order = append(o.order, r)
	}
	return o
}

// tag returns the value tag tracing one request back to a single op.
func tag(writer int, opid uint64) string { return tagOf(request{writer: writer, opid: opid}) }

// TestVerifyPassesHonestRun: ACKed values present, in-flight values allowed,
// untouched keys equal base.
func TestVerifyPassesHonestRun(t *testing.T) {
	tag1 := tag(1, 100)
	v1 := tag1 + "aaa"
	tag2 := tag(2, 200)
	v2 := tag2 + "bbb"
	o := newOracle(
		rec(1, 100, false, []op{putOp("k1", v1)}, acked, 1),
		rec(2, 200, false, []op{putOp("k2", v2)}, inFlight, 0),
	)
	state := map[string]string{"k1": v1, "k2": v2}
	if err := verify(state, o, nil); err != nil {
		t.Fatalf("honest run should pass: %v", err)
	}
}

// TestVerifyAckedValueMissing: an ACKed put must survive; absence is a failure.
func TestVerifyAckedValueMissing(t *testing.T) {
	v1 := tag(1, 100) + "aaa"
	o := newOracle(rec(1, 100, false, []op{putOp("k1", v1)}, acked, 1))
	state := map[string]string{} // k1 missing
	if err := verify(state, o, nil); err == nil {
		t.Fatal("expected failure for missing ACKed value")
	}
}

// TestVerifyUnknownValueRejected: a value whose tag matches no op touching
// the key is a failure (rule 7, subsumed by rule 4).
func TestVerifyUnknownValueRejected(t *testing.T) {
	v1 := tag(1, 100) + "aaa"
	o := newOracle(rec(1, 100, false, []op{putOp("k1", v1)}, acked, 1))
	state := map[string]string{"k1": tag(9, 999) + "zzz"}
	if err := verify(state, o, nil); err == nil {
		t.Fatal("expected failure for unknown value")
	}
}

// TestVerifyDeleteAbsent: an ACKed delete leaves the key absent.
func TestVerifyDeleteAbsent(t *testing.T) {
	o := newOracle(rec(1, 100, false, []op{delOp("k1")}, acked, 1))
	state := map[string]string{} // k1 correctly absent
	if err := verify(state, o, nil); err != nil {
		t.Fatalf("deleted key absent should pass: %v", err)
	}
	state = map[string]string{"k1": tag(1, 100) + "aaa"}
	if err := verify(state, o, nil); err == nil {
		t.Fatal("expected failure: deleted key present")
	}
}

// TestVerifyUntouchedKeysEqualBase: untouched keys must match base.
func TestVerifyUntouchedKeysEqualBase(t *testing.T) {
	o := newOracle(rec(1, 100, false, []op{putOp("k1", tag(1, 100)+"x")}, acked, 1))
	base := map[string]string{"k5": "base-value"}
	state := map[string]string{"k1": tag(1, 100) + "x", "k5": "base-value"}
	if err := verify(state, o, base); err != nil {
		t.Fatalf("untouched key matching base should pass: %v", err)
	}
	state = map[string]string{"k1": tag(1, 100) + "x", "k5": "changed"}
	if err := verify(state, o, base); err == nil {
		t.Fatal("expected failure for changed untouched key")
	}
}

// TestVerifyBatchAtomicity: an in-flight batch whose value shows must apply
// to every key it touches (unless another op also touches that key).
func TestVerifyBatchAtomicity(t *testing.T) {
	b := tag(1, 300)
	v1 := b + "v1"
	v2 := b + "v2"
	o := newOracle(rec(1, 300, true, []op{putOp("k1", v1), putOp("k2", v2)}, inFlight, 0))
	state := map[string]string{"k1": v1, "k2": v2}
	if err := verify(state, o, nil); err != nil {
		t.Fatalf("fully applied batch should pass: %v", err)
	}
	// partial application of the batch is a failure.
	state = map[string]string{"k1": v1}
	if err := verify(state, o, nil); err == nil {
		t.Fatal("expected failure for partially applied batch")
	}
}

// TestVerifyInFlightDeleteAbsent: an ACKed put followed by an in-flight delete
// of the same key may leave the key absent (rule 4: the delete's ABSENT effect
// is allowed).
func TestVerifyInFlightDeleteAbsent(t *testing.T) {
	v1 := tag(1, 100) + "aaa"
	o := newOracle(
		rec(1, 100, false, []op{putOp("k1", v1)}, acked, 1),
		rec(2, 200, false, []op{delOp("k1")}, inFlight, 0),
	)
	state := map[string]string{} // k1 removed by in-flight delete
	if err := verify(state, o, nil); err != nil {
		t.Fatalf("in-flight delete of an ACKed key may leave it absent: %v", err)
	}
	// But if nothing in flight can remove k1, its absence is a failure.
	o2 := newOracle(rec(1, 100, false, []op{putOp("k1", v1)}, acked, 1))
	state = map[string]string{}
	if err := verify(state, o2, nil); err == nil {
		t.Fatal("expected failure: ACKed value absent with no remover in flight")
	}
}
