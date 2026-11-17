package build_test

// Anchor-table inverse tests (docs/decisions/2026-11-06-gotool-
// position-inverse.md): with a unit's generated output and
// SourceMap registered, ResolveMatch inverts Go's //line
// continuation arithmetic — a report on a drifted generated line
// lands on the construct that produced it, a report several
// constructs claim is the honest union, and a report nothing
// claims keeps the naive answer.
//
// Since wave-16's per-line re-pin (decision
// 2026-11-17-sourcemap-repin) every generated line carries its own
// directive, so no line drifts: the remaining ambiguity is real —
// several constructs pinned to the *same* .gom line — and a .gom
// line nothing emits reports unmapped.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/podhmo/gom/internal/build"
	"github.com/podhmo/gom/internal/diag"
	"github.com/podhmo/gom/internal/goimport"
	"github.com/podhmo/gom/internal/lower"
)

// driftSrc expands `fn pair` into five generated lines for four
// source lines. With the per-line re-pin every generated line
// names its own construct's line — the closing `}` reports the
// enclosing function's line, and line 9's three siblings pin the
// same reported line:
//
//	gen 10 `func pair`  → 3     gen 16 `}` of pair → 3
//	gen 12 `tmp1`       → 4     gen 19 `func use`   → 8
//	gen 14 `return …`   → 5     gen 21..25         → 9 (3 siblings)
//	                            gen 27 `z`         → 10
//	                            gen 29 `return z`  → 11
//	                            gen 31 `}` of use  → 8
const driftSrc = `package m

fn pair (x: Int) -> (Int, Int) = {
    let tmp1 = x + 1
    (x, tmp1)
}

fn use () -> Int = {
    let (a, b) = pair 5
    let z = a + b
    z
}
`

// compileDrift compiles src and returns a map-aware index over it.
func compileDrift(t *testing.T, name, src string, opts *build.Options) (*build.GoPosIndex, *build.Result) {
	t.Helper()
	r, err := build.Compile(build.Input{Path: name, Src: []byte(src)}, opts)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if r.Output == nil {
		t.Fatalf("no output: %v", r.Diagnostics)
	}
	idx := build.NewGoPosIndex("", map[string][]byte{name: []byte(src)})
	idx.AddSourceMap(name, r.Output.Source, r.Output.SourceMap)
	return idx, r
}

