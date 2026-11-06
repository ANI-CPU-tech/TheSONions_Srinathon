package main

// Parser for ./bin/torture output, for the Torture panel's progress bar,
// chart and table. The line formats come from torture/main.go (start and
// PASS lines) and torture/report.go (iteration summaries and the failure
// report). Lines it does not know are still shown as plain output.

import (
	"regexp"
	"strconv"
	"strings"
)

// tortureLine is one recognised line.
type tortureLine struct {
	Kind       string `json:"kind"` // start, iter, pass, fail, failed, setup, repro, detail
	Iter       int    `json:"iter,omitempty"`
	Of         int    `json:"of,omitempty"`
	DelayMS    int    `json:"delayMs,omitempty"`
	Sent       int    `json:"sent,omitempty"`
	Acked      int    `json:"acked,omitempty"`
	Errs       int    `json:"errs,omitempty"`
	Inflight   int    `json:"inflight,omitempty"`
	Keys       int    `json:"keys,omitempty"`
	RecoveryMS string `json:"recoveryMs,omitempty"`
	Note       string `json:"note,omitempty"` // the [mode detail] of an iteration
	Seed       string `json:"seed,omitempty"`
	Mode       string `json:"mode,omitempty"`
	Text       string `json:"text,omitempty"`
}

var (
	reStart  = regexp.MustCompile(`^torture: seed=(-?\d+) mode=(\S+) iterations=(\d+), oracle log (.+)$`)
	reIter   = regexp.MustCompile(`^iter (\d+)/(\d+) ok  delay=(\d+)ms sent=(\d+) acked=(\d+) err=(\d+) inflight=(\d+) keys=(\d+) recovery_ms=(\S+)(?:  \[(.*)\])?$`)
	rePass   = regexp.MustCompile(`^PASS (\d+)/(\d+) iterations \(seed (-?\d+), mode (\S+)\)$`)
	reFail   = regexp.MustCompile(`^FAIL iter (\d+)/(\d+) delay=(\d+)ms sent=(\d+) acked=(\d+) err=(\d+) inflight=(\d+)$`)
	reFailed = regexp.MustCompile(`^FAILED after (\d+) of (\d+) iterations passed \(seed (-?\d+), mode (\S+)\)$`)
)

// parseTorture recognises one output line, or returns nil.
func parseTorture(line string) *tortureLine {
	n := func(s string) int { v, _ := strconv.Atoi(s); return v }
	if m := reIter.FindStringSubmatch(line); m != nil {
		return &tortureLine{Kind: "iter", Iter: n(m[1]), Of: n(m[2]), DelayMS: n(m[3]), Sent: n(m[4]), Acked: n(m[5]),
			Errs: n(m[6]), Inflight: n(m[7]), Keys: n(m[8]), RecoveryMS: m[9], Note: m[10]}
	}
	if m := reStart.FindStringSubmatch(line); m != nil {
		return &tortureLine{Kind: "start", Seed: m[1], Mode: m[2], Of: n(m[3]), Text: m[4]}
	}
	if m := rePass.FindStringSubmatch(line); m != nil {
		return &tortureLine{Kind: "pass", Iter: n(m[1]), Of: n(m[2]), Seed: m[3], Mode: m[4]}
	}
	if m := reFail.FindStringSubmatch(line); m != nil {
		return &tortureLine{Kind: "fail", Iter: n(m[1]), Of: n(m[2]), DelayMS: n(m[3]), Sent: n(m[4]), Acked: n(m[5]),
			Errs: n(m[6]), Inflight: n(m[7])}
	}
	if m := reFailed.FindStringSubmatch(line); m != nil {
		return &tortureLine{Kind: "failed", Iter: n(m[1]), Of: n(m[2]), Seed: m[3], Mode: m[4]}
	}
	if rest, ok := strings.CutPrefix(line, "FAIL setup: "); ok {
		return &tortureLine{Kind: "setup", Text: rest}
	}
	if rest, ok := strings.CutPrefix(line, "  repro: "); ok {
		return &tortureLine{Kind: "repro", Text: rest}
	}
	if strings.HasPrefix(line, "  ") {
		return &tortureLine{Kind: "detail", Text: strings.TrimSpace(line)}
	}
	return nil
}
