package goimport

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/podhmo/gom/internal/check"
	"github.com/podhmo/gom/internal/resolve"
	"github.com/podhmo/gom/internal/syntax"
	gt "github.com/podhmo/gom/internal/types"
)

func boundaryLoader(t *testing.T) (*Loader, string) {
	t.Helper()
	l, dir := moduleLoader(t)
	source := `package model
 import "fmt"
 type Empty interface{}
 func Accept(v Empty) string { return fmt.Sprint(v) }
 func Kind(v Empty) string { return fmt.Sprintf("%T",v) }
 func Channel() chan string { return nil }
 func ReceiveChannel() <-chan string { return nil }
 func SendChannel() chan<- string { return nil }
 func AnyChannel() chan any { return nil }
 type NamedString string
 type Embedded struct { Count int }
 type Container struct { Embedded; Text NamedString; Names []string; Reader Reader; Bytes []byte; hidden int }
 func NewContainer() *Container { return &Container{Embedded:Embedded{Count:41},Text:"hello"} }
 func Change(c *Container) int { c.Count=99; return 2 }
 func Pair(a,b int) int { return a*100+b }
 type Concrete struct{}
 func (*Concrete) Read() string { return "concrete" }
 func Pointer() *Concrete { return &Concrete{} }
 func NilPointer() *Concrete { return nil }
 func ConcreteValue() Concrete { return Concrete{} }
 func IsNil(r Reader) bool { return r==nil }
 func Use(r Reader) string { return r.Read() }
 type Other interface { Other() string }
 type OtherEmbedded struct { Count int }
 type Both struct { Embedded; OtherEmbedded }
 func Ambiguous() Both { return Both{} }
 `
	if err := os.WriteFile(filepath.Join(dir, "dependency/model/boundary.go"), []byte(source), 0644); err != nil {
		t.Fatal(err)
	}
	return l, dir
}

