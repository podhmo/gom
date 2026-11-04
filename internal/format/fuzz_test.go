package format_test

// Formatter fuzz target. The contract under test: for ANY byte
// string, format.Source must never panic; when the input parses it
// must (a) produce output that reparses without error diagnostics,
// (b) satisfy idempotency Source(Source(x)) == Source(x), and (c)
// preserve the comment multiset — every comment in the input appears
// in the output with the same text. A violation is a formatter bug;
// the Go fuzzer saves the failing input under testdata/fuzz/FuzzFormat/.
//
// The harness mirrors internal/syntax's FuzzParse: inputs run inside
// fuzzutil.Sandbox so a hang or runaway allocation dies in the child
// rather than taking down the worker.

import (
	"fmt"
	"os"
	"runtime/debug"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/podhmo/gom/internal/diag"
	"github.com/podhmo/gom/internal/format"
	"github.com/podhmo/gom/internal/fuzzutil"
	"github.com/podhmo/gom/internal/syntax"
)

// knownFormatBugs pins filed bugs to their TODO.md entry (see
// internal/fuzzutil). Idempotence findings share the single
// "Source is not idempotent" failure line, so this signature
// matches every input variant of the same class — a NEW
// non-idempotent shape should get its own registry entry rather
// than riding this one.
var knownFormatBugs = map[[32]byte]fuzzutil.KnownBug{
	fuzzutil.Sum256Hex("092f2beca713f4aabfa8eadbed6aa052621d7da4512af83e682b3955096fa893"): {
		Match: []string{"Source is not idempotent"},
		TODO:  "適用後セレクタが非冪等",
		Desc:  "formatter emits `(0 ()).A` for `0 ().A` — pass 2 rewrites it again; not idempotent (issue #12)",
	},
}

const formatBugsCap = 16

// checkFormatResult exercises the formatter contract on one input
// and returns one failure description per violation (empty = held).
func checkFormatResult(src []byte, iface bool) (failures []string) {
	entry := "Source"
	if iface {
		entry = "Source(interface)"
	}
	var out []byte
	var diags diag.List
	func() {
		defer func() {
			if r := recover(); r != nil {
				failures = append(failures, fmt.Sprintf("%s panicked\npanic: %v\n%s",
					entry, r, debug.Stack()))
			}
		}()
		out, diags = format.Source(src, "fuzz.gom", iface)
	}()
	if len(failures) != 0 {
		return failures
	}
	if diags.HasError() {
		// Unparseable input is legal output — nothing else to check.
		return nil
	}

	// (a) The output reparses cleanly.
	var d2 diag.List
	if iface {
		_, d2 = syntax.ParseInterface(out, "fuzz.gom")
	} else {
		_, d2 = syntax.Parse(out, "fuzz.gom")
	}
	if d2.HasError() {
		failures = append(failures, fmt.Sprintf(
			"%s output does not reparse: %v\n--- output ---\n%s", entry, d2, out))
		return failures
	}

	// (b) Idempotent.
	out2, _ := format.Source(out, "fuzz.gom", iface)
	if string(out2) != string(out) {
		failures = append(failures, fmt.Sprintf(
			"%s is not idempotent\n--- pass 1 ---\n%s\n--- pass 2 ---\n%s",
			entry, out, out2))
	}

	// (c) Comments survive with their text (same multiset — the
	// formatter may re-anchor but never drops or rewrites one).
	if in, got := commentTexts(src, iface), commentTexts(out, iface); !sameMultiset(in, got) {
		failures = append(failures, fmt.Sprintf(
			"%s changed comments\nin:  %q\nout: %q", entry, in, got))
	}
	return failures
}

// commentTexts returns the normalized text of every comment in src,
// in source order. Kind is ignored: the formatter may rewrite a //
// comment in an expression gap as /* */ (a comment there cannot
// survive as // because the reflowed line no longer ends where it
// did), and that rewrite pads its text and must neutralize any "*/"
// inside it ("* /") so the block comment does not terminate early.
// Whitespace is therefore stripped before comparing — the comment's
// words and punctuation are what must survive.
func commentTexts(src []byte, iface bool) []string {
	var f *syntax.File
	var diags diag.List
	if iface {
		f, diags = syntax.ParseInterface(src, "fuzz.gom")
	} else {
		f, diags = syntax.Parse(src, "fuzz.gom")
	}
	if diags.HasError() {
		return nil
	}
	var out []string
	for _, c := range f.Comments {
		out = append(out, strings.Join(strings.Fields(c.Text), ""))
	}
	return out
}

// sameMultiset compares two string slices as multisets.
func sameMultiset(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	as, bs := append([]string(nil), a...), append([]string(nil), b...)
	sort.Strings(as)
	sort.Strings(bs)
	for i := range as {
		if as[i] != bs[i] {
			return false
		}
	}
	return true
}

const fmtSandboxTimeout = 3 * time.Second

func FuzzFormat(f *testing.F) {
	if len(knownFormatBugs) > formatBugsCap {
		f.Fatalf("knownBugs has %d entries, cap is %d", len(knownFormatBugs), formatBugsCap)
	}
	seeds := fuzzutil.ReadSeeds(f,
		"../../testdata/fmt/*.gom",
		"../../testdata/fmt/*.gomi",
		"../../testdata/spec/*.gom",
		"../../testdata/golden/*.gom",
		"../../tutorial/*/main.gom",
		"../../tutorial/*/*.gom",
	)
	adversarial := []string{
		"",
		"package m\n",
		"package m\nfn f () -> Int = 1\n",
		"package m\nfn f () -> Int = -1\n\nfn g () -> Int = f () - 1\n",
		"package m\nfn f () -> Int =\n  match 1 {\n    -1 -> 1\n    _ -> 0\n  }\n",
		"package m\nlet x = { y = { z = { w = 1 } } }\n",
		"package m\n// only a comment\nfn f () -> Int = 1 // trailing\n",
	}
	for _, s := range adversarial {
		seeds = append(seeds, []byte(s))
	}
	for _, seed := range seeds {
		f.Add(seed)
	}
	sb := fuzzutil.NewSandbox("TestFuzzFormatSandbox", fmtSandboxTimeout, 2<<20)
	defer sb.Close()
	f.Fuzz(func(t *testing.T, src []byte) {
		if bug, ok := fuzzutil.SkipInput(src, knownFormatBugs); ok {
			t.Logf("input pinned as unsafe-to-run known bug %q — skipped", bug.Desc)
			return
		}
		fuzzutil.Report(t, src, sb.RunCapped(t, src, nil), knownFormatBugs)
	})
}

// TestFuzzFormatSandbox is the child side of fuzzutil.Sandbox.
func TestFuzzFormatSandbox(t *testing.T) {
	if os.Getenv(fuzzutil.EnvSandbox) != "1" {
		t.Skip("sandbox helper — spawned by FuzzFormat")
	}
	fuzzutil.ServeSandbox(t, func(src []byte) {
		fuzzutil.SandboxPhase("format")
		for _, s := range checkFormatResult(src, false) {
			fuzzutil.SandboxFail(s)
		}
		fuzzutil.SandboxPhase("format-interface")
		for _, s := range checkFormatResult(src, true) {
			fuzzutil.SandboxFail(s)
		}
	})
}

func TestKnownFormatBugs(t *testing.T) {
	fuzzutil.CheckRegistry(t, knownFormatBugs, formatBugsCap, "../../TODO.md")
}
