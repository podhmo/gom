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

func TestScratchModulePinsAndRemovesFiles(t *testing.T) {
	if testing.Short() {
		t.Skip("runs go mod download")
	}
	haveGo(t)
	dir := t.TempDir()
	cleanup, err := ensureScratchModule(dir)
	if err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(filepath.Join(dir, "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	firstTime := fi.ModTime()
	cleanup()
	for _, name := range []string{"go.mod", "go.sum"} {
		if _, err := os.Stat(filepath.Join(dir, name)); !os.IsNotExist(err) {
			t.Fatalf("%s remained: %v", name, err)
		}
	}

	cleanup, err = ensureScratchModule(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	fi, err = os.Stat(filepath.Join(dir, "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	if !fi.ModTime().Equal(firstTime) {
		t.Fatalf("same scratch content changed mtime: first %v, second %v", firstTime, fi.ModTime())
	}
	if _, err := os.Stat(filepath.Join(dir, "go.work")); !os.IsNotExist(err) {
		t.Fatalf("scratch go.work exists: %v", err)
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
