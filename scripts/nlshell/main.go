// Command nlshell is an interactive menu for testing NineLives by hand.
// Type "/" and the matching commands appear under the prompt: Up/Down to
// pick, Tab to complete, Enter to run. Arguments complete too (torture
// modes, crash-point names). Every command prints the exact shell command
// it runs, so the same steps can be typed without nlshell in the demo.
//
// Run it from the repo root: go run ./scripts/nlshell
// Standard library only: raw terminal mode comes from the stty command.
// With stdin not a terminal it reads one command per line (for scripts).
package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"unicode/utf8"
)

const (
	prompt   = "nl> "
	maxShown = 8 // suggestion rows under the prompt
)

var in = bufio.NewReader(os.Stdin) // the only reader of stdin, shared by the editor and /serve

func main() {
	if _, err := os.Stat("scripts/build.sh"); err != nil {
		fmt.Fprintln(os.Stderr, "nlshell: run me from the NineLives repo root (go run ./scripts/nlshell)")
		os.Exit(2)
	}
	signal.Notify(make(chan os.Signal, 1), os.Interrupt) // Ctrl-C stops the running command, not nlshell
	fmt.Printf("NineLives test shell. Type / for commands, Tab to complete, Enter to run, Ctrl-D to quit.\nDemo database: %s\n", db)
	saved, err := stty("-g")
	if err != nil {
		lineMode()
		return
	}
	e := &editor{width: termWidth()}
	defer stty(saved)
	for {
		stty("raw", "-echo")
		line, ok := e.readLine()
		stty(saved)
		fmt.Print("\r\n")
		if !ok {
			return
		}
		if !execute(line) {
			return
		}
	}
}

// lineMode runs one command per input line, for piped input and tests.
func lineMode() {
	for {
		line, err := in.ReadString('\n')
		if line = strings.TrimSpace(line); line != "" {
			fmt.Println(prompt + line)
			if !execute(line) {
				return
			}
		}
		if err != nil {
			return
		}
	}
}

// stty runs stty on our terminal and returns its output.
func stty(args ...string) (string, error) {
	cmd := exec.Command("stty", args...)
	cmd.Stdin = os.Stdin
	out, err := cmd.Output()
	return strings.TrimSpace(string(out)), err
}

func termWidth() int {
	if s, err := stty("size"); err == nil {
		if f := strings.Fields(s); len(f) == 2 {
			if w, err := strconv.Atoi(f[1]); err == nil && w > 20 {
				return w
			}
		}
	}
	return 80
}

// execute runs one command line. It returns false to quit.
func execute(line string) bool {
	f := strings.Fields(line)
	if len(f) == 0 {
		return true
	}
	c := lookup(f[0])
	if c == nil {
		fmt.Printf("unknown command %q: type / to see the list, or /help\n", f[0])
		return true
	}
	args := f[1:]
	if len(args) < c.minArgs {
		fmt.Printf("usage: /%s %s\n", c.name, c.args)
		return true
	}
	switch c.name {
	case "exit":
		return false
	case "help":
		for _, c := range commands {
			fmt.Printf("  /%-18s %-26s %s\n", c.name, c.args, c.desc)
		}
		return true
	case "serve":
		serve()
		return true
	}
	script := c.script(args)
	if !strings.HasPrefix(script, "set -x") { // set -x scripts echo each step themselves
		fmt.Printf("\x1b[2m$ %s\x1b[0m\n", strings.ReplaceAll(script, "\n", "\n$ "))
	}
	cmd := exec.Command("sh", "-c", script)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	err := cmd.Run()
	var exit *exec.ExitError
	switch {
	case err == nil:
		fmt.Println("\x1b[32m✓ done\x1b[0m")
	case errors.As(err, &exit):
		fmt.Printf("\x1b[31m✗ exit status %d\x1b[0m\n", exit.ExitCode())
	default:
		fmt.Println("✗", err)
	}
	return true
}

// serve runs `ninelives serve` on the demo database with your typing as
// its stdin. "!kill" sends a real SIGKILL (no clean shutdown); Ctrl-D
// closes stdin, which is a clean shutdown. Recovery is up to you: /recover.
func serve() {
	if err := os.MkdirAll(demoDir, 0o755); err != nil {
		fmt.Println("✗", err)
		return
	}
	fmt.Printf("\x1b[2m$ %s serve %s\x1b[0m\n", nl, q(db))
	fmt.Println("Type requests (put k v, del k, get k, batch N + N lines, compact, stats).")
	fmt.Println("!kill = SIGKILL serve now.  Ctrl-D = close stdin (clean exit).  Then try /recover.")
	cmd := exec.Command(nl, "serve", db)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	stdin, err := cmd.StdinPipe()
	if err == nil {
		err = cmd.Start()
	}
	if err != nil {
		fmt.Println("✗", err, "(run /build first?)")
		return
	}
	for {
		line, rerr := in.ReadString('\n')
		if strings.TrimSpace(line) == "!kill" {
			cmd.Process.Signal(syscall.SIGKILL)
			break
		}
		if line != "" {
			if _, err := io.WriteString(stdin, line); err != nil {
				break // serve is gone (killed from another terminal?)
			}
		}
		if rerr != nil {
			break // Ctrl-D
		}
	}
	stdin.Close()
	err = cmd.Wait()
	if err == nil {
		fmt.Println("\x1b[32mserve exited cleanly\x1b[0m")
		return
	}
	fmt.Printf("\x1b[31mserve ended: %v\x1b[0m  (now /recover)\n", err)
}

// item is one suggestion row.
type item struct{ text, hint string }