func TestResolveMatchDrift(t *testing.T) {
	idx, r := compileDrift(t, "m.gom", driftSrc, nil)
	li := diag.NewLineIndex([]byte(driftSrc))
	at := func(line int) diag.Span {
		s, e := li.LineBounds(line)
		return diag.SpanIn("m.gom", s, e)
	}

	// Reported 6: `fn pair`'s real `}` line — no generated line
	// prints it anymore (the `}` pins to its construct at line 3),
	// so the naive line mapping answers, unmapped.
	pm, ok := idx.ResolveMatch("m.gom", 6, 1)
	if !ok || pm.Mapped {
		t.Fatalf("ResolveMatch(6) = %+v, want the unmapped naive answer", pm)
	}
	if pm.Span != diag.Point("m.gom", at(6).Start) {
		t.Errorf("naive span = %v, want the line-6 head", pm.Span)
	}

	// Reported 3 is claimed by `func pair` and by its `}` — both
	// one span, so the answer is that span restricted to line 3.
	pm, ok = idx.ResolveMatch("m.gom", 3, 1)
	if !ok || pm.Kind != build.MatchExact || pm.Span != diag.SpanIn("m.gom", 11, 45) {
		t.Errorf("ResolveMatch(3) = %+v, want exact [11,45)", pm)
	}

	// Reported 12: `fn use`'s real `}` line — nothing prints it
	// (the `}` pins to line 8); the naive answer, unmapped.
	pm, ok = idx.ResolveMatch("m.gom", 12, 1)
	if !ok || pm.Mapped || pm.Span != diag.Point("m.gom", at(12).Start) {
		t.Errorf("ResolveMatch(12) = %+v, want the unmapped line-12 head", pm)
	}

	// Reported 9: three anchored siblings pin it — the `pair 5`
	// call and the `(a, b)` pattern are distinct constructs.
	// Two distinct spans claim the line: the one surviving
	// ambiguity, now genuine rather than drifted.
	pm, ok = idx.ResolveMatch("m.gom", 9, 1)
	if !ok || pm.Kind != build.MatchAmbiguous || !pm.Mapped {
		t.Fatalf("ResolveMatch(9) = %+v, want a mapped ambiguous", pm)
	}
	if len(pm.Candidates) != 2 {
		t.Fatalf("candidates = %v, want the `pair 5` call and the `(a, b)` pattern", pm.Candidates)
	}
	if pm.Span != diag.SpanIn("m.gom", 113, 128) {
		t.Errorf("ambiguous union = %v, want [113,128)", pm.Span)
	}

	// Reported 5: the pinned `return` line alone claims it —
	// on-station, so the answer is the span restricted to line 5.
	pm, ok = idx.ResolveMatch("m.gom", 5, 1)
	if !ok || pm.Kind != build.MatchExact || pm.Span != diag.SpanIn("m.gom", 71, 80) {
		t.Errorf("ResolveMatch(5) = %+v, want exact [71,80)", pm)
	}

	// Reported 7: only a blank generated line prints 7 — nothing
	// a diagnostic could sit on — so the naive line mapping
	// answers, unmapped.
	pm, ok = idx.ResolveMatch("m.gom", 7, 1)
	if !ok || pm.Mapped || pm.Kind != build.MatchExact {
		t.Fatalf("ResolveMatch(7) = %+v, want the naive answer", pm)
	}
	if pm.Span != diag.Point("m.gom", at(7).Start) {
		t.Errorf("naive span = %v, want the line-7 head", pm.Span)
	}

	// An unknown file still reports false; an index without maps
	// keeps the naive behavior verbatim.
	if _, ok := idx.ResolveMatch("other.gom", 1, 1); ok {
		t.Error("ResolveMatch on an unknown file matched")
	}
	bare := build.NewGoPosIndex("", map[string][]byte{"m.gom": []byte(driftSrc)})
	pm, ok = bare.ResolveMatch("m.gom", 6, 1)
	if !ok || pm.Mapped {
		t.Fatalf("unmapped ResolveMatch(6) = %+v, want naive", pm)
	}
	if want, _ := bare.Resolve("m.gom", 6, 1); pm.Span != want {
		t.Errorf("unmapped span = %v, want Resolve's %v", pm.Span, want)
	}
	_ = r
}

// driftSrcGo carries a mid-construct expansion: `ignore
// fmt.Println z` emits four lines that under the per-line re-pin
// all report line 13 — including the `_ = result.FromGo(...)`
// materialization, which previously drifted onto line 14.
const driftSrcGo = `package m

import go "fmt"

fn pair (x: Int) -> (Int, Int) = {
    let tmp1 = x + 1
    (x, tmp1)
}

fn main () -> Unit = {
    let (a, b) = pair 5
    let z = a + b
    ignore fmt.Println z
    ()
}
`

func TestResolveMatchDriftCollision(t *testing.T) {
	gl := goimport.New(&goimport.Config{Dir: "../.."})
	idx, _ := compileDrift(t, "main.gom", driftSrcGo, suiteOptionsWith(gl))

	// Reported 14: only the `()` literal's `return struct{}{}`
	// prints it — the `ignore` construct's four lines all report
	// their own line 13 now. One span claims the line: exact.
	pm, ok := idx.ResolveMatch("main.gom", 14, 1)
	if !ok || pm.Kind != build.MatchExact || !pm.Mapped {
		t.Fatalf("ResolveMatch(14) = %+v, want a mapped exact", pm)
	}
	if pm.Span != diag.SpanIn("main.gom", 195, 197) {
		t.Errorf("exact span = %v, want the `()` literal [195,197)", pm.Span)
	}

	// Reported 13 is claimed by the `ignore` construct's own
	// lines — the materialization and the `fmt.Println` call are
	// distinct spans under one line: the honest union.
	pm, ok = idx.ResolveMatch("main.gom", 13, 1)
	if !ok || pm.Kind != build.MatchAmbiguous || pm.Span != diag.SpanIn("main.gom", 177, 190) {
		t.Errorf("ResolveMatch(13) = %+v, want ambiguous union [177,190)", pm)
	}

	// Reported 8: `fn pair`'s real `}` line — nothing prints it
	// anymore (the `}` reports line 5), so the naive answer.
	pm, ok = idx.ResolveMatch("main.gom", 8, 1)
	if !ok || pm.Mapped {
		t.Errorf("ResolveMatch(8) = %+v, want the unmapped naive answer", pm)
	}

	// Reported 15: `fn main`'s real `}` line — same, unmapped.
	pm, ok = idx.ResolveMatch("main.gom", 15, 1)
	if !ok || pm.Mapped || pm.Span != diag.Point("main.gom", 198) {
		t.Errorf("ResolveMatch(15) = %+v, want the unmapped line-15 head", pm)
	}
}

