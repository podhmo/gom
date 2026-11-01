package syntax_test

// Parser fuzz target. The contract under test (docs/decisions/
// 2026-11-02-fuzz-strategy.md): for ANY byte string, Parse and
// ParseInterface must (a) never panic, (b) return a non-nil *File,
// and (c) produce only well-formed diagnostics — each Diagnostic
// passes diag.Diagnostic.Validate and every located span lies inside
// the input. A violation of any of these is a parser bug; the Go
// fuzzer saves the failing input under testdata/fuzz/FuzzParse/.
//
// The seed corpus is every .gom fixture this package already owns
// plus the repository-level spec/tutorial files and handwritten
// adversarial inputs. `go test` runs the seeds deterministically;
// `make check-fuzz` adds a bounded -fuzztime run. Bugs already filed
// in TODO.md live in knownParseBugs (see internal/fuzzutil).

import (
	"fmt"
	"os"
	"runtime/debug"
	"strings"
	"testing"
	"time"

	"github.com/podhmo/gom/internal/diag"
	"github.com/podhmo/gom/internal/fuzzutil"
	"github.com/podhmo/gom/internal/syntax"
)

// knownParseBugs is capped — a growing list means fuzzing finds bugs
// faster than they get fixed, which is itself a signal worth
// surfacing rather than accumulating silently.
const knownBugsCap = 16

// knownParseBugs is currently empty: every parse bug the fuzzer
// found has been fixed (the corpus files stay as regression seeds —
// a re-regression reports a NEW failure signature). New findings go
// here with their TODO.md entry.
var knownParseBugs = map[[32]byte]fuzzutil.KnownBug{}

// parseSeeds collects every .gom file the repo already ships as a
// parse-level fixture, plus adversarial inputs chosen to hit panic
// mode, the lexer and the unclosed-delimiter paths.
func parseSeeds(f *testing.F) [][]byte {
	f.Helper()
	seeds := fuzzutil.ReadSeeds(f,
		"testdata/spec/*.gom",       // parser-phase fixtures
		"../../testdata/spec/*.gom", // full-pipeline fixtures
		"../../testdata/golden/*.gom",
		"../../tutorial/*/main.gom",
		"../../tutorial/*/*.gom",
	)
	adversarial := []string{
		"",                             // empty input
		" ",                            // whitespace only
		"\x00",                         // NUL byte
		"\xff\xfe",                     // invalid UTF-8
		"package",                      // truncated decl
		"fn f (x: Int -> Int = x",      // unclosed param list
		"fn f () -> Int = {",           // unclosed block
		"fn f () -> Int = (1 + 2",      // unclosed paren
		"fn f () -> Int = [1, 2",       // unclosed list
		"type T = | A (x: Int",         // unclosed ctor
		"\"unterminated",               // unclosed string (E0102)
		"fn f () -> Int = \"${",        // unclosed interpolation
		"match x {",                    // unclosed match
		"select {",                     // unclosed select
		"} ) ] | -> =>",                // orphan closers
		";;;",                          // separator run
		"\r\n\r\n",                     // CRLF-only
		"fn f () -> Int = 1 == 2 == 3", // chained comparison (E0101)
		"日本語の識別子 fn 関数名 () -> Int = 1", // non-ASCII idents
		"s${\"${\"${\"\"}\"}\"}",       // nested interpolation
		"fn f () -> Int = " + strings.Repeat("(", 500) + "1" + strings.Repeat(")", 500),
		strings.Repeat("let x = 1\n", 2000), // long token run
		strings.Repeat("{", 4000),           // deep brace run, unclosed
		"package m\n" + strings.Repeat("fn f () -> Int = 0\n", 500),
		// Every token keyword in a row — stresses syncDecl keyword
		// recognition mid-panic-mode.
		"package import go type fn let var match if else for while select try test extern pub use as in of",
	}
	for _, s := range adversarial {
		seeds = append(seeds, []byte(s))
	}
	return seeds
}

