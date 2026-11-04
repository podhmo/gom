# 決定の記録

1 件 1 ファイル。決定を覆すときは元のファイルを消さず、新しいファイルの「Supersedes」で参照する。

## ファイル名

- 新しい決定は `YYYY-MM-DD-<slug>.md`。日付は作成日で、状態や内容を更新してもファイル名は変えない。並列の worktree で同時に足しても名前が衝突しないように、連番は使わない
- 同じ日付で slug がかぶったら slug を変える
- 以前は連番（[juxtaposition-and-currying](2026-11-01-juxtaposition-and-currying.md)〜[development-order](2026-11-01-development-order.md)）だったが、日付形式に改名した
- 一覧表は `go generate ./internal/tool/decisionsdoc` で各ファイルの frontmatter（`# タイトル`、`- 状態:`、`- 日付:`、`- Supersedes:`）から生成する。行を手で編集しない。squash merge で衝突したら、マージ後に再生成すればよい
- `Supersedes:` の行の値に `のうち` が含まれると「一部を置き換え」として一覧に注記される（含まれなければ全置き換え扱い）

## 状態

実装中に生まれた決定は次の順で進める。

1. `draft`: 案と背景を書いた段階。まだ spec / design には反映しない
2. `prototype`: 案を検証するための prototype を作っている段階。検証の手順と結果は `docs/sketch/` にレポートとして書き、ここからリンクする
3. `採用` / `不採用`: 検証結果を踏まえて決着した状態。`採用` なら spec.md / design.md を更新し、作業を TODO.md に足す。`不採用` でもファイルは消さない

既存の「一部採用、一部未決」「未決」は、P0 の実験で決める決定に使っている。

## 一覧

