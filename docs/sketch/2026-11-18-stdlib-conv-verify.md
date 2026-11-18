# `Result.to_option` / `Option.to_list` 配置決定の検証

2026-11-18、wave21 lane 2。
[decision](../decisions/2026-11-18-stdlib-conv-placement.md)（draft → 採用）の検証メモ。

## 決定の確認どころ

- メンバーは下層モジュールの面に載る（`Result.to_option`、`Option.to_list`）
- forward 先は上層パッケージの手書き Go（`option.ResultToOption`、
  `list.OptionToList`）— Go import DAG（`result < option < list`）に沿う
- `.gomi` には宣言せず、`internal/stdlib` が `.gomi` 由来の表を拡張する
  （`.gomi` が公開面の正という原則へのコメント注記を両 `.gomi` に残した）

## 検証と結果

1. **型検査**: `testdata/spec/ok_stdlib_conv.gom` —
   `Result.to_option r`、`Option.to_list o`、`|>` チェーン、
   高階引数への裸参照（`run Result.to_option`、DeclArity 1 での
   adaptation）がすべて診断なしで通る（TestSpec）。
2. **emission**: `testdata/golden/stdlib_conv.golden.go` —
   `option.ResultToOption[int64, error](r)` /
   `list.OptionToList[int64](o)` の形で、上位パッケージの関数に
   `CallInfo.TypeArgs` 順の instantiation 付きで emit される
   （generic callee の `gosig.TypeParams` は両者とも宣言順に設定済み）。
3. **Go 単体**: `TestResultToOption`（stdlib/option）、
   `TestOptionToList`（stdlib/list）— Ok/Some → 値保持、Err/None →
   空の変換を pin。
4. **依存 DAG 不変**: `option/conv.go` は既存の `result` import を
   使うだけ、`list/impl.go` は `option` の import を追加 — どちらも
   下層を参照しない新規 import なので `result < option < list` を維持。
   `go build ./...` 通過（循環なし）。
5. **回帰**: `go test ./...` 全パッケージ PASS、`gofmt -l .` 空、
   `go vet ./...` クリーン、`sh scripts/check-sumtype.sh` 通過。

## 補足

- 手組みメンバーは `check.Signature.TypeParams`（`[a, e]`/`[a]`、
  `any` 制約）と `lower.GoSig.TypeParams` を揃えて登録した —
  `instantiateGoSig` が `CallInfo.TypeArgs` を宣言順に代入するので、
  emit される `pkg.F[T, U]` と境界変換の型が一致する。
- `sym.Surface` は `Result.to_option` / `Option.to_list`（診断・
  ドキュメント名）、`sym.Name` は emit 名（`ResultToOption` /
  `OptionToList`）。
