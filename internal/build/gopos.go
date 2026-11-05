package build

import (
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/podhmo/gom/internal/diag"
)

// GoPosIndex restores positions printed by the Go toolchain back to
// .gom byte offsets (docs/design.md section 2.2). Generated code
// carries `//line` directives, so go build / go vet / go test errors
// and panic stack frames already name the .gom file and a line
// number — the index supplies the last step: matching that name to
// one of the compiled inputs and turning line:col into a byte
// offset.
//
// Build it once per set of inputs the tool run covered; LineIndex
// values are cached across lookups.
//
// A GoPosIndex is NOT safe for concurrent use: LineIndex and the
// path cache are filled lazily during lookups. If a future consumer
// (P4-6's LSP) shares one across goroutines, build the indexes up
// front (a LineIndex call per input) or guard with a mutex.
type GoPosIndex struct {
	// dir is the directory the go tool ran in — the base for
	// resolving relative position names to absolute paths.
	dir string
	// srcs maps each input's .gom path (as the user refers to it —
	// the same name diagnostics and //line operands carry) to its
	// source.
	srcs     map[string][]byte
	lines    map[string]*diag.LineIndex
	absCache map[string]string
}

// NewGoPosIndex builds an index over srcs. dir is the directory the
// go tool runs (or ran) in; pass "" when positions should only match
// the srcs keys verbatim — an empty dir never absolutizes, which is
// what callers holding in-memory sources want.
func NewGoPosIndex(dir string, srcs map[string][]byte) *GoPosIndex {
	return &GoPosIndex{
		dir:      dir,
		srcs:     srcs,
		lines:    map[string]*diag.LineIndex{},
		absCache: map[string]string{},
	}
}

// LineIndex returns the line index of the input file names — the
// name must resolve to a known .gom source: an exact srcs key, the
// same with a "./" prefix stripped, or the same file reached by
// absolutizing it against dir. The second result is false when the
// name is no known input (a generated-adjacent hand-written file, a
// toolchain file, an rt/stdlib path in a stack frame).
func (x *GoPosIndex) LineIndex(file string) (*diag.LineIndex, bool) {
	path, ok := x.file(file)
	if !ok {
		return nil, false
	}
	return x.index(path), true
}

// Resolve maps a Go tool position — file, 1-based line, 1-based byte
// column as `file:line:col:` errors and stack frames print them — to
// a point span in the .gom source. The column is the generated
// line's column (`//line` carries no columns), so the span points at
// the named position approximately; it is clamped inside the line
// when the column runs past the .gom text, and line is clamped into
// the file's bounds (Go can report lines beyond EOF when `//line`
// arithmetic runs past the source's real extent). Unmatched files
// report false.
func (x *GoPosIndex) Resolve(file string, line, col int) (diag.Span, bool) {
	path, ok := x.file(file)
	if !ok {
		return diag.Span{}, false
	}
	li := x.index(path)
	ls, le := li.LineBounds(line)
	off := max(ls+col-1,
		// a line with no column resolves to the line head
		ls)
	if off > le {
		off = le
	}
	return diag.Point(path, off), true
}

// StackPos is one frame of a panic/goroutine dump resolved against
// the index — the last step of the "panic のスタックの変換" half of
// docs/design.md section 2.2.
type StackPos struct {
	diag.StackFrame
	// Span is the frame's .gom point — the head of the line the
	// trace reports (stack positions carry no column). It is valid
	// only when the frame's file names a compiled input through the
	// generated code's //line; frames in rt, the toolchain, or
	// hand-written Go files leave it zero.
	Span diag.Span
}

// ResolveStack parses trace into frames with diag.ParseStack and
// resolves each against the index. Frames keep trace order; callers
// tell mapped .gom frames from toolchain ones by Span.Valid().
func (x *GoPosIndex) ResolveStack(trace string) []StackPos {
	frames := diag.ParseStack(trace)
	out := make([]StackPos, len(frames))
	for i, f := range frames {
		out[i].StackFrame = f
		if sp, ok := x.Resolve(f.File, f.Line, 1); ok {
			out[i].Span = sp
		}
	}
	return out
}

// RenderStack re-renders a panic or goroutine dump with .gom
// positions: every tab-indented "file:line" frame position whose
// file resolves through the index is rewritten to the input's .gom
// path and line. Everything else stays verbatim — call lines,
// goroutine headers, the panic message, and frames in the
// toolchain, rt, or hand-written Go keep their raw text. The
// PC-offset trailer drops from resolved lines; unresolved lines
// keep it. An output identical to the input means no frame
// resolved, so a caller can tell "nothing mapped" from "mapped".
//
// The rewrite is line-local: a line that fails to parse or resolve
// can never corrupt a neighbour, so the rendered half of spec
// section 12's two-part panic output degrades per frame rather than
// per dump (docs/decisions/2026-11-05-gom-test-panic-stack.md).
func (x *GoPosIndex) RenderStack(trace string) string {
	lines := strings.Split(trace, "\n")
	for i, l := range lines {
		if !strings.HasPrefix(l, "\t") {
			continue
		}
		pos := strings.TrimLeft(l, "\t")
		file, ln, ok := diag.ParseStackPos(pos)
		if !ok {
			continue
		}
		sp, ok := x.Resolve(file, ln, 1)
		if !ok {
			continue
		}
		lines[i] = l[:len(l)-len(pos)] + sp.File + ":" +
			strconv.Itoa(x.index(sp.File).Pos(sp.Start).Line)
	}
	return strings.Join(lines, "\n")
}