func TestInterfaceAssignmentAndFieldsBothDirections(t *testing.T) {
	if testing.Short() {
		t.Skipf("long test: %.2fs on last run, skipping in short mode", 2.14)
	}

	l, dir := boundaryLoader(t)
	src := `package bridge
 import go "example.test/dependency/model"
 import go "net/http"
 import go "strings"
 fn read () -> String = model.Use (model.Pointer ())
 fn numeric () -> String = model.Accept (1 + 2)
 fn int_kind () -> String = model.Kind 1
 fn float_kind () -> String = model.Kind 1.5
 fn char_kind () -> String = model.Kind 'a'
 fn channel () -> String = model.Kind (model.Channel ())
 fn receive_channel () -> String = model.Kind (model.ReceiveChannel ())
 fn send_channel () -> String = model.Kind (model.SendChannel ())
 fn any_channel () -> String = model.Kind (model.AnyChannel ())
 fn channel_identity (c: Chan String) -> model.Empty = c
 fn consume (f: (Ptr model.Container -> String)) (c: Ptr model.Container) -> String = f c
 fn delayed_field () -> String = consume (fn x -> model.Accept x.Count) (model.NewContainer ())
 fn delayed_alias () -> String = consume (fn x -> {let value = x.Count; model.Accept value}) (model.NewContainer ())
 fn choose (b: Bool) -> model.Reader = if b { model.Pointer () } else { model.NilPointer () }
 fn choose_match (b: Bool) -> model.Reader = match b {true -> model.Pointer (); false -> model.NilPointer ()}
 let inferred = fn x -> model.Accept x
 fn annotated () -> String = {let r: model.Reader = model.Pointer (); model.Use r}
 fn indirect (r: model.Reader) -> String = model.Use r
 fn argument () -> String = indirect (model.Pointer ())
 fn ignored_field (c: Ptr model.Container) -> Unit = ignore c.Text
 fn nil_pointer () -> model.Reader = model.NilPointer ()
 fn named (c: Ptr model.Container) -> model.NamedString = c.Text
 fn count (c: Ptr model.Container) -> Int = c.Count
 fn names (c: Ptr model.Container) -> Go.Slice String = c.Names
 fn reader (c: Ptr model.Container) -> model.Reader = c.Reader
 fn order (c: Ptr model.Container) -> Int = model.Pair (c.Count, model.Change c)
 fn request () -> Result String GoError = {
  let req = try http.NewRequest ("POST", "http://example.invalid/gists", strings.NewReader "body")
  req.Header.Set ("Accept", "application/json")
  Ok (req.Header.Get "Accept")
 }
 `
	gen := compileSource(t, l, src)
	if !strings.Contains(string(gen), "int64(") || !strings.Contains(string(gen), "model.Reader(") {
		t.Fatalf("missing explicit boundary conversions:\n%s", gen)
	}
	test := `package bridge
 import("testing";"example.test/dependency/model")
 func TestBoundary(t *testing.T) {
  if inferred("inferred")!="inferred" {t.Fatal("unannotated interface parameter")}
  if numeric(struct{}{})!="3" {t.Fatal("numeric interface inference")}
  if intKind(struct{}{})!="int64" || floatKind(struct{}{})!="float64" || charKind(struct{}{})!="int32" {t.Fatal("interface dynamic scalar type")}
  if channel(struct{}{})!="chan string" || receiveChannel(struct{}{})!="<-chan string" || sendChannel(struct{}{})!="chan<- string" || anyChannel(struct{}{})!="chan interface {}" {t.Fatal("channel directions")}
  cchan:=make(chan string);if channelIdentity(cchan).(chan string)!=cchan {t.Fatal("channel identity")}
  var nilchan chan string;boxed:=channelIdentity(nilchan);if boxed==nil || boxed.(chan string)!=nil {t.Fatal("typed nil channel")}
  if delayedField(struct{}{})!="41" || delayedAlias(struct{}{})!="41" {t.Fatal("deferred field inference")}
  if read(struct{}{})!="concrete" || argument(struct{}{})!="concrete" || annotated(struct{}{})!="concrete" {t.Fatal("interface argument")}
  if choose(true).Read()!="concrete" || chooseMatch(true).Read()!="concrete" {t.Fatal("branch interface")}
  if choose(false)==nil || chooseMatch(false)==nil {t.Fatal("branch typed nil")}
  func(){defer func(){if recover()==nil {t.Fatal("nil field read did not panic")}}();ignoredField(nil)}()
  p:=nilPointer(struct{}{});if p==nil {t.Fatal("typed nil lost")}
  if p.(*model.Concrete)!=nil {t.Fatal("typed nil changed")}
  c:=model.NewContainer()
  if count(c)!=41 || named(c)!="hello" {t.Fatal("fields")}
  if names(c)!=nil || reader(c)!=nil {t.Fatal("nil field changed")}
  c.Names=[]string{"a"};ns:=names(c);ns[0]="b";if c.Names[0]!="a" {t.Fatal("List boundary shared the backing array")}
  if order(c)!=4102 {t.Fatal("field read moved after mutation")}
  r:=request(struct{}{});if !r.IsOk() || r.Value!="application/json" {t.Fatal(r)}
 }
 `
	for name, data := range map[string][]byte{"bridge.go": gen, "bridge_test.go": []byte(test)} {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0644); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command("go", "test", "./...")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generated bridge: %v\n%s\n%s", err, out, gen)
	}
}

