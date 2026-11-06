// Crash-point tests (PR A5): for every SPEC section 8 crash point, run a
// scripted workload in a child process with NINELIVES_CRASH_AT set, let it
// SIGKILL itself, then reopen in the parent and check that the state is
// the last acked step or the step that was in flight, and that Check is OK.
package engine_test

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"ninelives/src/engine"
)

// crashStep is one step of the child's workload and its effect on the state.
type crashStep struct {
	run   func(db *engine.DB) error
	apply func(m map[string]string)
}

var crashWorkload = []crashStep{
	{
		func(db *engine.DB) error { _, err := db.Put([]byte("k1"), []byte("v1")); return err },
		func(m map[string]string) { m["k1"] = "v1" },
	},
	{
		func(db *engine.DB) error {
			_, err := db.Batch([]engine.Op{put("k2", "v2"), put("k3", "v3"), put("k1", "v1b")})
			return err
		},
		func(m map[string]string) { m["k2"], m["k3"], m["k1"] = "v2", "v3", "v1b" },
	},
	{
		func(db *engine.DB) error { _, err := db.Delete([]byte("k1")); return err },
		func(m map[string]string) { delete(m, "k1") },
	},
	{
		func(db *engine.DB) error { return db.Compact() },
		func(map[string]string) {},
	},
	{
		func(db *engine.DB) error { _, err := db.Put([]byte("k4"), []byte("v4")); return err },
		func(m map[string]string) { m["k4"] = "v4" },
	},
}

// startFile is the file every child opens: two committed records and half
// of a third, so the child's open() always has a torn tail to cut.
func startFile(t *testing.T) (path string, base map[string]string) {
	torn := engine.EncodePut(3, []byte("r3"), []byte("lost"))
	path = writeLog(t, 1,
		engine.EncodePut(1, []byte("r1"), []byte("a")),
		engine.EncodePut(2, []byte("r2"), []byte("b")),
		torn[:len(torn)/2],
	)
	return path, map[string]string{"r1": "a", "r2": "b"}
}

// TestCrashChild is the child process. It does nothing in a normal run.
func TestCrashChild(t *testing.T) {
	path := os.Getenv("NL_CHILD_PATH")
	if path == "" {
		t.Skip("only runs as the child of TestCrashPoints")
	}
	db, err := engine.Open(path, engine.Options{})
	if err != nil {
		fmt.Println("OPEN-ERROR", err)
		os.Exit(3)
	}
	for i, s := range crashWorkload {
		if err := s.run(db); err != nil {
			fmt.Println("STEP-ERROR", i, err)
			os.Exit(3)
		}
		fmt.Printf("ACK %d\n", i) // like serve: printed only after success
	}
	fmt.Println("DONE")
}

func TestCrashPoints(t *testing.T) {
	if testing.Short() {
		t.Skip("spawns child processes")
	}
	cases := []struct {
		name  string
		after string // NINELIVES_CRASH_AFTER
	}{
		{"before_write", ""},
		{"mid_write", ""},
		{"after_write", ""},
		{"after_write", "2"}, // batch written, not fsynced, step 1 (B3)
		{"after_fsync", ""},
		{"after_fsync", "3"}, // dies inside the delete, step 2
		{"after_index", ""},
		{"batch_mid_write", ""},
		{"recovery_mid_scan", ""},
		{"recovery_mid_scan", "2"},
		{"recovery_before_truncate", ""},
		{"recovery_after_truncate", ""},
		{"compact_mid_temp", ""},
		{"compact_after_temp_fsync", ""},
		{"compact_after_rename", ""},
		{"compact_after_dir_fsync", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name+"/"+tc.after, func(t *testing.T) {
			path, base := startFile(t)
			acked := runCrashChild(t, path, tc.name, tc.after)
			t.Logf("child acked %d of %d steps before the kill", acked, len(crashWorkload))

			// Allowed: every acked step applied, plus maybe the in-flight one.
			want := map[string]string{}
			for k, v := range base {
				want[k] = v
			}
			for i := 0; i < acked; i++ {
				crashWorkload[i].apply(want)
			}
			withInFlight := map[string]string{}
			for k, v := range want {
				withInFlight[k] = v
			}
			if acked < len(crashWorkload) {
				crashWorkload[acked].apply(withInFlight)
			}

			db := mustOpen(t, path)
			got := state(t, db)
			if !sameState(got, want) && !sameState(got, withInFlight) {
				t.Fatalf("after kill at %s (acked %d steps): state %v, want %v or %v", tc.name, acked, got, want, withInFlight)
			}
			if err := engine.Check(path); err != nil {
				t.Fatalf("Check after reopen: %v", err)
			}
		})
	}
}

// runCrashChild runs the workload in a child with the crash point armed,
// requires that it died by SIGKILL, and returns how many steps it acked.
func runCrashChild(t *testing.T, path, name, after string) int {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^TestCrashChild$", "-test.count=1")
	cmd.Env = append(os.Environ(), "NL_CHILD_PATH="+path, "NINELIVES_CRASH_AT="+name, "NINELIVES_CRASH_AFTER="+after)
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	err := cmd.Run()

	var exit *exec.ExitError
	if !errors.As(err, &exit) {
		t.Fatalf("child was not killed (err %v); crash point %s never hit?\n%s", err, name, out.String())
	}
	if ws, ok := exit.Sys().(syscall.WaitStatus); !ok || !ws.Signaled() || ws.Signal() != syscall.SIGKILL {
		t.Fatalf("child ended with %v, want SIGKILL\n%s", err, out.String())
	}
	acked := 0
	sc := bufio.NewScanner(&out)
	for sc.Scan() {
		if strings.HasPrefix(sc.Text(), "ACK ") {
			acked++
		}
	}
	return acked
}

func sameState(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if bv, ok := b[k]; !ok || bv != v {
			return false
		}
	}
	return true
}

// Crash points must cost nothing when unarmed: this runs in the normal
// test process, where NINELIVES_CRASH_AT is unset, and must simply work.
func TestCrashPointsUnarmed(t *testing.T) {
	if os.Getenv("NINELIVES_CRASH_AT") != "" {
		t.Skip("armed environment")
	}
	path := filepath.Join(t.TempDir(), "unarmed.db")
	db := mustOpen(t, path)
	for _, s := range crashWorkload {
		if err := s.run(db); err != nil {
			t.Fatal(err)
		}
	}
}
