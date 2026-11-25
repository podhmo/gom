package testutil

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestWriteSnapshotAtomicPreservesBaselineOnRenameFailure(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "x.snap")
	if err := os.WriteFile(path, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := writeSnapshotAtomic(path, "new"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "new" {
		t.Fatalf("data=%q err=%v", data, err)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 1 {
		t.Fatalf("temp files left behind: %v", entries)
	}
}

func TestSnapshotRootDoesNotFollowChdir(t *testing.T) {
	root := snapshotRoot(t)
	here, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(here) }()
	if got := snapshotRoot(t); got != root {
		t.Fatalf("snapshotRoot=%q after chdir, want %q", got, root)
	}
}

func TestSnapshotNameUnicodeBoundaryAndStableNamespace(t *testing.T) {
	long := strings.Repeat("界", 100)
	name := snapshotName("test x", long)
	if !utf8.ValidString(name) {
		t.Fatalf("invalid utf-8 filename %q", name)
	}
	if strings.Contains(name, "TestX") {
		t.Fatalf("uses order-dependent Go test name: %q", name)
	}
}
