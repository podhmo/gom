package diag_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/podhmo/gom/internal/diag"
)

func TestMapResolver(t *testing.T) {
	res := diag.NewMapResolver(map[string][]byte{
		"a.gom": []byte("ab\ncd\n"),
	})

	li, err := res.LineIndex("a.gom")
	if err != nil {
		t.Fatalf("LineIndex: %v", err)
	}
	if got := li.Pos(3); got.Line != 2 || got.Col != 1 {
		t.Errorf("Pos(3) = %+v, want line 2 col 1", got)
	}

	// Indexes are cached: a second call returns the same index.
	again, err := res.LineIndex("a.gom")
	if err != nil {
		t.Fatalf("LineIndex again: %v", err)
	}
	if again != li {
		t.Error("LineIndex not cached: got a different index")
	}

	if _, err := res.LineIndex("missing.gom"); err == nil {
		t.Error("LineIndex(missing.gom) = nil error, want error")
	}
}

func TestDirResolver(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "sub")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub, "f.gom"), []byte("ab\ncd\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	res := diag.NewDirResolver(dir)
	li, err := res.LineIndex("sub/f.gom")
	if err != nil {
		t.Fatalf("LineIndex: %v", err)
	}
	if got := li.Pos(4); got.Line != 2 || got.Col != 2 {
		t.Errorf("Pos(4) = %+v, want line 2 col 2", got)
	}

	// Missing files fail, and the failure is memoized.
	err1 := func() error { _, err := res.LineIndex("none.gom"); return err }()
	err2 := func() error { _, err := res.LineIndex("none.gom"); return err }()
	if err1 == nil || err2 == nil {
		t.Fatal("LineIndex(none.gom) should fail twice")
	}
	if err1 != err2 {
		t.Error("failed lookups should be memoized: errors differ")
	}
}

func TestResolveSpan(t *testing.T) {
	res := diag.NewMapResolver(map[string][]byte{"f.gom": []byte("ab\ncd\nef\n")})
	rp, err := diag.ResolveSpan(res, diag.SpanIn("f.gom", 3, 6))
	if err != nil {
		t.Fatalf("ResolveSpan: %v", err)
	}
	want := diag.SpanPos{
		File:  "f.gom",
		Start: diag.Pos{Line: 2, Col: 1, Offset: 3},
		End:   diag.Pos{Line: 3, Col: 1, Offset: 6},
	}
	if rp != want {
		t.Errorf("ResolveSpan = %+v, want %+v", rp, want)
	}
}

func TestResolveSpanNilResolver(t *testing.T) {
	// A nil resolver reports an error rather than panicking; emitters
	// rely on this when no sources are supplied.
	if _, err := diag.ResolveSpan(nil, diag.SpanIn("f.gom", 0, 1)); err == nil {
		t.Error("ResolveSpan(nil, ...) = nil error, want error")
	}
}

func TestResolverFunc(t *testing.T) {
	var calls []string
	res := diag.ResolverFunc(func(file string) (*diag.LineIndex, error) {
		calls = append(calls, file)
		return diag.NewLineIndex([]byte("x\n")), nil
	})
	if _, err := res.LineIndex("a.gom"); err != nil {
		t.Fatal(err)
	}
	if len(calls) != 1 || calls[0] != "a.gom" {
		t.Errorf("calls = %v, want [a.gom]", calls)
	}
}
