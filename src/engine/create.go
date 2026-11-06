// New-file creation (docs/SPEC.md section 5): header to <path>.init,
// fsync, rename to <path>, fsync the directory. So <path> either does not
// exist or has a complete, fsynced header.
package engine

import "os"

// firstSeq is the SeqBase of a brand-new file: the first record gets seq 1.
const firstSeq = 1

// createFile makes an empty database file at path (header only).
func createFile(path string) error {
	tmp := initPath(path)
	// CRASH: before or during create/write: only <path>.init exists (empty or
	// partial). <path> does not exist. open() deletes .init and starts over.
	f, err := os.OpenFile(tmp, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	if _, err := f.Write(EncodeFileHeader(firstSeq)); err != nil {
		f.Close()
		return err
	}
	// CRASH: before Sync: .init holds the header in the page cache only.
	// <path> still does not exist; open() deletes .init and starts over.
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	// CRASH: before rename: .init is complete but <path> does not exist.
	// open() deletes .init and creates again. Nothing was ever acked.
	if err := os.Rename(tmp, path); err != nil {
		return err
	}
	// CRASH: after rename, before dir fsync: <path> has a complete header
	// (fsynced above). After a kill the rename is visible, so open() uses it.
	// Only power loss could undo the rename, and then open() recreates it.
	return syncDir(path)
}
