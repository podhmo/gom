package skills_test

// Snippet extraction test: every fenced `gom`/`gomi` code block in
// the agent-facing skills under skills/ is compiled with the same
// `gom check` semantics the CLI uses (parse → resolve → check →
// pattern), so examples in the skill cannot silently rot — the
// MoonBit SKILL.mbt.md CI pattern applied to Gom.
//
// Fence conventions:
//
//	```gom            — must compile clean (blocks without a package
//	                    clause are checked as `package main` fragments)
//	```gom fail       — must produce at least one error diagnostic
//	```gom fail=E0301 — must produce a diagnostic with that code
//	```gom skip       — extracted but not compiled (fragments,
//	                    not-yet-implemented features)
//	```gomi           — checked as an interface file (CheckOnly implied)
//
// Other languages (```json, ```sh, ```text) are ignored. Fences are
// matched at line start — the conventional form — so inline ``` in
// prose is never mistaken for a block.
//
// TestVendoredSkills additionally asserts .devin/skills/ holds a
// byte-identical copy of every skills/<name>/ tree; refresh it with
// `make vendor-skills`.

import (
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/podhmo/gom/internal/build"
	"github.com/podhmo/gom/internal/diag"
	"github.com/podhmo/gom/internal/goimport"
	"github.com/podhmo/gom/internal/lower"
)

// fence matches a markdown fenced code block anchored at line start;
// the whole info string is captured (`gom`, ` gom fail=E0301`,
// `GOM`, `json`, ... — whitespace and case are normalized by the
// caller, so sloppy info strings cannot silently evade the test).
// The closing fence must be at line start too.
var fence = regexp.MustCompile("(?m)^```([^\n]*)\n([\\s\\S]*?)^```[ \t]*$")

// packageClause reports whether a snippet declares its own package —
// fragments without one are checked as `package main` sources.
var packageClause = regexp.MustCompile(`(?m)^[ \t]*package\b`)

// snippet is one extracted code block.
type snippet struct {
	mdFile string // repo-relative markdown path
	line   int    // 1-based line of the opening fence
	lang   string // first word of the info string ("gom", "gomi", ...)
	flags  string // rest of the info string after lang
	src    []byte
}

func (s snippet) name() string { return fmt.Sprintf("%s:%d", s.mdFile, s.line) }

// expectation reports the block's declared outcome: it must produce
// errors (wantErr) — optionally one with a specific code (wantCode)
// — or the block is opted out of checking entirely (skip).
func (s snippet) expectation() (wantErr bool, wantCode string, skip bool) {
	for _, f := range strings.Fields(s.flags) {
		switch {
		case f == "skip":
			skip = true
		case f == "fail":
			wantErr = true
		case strings.HasPrefix(f, "fail="):
			wantErr = true
			wantCode = strings.TrimPrefix(f, "fail=")
		}
	}
	return wantErr, wantCode, skip
}

// repoRoot returns the repository root; snippets_test.go lives in
// skills/, one level below it.
func repoRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	return root
}