// index returns the (cached) line index for a resolved srcs key.
func (x *GoPosIndex) index(path string) *diag.LineIndex {
	if li, ok := x.lines[path]; ok {
		return li
	}
	li := diag.NewLineIndex(x.srcs[path])
	x.lines[path] = li
	return li
}

// file resolves a printed position name to the srcs key it names.
// Matching is two-pass so the result cannot depend on map iteration
// order: an exact or "./"-stripped key match always wins over an
// absolutized-path match.
func (x *GoPosIndex) file(file string) (string, bool) {
	clean := strings.TrimPrefix(file, "./")
	for path := range x.srcs {
		if file == path || clean == path {
			return path, true
		}
	}
	if x.dir == "" {
		return "", false
	}
	af := x.abs(file)
	// Sorted keys keep the abs pass deterministic too: two srcs keys
	// that absolutize to the same file resolve to the
	// lexicographically-first spelling.
	keys := make([]string, 0, len(x.srcs))
	for path := range x.srcs {
		keys = append(keys, path)
	}
	sort.Strings(keys)
	for _, path := range keys {
		if af == x.abs(path) {
			return path, true
		}
	}
	return "", false
}

// abs absolutizes p against the tool's directory (no-op when dir is
// empty or p is already absolute). Results are memoized — the same
// few names recur across an error stream.
func (x *GoPosIndex) abs(p string) string {
	if a, ok := x.absCache[p]; ok {
		return a
	}
	a := p
	if x.dir != "" {
		c := strings.TrimPrefix(p, "./")
		if !filepath.IsAbs(c) {
			c = filepath.Join(x.dir, c) // Join appends even when c is absolute — guard first
		}
		if abs, err := filepath.Abs(c); err == nil {
			a = abs
		}
	}
	x.absCache[p] = a
	return a
}

// goErrorLine matches the "file:line:col: msg" head of a Go build
// diagnostic — go build, go vet and go test build-output all share
// the shape, and the column group is optional for the "file:line:
// msg" variants some tools print. The file group is non-greedy so
// the FIRST numeric fields become line and col; a ':' inside the
// path falls out naturally when the early split fails to parse.
var goErrorLine = regexp.MustCompile(`^(.+?):([0-9]+)(?::([0-9]+))?:[ \t]+(.*)$`)

// ParseGoErrorLine parses one "file:line[:col]: msg" diagnostic line
// of go build / go vet / go test output. col is 0 when the line has
// no column field.
func ParseGoErrorLine(line string) (file string, lineno, col int, msg string, ok bool) {
	m := goErrorLine.FindStringSubmatch(line)
	if m == nil {
		return "", 0, 0, "", false
	}
	lineno, _ = strconv.Atoi(m[2])
	if m[3] != "" {
		col, _ = strconv.Atoi(m[3])
	}
	return m[1], lineno, col, m[4], true
}

// GoToolDiagnostics folds the diagnostic lines of a go tool run —
// `go build` or `go vet` stderr, `go test` build-output events — into
// E0001 diagnostics marked source "go" (docs/spec.md section 12,
// item 6). Each "file:line:col: msg" line becomes one diagnostic
// with the tool's own message; positions naming a known .gom input
// resolve to point spans through idx (nil idx leaves every line
// positionless). "# pkg" headers and empty lines are skipped, other
// non-position lines keep their raw text in data. tool is the tool
// name recorded in data ("go build", "go vet", "go test") and used
// in the fallback message when a run produced no parseable line at
// all.
func GoToolDiagnostics(lines []string, tool string, idx *GoPosIndex) []diag.Diagnostic {
	var out []diag.Diagnostic
	for _, chunk := range lines {
		for line := range strings.SplitSeq(strings.TrimRight(chunk, "\n"), "\n") {
			if line == "" || strings.HasPrefix(line, "# ") {
				continue // "# pkg [pkg.test]" headers name the package
			}
			dg := diag.Error("E0001", tool+" reported a build error").
				From(diag.SourceGo).
				Set("tool", tool).
				Set("raw", line)
			if file, ln, col, msg, ok := ParseGoErrorLine(line); ok {
				if msg != "" {
					dg.Message = msg
				}
				dg = dg.Set("file", file).
					Set("line", ln)
				if col != 0 {
					dg = dg.Set("col", col)
				}
				if idx != nil {
					if sp, ok := idx.Resolve(file, ln, col); ok {
						dg = dg.At(sp)
					}
				}
			}
			out = append(out, dg)
		}
	}
	if len(out) == 0 {
		out = append(out, diag.Error("E0001", tool+" reported a build error").
			From(diag.SourceGo).
			Set("tool", tool))
	}
	return out
}