<!-- BEGIN GENERATED -->
<!-- Code generated from docs/decisions/*.md by "go generate ./internal/tool/decisionsdoc". DO NOT EDIT. -->
| ファイル | 決定 | 状態 |
| --- | --- | --- |
| [compiler-in-go](2026-11-01-compiler-in-go.md) | コンパイラは Go で実装 | 採用 |
| [concurrency-model](2026-11-01-concurrency-model.md) | 並行処理は `go` 式・チャネル型・`select` 式だけを言語に入れる（一部を [select-receive-arms](2026-11-01-select-receive-arms.md) で置き換え） | 採用 |
| [core-ir-invariants](2026-11-01-core-ir-invariants.md) | Core IR の不変条件 | 採用 |
| [cute-placeholder](2026-11-01-cute-placeholder.md) | Go 呼び出しの `_` プレースホルダ（`cute` 方式） | 採用 |
| [development-order](2026-11-01-development-order.md) | 開発順序を P0〜P5 に変える | 採用 |
| [evaluation-order](2026-11-01-evaluation-order.md) | 評価順序 | 採用 |
| [formal-grammar-interpretations](2026-11-01-formal-grammar-interpretations.md) | Interpretations adopted in the formal grammar (spec 1.5 / 1.6 formalization) | 採用（P1-3 のパーサーがこの解釈のまま実装・検証した。パーサー側の追加解釈は [2026-11-02-p1-3-parser-interpretations](2026-11-02-p1-3-parser-interpretations.md) を参照） |
| [fsharp-pipes](2026-11-01-fsharp-pipes.md) | F# 流のパイプと関数合成 | 採用 |
| [function-representation](2026-11-01-function-representation.md) | 関数値の表現（一部を [function-value-representation](2026-11-02-function-value-representation.md) で置き換え） | 採用（P0-2 の実験で決定。根拠: [sketch: P0-2](../sketch/2026-11-01-p0-2-function-representation.md)） |
| [generated-code-goals](2026-11-01-generated-code-goals.md) | 生成 Go コードの位置づけ | 採用 |
| [go-calls-as-tuples](2026-11-01-go-calls-as-tuples.md) | Go 関数は Go の書き方で呼び、型は自動で取り込む（一部を [go-interop-boundary](2026-11-01-go-interop-boundary.md) で置き換え） | 採用 |
| [go-interop-boundary](2026-11-01-go-interop-boundary.md) | Go 相互運用の境界 | 採用 |
| [gomi-interface-files](2026-11-01-gomi-interface-files.md) | `.gomi` を採用し、ライブラリでは必須、`pub` は廃止 | 採用 |
| [inline-tests](2026-11-01-inline-tests.md) | テストは実装と同じファイルに書く | 採用 |
| [json-diagnostics](2026-11-01-json-diagnostics.md) | コンパイラのメッセージは既定で JSON | 採用 |
| [juxtaposition-and-currying](2026-11-01-juxtaposition-and-currying.md) | 関数適用は並置 `f x y`、関数はカリー化（一部を [function-representation](2026-11-01-function-representation.md) で置き換え） | 採用 |
| [labeled-required-arguments](2026-11-01-labeled-required-arguments.md) | ラベル付き必須引数 | 採用 |
| [language-name](2026-11-01-language-name.md) | 言語名は Gom | 採用 |
| [no-offside-rule-and-gom-fmt](2026-11-01-no-offside-rule-and-gom-fmt.md) | オフサイドルールを持たず、出力が一意な `gom fmt` を持つ | 採用 |
| [result-pipe-auto-wrap](2026-11-01-result-pipe-auto-wrap.md) | `\|?>` と `%w` による自動ラップ | 採用 |
| [select-receive-arms](2026-11-01-select-receive-arms.md) | `select` の受信腕はチャネルごとに 1 つの `case` | 採用 |
| [try-boundary](2026-11-01-try-boundary.md) | `try` の脱出先は最も内側の関数（一部を [result-pipe-local-boundary](2026-11-03-result-pipe-local-boundary.md) で置き換え） | 採用 |
| [try-keyword](2026-11-01-try-keyword.md) | 早期脱出は前置の `try` | 採用 |
| [value-representation](2026-11-01-value-representation.md) | 値の表現（ADT・List・Result） | 採用（P0-1・P0-3・P0-4 の実験で決定） |
| [value-restriction](2026-11-01-value-restriction.md) | 一般化は value restriction 付き | 採用 |
| [ast-representation](2026-11-02-ast-representation.md) | Placement and shape of the pre-Core intermediate representations | draft |
| [build-driver](2026-11-02-build-driver.md) | The P1-11 build driver: error boundaries, compilation unit, and CLI shape | 採用 |
| [builtin-type-identity](2026-11-02-builtin-type-identity.md) | Builtin type identity is decided by declaration, not by name | 採用 |
| [canary-vet-analysis](2026-11-02-canary-vet-analysis.md) | Compile canary: stdlib-only vet-equivalent analysis on generated Go | 採用 |
| [ci-check-scope](2026-11-02-ci-check-scope.md) | Scope of CI checks and `make check` (B-2) | 採用 |
| [constant-zero-division](2026-11-02-constant-zero-division.md) | `/` / `%` の定数ゼロ除数は型検査エラー（E0414） | 採用 |
| [core-ir-node-inventory](2026-11-02-core-ir-node-inventory.md) | Core IR node inventory and validator | draft |
| [diag-internal-model](2026-11-02-diag-internal-model.md) | Shape of the internal diagnostic model (internal/diag) | 採用 |
| [diag-output-formats](2026-11-02-diag-output-formats.md) | Diagnostic output formats: JSON Lines and text emitters (P1-10) | draft |
| [error-code-table](2026-11-02-error-code-table.md) | Error-code table and numbering scheme (D-2) | 採用 |
| [eval-value-model](2026-11-02-eval-value-model.md) | Value model of the typed-AST evaluator (internal/eval) | draft |
| [function-value-representation](2026-11-02-function-value-representation.md) | Function value representation: canonical curried form | 採用 |
| [fuzz-strategy](2026-11-02-fuzz-strategy.md) | Fuzz strategy: parser oracle, compile canary, and the knownBugs registry | 採用 |
| [generated-go-conventions](2026-11-02-generated-go-conventions.md) | Generated Go code conventions: names, unused binders, //line（一部を [source-location-correspondence](2026-11-03-source-location-correspondence.md) で置き換え） | draft |
| [generated-support-packages](2026-11-02-generated-support-packages.md) | Generated-code support packages: `rt/` and `stdlib/` | draft |
| [gom-fmt-layout](2026-11-02-gom-fmt-layout.md) | `gom fmt` layout: engine, width, and canonical style (P4-2) | adopted |
| [grammar-based-generator](2026-11-02-grammar-based-generator.md) | Grammar-based random program generator (internal/gomgen) | 採用 |
| [ignore-keyword](2026-11-02-ignore-keyword.md) | `ignore` is a keyword, not a plain function | 採用 |
| [import-go-loader-seam](2026-11-02-import-go-loader-seam.md) | The package-loader seam for `import` and `import go` | draft |
| [let-polymorphism-emission](2026-11-02-let-polymorphism-emission.md) | Emission shape for let-polymorphic bindings (P2-1) | 採用（[sketch: let-polymorphism-emission](../sketch/2026-11-02-let-polymorphism-emission.md) で検証） |
| [monomorphic-checker-design](2026-11-02-monomorphic-checker-design.md) | Design of the P1 monomorphic checker | draft |
| [name-resolution](2026-11-02-name-resolution.md) | Name resolution: scopes, shadowing, and constructor lookup | draft |
| [p1-3-parser-interpretations](2026-11-02-p1-3-parser-interpretations.md) | P1-3 parser: interpretations made while implementing grammar.ebnf | 採用 |
| [pattern-checking](2026-11-02-pattern-checking.md) | Pattern-matrix checking: domains, guards, counterexamples (internal/pattern) | draft |
| [record-recursion-by-value](2026-11-02-record-recursion-by-value.md) | レコード型は自分自身を値として内包できない（E0413） | 採用 |
| [sumtype-exhaustiveness-tool](2026-11-02-sumtype-exhaustiveness-tool.md) | Exhaustiveness checker for sealed interfaces (B-3) | 採用 |
| [tailrec-requirement](2026-11-02-tailrec-requirement.md) | `@tailrec`: a requirement attribute for tail-call optimization | 採用（実装・検証済み: [sketch: tailrec-validation](../sketch/2026-11-02-tailrec-validation.md)） |
| [test-harness](2026-11-02-test-harness.md) | テストハーネスの配置とファイル規約 (internal/gomtest) | 採用 |
| [treedump-format](2026-11-02-treedump-format.md) | IR テキストダンプ形式 (internal/treedump) | draft |
| [agent-skill-example-verification](2026-11-03-agent-skill-example-verification.md) | gom-agent-guide の例示コードを抽出テストで検証する方式（一部を [skill-vendoring-via-symlink](2026-11-03-skill-vendoring-via-symlink.md) で置き換え） | 採用 |
| [extern-override-binding](2026-11-03-extern-override-binding.md) | extern go binding: signature-level overrides with declared conversion rules | 採用（実装・検証した — [検証](../sketch/2026-11-03-extern-binding-verification.md)） |
| [generic-instantiation-cycles](2026-11-03-generic-instantiation-cycles.md) | Recursive generic calls must not grow a type argument (P2-5) | 採用（[sketch: p2-5-generic-emission-verification](../sketch/2026-11-03-p2-5-generic-emission-verification.md) で検証） |
| [gist-cli-host-boundary](2026-11-03-gist-cli-host-boundary.md) | Gist CLI as an executable Gom integration use case（一部を [go-interface-assignment-and-fields](2026-11-03-go-interface-assignment-and-fields.md) で置き換え） | 採用 |
| [go-error-alias-results](2026-11-03-go-error-alias-results.md) | Go の error alias は error 戻り値規約を引き継ぐ | 採用 |
| [go-interface-assignment-and-fields](2026-11-03-go-interface-assignment-and-fields.md) | Go interface assignments and read-only exported struct fields | 採用 |
| [go-types-mapping](2026-11-03-go-types-mapping.md) | go/types → Gom type mapping and where boundary conversions live（一部を [go-collections-and-generics](2026-11-04-go-collections-and-generics.md) で置き換え） | 採用（P1-9 の `internal/goimport` が実装・検証した — `strconv.Atoi`/`Itoa`・`os.LookupEnv`・`bytes.Buffer` メソッドが E2E で動く） |
| [gom-fix-explain-schema](2026-11-03-gom-fix-explain-schema.md) | gom fix / explain / schema: wire shapes and the fix-application policy (P4-4) | 採用 |
| [gom-test-message-stream](2026-11-03-gom-test-message-stream.md) | `gom test` message stream and `go test` execution granularity | 採用 |
| [import-go-loading-mechanics](2026-11-03-import-go-loading-mechanics.md) | How `import go` loads Go packages: go/types + go/importer over `go list -export` | 採用（P1-9 の `internal/goimport` が実装・検証した — E2E で `strconv`/`bytes`/`os`/`fmt` の呼び出しが生成コードの build・run まで通る） |
| [inference-witness-oracle](2026-11-03-inference-witness-oracle.md) | Inference witness oracle: generator-side wanted types vs `check`'s inference | 採用 |
| [list-literal-spread](2026-11-03-list-literal-spread.md) | Spread element in list literals `[x, ..xs]` | 採用 |
| [pipe-try-segmented-fusion](2026-11-03-pipe-try-segmented-fusion.md) | 明示的 `try` を含む `\|?>` チェーンの分割融合 | 採用（評価器対生成 Go の比較・生成コード検査で確認。[検証レポート](../sketch/2026-11-03-pipe-try-segmented-fusion.md)） |
| [result-pipe-local-boundary](2026-11-03-result-pipe-local-boundary.md) | Result パイプの局所的な制御フロー（一部を [pipe-try-segmented-fusion](2026-11-03-pipe-try-segmented-fusion.md) で置き換え） | 採用 |
| [skill-vendoring-via-symlink](2026-11-03-skill-vendoring-via-symlink.md) | `.devin/skills` への skill 公開は symlink で行う | 採用 |
| [source-location-correspondence](2026-11-03-source-location-correspondence.md) | Source-location correspondence: anchor table shape and Go-position restore | 採用 |
| [static-function-alias-declarations](2026-11-03-static-function-alias-declarations.md) | Static function aliases use named canonical function wrappers | 採用 |
| [stdlib-packaging](2026-11-03-stdlib-packaging.md) | stdlib packaging: .gomi-driven exports, builtin module tables, leaf layering | 採用 |
| [test-file-emission-layout](2026-11-03-test-file-emission-layout.md) | `@test` helper とテスト関数の出力先 | 採用 |
| [top-level-let-type-variable-scope](2026-11-03-top-level-let-type-variable-scope.md) | Top-level monomorphic lets cannot capture declaration type variables | 採用 |
| [tutorial-dependency-tracking](2026-11-03-tutorial-dependency-tracking.md) | Track executable tutorial inputs and compiler dependencies | 採用 |
| [tutorial-go-module-inputs](2026-11-03-tutorial-go-module-inputs.md) | Track actual Go module inputs of tutorial subprocesses | 採用 |
| [void-go-call-results](2026-11-03-void-go-call-results.md) | Zero-result Go calls are statements followed by Gom Unit | 採用 |
| [builtin-chan-task-rt](2026-11-04-builtin-chan-task-rt.md) | `Chan`/`Task` ship as compiler-owned member tables backed by rt | 採用（P5-1 で実装・検証した — [検証](../sketch/2026-11-04-p5-1-concurrency-verification.md)） |
| [chan-direction-go-boundary](2026-11-04-chan-direction-go-boundary.md) | Channel direction narrows implicitly only at Go-call boundaries | 採用（P5-1 で実装・検証した — [検証](../sketch/2026-11-04-p5-1-concurrency-verification.md)） |
| [diagnostic-json-schema](2026-11-04-diagnostic-json-schema.md) | 診断 JSON は独自スキーマを維持する | 採用 |
| [eval-concurrency-compiled-only](2026-11-04-eval-concurrency-compiled-only.md) | The evaluator keeps concurrency constructs compiled-only | 採用（P5-1 で実装・検証した — [検証](../sketch/2026-11-04-p5-1-concurrency-verification.md)） |
| [fn-main-entrypoint](2026-11-04-fn-main-entrypoint.md) | `fn main () -> Unit` in `package main` is the Go entrypoint | 採用（`cmd/gom.TestEndToEnd_BuildAndRun` が shim なしで `gom build` → `go build` → 実行まで検証） |
| [formatter-blank-lines](2026-11-04-formatter-blank-lines.md) | `gom fmt` はブロック内の空行を保持する | 採用 |
| [go-collections-and-generics](2026-11-04-go-collections-and-generics.md) | []T → List boundary copies, Go.Slice/Go.Map, and callable generic Go functions | 採用（P3-3 が実装・検証した — [検証](../sketch/2026-11-04-p3-3-go-collections-verification.md)） |
| [goconv-gotypes-oracle](2026-11-04-goconv-gotypes-oracle.md) | `Go.conv` legality: delegate to `go/types`, recheck after metas solve | draft |
| [goconv-list-slice-copies](2026-11-04-goconv-list-slice-copies.md) | `Go.conv` の `Go.Slice T ↔ List T` はコピーする | 採用（実装は TODO） |
| [gomi-conformance-in-gom-test](2026-11-04-gomi-conformance-in-gom-test.md) | `gom test` でも `.gomi` 適合検査を走らせる | 採用 |
| [mutable-sharing-lint](2026-11-04-mutable-sharing-lint.md) | Mutable-sharing lint: what W0701/W0702 check and where they run | 採用（P5-2 で実装・検証した — [検証](../sketch/2026-11-04-p5-2-mutable-lint-verification.md)） |
| [result-pipe-step-must-return-result](2026-11-04-result-pipe-step-must-return-result.md) | `\|?>` の右辺は `Result` を返す関数に限る | 採用 |
| [select-arm-choice](2026-11-04-select-arm-choice.md) | `select` の腕は Go と同じくランダムに選ぶ | 採用 |
| [structural-equality-emission](2026-11-04-structural-equality-emission.md) | Structural `==`/`!=` emission: `rt.Equal` except provably comparable scalars（一部を [generated-equality-comparators](2026-11-05-generated-equality-comparators.md) で置き換え） | draft |
| [try-no-auto-wrap](2026-11-04-try-no-auto-wrap.md) | `try` は `GoError` を自動ラップしない | 採用 |
| [aggregate-function-bindings](2026-11-05-aggregate-function-bindings.md) | Aggregate-bound function values become named wrappers; residual initialization cycles are rejected | 採用（P2-2c の実装・検証: [検証レポート](../sketch/2026-11-05-aggregate-function-bindings-verification.md)） |
| [deferred-member-application](2026-11-05-deferred-member-application.md) | Deferred member application: `x.M(args)` on an unresolved receiver | 採用 |
| [eq-comparator-budget-and-names](2026-11-05-eq-comparator-budget-and-names.md) | Comparator minting is bounded and file-unique; ctor lets keep the declared interface | draft |
| [exported-value-vs-type-name](2026-11-05-exported-value-vs-type-name.md) | Exported `let foo` vs `type Foo`: type names claim first | draft（prototype verified — [sketch: V-6 residual verification](../sketch/2026-11-05-v6-residual-verification.md)） |
| [generated-code-benchmark-harness](2026-11-05-generated-code-benchmark-harness.md) | Generated-code benchmarks live beside the artifacts they measure | 採用（実装・計測は [sketch: P5-3 generated-code perf](../sketch/2026-11-05-p5-3-generated-code-perf.md)） |
| [generated-equality-comparators](2026-11-05-generated-equality-comparators.md) | Generated per-type comparators for `==`/`!=`/`assert_eq` (core.Equal + File.EqDecls)（一部を [eq-comparator-budget-and-names](2026-11-05-eq-comparator-budget-and-names.md) で置き換え） | 採用 |
| [gom-implementation-import-cycles](2026-11-05-gom-implementation-import-cycles.md) | `.gom` implementation imports must be acyclic (E0606) | draft（prototype verified — [sketch: V-6 residual verification](../sketch/2026-11-05-v6-residual-verification.md)） |
| [interface-failure-withholds-output](2026-11-05-interface-failure-withholds-output.md) | `gom build` withholds a failed package's generated output | draft（prototype verified — [sketch: V-6 residual verification](../sketch/2026-11-05-v6-residual-verification.md)） |
| [package-compilation-unit](2026-11-05-package-compilation-unit.md) | The package compilation unit: shared top-level scope and .gomi imports | 採用（[sketch: multifile package verification](../sketch/2026-11-05-multifile-package-verification.md) — V-6 vertical slice verified end to end） |
| [package-initialization-order](2026-11-05-package-initialization-order.md) | Package initialization order: Go's dependency analysis over the whole unit | 採用（prototype で検証済み — [sketch](../sketch/2026-11-05-package-initialization-order-verification.md)。spec.md §5.1 へ反映済み） |
| [pkg-type-ctor-spelling](2026-11-05-pkg-type-ctor-spelling.md) | `pkg.Type.Ctor`: a per-type constructor table for imported types | draft（prototype verified — [sketch: V-6 residual verification](../sketch/2026-11-05-v6-residual-verification.md)） |
| [post-arg-selector-binding](2026-11-05-post-arg-selector-binding.md) | Post-argument selector binding: `x.M(1).N(2)` chains like Go | 採用 |
| [test-name-file-tag](2026-11-05-test-name-file-tag.md) | Same-named `test` blocks in sibling files take a file tag | draft（prototype verified — [sketch: V-6 residual verification](../sketch/2026-11-05-v6-residual-verification.md)） |
<!-- END GENERATED -->
