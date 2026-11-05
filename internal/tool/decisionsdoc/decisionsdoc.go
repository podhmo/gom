package decisionsdoc

//go:generate go test -run TestDecisionsReadmeInSync -update

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

// The generated region of docs/decisions/README.md is delimited by
// these markers. Everything between them — the generated-comment line
// and the index table — is rewritten by Render.
const (
	BeginMarker = "<!-- BEGIN GENERATED -->"
	EndMarker   = "<!-- END GENERATED -->"
)

var (
	// filenameRe is the decision-file naming convention:
	// YYYY-MM-DD-<slug>.md. Only bare filenames match; a Supersedes
	// link pointing outside the directory is not a decision
	// reference.
	filenameRe = regexp.MustCompile(`^(\d{4}-\d{2}-\d{2})-(.+)\.md$`)
	// fieldRe matches the frontmatter bullets ("- 状態: 採用").
	fieldRe = regexp.MustCompile(`^- (\S+):\s*(.*)$`)
	// linkRe extracts the target of a Markdown link.
	linkRe = regexp.MustCompile(`\[[^\]]*\]\(([^)]+)\)`)
)

// Supersession records that a decision file's "- Supersedes:" line
// links Filename (partially, when Partial is set — the line says
// "のうち", i.e. only part of the earlier decision is replaced).
type Supersession struct {
	Filename string
	Partial  bool
}

// Decision is the index-relevant frontmatter of one decision file.
type Decision struct {
	Date       string // YYYY-MM-DD, from the filename
	Slug       string // filename without the date prefix and .md
	Filename   string
	Title      string // text of the first "# " line
	Status     string // value of the "- 状態:" bullet, verbatim
	Supersedes []Supersession
}

// ScanDir reads every decision file in dir — files whose names match
// YYYY-MM-DD-<slug>.md — and returns them sorted by date, then slug
// (the same order the filenames sort into). Non-decision files such as
// README.md are ignored; a malformed decision file is an error.
func ScanDir(dir string) ([]Decision, error) {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var ds []Decision
	for _, e := range ents {
		if e.IsDir() || !filenameRe.MatchString(e.Name()) {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			return nil, err
		}
		d, err := ParseDecision(e.Name(), string(raw))
		if err != nil {
			return nil, err
		}
		ds = append(ds, d)
	}
	slices.SortFunc(ds, func(a, b Decision) int {
		if c := strings.Compare(a.Date, b.Date); c != 0 {
			return c
		}
		return strings.Compare(a.Slug, b.Slug)
	})
	return ds, nil
}

// ParseDecision extracts the index fields from one decision file.
// name is the file's basename and must match the naming convention.
// The frontmatter is everything before the first "## " heading: the
// "# " title line and the "- key:" bullets. 状態 and 日付 are required,
// and 日付 must agree with the filename date.
func ParseDecision(name, src string) (Decision, error) {
	m := filenameRe.FindStringSubmatch(name)
	if m == nil {
		return Decision{}, fmt.Errorf("decisionsdoc: %s: not a YYYY-MM-DD-<slug>.md name", name)
	}
	d := Decision{Date: m[1], Slug: m[2], Filename: name}

	var date string
	for line := range strings.SplitSeq(src, "\n") {
		if strings.HasPrefix(line, "## ") {
			break // end of the frontmatter region
		}
		if strings.HasPrefix(line, "# ") {
			if d.Title == "" {
				d.Title = strings.TrimSpace(line[2:])
			}
			continue
		}
		fm := fieldRe.FindStringSubmatch(line)
		if fm == nil {
			continue
		}
		key, val := fm[1], strings.TrimSpace(fm[2])
		switch key {
		case "状態":
			if d.Status == "" {
				d.Status = val
			}
		case "日付":
			if date == "" {
				date = val
			}
		case "Supersedes":
			for _, lm := range linkRe.FindAllStringSubmatch(val, -1) {
				if filenameRe.MatchString(lm[1]) {
					d.Supersedes = append(d.Supersedes, Supersession{
						Filename: lm[1],
						Partial:  strings.Contains(val, "のうち"),
					})
				}
			}
		}
	}

	if d.Title == "" {
		return Decision{}, fmt.Errorf("decisionsdoc: %s: missing \"# \" title line", name)
	}
	if d.Status == "" {
		return Decision{}, fmt.Errorf("decisionsdoc: %s: missing \"- 状態:\" line", name)
	}
	if date == "" {
		return Decision{}, fmt.Errorf("decisionsdoc: %s: missing \"- 日付:\" line", name)
	}
	if date != d.Date {
		return Decision{}, fmt.Errorf("decisionsdoc: %s: \"- 日付:\" says %s but the filename says %s",
			name, date, d.Date)
	}
	return d, nil
}