// TestGoToolDiagnosticsDrift folds tool lines through a map-aware
// index: ambiguous reports mark the diagnostic instead of
// silently picking a line; reports nothing claims keep their
// printed position unflagged.
func TestGoToolDiagnosticsDrift(t *testing.T) {
	idx, _ := compileDrift(t, "m.gom", driftSrc, nil)
	ds := build.GoToolDiagnostics([]string{
		"m.gom:6:1: missing return",
		"m.gom:9:2: declared and not used: a",
		"m.gom:10:1: something else",
		"unpositioned complaint",
	}, "go build", idx)
	if len(ds) != 4 {
		t.Fatalf("diagnostics = %d, want 4: %v", len(ds), ds)
	}
	if ds[0].Span != diag.SpanIn("m.gom", 81, 81) || ds[0].Data["drifted"] != nil || ds[0].Data["ambiguous"] != nil {
		t.Errorf("unclaimed fold = span %v data %v, want the reported point unflagged", ds[0].Span, ds[0].Data)
	}
	if ds[1].Span != diag.SpanIn("m.gom", 113, 128) || ds[1].Data["ambiguous"] != true {
		t.Errorf("ambiguous fold = span %v data %v, want the union + ambiguous", ds[1].Span, ds[1].Data)
	}
	// Candidates serialize the way diagnostics carry positions —
	// resolved file+line/col (spec section 12), not raw offsets.
	cands, ok := ds[1].Data["candidates"].([]diag.SpanPos)
	if !ok || len(cands) != 2 {
		t.Fatalf("ambiguous candidates = %v, want 2 resolved spans", ds[1].Data["candidates"])
	}
	for _, c := range cands {
		if c.File != "m.gom" || c.Start.Line != 9 {
			t.Errorf("candidate = %+v, want an m.gom line-9 span", c)
		}
	}
	if ds[2].Span != diag.SpanIn("m.gom", 141, 146) || ds[2].Data["drifted"] != nil || ds[2].Data["ambiguous"] != nil {
		t.Errorf("exact fold = span %v data %v, want [141,146) unflagged", ds[2].Span, ds[2].Data)
	}
	if ds[3].Span.Valid() {
		t.Errorf("unpositioned line got a span: %v", ds[3].Span)
	}
}

// TestResolveMatchMultiOutput registers a test output alongside the
// main output: a reported line both outputs' anchors claim is
// ambiguous across the pair.
func TestResolveMatchMultiOutput(t *testing.T) {
	src := `package m

fn f () -> Int = 1

test "one" {
    assert_eq (f ()) 1
}
`
	opts := &build.Options{Lower: &lower.Env{Tests: true}}
	r, err := build.Compile(build.Input{Path: "m.gom", Src: []byte(src)}, opts)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if r.Output == nil || r.Output.TestSourceMap == nil {
		t.Fatalf("no test output: %v", r.Diagnostics)
	}
	idx := build.NewGoPosIndex("", map[string][]byte{"m.gom": []byte(src)})
	idx.AddSourceMap("m.gom", r.Output.Source, r.Output.SourceMap)
	idx.AddSourceMap("m.gom", r.Output.TestSource, r.Output.TestSourceMap)

	// The test file's anchors claim .gom lines the main output's
	// anchors claim too — where they disagree the answer is the
	// honest union of both sides, covering every candidate.
	// Sweep every line rather than pinning a fragile golden.
	li := diag.NewLineIndex([]byte(src))
	ambiguous := 0
	for line := 1; line <= li.LineCount(); line++ {
		pm, ok := idx.ResolveMatch("m.gom", line, 1)
		if !ok {
			continue
		}
		if pm.Kind != build.MatchAmbiguous {
			continue
		}
		ambiguous++
		if len(pm.Candidates) < 2 {
			t.Errorf("line %d: ambiguous with %d candidates", line, len(pm.Candidates))
		}
		for _, c := range pm.Candidates {
			if c.Start < pm.Span.Start || c.End > pm.Span.End {
				t.Errorf("line %d: candidate %v escapes the union %v", line, c, pm.Span)
			}
		}
	}
	if ambiguous == 0 {
		t.Error("no line was ambiguous — the second map is not consulted")
	}
	// And the naive resolver still sees a plain index.
	if _, ok := idx.Resolve("m.gom", 3, 1); !ok {
		t.Error("Resolve broke under registered maps")
	}
}

