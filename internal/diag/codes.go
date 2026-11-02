package diag

//go:generate go test -run TestErrorCodesDocInSync -update

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
)

// Phase identifies the compiler component responsible for emitting a
// diagnostic code. It names the internal package (or the driver / lint
// passes) so that the table can point readers at the implementation that
// produces the diagnostic. It is not part of the wire format.
type Phase string

const (
	// PhaseDriver covers diagnostics emitted by cmd/gom and
	// internal/build, including Go toolchain diagnostics mapped back to
	// .gom positions ("source": "go", spec section 12 item 6).
	PhaseDriver Phase = "driver"
	// PhaseSyntax covers lexing and parsing (internal/syntax).
	PhaseSyntax Phase = "syntax"
	// PhaseResolve covers name resolution (internal/resolve).
	PhaseResolve Phase = "resolve"
	// PhaseCheck covers type checking (internal/check).
	PhaseCheck Phase = "check"
	// PhasePattern covers exhaustiveness and redundancy
	// (internal/pattern).
	PhasePattern Phase = "pattern"
	// PhaseLower covers desugaring to Core IR (internal/lower).
	PhaseLower Phase = "lower"
	// PhaseCodegen covers Go code generation (internal/codegen).
	PhaseCodegen Phase = "codegen"
	// PhaseIface covers .gomi interface files and the
	// implementation/interface comparison (internal/iface).
	PhaseIface Phase = "iface"
	// PhaseGoImport covers the Go-package import machinery
	// (internal/goimport).
	PhaseGoImport Phase = "goimport"
	// PhaseLint covers opt-in lints such as the mutable-sharing
	// warnings of spec section 10 (P5-2).
	PhaseLint Phase = "lint"
	// PhaseEval covers the typed-AST evaluator (internal/eval), the
	// tree-walking interpreter behind the semantics-comparison
	// testing of docs/design.md section 7.
	PhaseEval Phase = "eval"
)

// phases is the closed set of valid Phase values, in display order.
var phases = []Phase{
	PhaseDriver,
	PhaseSyntax,
	PhaseResolve,
	PhaseCheck,
	PhasePattern,
	PhaseLower,
	PhaseCodegen,
	PhaseIface,
	PhaseGoImport,
	PhaseLint,
	PhaseEval,
}

// validPhase reports whether p is one of the declared phases.
func validPhase(p Phase) bool {
	return slices.Contains(phases, p)
}

// Category groups the codes that share a two-digit range. The middle
// two digits of a code name the category; the letter gives the severity
// and the last two digits are a serial inside the category
// (LCCNN — see docs/decisions/2026-11-02-error-code-table.md).
type Category struct {
	// Number is the two-digit category prefix, e.g. "03".
	Number string
	// Title is a short English name, e.g. "Pattern matching".
	Title string
	// Blurb is a one-sentence description of the category's scope.
	Blurb string
}

// categories is the closed set of valid categories, in numeric order.
// New categories are appended (up to "99") as the language grows; a
// category number never changes meaning.
var categories = []Category{
	{"00", "Compiler and toolchain",
		"Driver-level diagnostics: failures of the compiler itself and Go toolchain messages mapped back to .gom sources."},
	{"01", "Syntax",
		"Lexing and parsing errors of .gom and .gomi sources (internal/syntax)."},
	{"02", "Name resolution",
		"Undefined or duplicate names, imports, and attributes (internal/resolve)."},
	{"03", "Pattern matching",
		"Exhaustiveness, redundancy, and pattern-shape rules of match, let, and function parameters (internal/pattern)."},
	{"04", "Type checking",
		"Unification failures and the expression-level typing rules of the spec (internal/check)."},
	{"05", "Go interop",
		"Go calls, the `_` placeholder, spreads, conversions, and symbols that Gom cannot represent (internal/check and internal/goimport)."},
	{"06", "Packages and interfaces",
		"Package layout, .gomi presence and contents, and implementation-to-interface agreement (internal/iface, internal/build)."},
	{"07", "Concurrency",
		"The go/select/channel rules of spec section 10 and the mutable-sharing lints."},
	{"08", "Tests",
		"Test declarations, the @test scope, and assertions (spec section 11)."},
	{"90", "Evaluation",
		"Run-time diagnostics of the typed-AST evaluator (internal/eval): unsupported constructs, panics, and broken interpreter invariants."},
}

// categoryNumbers returns the valid category prefixes for validation.
func categoryNumbers() map[string]bool {
	m := make(map[string]bool, len(categories))
	for _, c := range categories {
		m[c.Number] = true
	}
	return m
}