// RenderTable renders the index table (header included) for ds in the
// order given. A decision file that another file's Supersedes line
// points at gets a "（一部を X で置き換え）" marker in its 決定 cell —
// "一部を" only when every superseding reference is partial ("のうち").
// A Supersedes link naming a file that does not exist is an error:
// decision files are never renamed or deleted.
func RenderTable(ds []Decision) (string, error) {
	known := make(map[string]bool, len(ds))
	for _, d := range ds {
		known[d.Filename] = true
	}
	type ref struct {
		slug, filename string
		partial        bool
	}
	supersededBy := make(map[string][]ref)
	for _, d := range ds {
		for _, s := range d.Supersedes {
			if !known[s.Filename] {
				return "", fmt.Errorf("decisionsdoc: %s: Supersedes points at %s, which does not exist",
					d.Filename, s.Filename)
			}
			supersededBy[s.Filename] = append(supersededBy[s.Filename], ref{
				slug:     d.Slug,
				filename: d.Filename,
				partial:  s.Partial,
			})
		}
	}
	for _, refs := range supersededBy {
		slices.SortFunc(refs, func(a, b ref) int { return strings.Compare(a.filename, b.filename) })
	}

	var b strings.Builder
	b.WriteString("| ファイル | 決定 | 状態 |\n")
	b.WriteString("| --- | --- | --- |\n")
	for _, d := range ds {
		title := d.Title
		if refs := supersededBy[d.Filename]; len(refs) > 0 {
			links := make([]string, len(refs))
			allPartial := true
			for i, r := range refs {
				links[i] = fmt.Sprintf("[%s](%s)", r.slug, r.filename)
				if !r.partial {
					allPartial = false
				}
			}
			if allPartial {
				title += fmt.Sprintf("（一部を %s で置き換え）", strings.Join(links, "、"))
			} else {
				title += fmt.Sprintf("（%s で置き換え）", strings.Join(links, "、"))
			}
		}
		fmt.Fprintf(&b, "| [%s](%s) | %s | %s |\n",
			d.Slug, d.Filename, escapeCell(title), escapeCell(d.Status))
	}
	return strings.TrimSuffix(b.String(), "\n"), nil
}

// Render splices the generated block — the generated-comment line and
// the table of ds — into readme between BeginMarker and EndMarker.
// Each marker must appear exactly once, END must sit at the start of a
// line after BEGIN, and the prose outside the markers is preserved
// byte for byte.
func Render(readme string, ds []Decision) (string, error) {
	if n := strings.Count(readme, BeginMarker); n != 1 {
		return "", fmt.Errorf("decisionsdoc: README must contain exactly one %s, found %d", BeginMarker, n)
	}
	if n := strings.Count(readme, EndMarker); n != 1 {
		return "", fmt.Errorf("decisionsdoc: README must contain exactly one %s, found %d", EndMarker, n)
	}
	bi := strings.Index(readme, BeginMarker)
	ei := strings.Index(readme, EndMarker)
	if ei < bi {
		return "", fmt.Errorf("decisionsdoc: %s appears before %s", EndMarker, BeginMarker)
	}
	bLineStart := strings.LastIndex(readme[:bi], "\n") + 1
	if strings.TrimSpace(readme[bLineStart:bi]) != "" {
		return "", fmt.Errorf("decisionsdoc: %s must be at the start of a line", BeginMarker)
	}
	nl := strings.Index(readme[bi:], "\n")
	if nl < 0 {
		return "", fmt.Errorf("decisionsdoc: no newline after %s", BeginMarker)
	}
	regionStart := bi + nl + 1
	if ei < regionStart {
		return "", fmt.Errorf("decisionsdoc: %s appears before %s", EndMarker, BeginMarker)
	}
	lineStart := strings.LastIndex(readme[:ei], "\n") + 1
	if strings.TrimSpace(readme[lineStart:ei]) != "" {
		return "", fmt.Errorf("decisionsdoc: %s must be at the start of a line", EndMarker)
	}

	table, err := RenderTable(ds)
	if err != nil {
		return "", err
	}
	block := "<!-- Code generated from docs/decisions/*.md by \"go generate ./internal/tool/decisionsdoc\". DO NOT EDIT. -->\n" + table
	return readme[:regionStart] + block + "\n" + readme[lineStart:], nil
}

// RenderDir scans dir for decision files and renders the new content
// of dir/README.md. It does not write the file — the sync test decides
// whether to compare or rewrite.
func RenderDir(dir string) (string, error) {
	ds, err := ScanDir(dir)
	if err != nil {
		return "", err
	}
	readmePath := filepath.Join(dir, "README.md")
	raw, err := os.ReadFile(readmePath)
	if err != nil {
		return "", err
	}
	return Render(string(raw), ds)
}

// escapeCell keeps raw "|" characters — e.g. the `|?>` operator in a
// title — from breaking the Markdown table.
func escapeCell(s string) string {
	return strings.ReplaceAll(s, "|", "\\|")
}
