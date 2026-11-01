package gomtest

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGoldenEqual(t *testing.T) {
	for _, tt := range []struct {
		name string
		want string
		got  string
		eq   bool
	}{
		{"identical", "package x\n", "package x\n", true},
		{"trailing newline ignored", "package x\n", "package x", true},
		{"extra trailing blank lines ignored", "package x\n\n\n", "package x\n", true},
		{"crlf normalized", "package x\r\n", "package x\n", true},
		{"content differs", "package x\n", "package y\n", false},
		{"inner whitespace differs", "func  f() {}\n", "func f() {}\n", false},
		{"empty both", "", "", true},
		{"empty vs newline", "", "\n", true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if eq := goldenEqual([]byte(tt.want), []byte(tt.got)); eq != tt.eq {
				t.Errorf("goldenEqual(%q, %q) = %v, want %v", tt.want, tt.got, eq, tt.eq)
			}
		})
	}
}

func TestSimpleDiff(t *testing.T) {
	want := "package main\n\nfunc main() {\n\tprintln(\"a\")\n}\n"
	got := "package main\n\nfunc main() {\n\tprintln(\"b\")\n}\n"
	d := simpleDiff(want, got)
	for _, part := range []string{
		"--- expected", "+++ actual", "@@",
		`  func main() {`, `- 	println("a")`, `+ 	println("b")`,
	} {
		if !strings.Contains(d, part) {
			t.Errorf("diff missing %q:\n%s", part, d)
		}
	}
}

func TestWriteGolden(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "x.golden.go")

	// The written file always ends with exactly one newline so it
	// stays gofmt-clean.
	writeGolden(t, path, []byte("package x\n\n\n"))
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "package x\n" {
		t.Errorf("written = %q, want %q", data, "package x\\n")
	}
}

// TestGoldenUpdate exercises -update end to end: a missing golden file
// is created from the actual output and a subsequent run compares
// clean.
func TestGoldenUpdate(t *testing.T) {
	old := *update
	*update = true
	defer func() { *update = old }()

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "g.gom"), []byte("package g\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gen := func(f File) ([]byte, error) {
		return []byte("package main\n\nfunc g() {}\n"), nil
	}
	RunGoldenTests(t, dir, gen)
	data, err := os.ReadFile(filepath.Join(dir, "g.golden.go"))
	if err != nil {
		t.Fatalf("-update did not write the golden file: %v", err)
	}
	if string(data) != "package main\n\nfunc g() {}\n" {
		t.Errorf("golden = %q", data)
	}
}
