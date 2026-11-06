package main

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

func texts(items []item) []string {
	var s []string
	for _, it := range items {
		s = append(s, it.text)
	}
	return s
}

func TestSuggestions(t *testing.T) {
	cases := []struct {
		line string
		want []string // must be a prefix of the suggestions, in order
	}{
		{"/", []string{"/build", "/test"}},
		{"/test", []string{"/test", "/test-race", "/test-crashpoints"}}, // exact match first
		{"/tort", []string{"/torture", "/torture-all"}},
		{"/torture ", modes},
		{"/torture la", []string{"large-values", "large-batch"}},
		{"/crash after_f", []string{"after_fsync"}},
		{"/crash recovery_", []string{"recovery_mid_scan", "recovery_before_truncate", "recovery_after_truncate"}},
		{"/recov", []string{"/recover"}},             // prefix only: not /test-recovery-1m
		{"/recovery", []string{"/test-recovery-1m"}}, // no prefix match: contains
	}
	if got := texts(suggestions("/p")); strings.Join(got, ",") != "/put" {
		t.Errorf(`suggestions("/p") = %v, want only /put`, got)
	}
	for _, tc := range cases {
		got := texts(suggestions(tc.line))
		if len(got) < len(tc.want) || strings.Join(got[:len(tc.want)], ",") != strings.Join(tc.want, ",") {
			t.Errorf("suggestions(%q) = %v, want it to start with %v", tc.line, got, tc.want)
		}
	}
	for _, line := range []string{"", "test", "/put ", "/torture default ", "/crash after_fsync", "/nosuch x"} {
		if got := suggestions(line); len(got) != 0 {
			t.Errorf("suggestions(%q) = %v, want none", line, texts(got))
		}
	}
}

func TestAccept(t *testing.T) {
	for line, want := range map[string]string{
		"/to":            "/torture ",
		"/torture la":    "/torture large-values ",
		"/torture ":      "/torture large-values ",
		"/crash after_f": "/crash large-values ",
	} {
		pick := "/torture"
		if strings.Contains(line, " ") {
			pick = "large-values"
		}
		if got := accept(line, pick); got != want {
			t.Errorf("accept(%q, %q) = %q, want %q", line, pick, got, want)
		}
	}
}

// Every command has a description, and every script-backed one builds a
// script that sh can at least parse.
func TestEveryScriptParses(t *testing.T) {
	for _, c := range commands {
		if c.desc == "" {
			t.Errorf("/%s has no description", c.name)
		}
		if c.script == nil {
			continue
		}
		args := []string{"default", "1", "2"}
		if c.name == "crash" {
			args = []string{"recovery_mid_scan"}
		}
		if out, err := exec.Command("sh", "-n", "-c", c.script(args)).CombinedOutput(); err != nil {
			t.Errorf("/%s: sh cannot parse its script: %v\n%s", c.name, err, out)
		}
	}
}

func TestQuote(t *testing.T) {
	out, err := exec.Command("sh", "-c", "printf %s "+q("it's $HOME; rm")).Output()
	if err != nil || string(out) != "it's $HOME; rm" {
		t.Fatalf("q round trip = %q, %v", out, err)
	}
}

// Piped input (no terminal): one command per line, run for real.
func TestLineModeRunsCommands(t *testing.T) {
	if _, err := os.Stat("../../bin/ninelives"); err != nil {
		t.Skip("bin/ninelives not built (run scripts/build.sh)")
	}
	bin := t.TempDir() + "/nlshell"
	if out, err := exec.Command("go", "build", "-o", bin, ".").CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	cmd := exec.Command(bin)
	cmd.Dir = "../.."
	cmd.Env = append(os.Environ(), "TMPDIR="+t.TempDir())
	cmd.Stdin = strings.NewReader("/reset\n/put a 1\n/get a\n/batch put b 2; del a\n/dump\n/nosuch\n/exit\n/get a\n")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("nlshell: %v\n%s", err, out)
	}
	s := string(out)
	for _, want := range []string{"OK seq=1", "\n1\n", "OK seq=2", "b 2\n", `unknown command "/nosuch"`} {
		if !strings.Contains(s, want) {
			t.Errorf("output lacks %q:\n%s", want, s)
		}
	}
	if strings.Count(s, "nl> /get a") != 1 {
		t.Errorf("commands after /exit ran:\n%s", s)
	}
}
