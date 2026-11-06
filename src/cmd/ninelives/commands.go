// CLI commands open/put/get/del/batch/dump/check/compact, --max-bytes,
// output lines and exit codes (docs/SPEC.md section 11). PR K1.
package main

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	"ninelives/src/engine"
)

// Exit codes (SPEC section 11): success 0, any error 1, usage error 2,
// disk full 3.
const (
	exitOK       = 0
	exitError    = 1
	exitUsage    = 2
	exitDiskFull = 3
)

// errUsage marks a malformed invocation: exit code 2 and the usage line.
// The command has not touching the database.
var errUsage = errors.New("usage error")

// usageError carries a clean message while still unwrapping to errUsage, so
// nested usage errors (a bad token inside a batch file) print one readable
// line instead of "usage error: ... usage error: ...".
type usageError struct{ msg string }

func (e usageError) Error() string { return e.msg }
func (e usageError) Unwrap() error { return errUsage }

// usagef builds a usage error from a format string.
func usagef(format string, a ...any) error {
	return usageError{msg: fmt.Sprintf(format, a...)}
}

// errReported marks a failure the command already printed itself
// (check's "CORRUPT: <reason>"): exit code 1, no second line on stderr.
var errReported = errors.New("already reported")

// parsed is the shared flag-parsing result: positional arguments and the
// optional --max-bytes N cap that every command accepts (SPEC section 11).
type parsed struct {
	args     []string
	maxBytes int64
}

// run executes one CLI invocation and returns the process exit code.
func run(argv []string) int {
	cmd := ""
	var p parsed
	var err error

	if len(argv) == 0 {
		err = usagef("no command given")
	} else if n, ok := commands[argv[0]]; !ok {
		err = usagef("unknown command %q", argv[0])
	} else {
		cmd = argv[0]
		if p, err = parseFlags(argv[1:]); err == nil {
			err = dispatch(cmd, p, n)
		}
	}
	if err == nil {
		return exitOK
	}

	if !errors.Is(err, errReported) {
		where := "ninelives"
		if cmd != "" {
			where += " " + cmd
		}
		fmt.Fprintf(os.Stderr, "%s: %v\n", where, err)
	}
	switch {
	case errors.Is(err, errUsage):
		fmt.Fprintln(os.Stderr, usage)
		return exitUsage
	case errors.Is(err, engine.ErrDiskFull):
		return exitDiskFull
	default:
		return exitError
	}
}

// parseFlags separates --max-bytes N (space or "=" form) from the positional
// arguments. Any other "--..." flag is a usage error.
func parseFlags(args []string) (parsed, error) {
	var p parsed
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--max-bytes" || strings.HasPrefix(a, "--max-bytes="):
			val := a[len("--max-bytes"):]
			if strings.HasPrefix(val, "=") {
				val = val[1:]
			} else {
				i++
				if i >= len(args) {
					return p, usagef("--max-bytes needs a value")
				}
				val = args[i]
			}
			n, err := strconv.ParseInt(val, 10, 64)
			if err != nil || n < 0 {
				return p, usagef("--max-bytes must be a non-negative integer, got %q", val)
			}
			p.maxBytes = n
		case strings.HasPrefix(a, "--"):
			return p, usagef("unknown flag %q", a)
		default:
			p.args = append(p.args, a)
		}
	}
	return p, nil
}

// dispatch runs the command after checking positional arity. Every command
// takes the database path first.
func dispatch(cmd string, p parsed, want int) error {
	if len(p.args) != want {
		return usagef("expected %d arguments, got %d", want, len(p.args))
	}
	path := p.args[0]
	switch cmd {
	case "open":
		return cmdOpen(path, p.maxBytes)
	case "put":
		return cmdPut(path, p.args[1], p.args[2], p.maxBytes)
	case "get":
		return cmdGet(path, p.args[1], p.maxBytes)
	case "del":
		return cmdDel(path, p.args[1], p.maxBytes)
	case "batch":
		return cmdBatch(path, p.args[1], p.maxBytes)
	case "dump":
		return cmdDump(path, p.maxBytes)
	case "check":
		return cmdCheck(path)
	case "compact":
		return cmdCompact(path, p.maxBytes)
	case "serve":
		return cmdServe(path, p.maxBytes)
	}
	return usagef("unknown command %q", cmd)
}

// withDB opens the database (running recovery), runs fn, then closes it.
// On the success path a Close error is reported; a failing fn wins.
func withDB(path string, maxBytes int64, fn func(*engine.DB) error) error {
	db, err := engine.Open(path, engine.Options{MaxBytes: maxBytes})
	if err != nil {
		return err
	}
	if err := fn(db); err != nil {
		db.Close()
		return err
	}
	return db.Close()
}

// cmdOpen opens or creates the database and prints the SPEC section 11 line:
// keys=<n> recovery_ms=<ms>.
func cmdOpen(path string, maxBytes int64) error {
	return withDB(path, maxBytes, func(db *engine.DB) error {
		s := db.Stats()
		fmt.Printf("keys=%d recovery_ms=%d\n", s.Keys, s.RecoveryTime.Milliseconds())
		return nil
	})
}

