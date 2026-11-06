package build_test

// Anchor-table inverse tests (docs/decisions/2026-11-06-gotool-
// position-inverse.md): with a unit's generated output and
// SourceMap registered, ResolveMatch inverts Go's //line
// continuation arithmetic — a report on a drifted generated line
// lands on the construct that produced it, a report several
// constructs claim is the honest union, and a report nothing
// claims keeps the naive answer.

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
// source lines: each function's closing `}` drifts one line past
// its construct's last pinned line. The pinned generated layout:
//
//	gen 10 `func pair`  → 3     gen 15 `}` of pair → 6 (drift)
//	gen 12 `tmp1`       → 4     gen 16 (blank)      → 7
//	gen 14 `return …`   → 5     gen 18 `func use`   → 8
//	                            gen 20..24         → 9 (3 siblings)
//	                            gen 26 `z`         → 10
//	                            gen 28 `return z`  → 11
//	                            gen 29 `}` of use  → 12 (drift)
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

func lineOf(t *testing.T, src string, off int) int {
	t.Helper()
	return diag.NewLineIndex([]byte(src)).Pos(off).Line
}

func TestResolveMatchDrift(t *testing.T) {
	idx, r := compileDrift(t, "m.gom", driftSrc, nil)
	li := diag.NewLineIndex([]byte(driftSrc))
	at := func(line int) diag.Span {
		s, e := li.LineBounds(line)
		return diag.SpanIn("m.gom", s, e)
	}

	// Reported 6: only `fn pair`'s closing `}` prints 6 — drifted
	// past the `(x, tmp1)` construct it belongs to. The naive
	// mapping would land on .gom line 6, pair's real `}` line;
	// the table lands inside the construct.
	pm, ok := idx.ResolveMatch("m.gom", 6, 1)
	if !ok || pm.Kind != build.MatchDrift || !pm.Mapped {
		t.Fatalf("ResolveMatch(6) = %+v, want a mapped drift", pm)
	}
	if pm.Span != diag.SpanIn("m.gom", 71, 80) {
		t.Errorf("drift span = %v, want the `(x, tmp1)` expr [71,80)", pm.Span)
	}
	if lineOf(t, driftSrc, pm.Span.Start) != 5 {
		t.Errorf("drift answer lands on line %d, want 5", lineOf(t, driftSrc, pm.Span.Start))
	}
	naive, _ := idx.Resolve("m.gom", 6, 1)
	if naive == pm.Span {
		t.Error("naive and table answers agree — the drift shape is not exercised")
	}

	// Reported 12: `fn use`'s `}` — same shape, landing on the
	// `z` expr at line 11 rather than line 12's real `}`.
	pm, ok = idx.ResolveMatch("m.gom", 12, 1)
	if !ok || pm.Kind != build.MatchDrift || pm.Span != diag.SpanIn("m.gom", 151, 152) {
		t.Errorf("ResolveMatch(12) = %+v, want drift to the `z` expr", pm)
	}

	// Reported 9: three anchored siblings pin it — the `pair 5`
	// call and the `(a, b)` pattern are distinct constructs —
	// and the pin-8 header's region drifts a directive line onto
	// it (filtered). Two distinct spans claim the line.
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

// driftSrcGo carries the mid-construct drift: `ignore fmt.Println z`
// emits four anchored lines plus the unanchored
// `_ = result.FromGo(...)` materialization — a generated code line
// that reports the NEXT construct's line.
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

	// Reported 14 is claimed by two real code lines: the
	// unanchored `_ = result.FromGo(...)` — belonging to line
	// 13's `ignore` construct — drifting onto 14, and the
	// `return struct{}{}` pinned to it. The naive answer picks
	// the line-14 point silently; the table answers the union
	// of both constructs and says so.
	pm, ok := idx.ResolveMatch("main.gom", 14, 1)
	if !ok || pm.Kind != build.MatchAmbiguous || !pm.Mapped {
		t.Fatalf("ResolveMatch(14) = %+v, want a mapped ambiguous", pm)
	}
	if len(pm.Candidates) != 2 {
		t.Fatalf("candidates = %v, want the ignore construct and the `()`", pm.Candidates)
	}
	if pm.Span.Start != 177 || pm.Span.End != 197 {
		t.Errorf("union = %v, want [177,197) covering lines 13-14", pm.Span)
	}
	naive, _ := idx.Resolve("main.gom", 14, 1)
	if lineOf(t, driftSrcGo, naive.Start) != 14 {
		t.Fatalf("naive answer = %v — expected the line-14 point for contrast", naive)
	}

	// Reported 8: `fn pair`'s `}` drifts onto the real line 8 —
	// no other construct claims it, so the answer is the
	// enclosing construct's span rather than the `}` line.
	pm, ok = idx.ResolveMatch("main.gom", 8, 1)
	if !ok || pm.Kind != build.MatchDrift || pm.Span != diag.SpanIn("main.gom", 88, 97) {
		t.Errorf("ResolveMatch(8) = %+v, want drift to `(x, tmp1)`", pm)
	}

	// Reported 15: `fn main`'s `}` alone claims it — drift to the
	// `()` literal rather than line 15's `}`.
	pm, ok = idx.ResolveMatch("main.gom", 15, 1)
	if !ok || pm.Kind != build.MatchDrift || pm.Span != diag.SpanIn("main.gom", 195, 197) {
		t.Errorf("ResolveMatch(15) = %+v, want drift to `()`", pm)
	}
}

