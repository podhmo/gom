package syntax

import (
	"fmt"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/podhmo/gom/internal/diag"
)

// parseExprDump parses src as the body of a let declaration and
// returns the dump of that single expression — the comparison form
// for expression-level table tests.
func parseExprDump(t *testing.T, src string) (dump string, diags diag.List) {
	t.Helper()
	f, ds := Parse([]byte("package t\nlet v = "+src+"\n"), "test.gom")
	ld, ok := f.Decls[0].(*LetDecl)
	if !ok {
		t.Fatalf("expected a let decl, got %v", f.Decls)
	}
	var b strings.Builder
	dumpExpr(&b, ld.Value)
	return b.String(), ds
}

func TestParseExpr(t *testing.T) {
	for _, tc := range []struct {
		name, src, want string
	}{
		// precedence, tightest to loosest
		{"mul before add", `a + b * c`, `(+ a (* b c))`},
		{"add after mul", `a * b + c`, `(+ (* a b) c)`},
		{"apply before add", `f x + g y`, `(+ (apply f x) (apply g y))`},
		{"unary before apply", `-f x`, `(- (apply f x))`},
		{"try before apply", `try g x`, `(try (apply g x))`},
		{"try before add", `try a + b`, `(+ (try a) b)`},
		{"ignore before apply", `ignore f x`, `(ignore (apply f x))`},
		{"ignore before add", `ignore a + b`, `(+ (ignore a) b)`},
		{"ignore before member", `ignore x.y`, `(ignore (x.y))`},
		{"ignore as member name", `x.ignore`, `(x.ignore)`},
		{"and before or", `a && b || c`, `(|| (&& a b) c)`},
		{"or low", `a || b && c`, `(|| a (&& b c))`},
		{"comparison", `a == b`, `(== a b)`},
		{"sub left assoc", `a - b - c`, `(- (- a b) c)`},
		{"member before apply", `f x.y z`, `(apply f (x.y) z)`},
		{"select chains", `a.b.c`, `((a.b).c)`},
		{"module select", `fmt.Println`, `(fmt.Println)`},

		// A "." after a parenthesized argument continues the
		// application — "x.M(1).N(2)" is "((x.M 1).N) 2" like Go
		// (decisions/2026-11-05-post-arg-selector-binding).
		{"call chain select", `x.M(1).N(2)`, `(apply ((apply (x.M) 1).N) 2)`},
		{"call chain select bare", `x.M(1).N`, `((apply (x.M) 1).N)`},
		{"call chain tuple arg", `x.M(1, 0, 0).N(0, 1, 0)`,
			`(apply ((apply (x.M) (tuple 1 0 0)).N) (tuple 0 1 0))`},
		{"call chain three", `x.M(1).N(2).O(3)`,
			`(apply ((apply ((apply (x.M) 1).N) 2).O) 3)`},
		{"select on apply arg", `f(x).y`, `((apply f x).y)`},
		{"select inside arg parens", `f (x.y)`, `(apply f (x.y))`},
		{"select on unparen arg", `f x.y`, `(apply f (x.y))`},
		{"explicit paren chain", `(x.M(1)).N(2)`, `(apply ((apply (x.M) 1).N) 2)`},
		{"single call unchanged", `x.M(1)`, `(apply (x.M) 1)`},
		{"labeled arg paren chain", `f ~a: (v).m`, `((apply f ~a: v).m)`},
		{"ascription loosest", `x : Int`, `(: x Int)`},
		{"ascription type app", `x : List Int`, `(: x (List Int))`},

		// pipes and composition — left associative, lowest binary ops
		{"pipe fwd", `x |> f`, `(|> x f)`},
		{"pipe chain", `x |> f |> g`, `(|> (|> x f) g)`},
		{"pipe back", `f <| x`, `(<| f x)`},
		{"pipe try", `x |?> f`, `(|?> x f)`},
		{"compose fwd", `f >> g`, `(>> f g)`},
		{"compose back", `g << f`, `(<< g f)`},
		{"pipe lambda rhs", `x |> fn y -> y`, `(|> x (fn (y) -> y))`},
		{"pipe lambda body extends right", `x |> fn y -> y |> g`, `(|> x (fn (y) -> (|> y g)))`},
		{"pipe loosest of the binaries", `a |> b || c`, `(|> a (|| b c))`},

		// application
		{"apply two args", `f x y`, `(apply f x y)`},
		{"apply labeled", `f ~x : 1`, `(apply f ~x: 1)`},
		{"apply punned", `f ~x`, `(apply f ~x)`},
		{"apply mixed", `f ~x : 1 ~y 2`, `(apply f ~x: 1 ~y 2)`},
		{"greedy labeled colon", `f ~x : T : U`, `(: (apply f ~x: T) U)`},
		{"apply go tuple", `f(a, b)`, `(apply f (tuple a b))`},
		{"apply go spread", `f(..xs)`, `(apply f (..xs))`},
		{"apply placeholder", `f _`, `(apply f _)`},
		{"apply record arg", `f {x = 1}`, `(apply f (rec x = 1))`},
		{"apply block arg", "f { let x = 1\nx }", `(apply f (block (let x = 1) x))`},
		{"negative arg is binary minus", `f -1`, `(- f 1)`},
		{"negative arg spaced", `f - 1`, `(- f 1)`},
		{"neg int", `- 1`, `(- 1)`},
		{"leading-zero int normalized", `080`, `80`},
		{"underscored int kept", `1_000`, `1_000`},

		// control expressions
		{"if", `if c { a } else { b }`, `(if c (block a) else (block b))`},
		{"if no else", `if c { a }`, `(if c (block a))`},
		{"match", "match e { A -> 1\nB x -> x\n_ -> 0 }", `(match e ((ctor A) -> 1) ((ctor B x) -> x) (_ -> 0))`},
		{"match empty", `match e {}`, `(match e)`},
		{"match or-pat", "match e { A | B -> 1\nx if g x -> 2 }", `(match e ((or (ctor A) (ctor B)) -> 1) (x if (apply g x) -> 2))`},
		{"for", `for x in xs { f x }`, `(for x in xs (block (apply f x)))`},
		{"for tuple pat", `for (a, b) in ps { g a b }`, `(for (,pat a b) in ps (block (apply g a b)))`},
		{"go", `go { f () }`, `(go (block (apply f ())))`},
		{"select", "select { x <- ch -> f x\nsend ch v -> v\ndefault -> 0 }",
			`(select (recv x <- ch -> (apply f x)) (send ch v -> v) (default -> 0))`},
		{"select recv wildcard + call chan", "select { _ <- ch -> 0\nx <- f ch -> x }",
			`(select (recv _ <- ch -> 0) (recv x <- (apply f ch) -> x))`},
		{"select send paren value", "select { send ch (f x) -> 1 }", `(select (send ch (apply f x) -> 1))`},
		{"lambda", `fn x -> x`, `(fn (x) -> x)`},
		{"lambda multi", `fn a b -> a + b`, `(fn (a) (b) -> (+ a b))`},
		{"lambda unit", `fn () -> 1`, `(fn (()) -> 1)`},
		{"lambda wildcard", `fn _ -> 1`, `(fn (_) -> 1)`},

		// brace disambiguation (grammar.ebnf section 8)
		{"brace block", `{ x }`, `(block x)`},
		{"brace empty", `{}`, `(block)`},
		{"brace record", `{ x = 1 }`, `(rec x = 1)`},
		{"brace record trailing comma", `{ x = 1, }`, `(rec x = 1)`},
		{"brace record update", `{ e with x = 1, y = 2 }`, `(rec e with x = 1 y = 2)`},
		{"brace let-block", "{ let x = 1\nx }", `(block (let x = 1) x)`},
		{"brace block semicolons", `{ a; b; c }`, `(block a b c)`},
		{"brace in if block", `if c { {x = 1} }`, `(if c (block (rec x = 1)))`},

		// literals and brackets
		{"list", `[1, 2, 3]`, `(list 1 2 3)`},
		{"list empty", `[]`, `(list)`},
		{"list spread", `[1, ..rest]`, `(list 1 (..rest))`},
		{"list spread only", `[..rest]`, `(list (..rest))`},
		{"tuple", `(1, 2)`, `(tuple 1 2)`},
		{"tuple trailing comma", `(1, 2,)`, `(tuple 1 2)`},
		{"paren trailing comma is not a tuple", `(x,)`, `x`},
		{"paren spread", `(..xs)`, `(..xs)`},
		{"unit", `()`, `()`},
		{"int", `42`, `42`},
		{"float", `3.14`, `3.14`},
		{"char", `'a'`, `'a'`},
		{"string", `"s"`, `"s"`},
		{"true", `true`, `true`},
		{"false", `false`, `false`},
		{"interp", `"hi ${name}!"`, `(interp "hi " $(name) "!")`},
		{"interp expr", `"${a + b}"`, `(interp $((+ a b)))`},
		{"interp brace inside", `"${f {x = 1}}"`, `(interp $((apply f (rec x = 1))))`},
		{"apply interp arg", `f "${name}"`, `(apply f (interp $(name)))`},
		{"apply unit arg", `f ()`, `(apply f ())`},

		// head-expr: "{" inside parens is fine, bare is not
		{"match paren record arg", `match (f {x = 1}) { A -> 0 }`, `(match (apply f (rec x = 1)) ((ctor A) -> 0))`},
		{"if block with record", `if c { {x = 1} }`, `(if c (block (rec x = 1)))`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, diags := parseExprDump(t, tc.src)
			if len(diags) > 0 {
				t.Fatalf("unexpected diagnostics: %v", diags)
			}
			if got != tc.want {
				t.Errorf("parse(%s):\n got %s\nwant %s", tc.src, got, tc.want)
			}
		})
	}
}

