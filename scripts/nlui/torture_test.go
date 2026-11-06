package main

import (
	"context"
	"os"
	"testing"
)

// Lines copied from real runs (runs/logs/*.log and a fakeserve self-test).
func TestParseTorture(t *testing.T) {
	cases := []struct {
		line string
		want tortureLine
	}{
		{"torture: seed=12345 mode=default iterations=500, oracle log runs/12345/oracle.log",
			tortureLine{Kind: "start", Seed: "12345", Mode: "default", Of: 500, Text: "runs/12345/oracle.log"}},
		{"iter 4/500 ok  delay=369ms sent=306 acked=304 err=0 inflight=2 keys=45 recovery_ms=0",
			tortureLine{Kind: "iter", Iter: 4, Of: 500, DelayMS: 369, Sent: 306, Acked: 304, Inflight: 2, Keys: 45, RecoveryMS: "0"}},
		{"iter 1/100 ok  delay=160ms sent=95 acked=74 err=19 inflight=2 keys=28 recovery_ms=0",
			tortureLine{Kind: "iter", Iter: 1, Of: 100, DelayMS: 160, Sent: 95, Acked: 74, Errs: 19, Inflight: 2, Keys: 28, RecoveryMS: "0"}},
		{"iter 1/130 ok  delay=113ms sent=63 acked=61 err=0 inflight=2 keys=45 recovery_ms=0  [serve:before_write/54 hit]",
			tortureLine{Kind: "iter", Iter: 1, Of: 130, DelayMS: 113, Sent: 63, Acked: 61, Inflight: 2, Keys: 45, RecoveryMS: "0", Note: "serve:before_write/54 hit"}},
		{"iter 2/100 ok  delay=102ms sent=83 acked=81 err=0 inflight=2 keys=52 recovery_ms=1  [open-kill@1% open-kill@72% (1/2 open()s killed before finishing)]",
			tortureLine{Kind: "iter", Iter: 2, Of: 100, DelayMS: 102, Sent: 83, Acked: 81, Inflight: 2, Keys: 52, RecoveryMS: "1", Note: "open-kill@1% open-kill@72% (1/2 open()s killed before finishing)"}},
		{"PASS 500/500 iterations (seed 12345, mode default)",
			tortureLine{Kind: "pass", Iter: 500, Of: 500, Seed: "12345", Mode: "default"}},
		{"FAIL iter 1/5 delay=166ms sent=19038 acked=19038 err=0 inflight=0",
			tortureLine{Kind: "fail", Iter: 1, Of: 5, DelayMS: 166, Sent: 19038, Acked: 19038}},
		{"  repro: ./bin/torture --seed 11 --iterations 1 --mode default --bin ./bin/fakeserve --dir runs/selftest",
			tortureLine{Kind: "repro", Text: "./bin/torture --seed 11 --iterations 1 --mode default --bin ./bin/fakeserve --dir runs/selftest"}},
		{"FAILED after 0 of 5 iterations passed (seed 11, mode default)",
			tortureLine{Kind: "failed", Iter: 0, Of: 5, Seed: "11", Mode: "default"}},
		{"FAIL setup: mkdir runs/1: permission denied", tortureLine{Kind: "setup", Text: "mkdir runs/1: permission denied"}},
		{"  plan: kill 680us after compact #2", tortureLine{Kind: "detail", Text: "plan: kill 680us after compact #2"}},
	}
	for _, c := range cases {
		got := parseTorture(c.line)
		if got == nil || *got != c.want {
			t.Errorf("parseTorture(%q)\n got %+v\nwant %+v", c.line, got, c.want)
		}
	}
	for _, line := range []string{"", "ok  \tninelives/torture\t1.2s", "iter x/5 ok", "PASS 3/3"} {
		if got := parseTorture(line); got != nil {
			t.Errorf("parseTorture(%q) = %+v, want nil", line, got)
		}
	}
}

// For real: a short torture run streams a start, every iteration and PASS.
func TestTortureEventsForReal(t *testing.T) {
	needBinary(t)
	if _, err := os.Stat("../../bin/torture"); err != nil {
		t.Skip("bin/torture not built")
	}
	a := testApp(t)
	j, err := a.buildJob(runRequest{Action: "torture", Args: []string{"default", "4242", "3"}})
	if err != nil {
		t.Fatal(err)
	}
	j.Steps[0].Argv = append(j.Steps[0].Argv, "--dir", t.TempDir()) // keep runs/ clean
	evs, ok := runTestJob(t, a, context.Background(), j)
	var kinds []string
	for _, ev := range evs {
		if ev.Type == "torture" {
			kinds = append(kinds, ev.Torture.Kind)
		}
	}
	if !ok || len(kinds) != 5 || kinds[0] != "start" || kinds[3] != "iter" || kinds[4] != "pass" {
		t.Errorf("ok=%v torture events %v\n%s", ok, kinds, outText(evs, "stdout"))
	}
}