// TestRenderStackDrift: a frame on a reported line resolves to
// its construct's pinned line — under the per-line re-pin every
// printed line already names the construct that emitted it, so a
// line-11 frame stays 11 and an unclaimed line reports untouched.
func TestRenderStackDrift(t *testing.T) {
	idx, _ := compileDrift(t, "m.gom", driftSrc, nil)
	trace := "panic: x\n\nmain.use()\n\tm.gom:11 +0x20\n"
	got := idx.RenderStack(trace)
	if !strings.Contains(got, "\tm.gom:11\n") {
		t.Errorf("RenderStack = %q, want the frame kept on its construct line 11", got)
	}
}

// TestResolveStackDrift: resolved frames carry the match's
// classification — an ambiguous frame says so, an exact frame
// says so, so callers can tell confidence without re-resolving.
func TestResolveStackDrift(t *testing.T) {
	gl := goimport.New(&goimport.Config{Dir: "../.."})
	idx, _ := compileDrift(t, "main.gom", driftSrcGo, suiteOptionsWith(gl))
	trace := "panic: x\n\nmain.f()\n\tmain.gom:13 +0x20\nmain.g()\n\tmain.gom:14 +0x20\n"
	frames := idx.ResolveStack(trace)
	if len(frames) != 2 {
		t.Fatalf("frames = %d, want 2", len(frames))
	}
	if frames[0].Kind != build.MatchAmbiguous || !frames[0].Mapped ||
		frames[0].Span != diag.SpanIn("main.gom", 177, 190) {
		t.Errorf("ambiguous frame = %+v, want the union [177,190)", frames[0])
	}
	if frames[1].Kind != build.MatchExact || !frames[1].Mapped ||
		frames[1].Span != diag.SpanIn("main.gom", 195, 197) {
		t.Errorf("exact frame = %+v, want the `()` literal [195,197)", frames[1])
	}
}

// TestRenderStackAmbiguous: a frame several constructs claim keeps
// the printed position — no guessing at one side — but normalizes
// like a mapped frame (PC trailer dropped), so the two-part shape
// still marks it resolved.
func TestRenderStackAmbiguous(t *testing.T) {
	gl := goimport.New(&goimport.Config{Dir: "../.."})
	idx, _ := compileDrift(t, "main.gom", driftSrcGo, suiteOptionsWith(gl))
	trace := "panic: x\n\nmain.main()\n\tmain.gom:14 +0x20\n"
	got := idx.RenderStack(trace)
	want := "panic: x\n\nmain.main()\n\tmain.gom:14\n"
	if got != want {
		t.Errorf("RenderStack = %q, want %q — printed position kept, PC trailer dropped", got, want)
	}
	if got == trace {
		t.Error("ambiguous frame rendered byte-identical — the two-part marker would never appear")
	}
}

