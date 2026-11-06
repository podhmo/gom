package build

import (
	"bytes"
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
	srcs map[string][]byte
	// maps holds the generated outputs' anchor tables per srcs key —
	// registered through AddSourceMap, they make ResolveMatch
	// table-aware.
	maps     map[string][]fileMap
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
		maps:     map[string][]fileMap{},
		lines:    map[string]*diag.LineIndex{},
		absCache: map[string]string{},
	}
}

// fileMap pairs one generated output's bytes with its anchor table
// — codegen's Output.Source + Output.SourceMap, or TestSource +
// TestSourceMap.
type fileMap struct {
	m *diag.SourceMap
	// code marks generated lines that can carry a diagnostic:
	// non-empty after trimming and not comment-only (`//` — which
	// also covers the spliced //line directives). Blank and
	// comment-only lines drift like everything else but the
	// toolchain never reports on them, so they leave the
	// ambiguity set.
	code []bool
}

// newFileMap classifies the generated lines once — the table is
// consulted on every candidate lookup.
func newFileMap(gen []byte, m *diag.SourceMap) fileMap {
	code := make([]bool, 0, bytes.Count(gen, []byte("\n"))+1)
	for len(gen) > 0 {
		var line []byte
		if i := bytes.IndexByte(gen, '\n'); i >= 0 {
			line, gen = gen[:i], gen[i+1:]
		} else {
			line, gen = gen, nil
		}
		t := bytes.TrimSpace(line)
		code = append(code, len(t) > 0 && !bytes.HasPrefix(t, []byte("//")))
	}
	return fileMap{m: m, code: code}
}