func TestFieldAndInterfaceDiagnostics(t *testing.T) {
	if testing.Short() {
		t.Skipf("long test: %.2fs on last run, skipping in short mode", 0.40)
	}

	cases := []struct{ name, body, code, reason string }{
		{"unexported", "let x = (model.NewContainer ()).hidden", "E0401", "hidden"},
		{"unsupported", "let x = (model.NewContainer ()).Bytes", "E0501", "byte/uint8"},
		{"nonimplementer", "let x = model.Use 42", "E0401", "model.Reader"},
		{"pointer_only", "let x = model.Use (model.ConcreteValue ())", "E0401", "model.Reader"},
		{"channel_nonimplementer", "let x = model.Use (model.Channel ())", "E0401", "model.Reader"},
		{"invariant_channel", "fn bad (x: Chan (Ptr model.Concrete)) -> Chan model.Reader = x", "E0401", "model.Reader"},
		{"invariant_slice", "fn bad (x: Go.Slice (Ptr model.Concrete)) -> Go.Slice model.Reader = x", "E0401", "model.Reader"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			l, _ := boundaryLoader(t)
			src := "package bridge\nimport go \"example.test/dependency/model\"\n" + tc.body + "\n"
			sf, ds := syntax.Parse([]byte(src), "boundary.gom")
			if requireNoErrors(t, ds) {
				t.Fatal("parse")
			}
			rf, ds := resolve.File(sf, &resolve.Config{Loader: l})
			if requireNoErrors(t, ds) {
				t.Fatal("resolve")
			}
			_, ds = check.Check(rf, l)
			found := false
			for _, d := range ds {
				if string(d.Code) == tc.code && strings.Contains(d.Message, tc.reason) {
					found = true
				}
			}
			if !found {
				t.Fatalf("want %s containing %s; got %+v", tc.code, tc.reason, ds)
			}
		})
	}
}

func TestFieldLookupAndPointerMethodSet(t *testing.T) {
	l, _ := boundaryLoader(t)
	if l.InterfaceAssignable(&gt.Go{Ref: gt.NamedRef{Path: "missing", Name: "Unknown"}}, &gt.Go{Ref: gt.NamedRef{Path: "missing", Name: "Unknown"}}) {
		t.Fatal("unknown named type accepted")
	}
	pkg := membersOf(t, l, "example.test/dependency/model")
	sig, _ := l.SignatureOf(pkg.Members["NewContainer"])
	recv := gt.Chase(sig.Type).(*gt.Func).Ret
	sym, field, raw, ok := l.FieldOf(recv, "Count")
	if !ok || sym.Hidden != "" || !gt.Equal(field.Type, gt.Int) || !gt.Equal(raw, gt.GoInt) {
		t.Fatalf("field %v %v %v %v", sym, field, raw, ok)
	}
	if _, _, _, ok := l.FieldOf(recv, "hidden"); ok {
		t.Fatal("unexported field exposed")
	}
	amb, _ := l.SignatureOf(pkg.Members["Ambiguous"])
	if _, _, _, ok := l.FieldOf(gt.Chase(amb.Type).(*gt.Func).Ret, "Count"); ok {
		t.Fatal("ambiguous promoted field exposed")
	}
}

// An interface expectation must not bind or prematurely default a numeric meta.
// The generated-Go literal-to-Float issue is tracked separately in TODO.md.
func TestInterfaceAssignmentKeepsNumericInference(t *testing.T) {
	l, _ := boundaryLoader(t)
	src := `package bridge
 import go "example.test/dependency/model"
 fn late_numeric () -> String = {let n = 1; ignore model.Accept n; model.Accept (n + 0.5)}
 `
	sf, ds := syntax.Parse([]byte(src), "numeric.gom")
	if requireNoErrors(t, ds) {
		t.Fatal("parse")
	}
	rf, ds := resolve.File(sf, &resolve.Config{Loader: l})
	if requireNoErrors(t, ds) {
		t.Fatal("resolve")
	}
	cf, ds := check.Check(rf, l)
	if requireNoErrors(t, ds) {
		t.Fatal("check")
	}
	fn := cf.Decls[0].(*check.FnDecl)
	binding := fn.Body.(*check.Block).Stmts[0].(*check.Let)
	if !gt.Equal(gt.Chase(binding.Value.Type()), gt.Float) {
		t.Fatalf("numeric inference prematurely pinned to %v", binding.Value.Type())
	}
}
