// File-system helpers shared by create, recovery and compaction: directory
// fsync and temp-file names (<path>.init, <path>.compact).
package engine

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
)

// Temp files next to the database. Neither is ever authoritative: Open
// deletes both before it looks at <path>.
func initPath(path string) string    { return path + ".init" }
func compactPath(path string) string { return path + ".compact" }

// syncDir fsyncs the directory holding path, so a rename or create inside
// it survives power loss, not just a process kill.
func syncDir(path string) error {
	d, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}

// removeIfExists deletes path; a missing file is not an error.
func removeIfExists(path string) error {
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}