// cmdPut stores a value: OK seq=<n>, only after the record is fsynced.
func cmdPut(path, keyTok, valTok string, maxBytes int64) error {
	key, err := decodeToken(keyTok)
	if err != nil {
		return err
	}
	val, err := decodeToken(valTok)
	if err != nil {
		return err
	}
	return withDB(path, maxBytes, func(db *engine.DB) error {
		seq, err := db.Put(key, val)
		if err != nil {
			return err
		}
		fmt.Printf("OK seq=%d\n", seq)
		return nil
	})
}

// cmdGet prints the value token, or NOTFOUND (exit code 0 in both cases).
func cmdGet(path, keyTok string, maxBytes int64) error {
	key, err := decodeToken(keyTok)
	if err != nil {
		return err
	}
	return withDB(path, maxBytes, func(db *engine.DB) error {
		val, found, err := db.Get(key)
		if err != nil {
			return err
		}
		if !found {
			fmt.Println("NOTFOUND")
			return nil
		}
		fmt.Println(encodeToken(val))
		return nil
	})
}

// cmdDel deletes a key and prints OK seq=<n>. Deleting a missing key still
// writes a record, so it still prints OK.
func cmdDel(path, keyTok string, maxBytes int64) error {
	key, err := decodeToken(keyTok)
	if err != nil {
		return err
	}
	return withDB(path, maxBytes, func(db *engine.DB) error {
		seq, err := db.Delete(key)
		if err != nil {
			return err
		}
		fmt.Printf("OK seq=%d\n", seq)
		return nil
	})
}

// cmdBatch reads "put k v" / "del k" lines from a file and applies them as
// one atomic batch: OK seq=<n>.
func cmdBatch(path, file string, maxBytes int64) error {
	ops, err := parseBatchFile(file) // before opening: a bad file writes nothing
	if err != nil {
		return err
	}
	return withDB(path, maxBytes, func(db *engine.DB) error {
		seq, err := db.Batch(ops)
		if err != nil {
			return err
		}
		fmt.Printf("OK seq=%d\n", seq)
		return nil
	})
}

// parseBatchFile turns one op per line ("put k v" or "del k", PROBLEM.md
// section 6) into engine ops. Tokens follow SPEC section 10. Blank lines and
// #-comments are ignored; anything else is a usage error naming the line.
func parseBatchFile(name string) ([]engine.Op, error) {
	data, err := os.ReadFile(name)
	if err != nil {
		return nil, err
	}
	var ops []engine.Op
	for i, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSuffix(line, "\r")
		fields := strings.Fields(line)
		if len(fields) == 0 || strings.HasPrefix(fields[0], "#") {
			continue
		}
		bad := func(format string, a ...any) error {
			return usagef("%s:%d: %s", name, i+1, fmt.Sprintf(format, a...))
		}
		switch {
		case fields[0] == "put" && len(fields) == 3:
			key, err := decodeToken(fields[1])
			if err != nil {
				return nil, bad("bad key: %v", err)
			}
			val, err := decodeToken(fields[2])
			if err != nil {
				return nil, bad("bad value: %v", err)
			}
			ops = append(ops, engine.Op{Kind: engine.OpPut, Key: key, Value: val})
		case fields[0] == "del" && len(fields) == 2:
			key, err := decodeToken(fields[1])
			if err != nil {
				return nil, bad("bad key: %v", err)
			}
			ops = append(ops, engine.Op{Kind: engine.OpDel, Key: key})
		default:
			return nil, bad("want \"put <key> <value>\" or \"del <key>\", got %q", snippet(line))
		}
	}
	return ops, nil
}

// cmdDump prints "<key-token> <value-token>" per line, sorted by raw key
// bytes (ForEachSorted guarantees the order).
func cmdDump(path string, maxBytes int64) error {
	return withDB(path, maxBytes, func(db *engine.DB) error {
		return db.ForEachSorted(func(key, val []byte) error {
			fmt.Printf("%s %s\n", encodeToken(key), encodeToken(val))
			return nil
		})
	})
}

// cmdCheck is read-only. OK (exit 0) or "CORRUPT: <reason>" (exit 1),
// printed on stdout because that is the command's documented output.
func cmdCheck(path string) error {
	if err := engine.Check(path); err != nil {
		fmt.Printf("CORRUPT: %v\n", err)
		return fmt.Errorf("%w: %v", errReported, err)
	}
	fmt.Println("OK")
	return nil
}

// cmdCompact rewrites the log keeping only live keys (SPEC section 7) and
// prints OK.
func cmdCompact(path string, maxBytes int64) error {
	return withDB(path, maxBytes, func(db *engine.DB) error {
		if err := db.Compact(); err != nil {
			return err
		}
		fmt.Println("OK")
		return nil
	})
}
