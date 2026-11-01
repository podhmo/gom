package diag_test

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/podhmo/gom/internal/diag"
)

var update = flag.Bool("update", false,
	"rewrite docs/error-codes.md with the rendered code table")

// TestValidateCodeTable runs the registry's own invariant check:
// well-formed codes, letter-to-severity agreement, declared category
// and phase, non-empty title/explanation, sorted order, uniqueness.
func TestValidateCodeTable(t *testing.T) {
	if err := diag.ValidateCodeTable(); err != nil {
		t.Fatal(err)
	}
}

// TestCodeTableSortedAndUnique asserts the table is strictly sorted by
// code, which also proves uniqueness.
func TestCodeTableSortedAndUnique(t *testing.T) {
	tab := diag.CodeTable()
	if len(tab) == 0 {
		t.Fatal("empty code table")
	}
	for i := 1; i < len(tab); i++ {
		if tab[i-1].Code >= tab[i].Code {
			t.Fatalf("code table not strictly sorted at %q then %q",
				tab[i-1].Code, tab[i].Code)
		}
	}
}

// TestEveryCodeWellFormed checks the LCCNN shape of every registered
// code (severity letter + two-digit category + two-digit serial).
func TestEveryCodeWellFormed(t *testing.T) {
	for _, info := range diag.CodeTable() {
		if !diag.WellFormed(info.Code) {
			t.Errorf("malformed code %q", info.Code)
		}
	}
}

// TestCodeLetterMatchesSeverity ties the code's leading letter to the
// declared severity: E = error, W = warning, N = note.
func TestCodeLetterMatchesSeverity(t *testing.T) {
	for _, info := range diag.CodeTable() {
		var want diag.Severity
		switch string(info.Code)[0] {
		case 'E':
			want = diag.SeverityError
		case 'W':
			want = diag.SeverityWarning
		case 'N':
			want = diag.SeverityNote
		default:
			t.Errorf("%s: unknown severity letter", info.Code)
			continue
		}
		if info.Severity != want {
			t.Errorf("%s: severity %v, want %v", info.Code, info.Severity, want)
		}
	}
}

// TestCodeMetadata checks that every entry carries the metadata the
// table promises: a single-line title, a non-empty explanation (needed
// by gom explain), a declared phase, and a well-formed spec reference.
func TestCodeMetadata(t *testing.T) {
	for _, info := range diag.CodeTable() {
		if info.Title == "" || strings.ContainsAny(info.Title, "\r\n") {
			t.Errorf("%s: title must be a non-empty single line", info.Code)
		}
		if strings.TrimSpace(info.Explanation) == "" {
			t.Errorf("%s: empty explanation", info.Code)
		}
		if info.Phase == "" {
			t.Errorf("%s: empty phase", info.Code)
		}
	}
}

// TestLookupCode exercises the registry lookup: a live code, a retired
// code, and an unallocated number.
func TestLookupCode(t *testing.T) {
	info, ok := diag.LookupCode("E0301")
	if !ok {
		t.Fatal("E0301 not registered")
	}
	if info.Severity != diag.SeverityError || info.Phase != diag.PhasePattern {
		t.Fatalf("E0301: severity %v phase %v", info.Severity, info.Phase)
	}
	if info.Retired {
		t.Fatal("E0301 must not be retired")
	}

	retired, ok := diag.LookupCode("E0305")
	if !ok {
		t.Fatal("retired E0305 must stay in the table")
	}
	if !retired.Retired {
		t.Fatal("E0305 should be marked retired")
	}

	if _, ok := diag.LookupCode("E9999"); ok {
		t.Fatal("E9999 should not be registered")
	}
	if _, ok := diag.LookupCode("bogus"); ok {
		t.Fatal("bogus should not be registered")
	}
}

// TestWellFormed covers the format check directly.
func TestWellFormed(t *testing.T) {
	for _, c := range []diag.Code{"E0001", "W0701", "N0001", "E9999"} {
		if !diag.WellFormed(c) {
			t.Errorf("%q should be well-formed", c)
		}
	}
	for _, c := range []diag.Code{"", "E301", "E03010", "X0301", "e0301", "E03a1"} {
		if diag.WellFormed(c) {
			t.Errorf("%q should not be well-formed", c)
		}
	}
}

// TestErrorCodesDocInSync keeps docs/error-codes.md identical to the
// rendered table. Regenerate with:
//
//	go generate ./internal/diag
//
// (or: go test ./internal/diag -run TestErrorCodesDocInSync -update)
func TestErrorCodesDocInSync(t *testing.T) {
	path := filepath.Join("..", "..", "docs", "error-codes.md")
	want := diag.RenderCodeDoc()

	if *update {
		if err := os.WriteFile(path, []byte(want), 0o644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v (run go generate ./internal/diag)", path, err)
	}
	if string(got) != want {
		t.Fatalf("%s is out of sync with internal/diag/codes.go; run go generate ./internal/diag", path)
	}
}