// parsePatDump parses src as one match-arm pattern.
func parsePatDump(t *testing.T, src string) (dump string, diags diag.List) {
	t.Helper()
	f, ds := Parse([]byte("package t\nlet v = match e { "+src+" -> 0 }\n"), "test.gom")
	ld := f.Decls[0].(*LetDecl)
	m := ld.Value.(*Match)
	var b strings.Builder
	dumpPat(&b, m.Arms[0].Pat)
	return b.String(), ds
}

func TestParsePattern(t *testing.T) {
	for _, tc := range []struct {
		name, src, want string
	}{
		{"wildcard", `_`, `_`},
		{"var", `x`, `x`},
		{"int", `0`, `0`},
		{"neg int", `-1`, `-1`},
		{"neg float", `-1.5`, `-1.5`},
		{"float", `1.5`, `1.5`},
		{"char", `'a'`, `'a'`},
		{"string", `"s"`, `"s"`},
		{"true", `true`, `true`},
		{"unit", `()`, `()`},
		{"ctor", `Some x`, `(ctor Some x)`},
		{"ctor no arg", `None`, `(ctor None)`},
		{"ctor qualified lower", `option.Some y`, `(ctor (option.Some) y)`},
		{"ctor qualified upper", `Option.Some z`, `(ctor (Option.Some) z)`},
		{"ctor nested paren", `Add (Num 0) r`, `(ctor Add (ctor Num 0) r)`},
		{"ctor labeled arg", `Transfer ~to: t`, `(ctor Transfer ~to: t)`},
		{"ctor record fields", `Let { name = n, .. }`, `(ctor Let name = n ..)`},
		{"ctor record fields exact", `Let { name = n }`, `(ctor Let name = n)`},
		{"record pat", `{a, b = q, ..}`, `(recpat a b = q ..)`},
		{"record pat empty", `{}`, `(recpat)`},
		{"list pat", `[x, ..rest]`, `(listpat x ..rest)`},
		{"list pat empty", `[]`, `(listpat)`},
		{"list pat rest only", `[..rest]`, `(listpat ..rest)`},
		{"list pat wild rest", `[x, .._]`, `(listpat x .._)`},
		{"list pat elems", `[x, y]`, `(listpat x y)`},
		{"tuple pat", `(a, b)`, `(,pat a b)`},
		{"tuple pat trailing comma", `(a, b,)`, `(,pat a b)`},
		{"or pat", `A | B`, `(or (ctor A) (ctor B))`},
		{"or pat three", `A | B | C`, `(or (ctor A) (ctor B) (ctor C))`},
		{"as pat", `p as w`, `(as p w)`},
		{"as binds whole or", `C x | D y as w`, `(as (or (ctor C x) (ctor D y)) w)`},
		{"paren group", `(x)`, `x`},
		{"nested ctor in or", `Some x | None`, `(or (ctor Some x) (ctor None))`},

		// int literals normalize to their value's spelling (the
		// raw lexeme is decimal in Gom but means octal to Go).
		{"leading-zero int", `080`, `80`},
		{"neg leading-zero int", `-077`, `-77`},
		{"base-prefixed int kept", `0x1F`, `0x1F`},

		// A negative literal is rebuilt from the "-" and digit
		// tokens: bytes BETWEEN them are trivia, not part of the
		// spelling. "- 080" used to keep the verbatim span text —
		// the leading-zero normalization never saw it, and the
		// whitespace (or comment) leaked into the generated Go
		// (invalid octal, or worse: "- 0_17" is -15 in Go).
		{"neg spaced leading-zero int", `- 080`, `-80`},
		{"neg tabbed leading-zero int", "-\t080", `-80`},
		{"neg cr leading-zero int", "-\r080", `-80`},
		{"neg commented leading-zero int", `- /* c */ 080`, `-80`},
		{"neg spaced zero-underscore int", `- 0_17`, `-17`},
		{"neg paren leading-zero int", `(- 080)`, `-80`},
		{"ctor arg neg spaced int", `A - 080`, `(ctor A -80)`},
		{"neg spaced float", `- 1.5`, `-1.5`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, diags := parsePatDump(t, tc.src)
			if len(diags) > 0 {
				t.Fatalf("unexpected diagnostics: %v", diags)
			}
			if got != tc.want {
				t.Errorf("pat(%s):\n got %s\nwant %s", tc.src, got, tc.want)
			}
		})
	}
}

