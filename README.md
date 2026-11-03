> [!NOTE]
> Gom is under active development and is not yet complete. Syntax,
> semantics, generated code, and the command-line interface may change
> without notice.

# Gom

Gom is a statically typed, expression-oriented functional language that
transpiles to readable Go. It combines ML-style features — sum types,
exhaustive pattern matching, curried functions, and Hindley–Milner type
inference — with direct calls into Go packages.

```gom
package main

import go "fmt"

type Shape =
  | Circle (radius: Float)
  | Rect (w: Float) (h: Float)

fn area (s: Shape) -> Float = match s {
  Circle r -> 3.14159 * r * r
  Rect w h -> w * h
}

fn main () -> Unit = {
  ignore (fmt.Println (area (Rect 2.0 3.0)))
  ()
}
```

`gom build main.gom` writes `main.go`, which is ordinary Go you can run,
read, and debug. Curried functions become nested `func` values, ADTs become
sealed interfaces with variant structs, and `//line` directives point back
to the `.gom` source.

## Quick start

Go 1.27 or newer is required.

```sh
go build -o gom ./cmd/gom

cd tutorial/01-hello
gom build main.gom
go run .
```

Each tutorial step is its own Go module, with a `replace` directive that
points generated code at this repository.

## Commands

| Command | Purpose |
| --- | --- |
| `gom build` | Compile `.gom` files to `.go` |
| `gom check` | Type-check and pattern-check without emitting code |
| `gom test` | Run `test` blocks written in `.gom` files |
| `gom fmt` | Format `.gom` files (no configuration) |
| `gom fix` | Apply diagnostic fixes marked safe |
| `gom info` | Generate or update `.gomi` interface drafts |
| `gom dump` | Print a compiler phase's IR (developer tool) |
| `gom explain` | Explain an error code |
| `gom schema` | Print the JSON Schema of the message stream |

Diagnostics are JSON Lines by default; add `-format text` for a human-readable
rendering.

## Where to look

| Topic | File |
| --- | --- |
| Executable language tour | [`tutorial/README.md`](tutorial/README.md) |
| Practical guide for agents and newcomers (syntax tour, common mistakes) | [`skills/gom-agent-guide/SKILL.md`](skills/gom-agent-guide/SKILL.md) |
| Normative language spec (Japanese) | [`docs/spec.md`](docs/spec.md) |
| Formal grammar | [`docs/grammar.ebnf`](docs/grammar.ebnf) |
| Compiler design and implementation principles | [`docs/design.md`](docs/design.md) |
| Diagnostic catalog | [`docs/error-codes.md`](docs/error-codes.md) |
| Design decisions and their rationale | [`docs/decisions/`](docs/decisions/) |
| Known spec/implementation gaps and open concerns | [`docs/implementation-gaps.md`](docs/implementation-gaps.md), [`docs/problems/`](docs/problems/) |
| A larger example program (Gist CLI) | [`examples/gist-cli/README.md`](examples/gist-cli/README.md) |
| Repository layout and development commands | [`skills/gom-agent-guide/references/project-layout.md`](skills/gom-agent-guide/references/project-layout.md) |

The compiler itself is written in Go. Sources flow through
`syntax` → `resolve` → `check` → `pattern` → `lower` → `codegen` under
[`internal/`](internal/), with the CLI in [`cmd/gom/`](cmd/gom/) and runtime
support for generated code in [`rt/`](rt/) and [`stdlib/`](stdlib/).
