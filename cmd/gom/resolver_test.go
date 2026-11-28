package main

import (
	"bytes"
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
	res := newDiskFallbackResolver(diag.NewMapResolver(nil), nil)
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
	cache := newGomiSourceCache()
	cache.note(path, []byte("ab\ncd\n"))
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}

	res := newDiskFallbackResolver(diag.NewMapResolver(nil), cache)
	li, err := res.LineIndex(path)
	if err != nil {
		t.Fatalf("LineIndex: %v", err)
	}
	if got := li.Pos(4); got.Line != 2 || got.Col != 2 {
		t.Errorf("Pos(4) = %+v, want line 2 col 2", got)
	}
}

func TestEmitReportBorrowsGomiSourceCache(t *testing.T) {
	path := filepath.Join(t.TempDir(), "dep.gomi")
	cache := newGomiSourceCache()
	cache.note(path, []byte("package dep\n"))

	var out bytes.Buffer
	if err := emitReport(&out, "json", diag.Report{}, nil, cache); err != nil {
		t.Fatalf("emitReport: %v", err)
	}
	if _, ok := cache.load(path); !ok {
		t.Error("emitReport consumed the caller-owned .gomi cache")
	}
	cache.clear()
	if _, ok := cache.load(path); ok {
		t.Error("clear kept loaded .gomi bytes")
	}
}
