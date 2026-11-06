package main

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestGlyphsCoverTitle(t *testing.T) {
	for _, ch := range title {
		g, ok := glyphs[ch]
		if !ok {
			t.Fatalf("no glyph for %q", ch)
		}
		for r, row := range g {
			if utf8.RuneCountInString(row) != utf8.RuneCountInString(g[0]) {
				t.Errorf("glyph %q row %d is %d wide, row 0 is %d", ch, r, utf8.RuneCountInString(row), utf8.RuneCountInString(g[0]))
			}
		}
	}
}

func TestBannerPlain(t *testing.T) {
	rows := strings.Split(strings.TrimSuffix(banner(200, false), "\n"), "\n")
	if len(rows) != 6 {
		t.Fatalf("banner has %d rows, want 6 (5 letter rows + shadow)", len(rows))
	}
	for i, r := range rows {
		if strings.Contains(r, "\x1b") {
			t.Errorf("row %d has an escape code with colour off: %q", i, r)
		}
		if n := utf8.RuneCountInString(r); n > bannerWidth() {
			t.Errorf("row %d is %d wide, banner is %d", i, n, bannerWidth())
		}
		if strings.HasSuffix(r, " ") {
			t.Errorf("row %d has trailing spaces", i)
		}
	}
	if strings.Contains(rows[5], "█") || !strings.Contains(rows[5], "░") {
		t.Errorf("last row must be shadow only: %q", rows[5])
	}
}

func TestBannerNarrow(t *testing.T) {
	if got := banner(bannerWidth()+1, false); got != "NineLives\n" {
		t.Errorf("narrow banner = %q, want a one-line title", got)
	}
	if got := banner(bannerWidth()+2, false); strings.Count(got, "\n") != 6 {
		t.Errorf("banner at exactly its width + 2 should be the big one, got %q", got)
	}
}

func TestBannerColor(t *testing.T) {
	t.Setenv("COLORTERM", "truecolor")
	for _, r := range strings.Split(strings.TrimSuffix(banner(200, true), "\n"), "\n") {
		if !strings.Contains(r, "\x1b[38;2;") || !strings.HasSuffix(r, "\x1b[0m") {
			t.Errorf("truecolor row must use 24-bit codes and end in a reset: %q", r)
		}
	}
	if got := colorCode(brown); got != "\x1b[38;2;160;82;45m" {
		t.Errorf("colorCode(brown) = %q", got)
	}
	t.Setenv("COLORTERM", "")
	if !strings.Contains(banner(200, true), "\x1b[38;5;") {
		t.Error("without COLORTERM the banner must fall back to 256 colours")
	}
	// 255,159,28 -> cube 5,3,1 -> 16+180+18+1
	if got := colorCode(orange); got != "\x1b[38;5;215m" {
		t.Errorf("colorCode(orange) = %q, want 215", got)
	}
}

func TestBlendEnds(t *testing.T) {
	if blend(brown, orange, 0) != brown || blend(brown, orange, 1) != orange {
		t.Error("blend must return the end colours at t=0 and t=1")
	}
}

// A tip must never name a command that does not exist.
func TestTipsNameRealCommands(t *testing.T) {
	for _, tip := range tips {
		for _, w := range strings.Fields(tip) {
			if w = strings.TrimRight(w, ".,;"); strings.HasPrefix(w, "/") && w != "/" && lookup(w) == nil {
				t.Errorf("tip %q names %s, which is not a command", tip, w)
			}
		}
	}
}

func TestStatusLineFits(t *testing.T) {
	for _, w := range []int{40, 60, 80, 200} {
		line := strings.TrimSuffix(statusLine(w, false), "\n")
		// Over width is only allowed once the directory is dropped entirely.
		if n := utf8.RuneCountInString(line); n > w-1 && !strings.HasPrefix(line, "  demo db") {
			t.Errorf("width %d: status line is %d wide: %q", w, n, line)
		}
		if !strings.Contains(line, db) || !strings.Contains(line, "bin/ninelives") {
			t.Errorf("width %d: status line lacks the db path or build state: %q", w, line)
		}
	}
}
