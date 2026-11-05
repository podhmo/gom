package build_test

import (
	"strings"
	"testing"

	"github.com/podhmo/gom/internal/diag"
)

// Both directions are exercised: a Go caller invokes generated Gom functions,
// which call zero-result Go functions/methods and preserve their effect order.
func TestVoidGoCalls(t *testing.T) {
	if testing.Short() {
		t.Skipf("long test: %.2fs on last run, skipping in short mode", 0.44)
	}

	t.Parallel()

	src := `package main
import go "github.com/podhmo/gom/testdata/voidhost"
import go "io"
import go "runtime"
import go "os"
let top = voidhost.Mark "T"
fn stop () -> Unit = os.Exit 1
fn tail () -> Unit = voidhost.Mark "t"
fn consume (u: Unit) -> Unit = voidhost.Mark "u"
fn bound () -> Unit = { let u = voidhost.Mark "b"; consume u }
fn argument () -> Unit = consume (voidhost.Mark "a")
fn two (a: Unit) (b: Unit) -> Unit = voidhost.Mark "2"
fn ordered () -> Unit = two (voidhost.Mark "L") (voidhost.Mark "R")
fn branch (b: Bool) -> Unit = if b { voidhost.Mark "i" } else { voidhost.Mark "n" }
fn selected (b: Bool) -> Unit = match b { true -> voidhost.Mark "y"; false -> voidhost.Mark "z" }
fn values () -> Unit = { let effect = voidhost.Mark; effect "v" }
fn cute () -> Unit = { let effect = voidhost.Mark _; effect "c" }
fn method () -> Unit = { let probe = voidhost.NewProbe (); probe.Touch "d" }
fn noarg () -> Unit = runtime.Gosched ()
fn sliced (xs: Go.Slice String) -> Unit = voidhost.Slice xs
fn interfaced (w: io.Writer) -> Unit = voidhost.Interface w
fn named (n: voidhost.Name) -> Unit = voidhost.Named n
`
	r := compile(t, src, suiteOptions())
	for _, d := range r.Diagnostics {
		if d.Severity == diag.SeverityError {
			t.Fatalf("%s: %s", d.Code, d.Message)
		}
	}
	generated := string(r.Output.Source) + `
func main() {
    tail(struct{}{})
    bound(struct{}{})
    argument(struct{}{})
    ordered(struct{}{})
    branch(true); branch(false)
    selected(true); selected(false)
    values(struct{}{}); cute(struct{}{}); method(struct{}{}); noarg(struct{}{})
    sliced(nil); interfaced(nil); named(voidhost.Name("N"))
}
`
	if got := strings.TrimSpace(runCompiled(t, []byte(generated))); got != "TtbuauLR2inyzvcmdswN" {
		t.Fatalf("effect order = %q", got)
	}
}
