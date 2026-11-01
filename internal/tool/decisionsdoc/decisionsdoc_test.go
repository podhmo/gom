package decisionsdoc_test

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/podhmo/gom/internal/tool/decisionsdoc"
)

var update = flag.Bool("update", false,
	"rewrite docs/decisions/README.md with the rendered index table")

// TestDecisionsReadmeInSync keeps docs/decisions/README.md identical to
// the table rendered from the decision files' frontmatter. Regenerate
// with:
//
//	go generate ./internal/tool/decisionsdoc
//
// (or: go test ./internal/tool/decisionsdoc -run TestDecisionsReadmeInSync -update)
func TestDecisionsReadmeInSync(t *testing.T) {
	dir := filepath.Join("..", "..", "..", "docs", "decisions")
	path := filepath.Join(dir, "README.md")
	want, err := decisionsdoc.RenderDir(dir)
	if err != nil {
		t.Fatal(err)
	}

	if *update {
		if err := os.WriteFile(path, []byte(want), 0o644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v (run go generate ./internal/tool/decisionsdoc)", path, err)
	}
	if string(got) != want {
		t.Fatalf("%s is out of sync with docs/decisions/*.md; run go generate ./internal/tool/decisionsdoc", path)
	}
}

func writeDecision(t *testing.T, dir, name, src string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
}

const validDecision = `# ラベル付き必須引数

- 状態: 採用
- 日付: 2026-11-01

## 決定
`

// TestScanDir covers the happy path: files sort by date then slug,
// non-decision files are ignored, and Supersedes links become
// Supersession entries.
func TestScanDir(t *testing.T) {
	dir := t.TempDir()
	writeDecision(t, dir, "2026-11-01-b-thing.md", `# B

- 状態: draft
- 日付: 2026-11-01
- Supersedes: [a-thing](2026-11-01-a-thing.md) のうち一部
`)
	writeDecision(t, dir, "2026-11-01-a-thing.md", validDecision)
	writeDecision(t, dir, "2026-11-02-c-thing.md", `# C

- 状態: 採用（実験で決定）
- 日付: 2026-11-02
`)
	writeDecision(t, dir, "README.md", "# index\n")
	writeDecision(t, dir, "notes.md", "# not a decision file\n")

	ds, err := decisionsdoc.ScanDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(ds) != 3 {
		t.Fatalf("got %d decisions, want 3", len(ds))
	}
	if ds[0].Slug != "a-thing" || ds[1].Slug != "b-thing" || ds[2].Slug != "c-thing" {
		t.Fatalf("bad order: %q %q %q", ds[0].Slug, ds[1].Slug, ds[2].Slug)
	}
	if ds[1].Status != "draft" || ds[2].Status != "採用（実験で決定）" {
		t.Fatalf("status not kept verbatim: %q / %q", ds[1].Status, ds[2].Status)
	}
	sups := ds[1].Supersedes
	if len(sups) != 1 || sups[0].Filename != "2026-11-01-a-thing.md" || !sups[0].Partial {
		t.Fatalf("bad Supersedes: %+v", sups)
	}
}

// TestParseDecisionErrors makes malformed files fail loudly: a missing
// title, 状態, or 日付 line, and a 日付 that disagrees with the
// filename.
func TestParseDecisionErrors(t *testing.T) {
	for _, tc := range []struct {
		name, src, wantErr string
	}{
		{"no title", "- 状態: 採用\n- 日付: 2026-11-01\n", "title"},
		{"no status", "# T\n\n- 日付: 2026-11-01\n", "状態"},
		{"empty status", "# T\n\n- 状態:\n- 日付: 2026-11-01\n", "状態"},
		{"no date", "# T\n\n- 状態: 採用\n", "日付"},
		{"date mismatch", "# T\n\n- 状態: 採用\n- 日付: 2026-11-02\n", "filename"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := decisionsdoc.ParseDecision("2026-11-01-x.md", tc.src)
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("err = %v, want mention of %q", err, tc.wantErr)
			}
		})
	}

	// A "- 状態:" line in the body (after the first ## heading) is not
	// frontmatter and must not count.
	_, err := decisionsdoc.ParseDecision("2026-11-01-x.md",
		"# T\n\n- 日付: 2026-11-01\n\n## メモ\n\n- 状態: 良い\n")
	if err == nil || !strings.Contains(err.Error(), "状態") {
		t.Fatalf("body bullet leaked into frontmatter: %v", err)
	}
}

