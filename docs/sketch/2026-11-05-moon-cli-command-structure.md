# `moon` CLI command structure, and a proposed `gom` command set

- Purpose: decide gom's subcommand layout — in particular where `.gomi` emission should live (`gom gomi` vs `gom info`) and what `gom new` should look like.
- Related: TODO P4-1 (`.gomi` checking + draft generation), P4-4 (`fix`/`explain`/`schema`), P4-9 (`gom-agent-guide`; notes `gom new` generating an AGENTS.md router), spec §7 and §12, [decisions/2026-11-01-gomi-interface-files.md](../decisions/2026-11-01-gomi-interface-files.md).
- Sources (checked 2026-11-05): local checkout of <https://github.com/moonbitlang/moon> — `crates/moon/src/cli.rs` (clap enum), `crates/moon/src/cli/{info,new}.rs`, `docs/manual/src/commands.md` (generated help), `docs/dev/reference/{supported-targets,virtual-pkg,build}.md`, `crates/moonbuild/template/moon_new_template.toml`.

## 1. `moon` subcommand inventory

One line each, verbatim or condensed from generated help (`docs/manual/src/commands.md`).

| Group | Command | Purpose |
| --- | --- | --- |
| Project | `new` | Create a new MoonBit module (flags: `--user`, `--name` only) |
| Build | `check` | Type-check without object files; also refreshes `pkg.generated.mbti` (`--no-mbti` opts out); `-w/--watch`, `--fmt`, `--patch-file`, `--explain`, `--json` |
| | `build` | Build packages; `-g/--debug`, `--release`, `--target`, `-w/--watch`, `-d/--deny-warn`, `--frozen` |
| | `run` | Build and run a main package / `.mbt` file / `-e <SCRIPT>` / stdin `-` |
| | `test` | Build and run tests; `-u/--update` snapshots, `-f/--filter` glob, `-i/--index`, `--doc-index`, `-l/--limit`, `--build-only`, `--outline`, `--include-skipped` |
| | `bench` | Run benchmarks; same build flags plus `-p/--package`, `-f/--file`, `-i/--index` |
| | `bundle` | (hidden) Bundle the module |
| | `prove` | Formal verification via Why3 |
| | `runwasm` | (deprecated) Replaced by `moon run --target wasm` / `moonx` |
| | `clean` | Remove `_build` or global caches (`--dep-cache`, `--build-cache`) |
| Source tools | `fmt` | Format sources; `--check`, `--warn`, `--sort-input`, `-- <ARGS>` passthrough |
| | `doc` | Generate docs; `--serve`, `--bind`, `--port` |
| | `info` | Generate public interface (`.mbti`) files for all packages (details in §2) |
| | `explain` | Explain compiler diagnostics (`--diagnostic [CODE]`) and attributes (`--attribute [NAME]`); bare flag lists all |
| Deps | `add`, `remove`, `install`, `fetch`, `tree`, `search`, `view`, `work` | Registry deps, global binary install, dep tree, registry search/metadata, workspace (`init`/`use`/`sync`) |
| Registry | `login`, `whoami`, `register`, `publish`, `package`, `deprecate`, `update` | mooncakes.io account and publishing |
| Misc | `coverage` | Subcommand group: `analyze` / `report` / `clean` |
| | `generate-build-matrix` | Benchmark matrix generator (legacy) |
| | `upgrade` | Upgrade the toolchain itself (`-f`, `--dev`) |
| | `shell-completion` | Emit completions for bash/elvish/fish/pwsh/zsh to stdout |
| | `version` | `--all`, `--json`, `--no-path` |
| Hidden | `cram`, `generate-test-driver`, `tool` | `#[clap(hide = true)]` internal/test plumbing |
| External | `moon <x>` → `moon-<x>` on PATH | cargo-style plugin dispatch; e.g. `moon ide` resolves the `moon-ide` toolchain binary |

Universal flags (on every subcommand): `-q/--quiet`, `-v/--verbose`, `--trace`, `--dry-run`, `-Z` (unstable feature gates, env `MOON_UNSTABLE`); `--frozen` on commands that sync deps.

