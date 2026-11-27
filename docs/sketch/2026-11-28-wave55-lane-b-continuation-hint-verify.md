# wave55 lane B: 継続行 E0402 hint

- 日付: 2026-11-28
- lane: [wave55 prompts](2026-11-27-wave55-prompts.md) lane B
- decision: [continuation-line-diagnostics](../decisions/2026-11-27-continuation-line-diagnostics.md)

## 実施内容

- `syntax.File` に parser 由来の `Layout` を持たせ、byte offset から行・byte 列を引けるようにした。位置の正は従来どおり UTF-8 byte offset で、診断出力の Unicode 列計算とは分けている
- `resolve.Resolved.Layouts` で file ごとの layout を checker へ渡し、複数 file の package でも各 statement の所属 file の layout を使う
- mid-block statement が関数型で E0402 を出すときだけ、同一 block の次 statement が同じ file 内の後続行かつ深い列なら、次の note を付ける

```text
if these lines were meant as one application, use |> or wrap the whole application in parentheses
```

- 判定は字下げ幅を問わず「深いこと」だけを見る。間の空行・コメントは AST 上の次 statement を使うため無視される。診断 suppression、NL-2 変更、自動 rewrite は追加していない

## red-first

先に `testdata/spec/E0402_continuation_hint.gom` と期待 JSON を追加し、実装前に次の failure を確認した。

```text
diagnostics[0].notes[0]: expected note not found: "if these lines were meant as one application, use |> or wrap the whole application in parentheses" (actual notes: null)
```

実装後は positive fixture が green になった。

## fixture

- `testdata/spec/E0402_continuation_hint.gom`: `List.fold` を字下げした複数行へ書き、先頭 E0402 に note がある。間に空行とコメントを入れ、それらを無視することを固定した
- `testdata/spec/E0402_continuation_hint_negative.gom`: 同じ深さの独立した関数型式と、関数ではない Int 式では note がないことを、明示的な空 `notes` で固定した
- `testdata/spec/E0402_continuation_hint_paren.gom`: grouping parens が AST node を作らなくても、開き括弧を含む見た目の字下げで note を付けることを固定した
- `testdata/spec/E0402_continuation_hint_paren_negative.gom`: 次式が grouping parens 付きで見た目の深さが同じ場合は note がないことを固定した

## 出力確認

- JSON: spec fixture が期待 notes を含めて通る
- text: `go run ./cmd/gom check --format text testdata/spec/E0402_continuation_hint.gom` の先頭 E0402 に次が出る

```text
= note: if these lines were meant as one application, use |> or wrap the whole application in parentheses
```

## 検証

- `go test ./internal/build -run 'TestSpec/E0402_continuation_hint' -count=1`
- `go test ./internal/gomtest -run 'TestDiffDiagnostics.*Notes' -count=1`
- `go test ./internal/syntax ./internal/resolve ./internal/check -count=1`
- `gofmt -l .`（空）
- `go vet ./...`
- `go test ./...`

## 補助変更

- spec harness の `notes` は、明示的な空配列があれば actual notes が空であることを要求するようにした。省略時は従来どおり non-constraining、非空配列は従来どおり subsequence
- `Layout.StatementLineCol` は AST expression span ではなく、その行の最初の non-whitespace byte の列を返す。grouping parens は AST node を作らず expression span が括弧の内側から始まるため、見た目の字下げを正しく比較するために必要

## 残件

- 連鎖する後続 E0407/E0402/E0401 は decision どおり suppression せず残る
- 手書きの syntax tree では layout がなく note は付かない。compiler pipeline の parse 済み入力では layout が入る