// parseTypeDump parses src as a function's return type.
func parseTypeDump(t *testing.T, src string) (dump string, diags diag.List) {
	t.Helper()
	f, ds := Parse([]byte("package t\nfn f () -> "+src+" = ()\n"), "test.gom")
	fd := f.Decls[0].(*FnDecl)
	var b strings.Builder
	dumpType(&b, fd.Ret)
	return b.String(), ds
}

func TestParseTypeExpr(t *testing.T) {
	for _, tc := range []struct {
		name, src, want string
	}{
		{"name", `Int`, `Int`},
		{"var", `a`, `a`},
		{"qualified", `io.Reader`, `(io.Reader)`},
		{"qualified upper", `Go.Map`, `(Go.Map)`},
		{"app", `List Int`, `(List Int)`},
		{"app multi", `Result Unit TxError`, `(Result Unit TxError)`},
		{"app nested", `Option (List Int)`, `(Option (List Int))`},
		{"app qualified", `Go.Map K V`, `((Go.Map) K V)`},
		{"fn", `Int -> Int`, `(-> Int -> Int)`},
		{"fn right assoc", `a -> b -> c`, `(-> a b -> c)`},
		{"fn curried", `Int -> Int -> Int`, `(-> Int Int -> Int)`},
		{"fn labeled", `~to: Account -> R`, `(-> ~to: Account -> R)`},
		{"fn mixed", `Account -> ~to: Account -> R`, `(-> Account ~to: Account -> R)`},
		{"fn paren arg", `(Int -> Int) -> Int`, `(-> (-> Int -> Int) -> Int)`},
		{"unit", `()`, `Unit`},
		{"tuple", `(Int, Bool)`, `(,T Int Bool)`},
		{"tuple three", `(Int, Bool, String)`, `(,T Int Bool String)`},
		{"record", `{name: String}`, `(rectype (name: String))`},
		{"record two", `{name: String, age: Int}`, `(rectype (name: String) (age: Int))`},
		{"record in app", `List {name: String}`, `(List (rectype (name: String)))`},
		{"fn type field", `{f: Int -> Int}`, `(rectype (f: (-> Int -> Int)))`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, diags := parseTypeDump(t, tc.src)
			if len(diags) > 0 {
				t.Fatalf("unexpected diagnostics: %v", diags)
			}
			if got != tc.want {
				t.Errorf("type(%s):\n got %s\nwant %s", tc.src, got, tc.want)
			}
		})
	}
}