## 2. `moon info` in depth

- What it emits: `pkg.generated.mbti` next to each selected package's sources — the textual form of the compiled `.mi` interface, produced by a toolchain binary actually named `mooninfo`. It is a generated API snapshot the user commits; diffs of it in review show API-visible changes.
- Selection: positional `PATH`s, `-p/--package` (exact or fuzzy), or all local packages.
- `--target <B>`: also computes the interface for other backends and prints a red/green diff, but never changes which backend writes `pkg.generated.mbti` (canonical = module `preferred-backend` → workspace `preferred-backend` → `wasm`).
- Overlap with `check`: a successful `moon check` also publishes `pkg.generated.mbti` for checked packages whose canonical backend was checked — *only when content differs* (mtime preserved, so no spurious downstream rebuilds). Skipped on failure, `--dry-run`, `--patch-file`, `--no-mbti`, `--watch`.
- Why `info` not `mbti`: the name describes the artifact's role — "the package's public interface information" — not its file extension. `mbti` is one serialization; the command also diffs across backends and its implementation doubles as the `mooninfo` tool. Precedent in the family: `ocamlc -i` ("print inferred interface"). Same pattern elsewhere: `doc` emits a docs site, `package` emits an archive, `tree` prints a dep tree — no command is named after a file format.
- Note moon has *two* interface files: generated `pkg.generated.mbti` (every package, machine-written) and authored `pkg.mbti` (contract of a `virtual` package). `info` manages only the generated one.

## 3. Naming philosophy

- Verbs = actions performed on code or deps: `build check run test bench fmt clean add remove install fetch search login register publish deprecate update upgrade`.
- Nouns = named artifacts or grouped facilities: `info` (interface info), `doc`, `tree`, `coverage`, `work`, `shell-completion`, `version`, `new` (the odd verb that names the event).
- `explain` is a verb that takes the thing to explain — a good fit for gom's planned error-code explainer (moon's also covers language *attributes*, not only codes).
- Hidden subcommands keep dev plumbing out of help (`tool`, `cram`, `generate-test-driver`, `bundle`).
- External `moon-<name>` dispatch keeps the core small and lets tooling ship separately (the IDE helpers live in `moon-ide`, not `moon`).

## 4. Mechanisms worth borrowing

- `test -u/--update` snapshot refresh with `-l/--limit` update passes — maps to gom golden/snapshot tests.
- `-f/--filter` glob, `-i/--index` (single or `0-2` range), `--doc-index`, `--include-skipped`, `--outline` — fine-grained test selection gom will want eventually.
- `check -w/--watch` (and `--fmt` folding a format-check into check) — CI-friendly combos.
- Writing generated files only when content differs (mtime preservation) — directly applicable to `gom info` writing `.gomi`.
- `fmt --check` / `--warn` and `-- <ARGS>` passthrough.
- `-o -` analogue: moon has no stdout mode for `info`, but gom's `dump` already routes diagnostics to stderr so stdout can be a clean artifact — the same trick supports `gom info -o -` (inferred interface printed, `ocamlc -i` style).
- Universal `-q`/`-v`/`--dry-run`; `-Z`-style gated unstable flags.
- `moon-<x>` external subcommands — a cheap plugin seam for later (`gom-ide`, `gom-lsp`).

## 5. Mapping onto gom

gom's surface is smaller: it has no registry, no backends, and Go already owns running/benching/coverage/modules. Proposed set:

| `gom` command | Moon model | Notes |
| --- | --- | --- |
| `build` | `moon build` | exists; keep `-o`, `-format` |
| `check` | `moon check` | exists; future: `-w/--watch`, `--fmt`; authored `.gomi` verification already lives here per spec §7 rule 6 |
| `info` | `moon info` | **replaces planned `gom gomi`**: emit inferred interface — write `<pkg>.gomi` draft (default) or stdout with `-o -`; optional `--check`-style drift report can stay inside `gom check` instead |
| `test` | `moon test` | planned; mirror `-u/--update`, `-f/--filter`, `-i/--index`, `--build-only` |
| `fmt` | `moon fmt` | planned; `--check`, `--warn` |
| `fix` | — (no moon equivalent) | keep; applies `safe` fixes (spec §12) |
| `explain` | `moon explain` | planned; `gom explain <code>`, bare lists codes (moon's `--diagnostic` without arg lists all) |
| `schema` | — | keep; gom-specific because the JSON-lines message stream is a gom design point (moon uses `--output-json` flags instead) |
| `new` | `moon new` | see §6 |
| `dump` | — (moon hides equivalents under `tool`/toolchain binaries) | keep as dev tool; consider hiding from usage like moon's `hide = true` |
| `doc` | `moon doc` | future; docs from `///` comments |
| `clean` | `moon clean` | future, once the build cache lands (design §3 `internal/build`) |
| `version` | `moon version` | cheap win; `--json` matches spec §12's machine-readable stance |
| `run` | `moon run` | optional; thin `build` + `go run` convenience, low priority |
| `shell-completion` | `shell-completion` | future |
| — | `add remove install tree fetch search view work login whoami register publish package deprecate update coverage bench upgrade prove runwasm bundle` | **drop**: all registry/backend/toolchain concerns. `go` owns deps (`go get`), running, bench, coverage, toolchain. gom emitting `.go` means these problems are already solved downstream. |

Why `gom info` over `gom gomi`: moon's convention is command = role/artifact, never file extension (`info`→`.mbti`, `doc`→site, `package`→archive). `info` also covers both directions gom needs — print the inferred interface (`ocamlc -i` style) and write draft files — while `gomi` names an extension, not an operation. Semantic difference to record: gom's `.gomi` is *authored and required* (spec §7), moon's `pkg.generated.mbti` is *generated*; so `gom info` emits drafts the user edits and commits, and `gom check` (not `info`) verifies implementation↔interface. Spec §7 rule 5 and design.md's "`gom gomi` の下書き生成" would need rewording if adopted — that is a spec-facing rename, so it needs a decision first.

## 6. `gom new`, modeled on `moon new`

`moon new <PATH>` has deliberately few flags: `--user` (default: logged-in username, else `username`) and `--name` (default: last path segment; validated to package-id syntax). There is **no `--lib/--bin`** — the template always creates one module containing a root library package plus a `cmd/main` executable package, because a MoonBit module is multi-package anyway.

Generated files (template TOML): `moon.mod`, `moon.pkg` ×2, `main.mbt`, `{{package}}.mbt`, `{{package}}_test.mbt` + `{{package}}_wbtest.mbt`, `README.mbt.md`, `LICENSE` (Apache-2.0), `.gitignore`, `.githooks/pre-commit` (runs `moon check`), a Copilot-setup workflow, and **`AGENTS.md`** — a project-level agent guide that routes to the `moonbit-agent-guide` skill and spells out the `fmt` / `info` / `test` workflow.

Proposed `gom new <PATH> [--name N]`: create `go.mod` (or require running inside a Go module — open question, gom needs `go/types` anyway), a first `.gom` source + `.gomi` for the library package, a `cmd/<name>/` or `main.gom` executable, `_test.gom`, `.gitignore` (generated `.go`?), and the thin `AGENTS.md` router pointing at `$gom-agent-guide` (P4-9's stated consumer). Skip `--user` unless gom ever gains a registry; a `--license` flag is optional (moon hardcodes Apache-2.0).

## 7. Recommendation

1. Adopt `gom info` for `.gomi` draft emission (rename of spec §7 rule 5's `gom gomi`); file a decision, update spec/design/TODO P4-1.
2. `gom info` flags: positional `FILE|DIR` args (existing convention), `-o` for draft destination with `-o -` = stdout (diagnostics → stderr, as `dump` does); write-if-different semantics borrowed from `moon check`'s mbti publish.
3. Keep `check/build/fmt/test/fix/explain/schema/dump`; add `new`, `version` soon; `doc`, `clean`, `run`, `shell-completion`, `moon-*`-style `gom-*` dispatch later; drop all package-manager/registry commands permanently.
4. `gom new` should generate an `AGENTS.md` router like `moon new` does — this is the P4-9 tie-in; design the template once the skill exists.
