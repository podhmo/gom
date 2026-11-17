package main

import (
	"path/filepath"
	"strings"
	"testing"
)

// TestBuildGoFlagSilent: `gom build -go` on a package the real Go
// toolchain accepts writes its .go and reports no go-source
// diagnostics — the opt-in pass is silent when the tool is happy.
func TestBuildGoFlagSilent(t *testing.T) {
	if testing.Short() {
		t.Skipf("long test: go build invocation, skipping in short mode")
	}
	t.Parallel()

	haveGo(t)
	dir := writeFiles(t, map[string]string{
		"m.gom": "package main\n\nfn f () -> Int = 1\nfn main () -> Unit = { ignore (f ()); () }\n",
	})
	testModule(t, dir)

	code, out, errOut := invoke("build", "-go", filepath.Join(dir, "m.gom"))
	if code != 0 {
		t.Fatalf("exit = %d, want 0; out=%q err=%q", code, out, errOut)
	}
	for _, l := range jsonLines(t, out) {
		if l["kind"] == "diagnostic" {
			t.Fatalf("unexpected diagnostic on clean package: %v", l)
		}
	}
}

// TestBuildGoFlagFoldsError: a package that gom checks clean but
// `go build` rejects — via a sibling Go file whose //line directive
// names the .gom input — folds the tool's diagnostic back onto the
// .gom span (SourceGo, tool="go build"), the same fold `gom test`'s
// build-output events take.
func TestBuildGoFlagFoldsError(t *testing.T) {
	if testing.Short() {
		t.Skipf("long test: go build invocation, skipping in short mode")
	}
	t.Parallel()

	haveGo(t)
	dir := writeFiles(t, map[string]string{
		"m.gom": "package main\n\nfn f () -> Int = 1\nfn main () -> Unit = { ignore (f ()); () }\n",
		// The //line directive makes go build report the error on the
		// .gom path — the verbatim-position half of design.md 2.2 that
		// generated files exercise through their own directives.
		"z.go": "package main\n\n//line m.gom:2\nvar _ int = \"not-an-int\"\n",
	})
	testModule(t, dir)

	code, out, _ := invoke("build", "-go", filepath.Join(dir, "m.gom"))
	if code == 0 {
		t.Fatalf("exit = %d, want failure; out=%q", code, out)
	}
	var diags []map[string]any
	for _, l := range jsonLines(t, out) {
		if l["kind"] == "diagnostic" {
			diags = append(diags, l)
		}
	}
	if len(diags) == 0 {
		t.Fatalf("go build failure produced no diagnostics: %q", out)
	}
	d := diags[0]
	if d["source"] != "go" {
		t.Errorf("diagnostic source = %v, want go", d["source"])
	}
	if !strings.Contains(d["message"].(string), "not-an-int") &&
		!strings.Contains(d["message"].(string), "cannot use") {
		t.Errorf("diagnostic message = %v", d["message"])
	}
	span, ok := d["span"].(map[string]any)
	if !ok {
		t.Fatalf("diagnostic carries no span: %v", d)
	}
	if file, _ := span["file"].(string); !strings.HasSuffix(file, "m.gom") {
		t.Errorf("span file = %v, want the .gom input", span["file"])
	}
}

// TestBuildGoFlagOff: without -go the same directory builds clean —
// the hand-written sibling is invisible to the gom pipeline, and no
// go tool runs at all. The flag is what invokes the tool.
func TestBuildGoFlagOff(t *testing.T) {
	t.Parallel()

	dir := writeFiles(t, map[string]string{
		"m.gom": "package main\n\nfn f () -> Int = 1\n",
		"z.go":  "package main\n\nvar _ int = \"not-an-int\"\n",
	})
	testModule(t, dir)

	code, out, _ := invoke("build", filepath.Join(dir, "m.gom"))
	if code != 0 {
		t.Fatalf("exit = %d, want 0; out=%q", code, out)
	}
	for _, l := range jsonLines(t, out) {
		if l["kind"] == "diagnostic" {
			t.Fatalf("unexpected diagnostic without -go: %v", l)
		}
	}
}