// TestParseFile covers whole files — declarations and their shapes.
func TestParseFile(t *testing.T) {
	for _, tc := range []struct {
		name, src, want string
	}{
		{
			"minimal",
			"package m\n",
			`(file (package m))`,
		},
		{
			"fn",
			"package m\n\nfn add (x: Int) (y: Int) -> Int = x + y\n",
			"(file (package m)\n  (fn add (x : Int) (y : Int) -> Int = (+ x y)))",
		},
		{
			"fn labeled param",
			"package m\nfn f (x: Int) (~to: Int) -> Int = x\n",
			"(file (package m)\n  (fn f (x : Int) (~to : Int) -> Int = x))",
		},
		{
			"fn pattern param",
			"package m\nfn f ((a, b): Pair) -> Int = a\n",
			"(file (package m)\n  (fn f ((,pat a b) : Pair) -> Int = a))",
		},
		{
			"lets",
			"package m\nlet x = 1\nlet y : Int = 2\n",
			"(file (package m)\n  (let x = 1)\n  (let y : Int = 2))",
		},
		{
			"let tuple pat",
			"package m\nlet (a, b) = f ()\n",
			"(file (package m)\n  (let (,pat a b) = (apply f ())))",
		},
		{
			"sum type multiline",
			"package m\ntype Option a =\n  | Some a\n  | None\n",
			"(file (package m)\n  (type Option a = (sum |Some a |None)))",
		},
		{
			"sum type one line",
			"package m\ntype Shape = | Circle (radius: Float) | Rect (w: Float) (h: Float) | Empty\n",
			"(file (package m)\n  (type Shape = (sum |Circle (radius: Float) |Rect (w: Float) (h: Float) |Empty)))",
		},
		{
			"labeled payload",
			"package m\ntype Send = | Transfer (~to: Account) (amount: Int)\n",
			"(file (package m)\n  (type Send = (sum |Transfer (~to: Account) (amount: Int))))",
		},
		{
			"record type",
			"package m\ntype User = {name: String, age: Int}\n",
			"(file (package m)\n  (type User = (record (name: String) (age: Int))))",
		},
		{
			"imports",
			"package m\nimport go \"fmt\"\nimport go io \"os\"\nimport \"strings\"\n",
			"(file (package m)\n  (import go fmt \"fmt\")\n  (import go io \"os\")\n  (import strings \"strings\"))",
		},
		{
			"test decl",
			"package m\ntest \"adds\" { 1 + 2 }\n",
			"(file (package m)\n  (test \"adds\" (block (+ 1 2))))",
		},
		{
			"@test fn",
			"package m\n@test fn t () -> Unit = ()\n",
			"(file (package m)\n  (fn @test t (()) -> Unit = ()))",
		},
		{
			"@test own line",
			"package m\n@test\nfn t () -> Unit = ()\n",
			"(file (package m)\n  (fn @test t (()) -> Unit = ()))",
		},
		{
			"@test let",
			"package m\n@test let v = 1\n",
			"(file (package m)\n  (let @test v = 1))",
		},
		{
			"extern",
			"package m\nextern go \"flag\" {\n  fn Bool (name: String) (d: Bool) (usage: String) -> Go.Ptr\n}\n",
			"(file (package m)\n  (extern go \"flag\" (fn Bool (name : String) (d : Bool) (usage : String) -> (Go.Ptr))))",
		},
		{
			"doc comment attaches",
			"package m\n/// doc line\nfn f () -> Unit = ()\n",
			"(file (package m)\n  (fn f (()) -> Unit = ()))",
		},
		{
			"match all pattern forms",
			"package m\nfn f (e: T) -> Int = match e {\n  Some x -> x\n  Add (Num 0) r -> r\n  _ -> 2\n}\n",
			"(file (package m)\n  (fn f (e : T) -> Int = (match e ((ctor Some x) -> x) ((ctor Add (ctor Num 0) r) -> r) (_ -> 2))))",
		},
		{
			"semicolons between decls",
			"package m; let x = 1; let y = 2\n",
			"(file (package m)\n  (let x = 1)\n  (let y = 2))",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f, diags := Parse([]byte(tc.src), "test.gom")
			if len(diags) > 0 {
				t.Fatalf("unexpected diagnostics: %v", diags)
			}
			if got := dumpFile(f); got != tc.want {
				t.Errorf("parse:\n got %s\nwant %s", got, tc.want)
			}
		})
	}
}

// diagSummary renders one diagnostic as "code@start-end: message"
// for table comparisons.
func diagSummary(d diag.Diagnostic) string {
	return fmt.Sprintf("%s@%d-%d: %s", d.Code, d.Span.Start, d.Span.End, d.Message)
}