// Categories returns the defined code categories in numeric order.
func Categories() []Category {
	return slices.Clone(categories)
}

// CodeInfo is one entry of the error-code table: the metadata attached
// to a stable diagnostic code. The table is the single source of truth
// read by `gom explain` (spec section 12, kind "explanation"), rendered
// into docs/error-codes.md, and consulted by `gom schema`.
type CodeInfo struct {
	// Code is the stable identifier, e.g. "E0301".
	Code Code
	// Severity is the severity diagnostics with this code are emitted
	// with. It must agree with the code's leading letter
	// (E = error, W = warning, N = note).
	Severity Severity
	// Title is a one-line English description of the diagnostic.
	Title string
	// Explanation is the longer prose shown by `gom explain <code>`:
	// what the rule is, why the compiler reports it, and how to fix
	// the code. Markdown formatting is allowed.
	Explanation string
	// Phase is the compiler component that emits the diagnostic.
	Phase Phase
	// Spec optionally names the spec.md section that defines the rule,
	// e.g. "4" or "8.2".
	Spec string
	// Example is an optional .gom fragment that triggers the
	// diagnostic, shown by `gom explain`.
	Example string
	// Retired marks a code that is no longer emitted. Retired codes
	// stay in the table forever so their numbers are never reused
	// (spec section 12 item 2).
	Retired bool
}

// codeRE is the code format: a severity letter, a two-digit category,
// and a two-digit serial (LCCNN).
var codeRE = regexp.MustCompile(`^[EWN][0-9]{4}$`)

// specRE validates the optional Spec field ("4", "8.2", ...).
var specRE = regexp.MustCompile(`^[0-9]+(\.[0-9]+)*$`)

// WellFormed reports whether c has the LCCNN code shape. Well-formedness
// says nothing about registration: use LookupCode for that.
func WellFormed(c Code) bool {
	return codeRE.MatchString(string(c))
}

// expectedSeverity maps the code's leading letter to its severity.
func expectedSeverity(c Code) Severity {
	switch string(c)[0] {
	case 'E':
		return SeverityError
	case 'W':
		return SeverityWarning
	case 'N':
		return SeverityNote
	default:
		return 0
	}
}