// TestGoToolDiagnosticsDrift folds tool lines through a map-aware
// index: drifted and ambiguous reports mark the diagnostic instead
// of silently picking a line.
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
	if ds[0].Span != diag.SpanIn("m.gom", 71, 80) || ds[0].Data["drifted"] != true {
		t.Errorf("drifted fold = span %v data %v, want [71,80) + drifted", ds[0].Span, ds[0].Data)
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

// TestRenderStackDrift: a frame whose printed line is drifted
// resolves to its construct's line rather than the wrong-but-real
// line the trace printed.
func TestRenderStackDrift(t *testing.T) {
	idx, _ := compileDrift(t, "m.gom", driftSrc, nil)
	trace := "panic: x\n\nmain.use()\n\tm.gom:12 +0x20\n"
	got := idx.RenderStack(trace)
	if !strings.Contains(got, "\tm.gom:11\n") {
		t.Errorf("RenderStack = %q, want the drifted frame rewritten to its construct line 11", got)
	}
}

// TestResolveStackDrift: resolved frames carry the match's
// classification — a drifted frame says drift, an ambiguous one
// says so, so callers can tell confidence without re-resolving.
func TestResolveStackDrift(t *testing.T) {
	gl := goimport.New(&goimport.Config{Dir: "../.."})
	idx, _ := compileDrift(t, "main.gom", driftSrcGo, suiteOptionsWith(gl))
	trace := "panic: x\n\nmain.f()\n\tmain.gom:8 +0x20\nmain.g()\n\tmain.gom:14 +0x20\n"
	frames := idx.ResolveStack(trace)
	if len(frames) != 2 {
		t.Fatalf("frames = %d, want 2", len(frames))
	}
	if frames[0].Kind != build.MatchDrift || !frames[0].Mapped ||
		frames[0].Span != diag.SpanIn("main.gom", 88, 97) {
		t.Errorf("drifted frame = %+v, want drift to [88,97)", frames[0])
	}
	if frames[1].Kind != build.MatchAmbiguous || !frames[1].Mapped ||
		frames[1].Span != diag.SpanIn("main.gom", 177, 197) {
		t.Errorf("ambiguous frame = %+v, want the union [177,197)", frames[1])
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
			// The `}` of `fn pair` drifts onto .gom line 8 — a real
			// line inside the same decl. `missing return` reports
			// there; the match lands inside the construct it
			// belongs to instead of the `}` line the naive
			// mapping would point at.
			name: "missing return on a drifted }",
			old:  "return rt.Tuple2[int64, int64]{V0: x, V1: tmp1}",
			new:  "_ = tmp1",
			// go prints "./main.gom:8: missing return"
			wantKind:   build.MatchDrift,
			wantSpan:   diag.SpanIn("main.gom", 88, 97), // `(x, tmp1)`
			wantReport: "main.gom:8",
		},
		{
			// The unanchored `_ = result.FromGo(...)` — line 13's
			// `ignore` tail — drifts onto line 14, which the `()`
			// literal pins. The naive answer blames the line-14
			// construct for an error sitting in line 13's tail;
			// the honest answer covers both.
			name: "undefined ident on the drifted materialization",
			old:  "_ = result.FromGo(int64(v), err)",
			new:  "_ = nope.FromGo(int64(v), err)",
			// go prints "./main.gom:14: undefined: nope"
			wantKind:   build.MatchAmbiguous,
			wantSpan:   diag.SpanIn("main.gom", 177, 197), // lines 13-14
			wantReport: "main.gom:14",
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