func TestParseDiagnostics(t *testing.T) {
	for _, tc := range []struct {
		name string
		src  string
		want []string
	}{
		{
			"missing package clause",
			"let x = 1\n",
			[]string{`E0101@0-3: expected a package clause, found "let"`},
		},
		{
			"missing package name",
			"package\nfn f () -> Unit = ()\n",
			[]string{`E0101@8-10: expected a package name, found "fn"`},
		},
		{
			"decl without separator",
			"package m\nlet x = 1 let y = 2\n",
			[]string{`E0101@20-23: expected a newline or ";" between declarations, found "let"`},
		},
		{
			"stray token between decls",
			"package m\nx\n",
			[]string{`E0101@10-11: expected a declaration, found identifier 'x'`},
		},
		{
			"stray closer between decls",
			"package m\n)\n",
			[]string{`E0101@10-11: expected a declaration, found ")"`},
		},
		{
			"fn missing name",
			"package m\nfn () -> Unit = ()\n",
			[]string{`E0101@13-14: expected a function name, found "("`},
		},
		{
			"fn missing arrow",
			"package m\nfn f () = ()\n",
			[]string{`E0101@18-19: expected "->" followed by the return type, found "="`},
		},
		{
			"fn missing body",
			"package m\nfn f () -> Int\n",
			[]string{`E0101@24-25: expected "=" followed by the function body, found newline`},
		},
		{
			"fn param missing colon",
			"package m\nfn f (x) -> Int = x\n",
			[]string{`E0101@15-18: a function parameter needs a ": Type" annotation`},
		},
		{
			"let missing value",
			"package m\nlet\n",
			[]string{
				`E0101@14-14: expected a pattern, found end of file`,
				`E0101@14-14: expected "=" followed by the value, found end of file`,
			},
		},
		{
			"type missing body",
			"package m\ntype X\n",
			[]string{`E0101@16-17: expected "=" followed by the type body, found newline`},
		},
		{
			"type missing name",
			"package m\ntype\n",
			[]string{
				`E0101@15-15: expected a type name (uppercase identifier), found end of file`,
				`E0101@15-15: expected "=" followed by the type body, found end of file`,
			},
		},
		{
			"lowercase type name",
			"package m\ntype x = | A\n",
			[]string{`E0101@15-16: expected a type name (uppercase identifier), found identifier 'x'`},
		},
		{
			"import missing path",
			"package m\nimport\n",
			[]string{`E0101@17-17: expected an import path string, found end of file`},
		},
		{
			"attribute on non-decl",
			"package m\n@test 1\n",
			[]string{
				`E0101@10-15: an attribute applies only to a "fn" or "let" declaration, found integer literal '1'`,
				`E0101@16-17: expected a declaration, found integer literal '1'`,
			},
		},
		{
			"unknown attribute",
			"package m\n@wat fn f () -> Unit = ()\n",
			[]string{`E0101@11-14: unknown attribute "@wat": only "@test" and "@tailrec" are defined`},
		},
		{
			"tailrec on let",
			"package m\n@tailrec let f = fn n -> n\n",
			[]string{`E0101@11-18: the "@tailrec" attribute applies only to a "fn" declaration`},
		},
		{
			"tailrec on fn parses",
			"package m\n@tailrec fn f (n: Int) -> Int = f (n - 1)\n",
			nil,
		},
		{
			"if brace on next line",
			"package m\nfn f (x: Int) -> Int =\n  if x > 0\n  { x }\n  else { 0 }\n",
			[]string{
				`E0101@43-44: the "{" of an if must stay on the same line as its head`,
				`E0101@46-47: expected a declaration, found "{"`,
				`E0101@50-51: expected a newline or ";" between declarations, found "}"`,
				`E0101@54-58: expected a newline or ";" between declarations, found "else"`,
			},
		},
		{
			"match brace on next line",
			"package m\nlet v = match e\n{ A -> 1 }\n",
			[]string{
				`E0101@25-26: the "{" of a match must stay on the same line as its head`,
				`E0101@26-27: expected a declaration, found "{"`,
				`E0101@35-36: expected a newline or ";" between declarations, found "}"`,
			},
		},
		{
			"if without any brace",
			"package m\nlet v = if c\n  foo\n",
			[]string{
				`E0101@22-23: expected "{" to open an if, found newline`,
				`E0101@25-28: expected a declaration, found identifier 'foo'`,
			},
		},
		{
			"match arm arrow on next line",
			"package m\nlet v = match e {\n  A\n  -> 1\n}\n",
			[]string{`E0101@31-32: a match arm's "->" must stay on the same line as its pattern`},
		},
		{
			"guard ascription absorbs the arrow",
			"package m\nlet v = match s { p if x : T -> e }\n",
			[]string{
				`E0101@44-45: expected "->" between the pattern and the arm body, found "}"`,
				`E0101@44-45: expected an expression, found "}"`,
			},
		},
		{
			"recv channel ascription absorbs the arrow",
			"package m\nlet v = select { n <- ch : T -> e }\n",
			[]string{
				`E0101@44-45: expected "->" after the receive arm's channel, found "}"`,
				`E0101@44-45: expected an expression, found "}"`,
			},
		},
		{
			"send arm ascription is not absorbed",
			"package m\nlet v = select { send ch v : T -> e }\n",
			[]string{`E0101@37-38: expected "->" after the send arm's channel and value, found ":"`},
		},
		{
			"guard ascription without an arrow is just a missing arrow",
			"package m\nlet v = match s { p if x : T }\n",
			[]string{
				`E0101@39-40: expected "->" between the pattern and the arm body, found "}"`,
				`E0101@39-40: expected an expression, found "}"`,
			},
		},
		{
			"match arm bar separator",
			"package m\nlet v = match e { A -> 1 | B -> 2 }\n",
			[]string{`E0101@35-36: match arms are separated by newlines, not "|"`},
		},
		{
			"else if",
			"package m\nlet v = if c { a } else if d { b }\n",
			[]string{`E0101@34-36: "else" must be followed by a block; write "else { if ... }"`},
		},
		{
			"head expr brace",
			"package m\nlet v = for x in {a = 1} { b }\n",
			[]string{`E0101@27-28: "{" cannot start an expression in a head position (parenthesize it)`},
		},
		{
			"chained comparison",
			"package m\nlet v = a == b == c\n",
			[]string{`E0101@25-27: comparison operators do not chain; parenthesize, e.g. "(a < b) == c"`},
		},
		{
			"record field missing comma",
			"package m\nlet v = {x = 1\ny = 2}\n",
			[]string{`E0101@24-25: expected "," or "}" between record fields, found newline`},
		},
		{
			"unclosed paren",
			"package m\nlet v = (a\n",
			[]string{`E0101@21-21: expected ")", found end of file`},
		},
		{
			"unclosed block",
			"package m\nlet v = { a\n",
			[]string{`E0101@22-22: expected "}", found end of file`},
		},
		{
			"unclosed list",
			"package m\nlet v = [a\n",
			[]string{`E0101@21-21: expected "]", found end of file`},
		},
		{
			"empty match is fine, unclosed match errors",
			"package m\nlet v = match e { A -> 1\n",
			[]string{`E0101@35-35: expected "}", found end of file`},
		},
		{
			"interp string pattern",
			"package m\nlet v = match e { \"${x}\" -> 0 }\n",
			[]string{`E0101@28-29: an interpolated string cannot be a pattern`},
		},
		{
			"mismatched close in list",
			"package m\nlet v = [a )\n",
			[]string{
				`E0101@21-22: expected "]", found ")"`,
				`E0101@21-22: expected a newline or ";" between declarations, found ")"`,
			},
		},
		{
			"test missing name",
			"package m\ntest { 1 }\n",
			[]string{`E0101@15-16: expected a test name string, found "{"`},
		},
		{
			"ctor pat missing label name",
			"package m\nlet v = match e { A ~ -> 0 }\n",
			[]string{
				`E0101@32-34: expected a label name after "~", found "->"`,
				`E0101@32-34: expected a pattern, found "->"`,
			},
		},
		{
			"record update empty",
			"package m\nlet v = { e with }\n",
			[]string{
				`E0101@27-28: a record update needs at least one "name = value" field`,
			},
		},
		// The ctor-pattern + stray "-" family that looped forever
		// (TODO.md): the "-" is consumed, so the parse terminates
		// with ordinary recovery diagnostics.
		{
			"ctor pattern + stray - at EOF (let)",
			"package m\nlet A -",
			[]string{
				`E0101@16-17: expected a number after "-" in a pattern`,
				`E0101@17-17: expected "=" followed by the value, found end of file`,
			},
		},
		{
			"ctor pattern + stray - at EOF (match arm)",
			"package m\nlet v = match r {A -",
			[]string{
				`E0101@29-30: expected a number after "-" in a pattern`,
				`E0101@30-30: expected "->" between the pattern and the arm body, found end of file`,
				`E0101@30-30: expected an expression, found end of file`,
				`E0101@30-30: expected "}", found end of file`,
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, diags := Parse([]byte(tc.src), "test.gom")
			if len(diags) != len(tc.want) {
				var got []string
				for _, d := range diags {
					got = append(got, diagSummary(d))
				}
				t.Fatalf("diags = %v\nwant %v", got, tc.want)
			}
			for i, d := range diags {
				if got := diagSummary(d); got != tc.want[i] {
					t.Errorf("diags[%d] = %s\nwant %s", i, got, tc.want[i])
				}
			}
		})
	}
}

