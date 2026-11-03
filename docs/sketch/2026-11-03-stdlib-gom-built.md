# sketch: stdlib built by the compiler (P4-8 verification)

- 目的: `stdlib/{list,option,result}` を Gom で書き、`.gomi` 付きで
  コンパイラ自身がビルド・テストできることを確認する（P4-8 の検証記録）
- 関連: TODO P4-8、[decision: list-literal-spread](../decisions/2026-11-03-list-literal-spread.md)、
  [decision: stdlib-packaging](../decisions/2026-11-03-stdlib-packaging.md)

## 構成

各パッケージは 4 種のファイルで構成される:

| ファイル | 役割 |
| --- | --- |
| `types.go` | 手書きの Go 表現（`List[A] []A`、`Option[A]` 封印 interface、`Result[T,E]` タグ付き struct）＋境界ヘルパ |
| `*.gom` | Gom 実装＋ `test` 宣言（`gom test` で走る） |
| `*.gomi` | 公開面 — `gom check` が実装との適合を検査し、`iface.ExportNames` が公開名をエクスポートに流す |
| `*.go`, `*_test.go` | 生成物 — `gom build`/`gom test` の成果としてコミットする |

`internal/stdlib` は埋め込み `.gomi`（`stdlib/gomis.go` の
`embed.FS`）をパイプライン前半で一度だけコンパイルし、
`iface.Surface` のメンバーを

- resolver の組み込みモジュール表（`Modules["List"]["map"]` 等）
- checker のシグネチャ供給（`Sigs`）
- lowering の宣言 arity（`Lower.FuncArity`）

へ展開する。`gomstd.Apply` がこの 3 点を `build.Options` に合成
し、`cmd/gom`・spec/golden スイート・tutorial・gist-cli・
skill snippets・fuzz harness が全て同じ配線を使う。

## 検証手順と結果

```
$ gom check stdlib/list stdlib/option stdlib/result
{"kind":"summary","ok":true,"errors":0, ...}      # .gomi 適合検査込み

$ gom build stdlib/list stdlib/option stdlib/result
outputs: list.go option.go result.go             # 生成 Go は vet clean

$ gom test stdlib/list stdlib/option stdlib/result
tests: {"run":17,"failed":0,"skipped":0}          # 生成 *_test.go が go test で全パス
```

spec 冒頭例（`testdata/spec/ok_stdlib_preamble.gom` と同じ形）を
`gom build` → `go run` で実行:

```gom
let shapes = [Circle 1.0, Rect 2.0 3.0]
let total = shapes |> List.map area |> List.sum
```

出力 `9.14159`（π·1² + 2·3）。`List.map` が部分適用
（宣言 arity 2 で残りのリスト引数を取るクロージャ）として降り、
`list.Map` / `list.Sum` のエクスポート名が出ることは
`testdata/golden/stdlib_modules.golden.go` が固定する。

## 過程で潰した構造上の問題

1. **自己 import**: `stdlib/list/list.gom` の生成物は
   `package list` なのに builtin `List a` が `list.List` を要求し
   自己参照になる → `core.File.SelfImportPath` で自分の import
   path を知らせ、一致する参照は無修飾で出す。
2. **rt との循環**: `rt` が境界ヘルパのために `option`/`result`
   を import する一方、生成コードは `rt.InvalidVariant`/`rt.Todo`
   /`rt.Assert*` を使う → 境界ヘルパを対象パッケージへ移した
   （`result.FromGo`/`result.FromErr`/`result.AssertOk`、
   `option.FromGo`）。`rt` は葉になり、stdlib は自由に使える。
   `rt` のテストは `package rt_test` へ移した。
3. **stdlib 間の循環**: `option.to_list`（option→list）と
   `list.get`（list→option）、`result.to_option`（result→option）と
   `option.to_result`（option→result）は Go の import cycle を
   許さないため片側を採用しない — layering を
   `result < option < list` に固定し、`to_option`・`to_list` は
   存在しない（各 `.gom`/`.gomi` に理由を記した）。
4. **`[x, ..xs]` リテラル**: spec が書き方を規定し下流 phase が
   対応済みだったが parser だけが E0101 で拒否していた →
   `[x, ..rest]` を受理するよう parse_expr.go を修正
   （[decision](../decisions/2026-11-03-list-literal-spread.md)）。

## 残存する制約（将来の候補）

- `.gomi` の `let` メンバーは `MemberLet` として surface には出るが、
  generalized let は thunk 実装になるため外部参照との形が一致しない
  — 現行 stdlib の `.gomi` は `fn` のみを宣言する。
- `SelfImportPath` は `stdlib/<pkg>/` の予約ディレクトリ規約で決める。
  ユーザーのモジュールが同名レイアウトを使うと同じ規約が適用される。
- `.gomi` の type メンバー（`List.Foo` 的なモジュール内型）は
  member table に乗せていない。現行の `.gomi` は型宣言を持たない。
