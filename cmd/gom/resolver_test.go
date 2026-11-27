package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/podhmo/gom/internal/diag"
)

func TestDiskFallbackResolver(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "dep.gomi")
	if err := os.WriteFile(path, []byte("ab\ncd\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	res := newDiskFallbackResolver(diag.NewMapResolver(nil))
	li, err := res.LineIndex(path)
	if err != nil {
		t.Fatalf("LineIndex: %v", err)
	}
	if got := li.Pos(4); got.Line != 2 || got.Col != 2 {
		t.Errorf("Pos(4) = %+v, want line 2 col 2", got)
	}

	if _, err := res.LineIndex(filepath.Join(dir, "missing.gomi")); err == nil {
		t.Error("LineIndex(missing) = nil error, want error")
	}
}

func TestDiskFallbackResolverUsesCompileTimeGomiBytes(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "dep.gomi")
	if err := os.WriteFile(path, []byte("ab\ncd\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	noteLoadedGomiSource(path, []byte("ab\ncd\n"))
	defer loadedGomiSources.Delete(path)
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}

	res := newDiskFallbackResolver(diag.NewMapResolver(nil))
	li, err := res.LineIndex(path)
	if err != nil {
		t.Fatalf("LineIndex: %v", err)
	}
	if got := li.Pos(4); got.Line != 2 || got.Col != 2 {
		t.Errorf("Pos(4) = %+v, want line 2 col 2", got)
	}
}
