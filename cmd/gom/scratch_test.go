package main

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestModPathLiteralQuotesWhitespace(t *testing.T) {
	t.Parallel()

	got := modPathLiteral("/tmp/gom source")
	if got != "\"/tmp/gom source\"" {
		t.Fatalf("modPathLiteral = %q", got)
	}
}

func TestScratchModulePinsAndRemovesWorkspaceFiles(t *testing.T) {
	if testing.Short() {
		t.Skip("runs go mod download")
	}
	haveGo(t)
	dir := t.TempDir()
	cleanup, err := ensureScratchModule(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"go.mod", "go.work"} {
		fi, err := os.Stat(filepath.Join(dir, name))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if !fi.ModTime().Equal(scratchFileTime) {
			t.Fatalf("%s mtime = %v, want pinned %v", name, fi.ModTime(), scratchFileTime)
		}
	}
	cleanup()
	for _, name := range []string{"go.mod", "go.work", "go.sum", "go.work.sum"} {
		if _, err := os.Stat(filepath.Join(dir, name)); !os.IsNotExist(err) {
			t.Fatalf("%s remained: %v", name, err)
		}
	}
}

func TestModuleFileQuotesReplacePath(t *testing.T) {
	root := filepath.Join(t.TempDir(), "gom source")
	if err := os.Symlink(mustRepoRoot(t), root); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GOM_TOOLCHAIN_ROOT", root)
	body, err := moduleFile(scratchModulePath)
	if err != nil {
		t.Fatal(err)
	}
	want := "=> " + strconv.Quote(root)
	if !strings.Contains(string(body), want) {
		t.Fatalf("replace path is not quoted as %q: %q", want, body)
	}
}

func mustRepoRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	return root
}
