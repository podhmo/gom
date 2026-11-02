package build_test

import (
	"strings"
	"testing"

	"github.com/podhmo/gom/internal/build"
	"github.com/podhmo/gom/internal/diag"
)

// Go observes the generated Result API as well as Gom calling Go:
// error identities must survive exactly one %w wrap, and invalid
// foreign Result values must retain the runtime contract.
func TestPipeGoErrorChain(t *testing.T) {
	src := `package main
import go "strconv"
import go "errors"
fn keep (n: Int) -> Result Int GoError = Ok n
fn wrapped (s: String) -> Result Int GoError = s |?> strconv.Atoi |?> keep
fn tried (s: String) -> Result Int GoError = { let n = try (strconv.Atoi s); Ok n }
fn passed (s: String) -> Result Int GoError = (strconv.Atoi s) |?> keep
fn renamed (s: String) -> Result Int GoError = { let convert = strconv.Atoi; s |?> convert }
fn anonymous (s: String) -> Result Int GoError = s |?> fn t -> strconv.Atoi t
fn only (s: String) -> Result Unit GoError = s |?> errors.New
fn input (r: Result Int GoError) -> Result Int GoError = r |?> keep
`
	r, err := build.Compile(build.Input{Path: "pipe%name.gom", Src: []byte(src)}, suiteOptions())
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range r.Diagnostics {
		if d.Severity == diag.SeverityError {
			t.Fatalf("%s: %s", d.Code, d.Message)
		}
	}
	generated := string(r.Output.Source)
	wrappedCode := generated[strings.Index(generated, "func wrapped("):strings.Index(generated, "func tried(")]
	if strings.Contains(wrappedCode, "rt.ResultFromGo") {
		t.Fatal("direct pipeline calls should use fused raw Go result pairs")
	}
	// Imports are added to the same generated package; this is a real Go
	// caller of the generated API, without a special evaluator stub.
	generated = strings.Replace(generated, "import (", "import (\n\"strings\"", 1)
	generated += `
func main() {
  for _, f := range []struct { name string; run func(string) result.Result[int64,error] }{
    {"strconv.Atoi", wrapped}, {"convert", renamed}, {"pipe%name.gom:9", anonymous},
  } {
    r := f.run("bad")
    var n *strconv.NumError
    if !errors.Is(r.Error, strconv.ErrSyntax) || !errors.As(r.Error, &n) || n.Num != "bad" { panic("lost error chain") }
    if !strings.HasPrefix(r.Error.Error(), f.name + ": ") { panic(r.Error) }
    if errors.Unwrap(r.Error) != n { panic("error wrapped more than once") }
  }
  if errors.Unwrap(tried("bad").Error) == nil || errors.Unwrap(passed("bad").Error) == nil { panic("lost original NumError") }
  if _, ok := tried("bad").Error.(*strconv.NumError); !ok { panic("try wrapped error") }
  if _, ok := passed("bad").Error.(*strconv.NumError); !ok { panic("inflow wrapped error") }
  if wrapped("42").Value != 42 { panic("bad successful payload") }
  if only("bad").Error.Error() != "errors.New: bad" { panic("error-only call") }
  func() {
    defer func() { if recover() == nil { panic("invalid result accepted") } }()
    input(result.Result[int64,error]{})
  }()
  fmt.Println("ok")
}
`
	if got := strings.TrimSpace(runCompiled(t, []byte(generated))); got != "ok" {
		t.Fatalf("Go caller output = %q", got)
	}
}

func TestPipeGoInflowConversions(t *testing.T) {
	src := `package main
import go "fmt"
import go "strings"
import go "unicode/utf8"
import go "runtime"
fn next (n: Int) -> Result Int String = Ok (n + 1)
fn pair (p: (Int32, Int)) -> Result Int String = { let (_, n) = p; Ok n }
fn flag (p: (String, Bool)) -> Result Int String = { let (_, ok) = p; Ok (if ok { 1 } else { 0 }) }
fn unit (u: Unit) -> Result Int String = Ok 7
fn after (n: Int) (u: Unit) -> Result Int Bool = Ok n
fn explicit () -> Result Int String = {
  ignore ((runtime.Gosched ()) |?> after (try (Ok 4)))
  Ok 9
}
fn show (r: Result Int String) -> String = match r { Ok n -> "${n}"; Err e -> e }
let top = (strings.Count ("a", "a")) |?> next
fn main () -> Unit = {
  let a = (strings.Count ("a", "a")) |?> next
  let b = (utf8.DecodeRuneInString "あ") |?> pair
  let c = (runtime.Gosched ()) |?> unit
  let d = (strings.CutPrefix ("ab", "a")) |?> flag
  let e = ({ runtime.Gosched () }) |?> unit
  ignore (fmt.Println "${show top}/${show a}/${show b}/${show c}/${show d}/${show (explicit ())}/${show e}")
  ()
}
`
	r := compile(t, src, suiteOptions())
	for _, d := range r.Diagnostics {
		if d.Severity == diag.SeverityError {
			t.Fatalf("%s: %s", d.Code, d.Message)
		}
	}
	if got := strings.TrimSpace(runCompiled(t, r.Output.Source)); got != "2/2/3/7/1/9/7" {
		t.Fatalf("pipeline results = %q", got)
	}
}