// suggestions are the completions for the word being typed: command names
// after "/", or the current argument for commands that complete it.
func suggestions(line string) []item {
	if !strings.HasPrefix(line, "/") {
		return nil
	}
	f := strings.Fields(line)
	trailing := strings.HasSuffix(line, " ")
	if len(f) <= 1 && !trailing {
		return matchCommands(strings.TrimPrefix(line, "/"))
	}
	c := lookup(f[0])
	if c == nil || c.complete == nil {
		return nil
	}
	argIdx, word := len(f)-1, ""
	if !trailing {
		argIdx, word = len(f)-2, f[len(f)-1]
	}
	var out []item
	for _, o := range c.complete(argIdx) {
		if strings.HasPrefix(o, word) && o != word {
			out = append(out, item{text: o})
		}
	}
	return out
}

// matchCommands lists commands whose name starts with word, an exact
// match first. Only if none starts with it, those that contain it.
func matchCommands(word string) []item {
	var pre, sub []item
	for _, c := range commands {
		it := item{text: "/" + c.name, hint: strings.TrimSpace(c.args + "  " + c.desc)}
		switch {
		case c.name == word:
			pre = append([]item{it}, pre...)
		case strings.HasPrefix(c.name, word):
			pre = append(pre, it)
		case strings.Contains(c.name, word):
			sub = append(sub, it)
		}
	}
	if len(pre) > 0 {
		return pre
	}
	return sub
}

// accept replaces the word being typed with s.
func accept(line, s string) string {
	if i := strings.LastIndexByte(line, ' '); i >= 0 {
		return line[:i+1] + s + " "
	}
	return s + " "
}

// editor is the prompt line: text, the selected suggestion and history.
type editor struct {
	line    string
	sel     int
	history []string
	hpos    int
	width   int
}

// readLine edits one line until Enter. ok is false on Ctrl-D at an empty line.
func (e *editor) readLine() (line string, ok bool) {
	e.line, e.sel, e.hpos = "", 0, len(e.history)
	for {
		e.render()
		b, err := in.ReadByte()
		if err != nil {
			return "", false
		}
		sug := suggestions(e.line)
		switch b {
		case 3: // Ctrl-C
			e.line, e.sel = "", 0
		case 4: // Ctrl-D
			if e.line == "" {
				e.clearMenu()
				return "", false
			}
		case '\r', '\n':
			if len(sug) > 0 {
				e.line = accept(e.line, sug[e.sel].text)
				e.sel = 0
				c := lookup(strings.Fields(e.line)[0])
				if c == nil || len(strings.Fields(e.line)) <= c.minArgs {
					continue // still needs arguments
				}
			}
			e.clearMenu()
			if l := strings.TrimSpace(e.line); l != "" && (len(e.history) == 0 || e.history[len(e.history)-1] != l) {
				e.history = append(e.history, l)
			}
			return strings.TrimSpace(e.line), true
		case '\t':
			if len(sug) > 0 {
				e.line, e.sel = accept(e.line, sug[e.sel].text), 0
			}
		case 127, 8: // Backspace
			if e.line != "" {
				_, n := utf8.DecodeLastRuneInString(e.line)
				e.line, e.sel = e.line[:len(e.line)-n], 0
			}
		case 21: // Ctrl-U
			e.line, e.sel = "", 0
		case 27: // escape sequence: arrows
			e.arrow(sug)
		default:
			if b >= 32 {
				e.line += string(b)
				e.sel = 0
			}
		}
	}
}

// arrow handles ESC [ A/B: move in the menu if it is open, else in history.
func (e *editor) arrow(sug []item) {
	b, err := in.ReadByte()
	if err != nil || b != '[' {
		return
	}
	k, err := in.ReadByte()
	if err != nil {
		return
	}
	for k >= '0' && k <= '9' { // ESC [ 3 ~ and friends: skip
		if k, err = in.ReadByte(); err != nil {
			return
		}
	}
	up := k == 'A'
	if k != 'A' && k != 'B' {
		return
	}
	if n := min(len(sug), maxShown); n > 0 {
		if up {
			e.sel = (e.sel + n - 1) % n
		} else {
			e.sel = (e.sel + 1) % n
		}
		return
	}
	if up && e.hpos > 0 {
		e.hpos--
		e.line = e.history[e.hpos]
	} else if !up && e.hpos < len(e.history) {
		e.hpos++
		e.line = ""
		if e.hpos < len(e.history) {
			e.line = e.history[e.hpos]
		}
	}
}

// render redraws the prompt line and the menu under it, then puts the
// cursor back at the end of the line. Raw mode: newlines need \r\n.
func (e *editor) render() {
	var b strings.Builder
	b.WriteString("\r\x1b[J" + prompt + e.line)
	if e.line == "" {
		b.WriteString("\x1b[2mtype / for commands\x1b[0m\r\x1b[" + strconv.Itoa(len(prompt)) + "C")
	}
	sug := suggestions(e.line)
	if len(sug) > maxShown {
		sug = sug[:maxShown]
	}
	for i, s := range sug {
		row := fmt.Sprintf("  %-20s %s", s.text, s.hint)
		if len(row) > e.width-1 {
			row = row[:e.width-1]
		}
		if i == e.sel {
			row = "\x1b[7m" + row + "\x1b[0m"
		} else {
			row = "\x1b[2m" + row + "\x1b[0m"
		}
		b.WriteString("\r\n" + row)
	}
	if len(sug) > 0 {
		fmt.Fprintf(&b, "\x1b[%dA\r\x1b[%dC", len(sug), len(prompt)+utf8.RuneCountInString(e.line))
	}
	os.Stdout.WriteString(b.String())
}

// clearMenu wipes the menu rows before a command prints its output.
func (e *editor) clearMenu() {
	os.Stdout.WriteString("\r\x1b[J" + prompt + e.line)
}