// checkParseResult exercises the parse contract on one input and
// returns one failure description per violation found (empty = the
// contract held). parse is the entry under test (Parse or
// ParseInterface), named for failure messages.
func checkParseResult(src []byte, parse func([]byte, string) (*syntax.File, diag.List), entry string) (failures []string) {
	var f *syntax.File
	var diags diag.List
	func() {
		defer func() {
			if r := recover(); r != nil {
				failures = append(failures, fmt.Sprintf("%s panicked\npanic: %v\n%s",
					entry, r, debug.Stack()))
			}
		}()
		f, diags = parse(src, "fuzz.gom")
	}()
	if len(failures) != 0 {
		return failures
	}

	// Parse documents File as always non-nil, errors or not.
	if f == nil {
		failures = append(failures, fmt.Sprintf(
			"%s returned nil File (diagnostics: %v)", entry, diags))
	}
	for i, d := range diags {
		if err := d.Validate(); err != nil {
			failures = append(failures, fmt.Sprintf(
				"%s diagnostic %d fails validation: %v\ndiag: %+v",
				entry, i, err, d))
			continue
		}
		// Located spans must lie inside the input; the zero span
		// (positionless diagnostic) is allowed.
		if !d.Span.IsZero() && (d.Span.End > len(src) || d.Span.Start > len(src)) {
			failures = append(failures, fmt.Sprintf(
				"%s diagnostic %d has out-of-bounds span %v (input length %d)\ndiag: %s",
				entry, i, d.Span, len(src), d.Message))
		}
	}
	return failures
}

// sandboxTimeout bounds one input's turn inside the persistent
// helper subprocess — two parses are a few ms for real inputs; a
// hang is killed here and the child respawned, so nothing leaks
// into this process (see fuzzutil.Sandbox for why in-process
// watchdogs were not enough). The child's `ulimit -v` also absorbs
// allocation blowups: a runaway loop dies on alloc failure instead
// of growing into the worker.
const sandboxTimeout = 3 * time.Second

func FuzzParse(f *testing.F) {
	if len(knownParseBugs) > knownBugsCap {
		f.Fatalf("knownBugs has %d entries, cap is %d", len(knownParseBugs), knownBugsCap)
	}
	for _, seed := range parseSeeds(f) {
		f.Add(seed)
	}
	sb := fuzzutil.NewSandbox("TestFuzzParseSandbox", sandboxTimeout, 2<<20)
	// The parse phases get a tighter bound than the 3 s default: a
	// legit parse is milliseconds even for the largest seeds, and
	// the fixed ctor-pattern hang family taught us a sibling hang
	// would re-time-out on every mutation variant — the cap keeps
	// that cheap per eval if one regresses.
	sb.SetPhaseTimeout("parse", 1500*time.Millisecond)
	sb.SetPhaseTimeout("parse-interface", 1500*time.Millisecond)
	// Suspects (that family's shape) get a further-shrunk cap: the
	// mutator regenerates variants constantly and each costs
	// timeout+respawn — observed whole minutes at 0 execs/sec.
	suspect := map[string]time.Duration{
		"parse":           500 * time.Millisecond,
		"parse-interface": 500 * time.Millisecond,
	}
	defer sb.Close()
	f.Fuzz(func(t *testing.T, src []byte) {
		if bug, ok := fuzzutil.SkipInput(src, knownParseBugs); ok {
			t.Logf("input pinned as unsafe-to-run known bug %q — skipped", bug.Desc)
			return
		}
		var caps map[string]time.Duration
		if fuzzutil.LooksHangSuspect(src) {
			caps = suspect
		}
		fuzzutil.Report(t, src, sb.RunCapped(t, src, caps), knownParseBugs)
	})
}

// TestFuzzParseSandbox is the child side of fuzzutil.Sandbox: it
// loops length-prefixed inputs on stdin and runs the same checks
// as a direct call would, printing PHASE markers and FAIL lines.
// Skipped unless spawned via EnvSandbox.
func TestFuzzParseSandbox(t *testing.T) {
	if os.Getenv(fuzzutil.EnvSandbox) != "1" {
		t.Skip("sandbox helper — spawned by FuzzParse")
	}
	fuzzutil.ServeSandbox(t, func(src []byte) {
		fuzzutil.SandboxPhase("parse")
		for _, s := range checkParseResult(src, syntax.Parse, "Parse") {
			fuzzutil.SandboxFail(s)
		}
		fuzzutil.SandboxPhase("parse-interface")
		for _, s := range checkParseResult(src, syntax.ParseInterface, "ParseInterface") {
			fuzzutil.SandboxFail(s)
		}
	})
}

// TestKnownBugs pins the bookkeeping rules: the map stays under its
// cap and every entry names a TODO.md line that exists — an entry
// without a live task is an orphan.
func TestKnownBugs(t *testing.T) {
	fuzzutil.CheckRegistry(t, knownParseBugs, knownBugsCap, "../../TODO.md")
}