// TestArmArrowNotes checks the secondary note emitted when a
// top-level ": T -> U" ascription in a match guard or a select
// receive channel absorbs the arm's "->" — the grammar ambiguity of
// docs/problems/2026-11-02-guard-colon-arrow.md.
func TestArmArrowNotes(t *testing.T) {
	for _, tc := range []struct {
		name, src, wantNote string
	}{
		{
			"match guard",
			"package m\nlet v = match s { p if x : T -> e }\n",
			`the "->" was read as part of the type ascription; parenthesize it: "p if (x : T) -> e"`,
		},
		{
			"match or-pattern guard",
			"package m\nlet v = match s { A | B if x : T -> e }\n",
			`the "->" was read as part of the type ascription; parenthesize it: "A | B if (x : T) -> e"`,
		},
		{
			"multi-arrow type",
			"package m\nlet v = match s { p if x : Int -> Int -> e }\n",
			`the "->" was read as part of the type ascription; parenthesize it: "p if (x : Int -> Int) -> e"`,
		},
		{
			"select recv channel",
			"package m\nlet v = select { n <- ch : T -> e }\n",
			`the "->" was read as part of the type ascription; parenthesize it: "n <- (ch : T) -> e"`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, diags := Parse([]byte(tc.src), "test.gom")
			found := false
			for _, d := range diags {
				for _, n := range d.Notes {
					if n == tc.wantNote {
						found = true
					}
				}
			}
			if !found {
				t.Errorf("note %q not found; diags = %v", tc.wantNote, diags)
			}
		})
	}

	// A parenthesized or non-arrow ascription does not produce the
	// note; the first source parses without any diagnostic.
	for _, src := range []string{
		"package m\nlet v = match s { p if (x : T) -> e }\n",
		"package m\nlet v = match s { p if x : T }\n",
		"package m\nlet v = match s { p if f (x : T) -> e }\n",
	} {
		_, diags := Parse([]byte(src), "test.gom")
		for _, d := range diags {
			for _, n := range d.Notes {
				if strings.Contains(n, "type ascription") {
					t.Errorf("unexpected ascription note for %q: %v", src, n)
				}
			}
		}
	}
}

// TestParseInterface covers the .gomi signature subset: signatures
// parse cleanly, bodies and gom-only declarations are reported but
// still parsed for recovery.
func TestParseInterface(t *testing.T) {
	for _, tc := range []struct {
		name, src, want string
		wantDiags       []string
	}{
		{
			"signatures only",
			"package m\n\nfn add (x: Int) (y: Int) -> Int\nlet version : String\ntype Shape\nimport go \"fmt\"\n",
			"(file (package m)\n  (fn add (x : Int) (y : Int) -> Int)\n  (let version : String)\n  (type Shape = (abstract))\n  (import go fmt \"fmt\"))",
			nil,
		},
		{
			"concrete type body still allowed",
			"package m\ntype Shape = | Circle (radius: Float)\n",
			"(file (package m)\n  (type Shape = (sum |Circle (radius: Float))))",
			nil,
		},
		{
			"fn body rejected",
			"package m\nfn f () -> Unit = ()\n",
			"",
			[]string{`E0602@26-27: a signature in an interface file has no "= body"`},
		},
		{
			"let value rejected",
			"package m\nlet x : Int = 1\n",
			"",
			[]string{`E0602@22-23: a let signature in an interface file has no "= value"`},
		},
		{
			"let sig needs colon",
			"package m\nlet x\n",
			"",
			[]string{`E0101@15-16: expected ":" followed by the type, found newline`},
		},
		{
			"let tuple pattern rejected",
			"package m\nlet (a, b) : T\n",
			"",
			[]string{`E0602@14-20: a constant signature binds a plain name`},
		},
		{
			"test decl rejected",
			"package m\ntest \"x\" { 1 }\n",
			"",
			[]string{`E0602@10-14: a test declaration is not allowed in an interface file`},
		},
		{
			"extern decl rejected",
			"package m\nextern go \"m\" {}\n",
			"",
			[]string{`E0602@10-16: an extern declaration is not allowed in an interface file`},
		},
		{
			"attribute rejected",
			"package m\n@test fn f () -> Unit\n",
			"",
			[]string{`E0602@10-11: an attribute is not allowed in an interface file`},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f, diags := ParseInterface([]byte(tc.src), "test.gomi")
			if len(diags) != len(tc.wantDiags) {
				var got []string
				for _, d := range diags {
					got = append(got, diagSummary(d))
				}
				t.Fatalf("diags = %v\nwant %v", got, tc.wantDiags)
			}
			for i, d := range diags {
				if got := diagSummary(d); got != tc.wantDiags[i] {
					t.Errorf("diags[%d] = %s\nwant %s", i, got, tc.wantDiags[i])
				}
			}
			if tc.want != "" {
				if got := dumpFile(f); got != tc.want {
					t.Errorf("parse:\n got %s\nwant %s", got, tc.want)
				}
			}
		})
	}
}