// AddSourceMap registers one generated output's anchor table for
// the input named path — call once per generated file: the main
// output (Output.Source + Output.SourceMap) and, when it exists,
// the test output. Registered maps make ResolveMatch — and through
// it GoToolDiagnostics, ResolveStack and RenderStack — consult the
// correspondence table, so a report on a drifted generated line
// lands on its real construct instead of a wrong-but-real .gom
// line. Inputs without maps keep the naive line mapping.
func (x *GoPosIndex) AddSourceMap(path string, gen []byte, m *diag.SourceMap) {
	if m == nil || len(m.Anchors) == 0 {
		return
	}
	if p, ok := x.file(path); ok {
		path = p // normalize to the srcs key the lookup resolves to
	}
	x.maps[path] = append(x.maps[path], newFileMap(gen, m))
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

// PosMatchKind classifies how confidently a reported go tool
// position maps back to .gom through the anchor table
// (docs/design.md section 2.2's reverse direction).
type PosMatchKind int

const (
	// MatchExact: one construct claims the report on-station — the
	// reported line lies inside its span's .gom extent, so the span
	// is the construct's fragment on the reported line.
	MatchExact PosMatchKind = iota
	// MatchDrift: one construct claims the report, but the reported
	// line runs past its extent — the continuation numbering of a
	// multi-line expansion. The true position is somewhere inside
	// the construct, so the span is the construct's whole span,
	// not the printed line.
	MatchDrift
	// MatchAmbiguous: several distinct construct spans claim the
	// report — a drifted code line colliding with a real .gom
	// line, same-line siblings, or the main and test outputs both
	// claiming it. The span is the union of every candidate, so the
	// answer covers the truth rather than guessing at one side of
	// it; Candidates carries the contenders.
	MatchAmbiguous
)

// PosMatch is the resolved .gom position of one go tool report.
type PosMatch struct {
	// Span is the resolved .gom region: the construct's fragment on
	// the reported line (MatchExact), the whole construct
	// (MatchDrift), or the union of candidates (MatchAmbiguous).
	Span diag.Span
	Kind PosMatchKind
	// Mapped reports the span came from the correspondence table.
	// False means the naive line mapping answered — no map was
	// registered for the file, or no candidate claimed the report.
	Mapped bool
	// Candidates holds the distinct construct spans claiming the
	// report, in generated-line order — for ambiguous matches the
	// contenders, otherwise the single claimant. Naive answers
	// carry the resolved point.
	Candidates []diag.Span
}

// ResolveMatch maps a Go tool position — file, 1-based line,
// 1-based column — to a .gom span through the registered anchor
// tables (docs/design.md section 2.2's correspondence-table
// inverse). Where Resolve trusts the reported line, ResolveMatch
// asks which generated lines could print it and bounds the answer
// by the constructs anchored there:
//
//   - exactly one distinct span claims it on-station → MatchExact,
//     the span restricted to the reported line;
//   - exactly one distinct span claims it past its extent →
//     MatchDrift, the construct's whole span;
//   - several distinct spans claim it → MatchAmbiguous, their
//     union;
//   - no candidate (no maps for the file, or a printed line no
//     anchor's numbering produces) → the naive Resolve answer with
//     Mapped false.
//
// Candidate lines are counted only when the generated line can
// carry a diagnostic — non-blank, non-comment — and when the
// anchor's span names the reported file.
func (x *GoPosIndex) ResolveMatch(file string, line, col int) (PosMatch, bool) {
	path, ok := x.file(file)
	if !ok {
		return PosMatch{}, false
	}
	fms := x.maps[path]
	if len(fms) == 0 {
		return x.naiveMatch(file, line, col)
	}
	li := x.index(path)
	var spans []diag.Span
	drifted := false
	seen := map[diag.Span]bool{}
	for i := range fms {
		fm := &fms[i]
		for _, am := range fm.m.Candidates(line, li) {
			sp := am.Anchor.Span
			if sp.File != path {
				continue // a map belongs to one input — belt over the contract
			}
			if am.Line < 1 || am.Line > len(fm.code) || !fm.code[am.Line-1] {
				continue // generated line cannot carry a diagnostic
			}
			if seen[sp] {
				continue
			}
			seen[sp] = true
			spans = append(spans, sp)
			drifted = am.Drifted
		}
	}
	if len(spans) == 0 {
		return x.naiveMatch(file, line, col)
	}
	if len(spans) == 1 {
		if drifted {
			return PosMatch{Span: spans[0], Kind: MatchDrift, Mapped: true, Candidates: spans}, true
		}
		ls, le := li.LineBounds(line)
		frag := diag.SpanIn(path, max(spans[0].Start, ls), min(spans[0].End, le))
		if frag.End < frag.Start {
			frag.End = frag.Start // degenerate edge: keep the span valid
		}
		return PosMatch{Span: frag, Kind: MatchExact, Mapped: true, Candidates: spans}, true
	}
	u := diag.SpanIn(path, spans[0].Start, spans[0].End)
	for _, s := range spans[1:] {
		u.Start = min(u.Start, s.Start)
		u.End = max(u.End, s.End)
	}
	return PosMatch{Span: u, Kind: MatchAmbiguous, Mapped: true, Candidates: spans}, true
}

// naiveMatch is Resolve's answer wrapped as an unmapped PosMatch —
// the fallback when the correspondence table has nothing to say.
func (x *GoPosIndex) naiveMatch(file string, line, col int) (PosMatch, bool) {
	sp, ok := x.Resolve(file, line, col)
	if !ok {
		return PosMatch{}, false
	}
	return PosMatch{Span: sp, Kind: MatchExact, Candidates: []diag.Span{sp}}, true
}

// StackPos is one frame of a panic/goroutine dump resolved against
// the index — the last step of the "panic のスタックの変換" half of
// docs/design.md section 2.2.
type StackPos struct {
	diag.StackFrame
	// Span is the frame's .gom position — the head of the line the
	// trace reports (stack positions carry no column), or — with
	// anchor tables registered — the construct the frame belongs
	// to: a drifted frame resolves to its construct's span rather
	// than the wrong-but-real line the drift printed. Ambiguous
	// frames carry the candidates' union. It is valid only when
	// the frame's file names a compiled input through the
	// generated code's //line; frames in rt, the toolchain, or
	// hand-written Go files leave it zero.
	Span diag.Span
	// Kind is the match classification of Span — MatchDrift means
	// the printed frame line was drift arithmetic, MatchAmbiguous
	// means the union spans several constructs. It is meaningful
	// only together with Mapped: unresolvable frames (toolchain,
	// rt, hand-written Go — !Span.Valid()) and files resolved
	// without a registered map both leave it MatchExact.
	Kind PosMatchKind
	// Mapped reports the span came from the correspondence table
	// rather than the naive line mapping.
	Mapped bool
}

// ResolveStack parses trace into frames with diag.ParseStack and
// resolves each against the index. Frames keep trace order; callers
// tell mapped .gom frames from toolchain ones by Span.Valid() —
// and a mapped frame's confidence from Kind/Mapped.
func (x *GoPosIndex) ResolveStack(trace string) []StackPos {
	frames := diag.ParseStack(trace)
	out := make([]StackPos, len(frames))
	for i, f := range frames {
		out[i].StackFrame = f
		if pm, ok := x.ResolveMatch(f.File, f.Line, 1); ok {
			out[i].Span = pm.Span
			out[i].Kind = pm.Kind
			out[i].Mapped = pm.Mapped
		}
	}
	return out
}

// RenderStack re-renders a panic or goroutine dump with .gom
// positions: every tab-indented "file:line" frame position whose
// file resolves through the index is rewritten to the input's .gom
// path and line — a drifted frame resolves to its construct's line
// rather than the drifted number Go printed. Everything else stays
// verbatim — call lines, goroutine headers, the panic message,
// frames in the toolchain, rt, or hand-written Go, and ambiguous
// frames (the printed line is claimed by several constructs) keep
// their raw text: rewriting one side of an ambiguity would be a
// guess. The PC-offset trailer drops from resolved lines;
// unresolved lines keep it. An output identical to the input means
// no frame resolved, so a caller can tell "nothing mapped" from
// "mapped".
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
		pm, ok := x.ResolveMatch(file, ln, 1)
		if !ok {
			continue
		}
		if pm.Kind == MatchAmbiguous {
			// Ambiguous: the frame is a .gom frame but the printed
			// position is claimed by several constructs — keep the
			// printed position and normalize the line the same way
			// resolved frames are (PC-offset trailer dropped), so
			// the rendered half marks it as mapped without
			// pretending the ambiguity away.
			lines[i] = l[:len(l)-len(pos)] + file + ":" + strconv.Itoa(ln)
			continue
		}
		lines[i] = l[:len(l)-len(pos)] + pm.Span.File + ":" +
			strconv.Itoa(x.index(pm.Span.File).Pos(pm.Span.Start).Line)
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
// resolve through idx — point spans under the naive line mapping,
// or the anchor-table match when the input's outputs are
// registered: a drifted report lands on its construct's span with
// "drifted" in data, an ambiguous one covers the candidates' union
// with "ambiguous" and "candidates" in data (nil idx leaves every
// line positionless). "# pkg" headers and empty lines are skipped,
// other non-position lines keep their raw text in data. tool is
// the tool name recorded in data ("go build", "go vet", "go test")
// and used in the fallback message when a run produced no
// parseable line at all.
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
					if pm, ok := idx.ResolveMatch(file, ln, col); ok {
						dg = dg.At(pm.Span)
						switch pm.Kind {
						case MatchDrift:
							dg = dg.Set("drifted", true)
						case MatchAmbiguous:
							// Candidates go on the wire resolved to
							// line/col like every other position
							// (spec section 12) — raw byte spans
							// would leak the internal offset
							// shape.
							dg = dg.Set("ambiguous", true).
								Set("candidates", resolveSpans(idx, pm.Candidates))
						}
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

// resolveSpans renders candidate spans the way diagnostics carry
// positions — file + line/col, not byte offsets (spec section 12).
// All candidates of one match name the same file, which
// ResolveMatch already resolved to a srcs key.
func resolveSpans(idx *GoPosIndex, spans []diag.Span) []diag.SpanPos {
	out := make([]diag.SpanPos, len(spans))
	for i, sp := range spans {
		li, ok := idx.LineIndex(sp.File)
		if !ok {
			continue // unreachable: a match's spans come from a known input
		}
		out[i] = li.Resolve(sp)
	}
	return out
}