// extractSnippets walks dir for *.md files and yields every fenced
// gom/gomi block, in file order.
func extractSnippets(t *testing.T, dir string) []snippet {
	t.Helper()
	var out []snippet
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path != dir && strings.HasPrefix(d.Name(), ".") {
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(d.Name(), ".md") {
			return nil
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, m := range fence.FindAllSubmatchIndex(content, -1) {
			info := string(content[m[2]:m[3]])
			src := content[m[4]:m[5]]
			fields := strings.Fields(info)
			if len(fields) == 0 {
				continue
			}
			lang := strings.ToLower(fields[0])
			if lang != "gom" && lang != "gomi" {
				continue
			}
			out = append(out, snippet{
				mdFile: path,
				line:   1 + bytes.Count(content[:m[0]], []byte("\n")),
				lang:   lang,
				flags:  strings.Join(fields[1:], " "),
				src:    src,
			})
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(out) == 0 {
		t.Fatalf("no gom/gomi snippets found under %s", dir)
	}
	return out
}

// checkSnippet compiles one snippet through build.Compile with
// CheckOnly — the in-process equivalent of `gom check` (the wiring
// mirrors cmd/gom's compileOptions and tutorial_test.go's
// compileStep). It returns the diagnostics produced.
func checkSnippet(t *testing.T, root string, s snippet) diag.List {
	t.Helper()
	src := s.src
	if s.lang != "gomi" && !packageClause.Match(src) {
		src = append([]byte("package main\n\n"), src...)
	}
	name := fmt.Sprintf("%s#%d.gom", s.mdFile, s.line)
	in := build.Input{Path: name, Src: src, Interface: s.lang == "gomi"}
	loader := goimport.New(&goimport.Config{Dir: root})
	r, err := build.Compile(in, &build.Options{
		Loader: loader, Sigs: loader, Lower: &lower.Env{GoSig: loader.GoSigOf},
		CheckOnly: true,
	})
	if err != nil {
		t.Fatalf("%s: compiler API: %v", s.name(), err)
	}
	return r.Diagnostics
}

// render flattens diagnostics into a short "code: message" listing
// for failure output.
func render(diags diag.List) string {
	var b strings.Builder
	for _, d := range diags {
		fmt.Fprintf(&b, "  %s %s\n", d.Code, d.Message)
	}
	return b.String()
}

func TestSkillSnippets(t *testing.T) {
	root := repoRoot(t)
	_, goErr := exec.LookPath("go")
	skillDir := "gom-agent-guide"
	snippets := extractSnippets(t, skillDir)
	for _, s := range snippets {
		s := s
		wantErr, wantCode, skip := s.expectation()
		if skip {
			continue
		}
		t.Run(s.name(), func(t *testing.T) {
			if goErr != nil && bytes.Contains(s.src, []byte("import go")) {
				t.Skip("no go toolchain on PATH for `import go`")
			}
			diags := checkSnippet(t, root, s)
			if !wantErr {
				if diags.HasError() {
					t.Fatalf("snippet must compile clean:\n%s\n--- diagnostics ---\n%s", s.src, render(diags))
				}
				return
			}
			if !diags.HasError() {
				t.Fatalf("snippet must fail with a diagnostic but compiled clean:\n%s", s.src)
			}
			if wantCode != "" {
				found := false
				for _, d := range diags {
					if string(d.Code) == wantCode {
						found = true
					}
				}
				if !found {
					t.Fatalf("snippet must fail with %s:\n%s\n--- diagnostics ---\n%s", wantCode, s.src, render(diags))
				}
			}
		})
	}
}

// TestVendoredSkills asserts .devin/skills mirrors skills/ — the two
// copies must never drift; refresh with `make vendor-skills`.
func TestVendoredSkills(t *testing.T) {
	root := repoRoot(t)
	collect := func(dir string) map[string][]byte {
		files := map[string][]byte{}
		err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				return nil
			}
			rel, err := filepath.Rel(dir, path)
			if err != nil {
				return err
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			files[filepath.ToSlash(rel)] = data
			return nil
		})
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			t.Fatal(err)
		}
		return files
	}
	canon := collect(filepath.Join(root, "skills", "gom-agent-guide"))
	vend := collect(filepath.Join(root, ".devin", "skills", "gom-agent-guide"))
	if len(canon) == 0 {
		t.Fatal("skills/gom-agent-guide is empty or missing")
	}
	var problems []string
	for rel, want := range canon {
		got, ok := vend[rel]
		if !ok {
			problems = append(problems, fmt.Sprintf("%s missing from .devin/skills", rel))
			continue
		}
		if !bytes.Equal(want, got) {
			problems = append(problems, fmt.Sprintf("%s differs from skills/ copy", rel))
		}
	}
	for rel := range vend {
		if _, ok := canon[rel]; !ok {
			problems = append(problems, fmt.Sprintf("%s exists only in .devin/skills", rel))
		}
	}
	for _, p := range problems {
		t.Error(p)
	}
	if len(problems) > 0 {
		t.Log("run `make vendor-skills` to resync .devin/skills from skills/")
	}
}