// TestParseTrivia checks that comments and blank lines reach
// syntax.File and that /// doc comments attach to the following
// declaration.
func TestParseTrivia(t *testing.T) {
	src := `// leading comment
package m

import go "fmt" // trailing

/// doc for f
/// second doc line
fn f () -> Unit = ()

// loose line comment
/* block
   comment */
let x = 1 // tail
`
	f, diags := Parse([]byte(src), "test.gom")
	if len(diags) > 0 {
		t.Fatalf("diags = %v", diags)
	}

	var kinds []string
	for _, c := range f.Comments {
		kinds = append(kinds, fmt.Sprintf("%s:%q", c.Kind, c.Text))
	}
	want := []string{
		`line:" leading comment"`,
		`line:" trailing"`,
		`doc:" doc for f"`,
		`doc:" second doc line"`,
		`line:" loose line comment"`,
		`block:" block\n   comment "`,
		`line:" tail"`,
	}
	if len(kinds) != len(want) {
		t.Fatalf("comments = %v\nwant %v", kinds, want)
	}
	for i := range want {
		if kinds[i] != want[i] {
			t.Errorf("comments[%d] = %s, want %s", i, kinds[i], want[i])
		}
	}

	if len(f.Blanks) == 0 {
		t.Error("no blank lines recorded")
	}
	for _, b := range f.Blanks {
		if b.IsZero() {
			t.Errorf("zero-width blank span: %v", b)
		}
	}

	// The /// lines must attach to fn f.
	fn := f.Decls[1].(*FnDecl)
	if fn.Doc == nil || !strings.Contains(fn.Doc.Text, "doc for f") {
		t.Errorf("fn f doc = %v", fn.Doc)
	}
	if f.Decls[2].(*LetDecl).Doc != nil {
		t.Error("let x picked up a doc comment it should not have")
	}
}

// TestParseSpans spot-checks byte-offset spans on a small file.
func TestParseSpans(t *testing.T) {
	src := "package m\nfn f (x: Int) -> Int = x + 1\n"
	f, diags := Parse([]byte(src), "test.gom")
	if len(diags) > 0 {
		t.Fatalf("diags = %v", diags)
	}
	// File.Sp covers the whole source including the trailing NL.
	if f.Sp.Start != 0 || f.Sp.End != len(src) {
		t.Errorf("file span = %v", f.Sp)
	}
	fd := f.Decls[0].(*FnDecl)
	if fd.Sp.Start != 10 || fd.Sp.End != 38 {
		t.Errorf("fn span = %v", fd.Sp)
	}
	if fd.Name.Sp.Start != 13 || fd.Name.Sp.End != 14 {
		t.Errorf("fn name span = %v", fd.Name.Sp)
	}
	bin := fd.Body.(*Binary)
	if bin.Sp.Start != 33 || bin.Sp.End != 38 {
		t.Errorf("body span = %v", bin.Sp)
	}
}

