# Go HTTP boundary verification

- 日付: 2026-11-03
- 対象: P3-2a
- 決定: [Go interface assignment and fields](../decisions/2026-11-03-go-interface-assignment-and-fields.md)

## Prototype

Draft was recorded in `4ec2950`; `2ed33e4` implements the prototype. The loader's
shared `go/types` session checks assignability to named interfaces or `error`.
The checker inserts a typed `Ascribe` with `Iface` only at expression expectation
boundaries; numeric source metas and unresolved selectors retain inference until defaults
and field lookup resolve, then the pending assignment is checked. Invariant unification is unchanged. Lowering turns this into explicit
`ConvIface`, so codegen does not decide assignability.

`FieldOf` uses Go's selector lookup and exposes only exported struct fields.
The typed member records both Gom and raw Go field types; lowering emits the
raw selector, snapshots it immediately, and converts e.g. `int` to `Int`.
Snapshotting matters even for otherwise atom-like selectors: another argument
may mutate the field, and reading a field through a nil pointer must still panic
when its result is discarded.

## Executed probes

`internal/goimport/boundary_test.go` builds a separate replaced dependency,
transpiles a Gom bridge, then executes Go tests calling the generated functions.
The bridge calls back into the dependency and creates an HTTP request without
sending it. This verifies both directions with real Go types:

- Concrete `*strings.Reader` into `http.NewRequest`'s `io.Reader`, request Header
  read and named Header methods, no network request.
- Concrete custom pointer into a named interface; pointer-only method sets reject
  nonpointer values, and nonimplementers are rejected with E0401.
- Interface conversions in `if`/`match`, typed let bindings, return values and Gom
  function arguments. No covariance is introduced for `Go.Slice` element types.
- A typed nil pointer remains a nonnil interface with a nil dynamic pointer;
  a nil interface field stays nil.
- Named string fields retain identity, promoted `int` fields convert to `int64`,
  nil slices stay nil and nonnil `Go.Slice` fields retain shared backing storage.
- `model.Pair (c.Count, model.Change c)` snapshots Count before Change mutates it.
- A discarded field read through a nil pointer still panics.
- Unexported fields stay hidden; ambiguous promoted fields are not exposed;
  unsupported byte/function field types report E0501 only at use.
- Missing loader identities are rejected safely rather than dereferencing nil.

Focused boundary tests and the new spec/golden fixtures passed. The generated
Go golden explicitly shows `io.Reader(...)`, `io.Writer(...)`, raw selector
temporaries, and the `int64` status conversion.

## Scope

Go struct construction/writes, `byte` I/O, JSON destinations and slice/List copy
conversion remain separate tasks. This does not complete all of P3-2 or P3-3.
The former Gist adapter boundary is reduced by direct reader/writer assignments
and field selectors; remaining generic I/O and JSON primitives remain explicit.

## Final checks

After removing `Reader`, `Stderr`, `RequestHeader`, and `StatusCode` from the
example host, the CLI directly uses `strings.NewReader`, `os.Stderr`,
`request.Header`, and `response.StatusCode`. The fake-server integration suite
passed with `-count=1`, including create/update and all existing failure cases.
No live Gist API was called.

Final commands passed:

- `gofmt -l .`: empty output.
- `go vet ./...` and `go test ./...`.
- `make check-sumtype check-staticcheck`; the sumtype canary rejected its
  intentional missing case as expected. Staticcheck first exposed an existing
  Gist test's numeric HTTP status literal, now replaced with
  `http.StatusTemporaryRedirect`.
- `go test -race ./internal/goimport ./examples/gist-cli`.
- `git diff --check`.

P3-2 remains partially open; only P3-2a is marked complete.

## Additional inference probe

A named empty Go interface accepts defaulted numeric expressions, without
injecting its interface type into the numeric meta. Such conversions are checked
after default resolution. A check-only regression confirms that
`let n = 1; ignore model.Accept n; model.Accept (n + 0.5)` still infers Float.
Its compiled execution is blocked by a separately confirmed pre-existing codegen
bug: even `fn f () -> Float = { let n = 1; n + 0.5 }` produces `n := 1` and Go
rejects `0.5 truncated to int`. This is recorded in TODO and the earlier
[integer-let concern](../problems/2026-11-02-let-int-literal-go-type.md).

## Independent review and implementation response

The fresh review of `32af0c3` found three P2 issues. As the original implementer,
I judged all three to require changes because each violates the assignment
boundary being added here:

1. `model.Kind 1`, where Kind reports `%T` of a named empty interface argument,
   stored a Go `int` although the oracle validated Gom `Int` as `int64`. Lowering
   now materializes numeric values with explicit `ConvNum` before `ConvIface`.
   Execution checks `int64`, `float64`, and `int32` (Char) dynamic types.
2. `chan string`, `<-chan string`, and `chan<- string` were missing from both
   Go-origin detection and the oracle's Go-type reconstruction. Both now preserve
   channel direction; `chan any` is covered too. Execution verifies dynamic types,
   channel identity, and typed nil distinct from a nil interface. Nonimplementers
   and invariant channel element mismatches remain rejected.
3. A field selected on a later-inferred lambda receiver was unified with an
   interface before field resolution. `consume (fn x -> model.Accept x.Count)
   (model.NewContainer ())` now defers the assignment until selector resolution.
   Bare unconstrained lambda parameters still infer their interface type from use;
   deferral is limited to source metas with independent numeric or selector
   constraints. Aliases of unresolved field values are covered too.

Each issue was registered in TODO before its fix. The new
`go_interface_review` golden and spec fixture exercise all three with a small
Go fixture package; the build harness imports that package so changes invalidate
its test cache. The separate module's Go→Gom→Go execution additionally checks
the observable dynamic types and result of the delayed field case.

Review-response validation passed: `gofmt -l .` (empty), `go vet ./...`,
`go test ./...`, uncached fake-server CLI tests, focused interface/field execution,
`go test -race ./internal/goimport ./examples/gist-cli`,
`make check-sumtype check-staticcheck`, and `git diff --check`.

`Go.Any` retains its pre-existing dedicated `ConvBoxAny` path, which accepts all
Gom values rather than just Go-origin values; reconstructing Go `any` for channel
elements must not accidentally route this expectation through the named-interface
checker. A separate existing direct-literal dynamic-type bug in that path was
reported to the parent and reproduced there as `fmt.Sprintf ("%T", 1)` yielding
`int`; it is recorded in the persistent gaps ledger and TODO independently of
this review response.

## Re-review and final implementation judgment

The fresh reviewer rechecked `d807b34` and found no additional issue requiring a
change. The reviewer confirmed the three fixes and their regression coverage,
empty `gofmt` output, full vet/test results, uncached fake-server CLI execution,
and `git diff --check`.

As the original implementer, I judge that no further change is needed for P3-2a:
the independent re-review agrees with the executable results and confirms the
identified assignment defects are addressed. The separate `Go.Any` boxing and
Float-literal binding issues remain recorded follow-ups. This final response
changes documentation only, so the full test suite was not repeated;
`git diff --check` passed.