// TestRenderTable checks escaping of "|" in titles and the supersession
// markers (partial refs get 一部を, full refs do not; dangling refs are
// an error).
func TestRenderTable(t *testing.T) {
	ds := []decisionsdoc.Decision{
		{Date: "2026-11-01", Slug: "old", Filename: "2026-11-01-old.md",
			Title: "古い決定", Status: "採用"},
		{Date: "2026-11-01", Slug: "pipe", Filename: "2026-11-01-pipe.md",
			Title: "`|?>` と `%w`", Status: "採用"},
		{Date: "2026-11-02", Slug: "new", Filename: "2026-11-02-new.md",
			Title: "新しい決定", Status: "draft",
			Supersedes: []decisionsdoc.Supersession{
				{Filename: "2026-11-01-old.md", Partial: true},
			}},
	}
	got, err := decisionsdoc.RenderTable(ds)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "`\\|?>` と `%w`") {
		t.Fatalf("pipe not escaped:\n%s", got)
	}
	if !strings.Contains(got, "古い決定（一部を [new](2026-11-02-new.md) で置き換え）") {
		t.Fatalf("supersession marker missing:\n%s", got)
	}

	ds[2].Supersedes[0].Partial = false
	got, err = decisionsdoc.RenderTable(ds)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "古い決定（[new](2026-11-02-new.md) で置き換え）") {
		t.Fatalf("full supersession marker wrong:\n%s", got)
	}

	ds[2].Supersedes[0].Filename = "2026-11-03-gone.md"
	if _, err := decisionsdoc.RenderTable(ds); err == nil ||
		!strings.Contains(err.Error(), "does not exist") {
		t.Fatalf("dangling Supersedes should error, got %v", err)
	}
}

// TestRenderSplice covers the marked-region replacement: prose is
// preserved, exactly one of each marker is required, and END must be on
// its own line after BEGIN.
func TestRenderSplice(t *testing.T) {
	readme := "# head prose\n\n" + decisionsdoc.BeginMarker + "\nstale rows\n" +
		decisionsdoc.EndMarker + "\n\ntail prose\n"
	ds := []decisionsdoc.Decision{
		{Date: "2026-11-01", Slug: "x", Filename: "2026-11-01-x.md",
			Title: "X", Status: "採用"},
	}
	got, err := decisionsdoc.Render(readme, ds)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(got, "# head prose\n\n"+decisionsdoc.BeginMarker+"\n") ||
		!strings.HasSuffix(got, decisionsdoc.EndMarker+"\n\ntail prose\n") {
		t.Fatalf("prose not preserved:\n%s", got)
	}
	if strings.Contains(got, "stale rows") ||
		!strings.Contains(got, "| [x](2026-11-01-x.md) | X | 採用 |") {
		t.Fatalf("region not replaced:\n%s", got)
	}

	// Idempotent: rendering the rendered file changes nothing.
	again, err := decisionsdoc.Render(got, ds)
	if err != nil {
		t.Fatal(err)
	}
	if again != got {
		t.Fatal("Render is not idempotent")
	}

	for _, tc := range []struct{ name, readme string }{
		{"no markers", "# just prose\n"},
		{"begin only", decisionsdoc.BeginMarker + "\n"},
		{"end only", decisionsdoc.EndMarker + "\n"},
		{"swapped", decisionsdoc.EndMarker + "\n" + decisionsdoc.BeginMarker + "\n"},
		{"end mid-line", decisionsdoc.BeginMarker + "\nx " + decisionsdoc.EndMarker + "\n"},
		{"two begins", decisionsdoc.BeginMarker + "\n" + decisionsdoc.BeginMarker + "\n" + decisionsdoc.EndMarker + "\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := decisionsdoc.Render(tc.readme, ds); err == nil {
				t.Fatal("expected an error")
			}
		})
	}
}
