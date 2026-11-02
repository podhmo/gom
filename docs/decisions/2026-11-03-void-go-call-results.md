# Zero-result Go calls are statements followed by Gom Unit

- 状態: 採用
- 日付: 2026-11-03

## Problem

The import loader currently classifies both zero and one Go result as
`GoResultsOne`. A zero-result call in a Gom expression therefore reaches
codegen as `return runtime.Gosched()` or `x := runtime.Gosched()`, which
Go rejects. The same gap appears in function-value adapters and cute
placeholders, not only named function tails.

## Proposal

Represent the zero-result signature explicitly as `GoResultsNone`.
Lowering executes its CallGo in a discard binding and returns an explicit
Unit literal to all value consumers. Core validation accepts this shape
only in a discard binding, with a Unit type; it rejects direct value/tail
uses. The Go emitter continues to render the already decided statements
and values without inferring the signature from names or AST appearance.

The loader supplies authoritative result counts. The legacy signature
fallback interprets a Gom Unit result as zero Go results; a real
single-result `struct{}` is still an unsupported Go signature rather
than being mistaken for void.

## Verification plan

Compile and run tails, Unit bindings/arguments, nested branches, top-level
initializers, Go function values and cute calls. Go callers must be able
to call the generated Unit-returning functions. Pin the import result
shape and malformed Core rejection; inspect generated golden output.

## Verification

[Prototype and regression report](../sketch/2026-11-03-void-go-call-verification.md).
The prototype, generated golden and Go-caller execution tests passed;
`gofmt -l .`, `go vet ./...`, and `go test ./...` also passed on Go 1.27.3.
The language behavior is unchanged; this repairs the promised Unit mapping.
