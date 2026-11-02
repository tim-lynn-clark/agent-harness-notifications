//go:build windows

package state

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// A reader holding the target open blocks the rename until it closes the file.
func TestWriteFileAtomicWaitsForABriefReader(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.json")
	if err := os.WriteFile(path, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		time.Sleep(100 * time.Millisecond)
		f.Close()
	}()
	if err := WriteFileAtomic(path, []byte("new")); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(path); string(got) != "new" {
		t.Fatalf("got %q", got)
	}
}

// A reader that outlasts the retries makes the write fail without leaving its
// temporary file behind.
func TestWriteFileAtomicGivesUpWithoutLeavingATempFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "s.json")
	if err := os.WriteFile(path, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := WriteFileAtomic(path, []byte("new")); err == nil {
		t.Fatal("want an error while the file is held open")
	}
	if tmps, _ := filepath.Glob(filepath.Join(dir, ".tmp-*")); len(tmps) != 0 {
		t.Fatalf("left %v", tmps)
	}
}
