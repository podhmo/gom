# Go interface assignments and read-only exported struct fields

- 状態: 採用
- 日付: 2026-11-03
- Supersedes: [gist-cli-host-boundary](2026-11-03-gist-cli-host-boundary.md) のうち field/interface の host primitive

The Gist CLI prototype needs `*strings.Reader` at `io.Reader` arguments,
`os.Stderr` at `io.Writer`, and reads of `http.Request.Header` and
`http.Response.StatusCode`/`Body`.

## Proposal

Keep type equality invariant. At expression expectation boundaries only,
consult the loader's shared `go/types` session for Go interface assignability
and record an explicit typed conversion for lowering to `ConvIface`.
Do not make containers or function types covariant through unification.

Allow read-only selectors of exported fields on named Go struct values and
pointers. Use Go lookup rules, including promoted fields and ambiguity.
Unexported or ambiguous members remain missing; unsupported field types
produce E0501 at the selector only. The typed selector records its raw Go
field type so lowering converts `int` to Gom `Int` explicitly. The Core
selector retains the exact Go field symbol, and codegen merely prints it.

No field writes, Go struct literals, anonymous struct type surface, or
container copy conversion is introduced. Those remain separate work.

## Verification plan

Prototype actual HTTP selectors and concrete-to-interface calls, test rejected
nonimplementers and pointer method sets, nil interface/typed nil pointers,
exported/unexported/promoted fields, raw integer conversion, unsupported byte
fields, and Go callers of generated Gom APIs. Record results under docs/sketch
before adopting and updating spec/design/open questions.

## Outcome

The prototype passed the focused executable tests and spec/golden fixtures.
Adopt the assignment and read-only selector rules. See the
[verification report](../sketch/2026-11-03-go-http-boundary-verification.md).
Go struct construction and writes remain undecided; byte/container boundaries
remain TODOs. Gist-specific policy stays in Gom while the four now-unnecessary
host primitives are removed.