// codeTable is the registry of every diagnostic code, sorted by code.
// When adding a code, take the lowest free serial in the right
// category; never reuse a number, even after its diagnostic is
// removed — mark the entry Retired instead.
var codeTable = []CodeInfo{
	{
		Code:     "E0001",
		Severity: SeverityError,
		Title:    "Go toolchain reported an error",
		Phase:    PhaseDriver,
		Spec:     "12",
		Explanation: `A command from the Go toolchain (go build, go vet, go test) failed
on code generated from a .gom source. The diagnostic is mapped back to
the .gom position and marked with "source": "go" so it can be told apart
from diagnostics produced by gom itself (spec section 12 item 6). The
original tool output is carried in the diagnostic's structured data.

This usually indicates a compiler bug rather than a mistake in the .gom
source: generated code is expected to always pass Go's own checks.`,
	},

	{
		Code:     "E0101",
		Severity: SeverityError,
		Title:    "syntax error",
		Phase:    PhaseSyntax,
		Explanation: `The parser encountered a token that cannot appear at this position
— the generic fallback for inputs that do not match the grammar
(docs/grammar.ebnf). The structured data carries the found token and,
when known, the set of expected tokens. Frequent causes are a "{"
placed on the line after a control-expression head (the newline rules
of spec section 1.5 require it on the same line, as in Go), a match arm
whose "->" starts a new line, and characters such as "?", "!", "&" or
"#" that are not Gom tokens.`,
		Example: `fn f (x: Int) -> Int =
  if x > 0
  { x }            // "{" must stay on the condition's line (spec 1.5)
  else { 0 }`,
	},
	{
		Code:     "E0102",
		Severity: SeverityError,
		Title:    "unterminated string literal",
		Phase:    PhaseSyntax,
		Explanation: `A string literal was not closed by a '"' on the same line, or a
"${" interpolation inside it was never closed by a "}". Multi-line
strings do not exist; write an explicit escape or concatenate.`,
		Example: `let msg = "hello, world`,
	},
	{
		Code:     "E0103",
		Severity: SeverityError,
		Title:    "unterminated block comment",
		Phase:    PhaseSyntax,
		Explanation: `A "/*" comment was not closed by "*/" before the end of the file.
Block comments do not nest.`,
		Example: `/* this comment is never closed
let x = 1`,
	},
	{
		Code:     "E0104",
		Severity: SeverityError,
		Title:    "invalid escape sequence",
		Phase:    PhaseSyntax,
		Explanation: `The backslash sequence in a character or string literal is not one
of the escapes the lexer accepts (docs/grammar.ebnf section 2.3).
Supported escapes include \n, \t, \r, \0, \\, \', \", \$, \uXXXX and
\u{X...}.`,
		Example: `let s = "column\qvalue"`,
	},
	{
		Code:     "E0105",
		Severity: SeverityError,
		Title:    "invalid byte in literal",
		Phase:    PhaseSyntax,
		Explanation: `A string or character literal contains a byte that cannot appear
in generated Go source: an invalid UTF-8 byte, or the NUL byte. Both
would be emitted verbatim and rejected by the Go toolchain, so the
literal is refused at lex time. Write an escape instead: \0 for NUL
and \uXXXX or \u{X...} for code points — source text must be valid
UTF-8, so arbitrary bytes cannot be spelled in a literal.`,
		Example: `let s = "<0xEB>"`,
	},

	{
		Code:     "E0201",
		Severity: SeverityError,
		Title:    "undefined name",
		Phase:    PhaseResolve,
		Spec:     "2",
		Explanation: `The name does not resolve to any variable, constructor, type, or
module in scope. This also covers the label punning form "~name",
which is defined to reference a variable of the same name (spec
section 2.7): "~to" without a variable named "to" reports "undefined
variable ` + "`to`" + `". The same code covers wrong-kind uses that
have no legitimate reading — a name that resolves to a namespace
where a value was written: a module is only valid as the qualifier
of ` + "`mod.sel`" + `, so a module name in expression or callee
position reports "` + "`fmt` is a module, not a value" + `" rather
than an internal-invariant diagnostic downstream.`,
		Example: `transfer alice ~to ~amount: 100   // error when no variable
                                    // named "to" is in scope`,
	},
	{
		Code:     "E0202",
		Severity: SeverityError,
		Title:    "duplicate definition",
		Phase:    PhaseResolve,
		Spec:     "11",
		Explanation: `Two top-level definitions in one package use the same name.
Definitions marked @test share the package namespace, so a test
definition may not reuse the name of an ordinary definition either
(spec section 11.3).`,
		Example: `fn area (s: Shape) -> Float = ...
fn area (s: Shape) -> Float = ...   // duplicate`,
	},
	{
		Code:     "E0203",
		Severity: SeverityError,
		Title:    "cannot resolve import",
		Phase:    PhaseResolve,
		Spec:     "8",
		Explanation: `An "import" declaration names a package that cannot be loaded:
a Gom package whose .gomi is missing or unreadable, or a Go package
that the toolchain cannot find. Symbols inside an importable Go
package that Gom cannot represent are reported separately (E0501);
"import go" itself succeeds for them (spec section 8.5).`,
		Example: `import go "example.com/no/such/pkg"`,
	},
	{
		Code:     "E0204",
		Severity: SeverityError,
		Title:    "unknown attribute",
		Phase:    PhaseResolve,
		Spec:     "11",
		Explanation: `An "@name" attribute other than the defined "@test" was used.
The grammar accepts attributes syntactically and rejects unknown ones
during name resolution (docs/grammar.ebnf section 1).`,
		Example: `@memo
fn fib (n: Int) -> Int = ...`,
	},

	{
		Code:     "E0301",
		Severity: SeverityError,
		Title:    "non-exhaustive match",
		Phase:    PhasePattern,
		Spec:     "4",
		Explanation: `The match arms do not cover every value of the scrutinee type.
The diagnostic lists up to five counterexample patterns
("missing_patterns" in the structured data) and usually offers a
placeholder fix that adds arms with "todo ()" bodies. Arms with guards
never count toward exhaustiveness: a guarded arm covers nothing the
checker can rely on. Patterns over infinite types (Int, String) need
a wildcard or variable arm.`,
		Example: `fn is_some (o: Option Int) -> Bool =
  match o {
    Some _ -> true
    // None is not covered
  }`,
	},
	{
		Code:     "E0302",
		Severity: SeverityError,
		Title:    "unreachable match arm",
		Phase:    PhasePattern,
		Spec:     "4",
		Explanation: `The arm can never match: earlier arms already cover every value
it would accept. Guarded arms are exempt — a guard may fail, so the
arm can still be reached. Remove the arm or reorder it before the
arms that shadow it.`,
		Example: `match o {
  _ -> 0
  Some x -> x   // unreachable: the wildcard already covers Some x
}`,
	},
	{
		Code:     "E0303",
		Severity: SeverityError,
		Title:    "refutable pattern in an irrefutable position",
		Phase:    PhasePattern,
		Spec:     "4",
		Explanation: `Positions that must always bind — the left side of "let" and
"for ... in", and function parameters — accept only irrefutable
patterns (spec section 1.1.5): variables, wildcards, tuples, record
destructuring, and single-constructor types. A pattern that can fail
(a constructor of a multi-variant type, a literal, a non-empty list)
must be matched with "match" instead.`,
		Example: `let Some x = option_value   // Some can fail; use match`,
	},
	{
		Code:     "E0304",
		Severity: SeverityError,
		Title:    "or-pattern alternatives bind different variables",
		Phase:    PhasePattern,
		Spec:     "4",
		Explanation: `Every alternative of an or-pattern ("p | q") must bind the same
set of variables with the same types, so the arm body sees identical
bindings whichever alternative matched. Give the differing variables
the same name and type, or split the arm.`,
		Example: `match s {
  Circle r | Rect w h -> ...   // r vs w, h: different bindings
}`,
	},
	{
		Code:     "E0305",
		Severity: SeverityError,
		Title:    "non-exhaustive select (retired)",
		Phase:    PhasePattern,
		Retired:  true,
		Explanation: `This code was reserved for a select-specific exhaustiveness
check. It is retired and will never be reassigned: select receive
arms bind Option values and branch in their bodies with ordinary
match expressions, so coverage is reported by E0301 instead
(decisions/2026-11-01-select-receive-arms.md).`,
	},

	{
		Code:     "E0401",
		Severity: SeverityError,
		Title:    "type mismatch",
		Phase:    PhaseCheck,
		Explanation: `The generic unification failure: an expression's inferred type
cannot be made equal to the type its context requires. This covers
wrong operand and argument types, arms of if/match/select that
disagree, "|?>" pipelines whose value or step is not a matching
Result, and Go assignments that violate the interop typing rules.
The structured data carries the expected and actual types.`,
		Example: `let x = 1 + "a"`,
	},
	{
		Code:     "E0402",
		Severity: SeverityError,
		Title:    "non-Unit expression in a block",
		Phase:    PhaseCheck,
		Spec:     "1.1",
		Explanation: `Every expression in a block except the last one must have type
Unit (spec section 1.1.2); a non-Unit value produced mid-block is a
compile error because silently dropping results hides bugs. To
discard a value deliberately, write "ignore e".`,
		Example: `fn main () -> Unit = {
  let xs = [1, 2, 3]
  List.map double xs   // computed and dropped: write "ignore (...)"
  ()
}`,
	},
	{
		Code:     "E0403",
		Severity: SeverityError,
		Title:    "missing else branch",
		Phase:    PhaseCheck,
		Spec:     "1.1",
		Explanation: `"else" may be omitted only when the "if" branch has type Unit
(spec section 1.1.3). An if expression that produces a value must
provide both branches.`,
		Example: `let sign = if x > 0 { 1 }   // needs "else { ... }"`,
	},
	{
		Code:     "E0404",
		Severity: SeverityError,
		Title:    "missing labeled argument",
		Phase:    PhaseCheck,
		Spec:     "2",
		Explanation: `The callee declares a labeled required argument ("~name: T")
that the call does not supply. Labeled arguments may be written in
any order but may not be omitted; partial application supplies some
labels now and leaves the rest in the resulting function type.`,
		Example: `transfer alice ~to: bob          // ~amount: is missing`,
	},
	{
		Code:        "E0405",
		Severity:    SeverityError,
		Title:       "duplicate labeled argument",
		Phase:       PhaseCheck,
		Spec:        "2",
		Explanation: `The same label was supplied twice in one call.`,
		Example:     `transfer alice ~to: bob ~to: carol ~amount: 100`,
	},
	{
		Code:     "E0406",
		Severity: SeverityError,
		Title:    "unknown label",
		Phase:    PhaseCheck,
		Spec:     "2",
		Explanation: `The call passes a "~name:" argument that the callee does not
declare. Check the spelling against the function's signature (or the
.gomi entry for imported functions).`,
		Example: `transfer alice ~to: bob ~amont: 100   // ~amont is not declared`,
	},
	{
		Code:     "E0407",
		Severity: SeverityError,
		Title:    "type annotation required",
		Phase:    PhaseCheck,
		Spec:     "3.3",
		Explanation: `Arithmetic operators are overloaded over the built-in numeric
types and are resolved to a concrete type at the end of type checking
(spec section 3.3). When nothing fixes the type, the compiler asks
for an annotation rather than guessing.`,
		Example: `let x = read_number () + 1   // + stays ambiguous: annotate x`,
	},
	{
		Code:     "E0408",
		Severity: SeverityError,
		Title:    "labeled call on an unresolved function type",
		Phase:    PhaseCheck,
		Spec:     "2",
		Explanation: `A call that uses labels requires the callee's type to be known
at the call site (spec section 2.1), because labels are part of the
function type. When the callee's type is still a type variable, add a
type annotation.`,
		Example: `fn apply f = f ~to: 1   // the type of f is unknown here`,
	},
	{
		Code:     "E0409",
		Severity: SeverityError,
		Title:    "== applied to a type without structural equality",
		Phase:    PhaseCheck,
		Spec:     "3.3",
		Explanation: `"==" is structural equality and can only be used on types that
contain no function (spec section 3.3); equality of type variables is
not allowed in v1. The same restriction applies to assert_eq in
tests (spec section 11.4).`,
		Example: `(fn x -> x) == (fn x -> x)`,
	},
	{
		Code:     "E0410",
		Severity: SeverityError,
		Title:    "try in a function that does not return Result",
		Phase:    PhaseCheck,
		Spec:     "6",
		Explanation: `"try e" escapes to the innermost enclosing function (fn
definition, lambda, or test block) by returning the error from it, so
that function's return type must be "Result _ e" (spec section 6.2).
"go { ... }" is not a function boundary — see E0702.`,
		Example: `fn parse (s: String) -> Int =
  try strconv.Atoi(s)   // parse must return Result Int GoError`,
	},
	{
		Code:     "E0411",
		Severity: SeverityError,
		Title:    "try error type does not match the function's error type",
		Phase:    PhaseCheck,
		Spec:     "6",
		Explanation: `The error type of the tried Result must equal the error type of
the enclosing function's return type (spec section 6.2): a function
returning "Result a GoError" cannot try a "Result b MyError" without
converting the error first.`,
		Example: `fn f () -> Result Unit MyError = {
  try strconv.Atoi(s)   // GoError is not MyError
  Ok ()
}`,
	},
	{
		Code:     "E0412",
		Severity: SeverityError,
		Title:    "unbound type variable",
		Phase:    PhaseCheck,
		Spec:     "3.3",
		Explanation: `A type variable in a "let" annotation has nothing to bind it:
type variables quantify over a declaration — a "fn" signature, a type
declaration, or a generalized top-level "let" — and a "let" whose
pattern is not a bare name, whose right-hand side is not a syntactic
value, or that sits inside a function body cannot introduce them
(spec section 3.3, value restriction). Declare the variable on the
enclosing signature or remove it from the annotation.`,
		Example: `fn f () -> Unit = {
  let x : a = ()   // a is not bound by f's signature
  ()
}`,
	},
	{
		Code:     "E0413",
		Severity: SeverityError,
		Title:    "record type contains itself by value",
		Phase:    PhaseCheck,
		Explanation: `A record's Go representation is a struct whose
fields are stored inline, so a field whose type expands back to the
same record — directly or through a cycle of records — describes an
infinitely large value, which Go rejects as an invalid recursive
type. Recursion is still allowed across representations that box
their payload: sum-type variants (sealed interfaces), function
types, and the reference-shaped builtins List, Ref, Ptr, Array,
Go.Slice, Go.Map, and the channel types.`,
		Example: `type Point = {x: Point}   // use a sum type or {f: () -> Point}`,
	},

	{
		Code:     "E0501",
		Severity: SeverityError,
		Title:    "Go symbol cannot be represented in Gom",
		Phase:    PhaseGoImport,
		Spec:     "8.5",
		Explanation: `The Go package is importable, but this symbol's signature uses
types Gom cannot represent (unsafe.Pointer, generic functions with
constraints other than any/comparable, and so on) and is therefore not
exposed (spec section 8.5). The structured data carries the reason.
An extern declaration can override the imported signature when a
representable view exists.`,
		Example: `import go "unsafe"
let p = unsafe.Pointer(x)   // unsafe.Pointer is not a Gom type`,
	},
	{
		Code:     "E0502",
		Severity: SeverityError,
		Title:    "method call on a Gom value",
		Phase:    PhaseCheck,
		Spec:     "8.1",
		Explanation: `Go-style method calls "x.M(args)" are only valid on values of Go
types (spec section 8.1.3); Gom types have no methods. Call the
corresponding Gom function instead.`,
		Example: `shape.Area()   // write: area shape`,
	},
	{
		Code:     "E0503",
		Severity: SeverityError,
		Title:    "implicit numeric conversion not allowed",
		Phase:    PhaseCheck,
		Spec:     "8.1",
		Explanation: `Between Gom and Go, numeric types convert implicitly only when
no information is lost: Int ↔ int/int64, Float ↔ float64 (spec
section 8.1.5). Other conversions (Int to int32 or float32, for
example) must be written explicitly with Go.conv.`,
		Example: `let r: Int32 = Go.conv x : Int32   // x: Int needs the explicit form`,
	},
	{
		Code:     "E0504",
		Severity: SeverityError,
		Title:    "misplaced _ placeholder",
		Phase:    PhaseCheck,
		Spec:     "8.2",
		Explanation: `"_" as a call argument is the partial-application placeholder
(spec section 8.2): it is allowed only inside a Go function call and
at most once per call. In patterns and let bindings "_" is the
wildcard, which is a different feature.`,
		Example: `strings.Repeat(_, _, 3)   // only one _ is allowed`,
	},
	{
		Code:     "E0505",
		Severity: SeverityError,
		Title:    "misplaced spread argument",
		Phase:    PhaseCheck,
		Spec:     "8.1",
		Explanation: `A spread argument "..xs" expands a list into a variadic Go call;
it may only appear as the last argument of a Go function call (spec
section 8.1.4). It is not a general list-splicing syntax.`,
		Example: `fmt.Println(..xs, "end")   // the spread must be last`,
	},
	{
		Code:     "E0506",
		Severity: SeverityError,
		Title:    "unsupported generic constraint",
		Phase:    PhaseCheck,
		Spec:     "8.5",
		Explanation: `Gom can call a generic Go function only when every type
parameter's constraint is any or comparable (spec section 8.5); other
constraints have no Gom-side meaning. Wrap the call behind an extern
binding or a Go helper.`,
		Example: `maps.Clone(m)   // only if the constraint is any/comparable`,
	},
	{
		Code:     "E0507",
		Severity: SeverityError,
		Title:    "invalid Go.conv conversion",
		Phase:    PhaseCheck,
		Spec:     "8.4",
		Explanation: `"Go.conv e : T" is checked against Go's conversion rules (spec
section 8.4): it works for converting between a named type and its
underlying type or between numeric types, but not for conversions Go
itself would reject.`,
		Example: `Go.conv "abc" : Int   // String is not convertible to Int`,
	},

	{
		Code:     "E0601",
		Severity: SeverityError,
		Title:    "missing .gomi interface file",
		Phase:    PhaseIface,
		Spec:     "7",
		Explanation: `Every library package (anything but "package main") must have
exactly one .gomi interface file that declares its public API (spec
section 7.4). "gom gomi" generates a draft from the inferred
signatures.`,
	},
	{
		Code:     "E0602",
		Severity: SeverityError,
		Title:    "disallowed declaration in .gomi",
		Phase:    PhaseIface,
		Spec:     "7",
		Explanation: `A .gomi file may contain only type declarations, function and
constant signatures, and /// doc comments (spec section 7.1). It
contains no expressions: signatures describe the interface, the .gom
files provide the implementation.`,
	},
	{
		Code:     "E0603",
		Severity: SeverityError,
		Title:    "implementation does not match .gomi",
		Phase:    PhaseIface,
		Spec:     "7",
		Explanation: `The package's definitions must agree with the signatures in its
.gomi: same names, same argument shapes (including labels), same
types up to type-variable renaming (spec section 7.6). Either fix the
implementation or update the interface.`,
	},

	{
		Code:     "E0701",
		Severity: SeverityError,
		Title:    "go block must produce Unit",
		Phase:    PhaseCheck,
		Spec:     "10",
		Explanation: `"go { e }" starts a goroutine and has type Unit, so e must have
type Unit (spec section 10.1) — a value produced by a goroutine would
be silently lost. Return results through a channel or Task.`,
		Example: `go { compute x }   // if compute returns a value it is lost`,
	},
	{
		Code:     "E0702",
		Severity: SeverityError,
		Title:    "try escapes across a go boundary",
		Phase:    PhaseCheck,
		Spec:     "6",
		Explanation: `A "go { ... }" block is not a function boundary (spec section
6.3): a "try" written directly inside it would escape to the enclosing
function — across a goroutine boundary — which is an error. Wrap the
body in a lambda or use Task.spawn so the try has a function to
escape to inside the goroutine.`,
		Example: `go { let r = try fetch url }   // use: go { (fn () -> { ... }) () }`,
	},
	{
		Code:     "E0703",
		Severity: SeverityError,
		Title:    "multiple default arms in select",
		Phase:    PhaseCheck,
		Spec:     "10",
		Explanation: `A select expression may contain at most one "default ->" arm,
matching Go's select.`,
		Example: `select {
  r <- jobs -> handle r
  default -> 0
  default -> 1   // only one default is allowed
}`,
	},
	{
		Code:     "E0704",
		Severity: SeverityError,
		Title:    "select receive arm must bind a variable or _",
		Phase:    PhaseCheck,
		Spec:     "10",
		Explanation: `The left side of a select receive arm "x <- ch" may only be a
variable or "_" (spec section 10.5) — every arm maps one-to-one onto a
Go select case, so a destructuring pattern cannot be decided there.
Match on the received "Option a" value in the arm's body instead.`,
		Example: `select {
  Some job <- jobs -> work job   // write: r <- jobs -> match r { ... }
}`,
	},
	{
		Code:     "E0705",
		Severity: SeverityError,
		Title:    "empty select",
		Phase:    PhaseCheck,
		Spec:     "10",
		Explanation: `A select expression requires at least one arm. The empty form
"select {}" would lower to Go's "select {}", which blocks forever —
hardly ever the intent, and never a value-producing expression.`,
		Example: `fn poll () -> Int = select {}   // write at least one arm`,
	},
	{
		Code:     "E0801",
		Severity: SeverityError,
		Title:    "invalid test body type",
		Phase:    PhaseCheck,
		Spec:     "11",
		Explanation: `A test declaration's block must have type Unit or
"Result Unit e" (spec section 11.1); returning Err fails the test and
any other type is rejected.`,
		Example: `test "doubling" { 1 + 1 }   // Int is not Unit or Result Unit e`,
	},
	{
		Code:     "E0802",
		Severity: SeverityError,
		Title:    "@test definition referenced from non-test code",
		Phase:    PhaseResolve,
		Spec:     "11",
		Explanation: `Definitions marked @test live in a test-only scope: they can be
referenced only from test declarations and other @test definitions
(spec section 11.3), and they never appear in the package's .gomi.`,
		Example: `@test
fn helper () -> Int = 1

fn prod () -> Int = helper ()   // helper is test-only`,
	},
	{
		Code:     "E0803",
		Severity: SeverityError,
		Title:    "assert outside a test block",
		Phase:    PhaseCheck,
		Spec:     "11",
		Explanation: `assert and assert_eq compile to test-assertion calls that need the
*testing.T a generated test function binds, so they may appear only
inside a test block (spec section 11.2). Whether assertions will gain
a meaning in ordinary code is an open question; until then this
diagnostic reports the restriction.`,
		Example: `test "ok" {
  assert (1 + 1 == 2)
}

fn prod () -> Unit = assert true   // no test function binds t`,
	},

	{
		Code:     "E9001",
		Severity: SeverityError,
		Title:    "construct not supported by the evaluator",
		Phase:    PhaseEval,
		Explanation: `The typed-AST evaluator (internal/eval) — the tree-walking
interpreter the semantics-comparison harness checks generated Go
against (design.md section 7) — does not implement this construct in
its current scope. Reported for "go"/"select" expressions, "for"
over non-List iterables, "Go.conv" ascriptions, misplaced holes and
spreads, Ref/Array/Ptr/Chan/Task values, unknown builtins, module
members in expression position, and Go calls for which no host
function was bound (eval.NewHostFunc). This is a limitation of the
interpreter, not a rule of the language: the same program may compile
and run through the Go backend.`,
		Example: `go { ping () }   // goroutines are outside the evaluator's scope`,
	},
	{
		Code:     "E9002",
		Severity: SeverityError,
		Title:    "no match arm accepted the value",
		Phase:    PhaseEval,
		Spec:     "8.6",
		Explanation: `Every arm of a match rejected the scrutinee value at run time.
In a checked program the pattern checker guarantees coverage, so
this means a contract-violating value — a nil or malformed ADT
value produced by Go code — reached the match, which the spec
defines as a panic naming the type (spec section 8.6). On a
hand-built typed AST it can also mean the arms genuinely do not
cover the value.`,
	},
	{
		Code:     "E9003",
		Severity: SeverityError,
		Title:    "panic during evaluation",
		Phase:    PhaseEval,
		Explanation: `A Gom-level panic surfaced as a diagnostic instead of a Go
panic: "todo ()" was reached (spec section 12 item 5), "assert" or
"assert_eq" failed (spec section 11.4), an invalid variant value
was matched, a division or modulo by zero was evaluated, or a host
function raised a Go panic. The message carries the panic text.`,
		Example: `let x = todo ()   // panic: todo reached`,
	},
	{
		Code:     "E9004",
		Severity: SeverityError,
		Title:    "evaluator or compiler invariant violated",
		Phase:    PhaseEval,
		Explanation: `A broken invariant inside the evaluator or the typed AST it
consumes: an unbound symbol, a non-function applied, a pattern
typed as irrefutable that failed anyway, a "try" escaping with no
enclosing function boundary, malformed literal text, and similar.
This indicates a bug in gom itself — or a malformed hand-built AST
in a test — not a mistake in the .gom program.`,
	},
	{
		Code:     "E9005",
		Severity: SeverityError,
		Title:    "test returned Err",
		Phase:    PhaseEval,
		Spec:     "11",
		Explanation: `A "test" declaration's body evaluated to "Err e", which fails
the test (spec section 11). The structured data carries the test
name ("test") and the rendered result ("value").`,
		Example: `test "round-trip" {
  try decode input   // an Err here fails the test
  ()
}`,
	},

	{
		Code:     "W0701",
		Severity: SeverityWarning,
		Title:    "channel of mutable element type",
		Phase:    PhaseLint,
		Spec:     "10",
		Explanation: `The channel's element type contains mutable values (Ref a,
Array a, Ptr T, Go.Slice a, Go.Map k v). Sharing mutable values
across goroutines is legal but race-prone, so the linter warns
(spec section 10). Prefer sending immutable Gom values, or make the
sharing explicit.`,
		Example: `let ch = Chan.make 0   // inferred as Chan (Ref Int)`,
	},
	{
		Code:     "W0702",
		Severity: SeverityWarning,
		Title:    "go expression captures a mutable variable",
		Phase:    PhaseLint,
		Spec:     "10",
		Explanation: `The "go { ... }" block captures a variable whose type contains
mutable values (Ref a, Array a, Ptr T, Go.Slice a, Go.Map k v), so
the goroutine can mutate state shared with its parent (spec section
10). Pass copies or immutable values instead.`,
		Example: `go { Ref.set counter 1 }   // counter: Ref Int is shared`,
	},
}

