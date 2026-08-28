package core

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWriteFileAtomicReplacesAndLeavesNoTemporaries(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sub", "file.conf")

	if err := WriteFileAtomic(path, []byte("first\n"), 0o644); err != nil {
		t.Fatalf("WriteFileAtomic: %v", err)
	}
	if err := WriteFileAtomic(path, []byte("second\n"), 0o600); err != nil {
		t.Fatalf("WriteFileAtomic: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "second\n" {
		t.Errorf("content = %q, want %q", data, "second\n")
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("mode = %v, want 0600", info.Mode().Perm())
	}

	// The temporary file is what makes the replacement atomic, so it has to be
	// gone afterwards — a leftover would be walked by anything scanning the
	// directory and planned as a file to delete.
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		names := make([]string, len(entries))
		for i, e := range entries {
			names[i] = e.Name()
		}
		t.Errorf("temporary files left behind: %v", names)
	}
}