// TestParseRecovery parses malformed inputs and asserts the parser
// always terminates, reports at least one diagnostic, and — where
// possible — recovers enough to keep the trailing declaration.
func TestParseRecovery(t *testing.T) {
	for _, tc := range []struct {
		name string
		src  string
		// declDumps lists the s-expr of every decl that should
		// survive recovery; empty means "no assertion".
		declDumps []string
	}{
		{
			"garbage between decls",
			"package m\nlet a = 1\n@@@ let\nlet b = 2\n",
			[]string{`(let a = 1)`, `(let b = 2)`},
		},
		{
			"unclosed expr then decl",
			"package m\nlet a = (\nlet b = 2\n",
			[]string{`(let a = _)`, `(let b = 2)`},
		},
		{
			// Inside a "{", "let" is a legal block item, so the
			// unclosed block legitimately absorbs the next line.
			"unclosed block then decl",
			"package m\nlet a = { x\nlet b = 2\n",
			[]string{`(let a = (block x (let b = 2)))`},
		},
		{
			// Inside a match, "let" cannot start an arm; recovery
			// turns the junk into a placeholder arm.
			"unclosed match then decl",
			"package m\nlet a = match e { A -> 1\nlet b = 2\n",
			[]string{`(let a = (match e ((ctor A) -> 1) (_ -> _)))`},
		},
		{
			// A stray token where the "->" belongs is skipped to the
			// arm's real arrow instead of being read as the body.
			"match arm stray token before arrow",
			"package m\nlet a = match e { p junk -> e\nq -> 2 }\n",
			[]string{`(let a = (match e (p -> e) (q -> 2)))`},
		},
		{
			"select arm stray token before arrow",
			"package m\nlet a = select { send ch v junk -> e\ndefault -> 0 }\n",
			[]string{`(let a = (select (send ch v -> e) (default -> 0)))`},
		},
		{
			"truncated decl head",
			"package m\nfn\nlet b = 2\n",
			[]string{`(fn _ -> <?>)`, `(let b = 2)`},
		},
		{
			"stray close parens",
			"package m\n) ] }\nlet b = 2\n",
			[]string{`(let b = 2)`},
		},
		{
			"nested mismatched closers",
			"package m\nlet a = f (x, y ] }\nlet b = 2\n",
			[]string{`(let a = (apply f (tuple x y)))`, `(let b = 2)`},
		},
		{
			"unterminated interpolation",
			"package m\nlet a = \"${x\n",
			nil,
		},
		{
			"EOF inside everything",
			"package m\nfn f (x: Int) -> Int = if x { match y { A -> [1, (",
			nil,
		},
		{
			// Fuzz hang regression: the stray "-" after a
			// constructor pattern used to spin the pattern-argument
			// loop forever at EOF.
			"ctor pattern + stray - at EOF (let)",
			"package m\nlet A -",
			nil,
		},
		{
			"ctor pattern + stray - at EOF (match arm)",
			"package m\nfn f () -> Int = match r {A -",
			nil,
		},
		{
			"ctor pattern + stray - mid-file recovers",
			"package m\nlet A -\nlet b = 2\n",
			[]string{`(let (ctor A _))`, `(let b = 2)`},
		},
		{
			// The same shared-machinery path through a for header.
			"ctor pattern + stray - in for pattern",
			"package m\nlet v = { for A - in xs { b } }",
			nil,
		},
		{
			// Fuzz hang regression: the "{" swallowed by the
			// unclosed "[" makes the lexer emit a newline the
			// list loop cannot resync past — it reported the
			// same token forever (corpus b42a89eef6620e98).
			"stray newline inside a desynced list",
			"package m\nlet x = [\ny = {\n]\n",
			nil,
		},
		{
			// A bare "]" inside a block body is left for the
			// decl loop, which reports and skips it.
			"stray close inside a block body",
			"package m\nfn h () -> Unit = {\n  ]\n}\n",
			[]string{`(fn h (()) -> Unit = (block))`},
		},
		{
			"empty source",
			"",
			nil,
		},
		{
			"only package clause",
			"package m",
			nil,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			done := make(chan struct{})
			var f *File
			var diags diag.List
			go func() {
				f, diags = Parse([]byte(tc.src), "test.gom")
				close(done)
			}()
			select {
			case <-done:
			case <-time.After(10 * time.Second):
				t.Fatal("parser did not terminate")
			}
			if tc.name != "only package clause" && len(diags) == 0 {
				t.Error("no diagnostics on malformed input")
			}
			if tc.declDumps == nil {
				return
			}
			var got []string
			for _, d := range f.Decls {
				var b strings.Builder
				dumpDecl(&b, d)
				got = append(got, b.String())
			}
			if len(got) != len(tc.declDumps) {
				t.Fatalf("decls = %v\nwant %v", got, tc.declDumps)
			}
			for i := range got {
				if got[i] != tc.declDumps[i] {
					t.Errorf("decls[%d] = %s\nwant %s", i, got[i], tc.declDumps[i])
				}
			}
		})
	}
}

// TestIntLitText pins the literal normalization: Gom's dec-int
// accepts leading zeros (decimal semantics) while the same spelling
// in Go is an octal literal — so a leading-zero decimal is rewritten
// to plain decimal digits for the codegen-facing Text.
func TestIntLitText(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"0", "0"},
		{"42", "42"},
		{"1_000", "1_000"},
		{"080", "80"},
		{"077", "77"},
		{"007", "7"},
		{"00", "0"},
		{"0_0", "0"},
		{"0_17", "17"},
		{"0x1F", "0x1F"},
		{"0o17", "0o17"},
		{"0b101", "0b101"},
		{"-0", "-0"},
		{"-080", "-80"},
	} {
		if got := intLitText(tc.in); got != tc.want {
			t.Errorf("intLitText(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// TestQuoteLexeme pins the escaping that keeps a token's raw text
// from breaking the single-line rule of Diagnostic.Validate.
func TestQuoteLexeme(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"x", "x"},
		{"\r", `\r`},
		{"a\nb", `a\nb`},
		{"a\tb", `a\tb`},
		{`'a'`, `'a'`},
		{`"s"`, `\"s\"`},
		{"\x01", `\x01`},
		{"日本語", "日本語"}, // printable non-ASCII is kept, not escaped
		{strings.Repeat("a", 100), strings.Repeat("a", describeLexemeMax) + "..."},
	} {
		if got := quoteLexeme(tc.in); got != tc.want {
			t.Errorf("quoteLexeme(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
	// A multi-byte rune at the cut boundary must not be split.
	got := quoteLexeme("a" + strings.Repeat("é", 50))
	if !utf8.ValidString(got) {
		t.Errorf("truncated mid-rune: %q", got)
	}
}

// TestDescribeEscapesLiteralText: a control character inside a
// string literal must reach the "found" detail escaped — the raw
// byte would make the message multi-line (TODO.md fuzz finding).
func TestDescribeEscapesLiteralText(t *testing.T) {
	_, diags := Parse([]byte("\"\r"), "t.gom")
	for _, d := range diags {
		if err := d.Validate(); err != nil {
			t.Fatalf("invalid diagnostic: %v (%+v)", err, d)
		}
	}
	found := false
	for _, d := range diags {
		if strings.Contains(d.Message, `'\"\r'`) {
			found = true
		}
	}
	if !found {
		t.Errorf("no diagnostic quotes the escaped lexeme: %v", diags)
	}
}