// CodeTable returns the error-code table sorted by code. Use
// LookupCode for a single entry.
func CodeTable() []CodeInfo {
	return slices.Clone(codeTable)
}

// LookupCode returns the table entry for c, or false when the code is
// not registered.
func LookupCode(c Code) (CodeInfo, bool) {
	for _, info := range codeTable {
		if info.Code == c {
			return info, true
		}
	}
	return CodeInfo{}, false
}

// ValidateCodeTable checks the registry's invariants: well-formed,
// unique, sorted codes; letter-to-severity agreement; a declared
// category and phase; a single-line Title and a non-empty Explanation
// on every entry; and a well-formed Spec section reference when given.
// It reports the first violation found.
func ValidateCodeTable() error {
	cats := categoryNumbers()
	for i, info := range codeTable {
		code := string(info.Code)
		if !WellFormed(info.Code) {
			return fmt.Errorf("diag: code table entry %d: malformed code %q", i, code)
		}
		if i > 0 && codeTable[i-1].Code >= info.Code {
			return fmt.Errorf("diag: code table not sorted or duplicate code %q", code)
		}
		if want := expectedSeverity(info.Code); info.Severity != want {
			return fmt.Errorf("diag: %s: severity %v does not match letter %q",
				code, info.Severity, code[0])
		}
		if !cats[code[1:3]] {
			return fmt.Errorf("diag: %s: unknown category %q", code, code[1:3])
		}
		if info.Title == "" {
			return fmt.Errorf("diag: %s: empty title", code)
		}
		if strings.ContainsAny(info.Title, "\r\n") {
			return fmt.Errorf("diag: %s: title must be a single line", code)
		}
		if strings.TrimSpace(info.Explanation) == "" {
			return fmt.Errorf("diag: %s: empty explanation", code)
		}
		if !validPhase(info.Phase) {
			return fmt.Errorf("diag: %s: unknown phase %q", code, info.Phase)
		}
		if info.Spec != "" && !specRE.MatchString(info.Spec) {
			return fmt.Errorf("diag: %s: malformed spec reference %q", code, info.Spec)
		}
	}
	return nil
}
