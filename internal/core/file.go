package core

import (
	"io/fs"
	"os"
	"path/filepath"
)

// WriteFileAtomic replaces a file in one step, so a reader never sees a
// half-written file (design.md §3.3, atomic file replacement).
//
// Two readers depend on this and neither of them is us. The daemon re-reads a
// hosts directory whenever it feels like it, and an operator recovering a
// broken box reads the intent file by hand — a truncated config would be the
// worst thing either could find. It lives in core rather than in a module
// because every module that renders a file needs exactly this, and a second
// copy would eventually lose the Sync.
func WriteFileAtomic(path string, data []byte, mode fs.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	// The temporary file is created in the destination directory so the rename
	// below stays within one filesystem, and named with a leading dot so that
	// anything walking the directory can recognise and skip it.
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // no-op once the rename succeeds

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	// Durability before visibility: a rename that survives a crash pointing at
	// unflushed content would leave an empty config where a valid one was.
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpName, mode); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}
