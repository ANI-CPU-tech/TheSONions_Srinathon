package main

import (
	"fmt"
	"math"
	"os"
	"strings"
	"unicode/utf8"
)

// The welcome screen: a big NINELIVES in block letters with a drop shadow
// and a brown-to-orange gradient, numbered tips and a status line. Shown
// only on a real terminal; piped input gets the old one-line text.

// glyphs are 5-row block letters, only the ones NINELIVES needs.
var glyphs = map[rune][5]string{
	'N': {"███  ██", "████ ██", "██ ████", "██  ███", "██   ██"},
	'I': {"██", "██", "██", "██", "██"},
	'E': {"██████", "██    ", "█████ ", "██    ", "██████"},
	'L': {"██    ", "██    ", "██    ", "██    ", "██████"},
	'V': {"██   ██", "██   ██", "██   ██", " ██ ██ ", "  ███  "},
	'S': {" █████", "██    ", " ████ ", "    ██", "█████ "},
}

const (
	title     = "NINELIVES"
	letterGap = 2 // blank columns between letters
)

// Gradient ends: sienna brown on the left, orange on the right.
var brown, orange = [3]int{160, 82, 45}, [3]int{255, 159, 28}

var tips = []string{
	"Type / to see the commands. Tab completes, Up/Down picks, Enter runs.",
	"Run /build first, then /reset and /put a 1 to try the database.",
	"/crash mid_write kills the CLI with SIGKILL mid-write, then shows recovery.",
	"/help lists every command. Ctrl-C stops a command, Ctrl-D quits.",
}

// letterGrid turns NINELIVES into rows of lit (true) and dark cells.
func letterGrid() [5][]bool {
	var g [5][]bool
	for i, ch := range title {
		for r, row := range glyphs[ch] {
			if i > 0 {
				g[r] = append(g[r], make([]bool, letterGap)...)
			}
			for _, c := range row {
				g[r] = append(g[r], c == '█')
			}
		}
	}
	return g
}

// bannerWidth is the column count of the banner, shadow included.
func bannerWidth() int { return len(letterGrid()[0]) + 1 }

// banner draws the big title. Each output cell is a block if lit, a shadow
// if the cell up and to the left of it is lit, else a space. The shadow is
// one row and one column bigger than the letters. On a terminal too narrow
// for it, it returns a one-line title instead.
func banner(width int, color bool) string {
	if width < bannerWidth()+2 {
		return paint("NineLives", orange, color) + "\n"
	}
	g := letterGrid()
	lit := func(r, c int) bool { return r >= 0 && r < 5 && c >= 0 && c < len(g[r]) && g[r][c] }
	w := bannerWidth()
	var b strings.Builder
	for r := 0; r < 6; r++ {
		var row strings.Builder
		for c := 0; c < w; c++ {
			col := blend(brown, orange, float64(c)/float64(w-1))
			switch {
			case lit(r, c):
				row.WriteString(paint("█", col, color))
			case lit(r-1, c-1):
				row.WriteString(paint("░", shade(col), color))
			default:
				row.WriteByte(' ')
			}
		}
		b.WriteString(strings.TrimRight(row.String(), " ") + "\n")
	}
	return b.String()
}

// blend mixes two colours: t=0 gives a, t=1 gives b.
func blend(a, b [3]int, t float64) [3]int {
	var out [3]int
	for i := range out {
		out[i] = a[i] + int(math.Round(float64(b[i]-a[i])*t))
	}
	return out
}

// shade darkens a colour for the shadow.
func shade(c [3]int) [3]int { return blend([3]int{}, c, 0.55) }

// paint colours s, or returns it unchanged when colour is off.
func paint(s string, c [3]int, color bool) string {
	if !color {
		return s
	}
	return colorCode(c) + s + "\x1b[0m"
}

// colorCode is the escape for foreground colour c: 24-bit when the
// terminal says it supports it (COLORTERM), else the nearest colour of the
// xterm 256-colour 6x6x6 cube, which nearly every terminal has.
func colorCode(c [3]int) string {
	if ct := os.Getenv("COLORTERM"); ct == "truecolor" || ct == "24bit" {
		return fmt.Sprintf("\x1b[38;2;%d;%d;%dm", c[0], c[1], c[2])
	}
	cube := func(v int) int { return (v*5 + 127) / 255 }
	return fmt.Sprintf("\x1b[38;5;%dm", 16+36*cube(c[0])+6*cube(c[1])+cube(c[2]))
}

// useColor follows the NO_COLOR convention (no-color.org) and turns
// colour off when stdout is not a terminal.
func useColor() bool {
	if _, set := os.LookupEnv("NO_COLOR"); set {
		return false
	}
	fi, err := os.Stdout.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}

// tipLines numbers the tips and paints every /command orange.
func tipLines(color bool) string {
	var b strings.Builder
	b.WriteString("Tips for getting started:\n")
	for i, t := range tips {
		words := strings.Split(t, " ")
		for j, w := range words {
			if strings.HasPrefix(w, "/") {
				words[j] = paint(w, orange, color)
			}
		}
		fmt.Fprintf(&b, "%d. %s\n", i+1, strings.Join(words, " "))
	}
	return b.String()
}

// statusLine shows where nlshell runs, its database, and whether the CLI
// it drives has been built yet. A long directory is cut from the left so
// the line fits in width columns.
func statusLine(width int, color bool) string {
	dir, err := os.Getwd()
	if err != nil {
		dir = "?"
	}
	if home, err := os.UserHomeDir(); err == nil && (dir == home || strings.HasPrefix(dir, home+"/")) {
		dir = "~" + strings.TrimPrefix(dir, home)
	}
	built := "bin/ninelives built"
	if _, err := os.Stat(nl); err != nil {
		built = "bin/ninelives not built (run /build)"
	}
	rest := "  demo db " + db + "  ·  " + built
	if d := []rune(dir); len(d)+utf8.RuneCountInString(rest) > width-1 {
		dir = "" // too narrow for even a short tail of it
		if keep := width - 2 - utf8.RuneCountInString(rest); keep >= 8 {
			dir = "…" + string(d[len(d)-keep:])
		}
	}
	gray := [3]int{140, 140, 140}
	return paint(dir, orange, color) + paint(rest, gray, color) + "\n"
}

// welcome is the whole start screen.
func welcome(width int, color bool) string {
	return "\n" + banner(width, color) + "\n" + tipLines(color) + "\n" + statusLine(width, color) + "\n"
}