// TestGoBuildDiagnosticsDriftE2E runs the real `go build` on a
// tampered generated file and folds its stderr through the
// map-aware index — the end-to-end proof of the inverse: a report
// printed on a drifted line lands on the construct that produced
// it, and the line claimed by two constructs is honestly
// ambiguous.
func TestGoBuildDiagnosticsDriftE2E(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e: compiles a module and runs go build")
	}
	gobin, err := lookupGo(t)
	if err != nil {
		t.Skip(err)
	}
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	gl := goimport.New(&goimport.Config{Dir: root})
	idx, r := compileDrift(t, "main.gom", driftSrcGo, suiteOptionsWith(gl))
	gen := string(r.Output.Source)

	for _, tt := range []struct {
		name       string
		old, new   string
		wantKind   build.PosMatchKind
		wantSpan   diag.Span
		wantReport string
	}{
		{
			// `missing return` reports on `func pair`'s `}` line —
			// pinned to its own construct now, so the report names
			// `fn pair`'s head line rather than a drifted line.
			name: "missing return on the pinned }",
			old:  "return rt.Tuple2[int64, int64]{V0: x, V1: tmp1}",
			new:  "_ = tmp1",
			// go prints "./main.gom:5: missing return"
			wantKind:   build.MatchExact,
			wantSpan:   diag.SpanIn("main.gom", 28, 62), // `fn pair`'s head
			wantReport: "main.gom:5",
		},
		{
			// The `_ = result.FromGo(...)` materialization reports
			// line 13 like the rest of the `ignore` construct — the
			// line is claimed by two distinct spans, so the honest
			// answer is the union.
			name: "undefined ident on the pinned materialization",
			old:  "_ = result.FromGo(int64(v), err)",
			new:  "_ = nope.FromGo(int64(v), err)",
			// go prints "./main.gom:13: undefined: nope"
			wantKind:   build.MatchAmbiguous,
			wantSpan:   diag.SpanIn("main.gom", 177, 190), // line 13's constructs
			wantReport: "main.gom:13",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			gomod := "module probe\n\ngo 1.27\n\n" +
				"require github.com/podhmo/gom v0.0.0\n\n" +
				"replace github.com/podhmo/gom => " + root + "\n"
			bad := strings.Replace(gen, tt.old, tt.new, 1)
			if bad == gen {
				t.Fatalf("tamper target %q not in generated source", tt.old)
			}
			for name, content := range map[string]string{
				"go.mod":  gomod,
				"main.go": bad,
			} {
				if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			cmd := exec.Command(gobin, "build", ".")
			cmd.Dir = dir
			cmd.Env = append(os.Environ(), "GOWORK=off", "GOFLAGS=-mod=mod")
			start := time.Now()
			out, err := cmd.CombinedOutput()
			t.Logf("go build took %v", time.Since(start))
			if err == nil {
				t.Fatalf("tampered build succeeded:\n%s", bad)
			}
			ds := build.GoToolDiagnostics([]string{string(out)}, "go build", idx)
			var dg *diag.Diagnostic
			for i := range ds {
				if strings.Contains(ds[i].Message, "main.gom:") ||
					(ds[i].Span.Valid() && ds[i].Span.File == "main.gom") {
					dg = &ds[i]
					break
				}
			}
			if dg == nil {
				t.Fatalf("no .gom-positioned diagnostic in %v\noutput:\n%s", ds, out)
			}
			if !strings.Contains(string(out), tt.wantReport+":") {
				t.Fatalf("expected report %q absent from tool output:\n%s", tt.wantReport, out)
			}
			if dg.Span != tt.wantSpan {
				t.Errorf("folded span = %v, want %v", dg.Span, tt.wantSpan)
			}
			switch tt.wantKind {
			case build.MatchDrift:
				if dg.Data["drifted"] != true {
					t.Errorf("drifted flag = %v", dg.Data)
				}
			case build.MatchAmbiguous:
				if dg.Data["ambiguous"] != true {
					t.Errorf("ambiguous flag = %v", dg.Data)
				}
			}
			// Contrast: the naive mapping would land somewhere else.
			if naive, ok := idx.Resolve("main.gom", mustLine(t, tt.wantReport), 1); ok && naive == dg.Span {
				t.Errorf("naive and table answers agree — the drift is not exercised")
			}
		})
	}
}

// mustLine extracts the line number from a "file:N" report string.
func mustLine(t *testing.T, report string) int {
	t.Helper()
	_, ln, ok := diag.ParseStackPos(report)
	if !ok {
		t.Fatalf("bad report %q", report)
	}
	return ln
}
