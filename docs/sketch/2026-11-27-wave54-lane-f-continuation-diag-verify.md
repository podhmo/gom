# wave54 lane F: 継続行らしき字下げ行の誤パース診断調査

- 日付: 2026-11-27
- lane: [wave54 prompts](2026-11-27-wave54-prompts.md) lane F（調査）
- decision: [continuation-line-diagnostics](../decisions/2026-11-27-continuation-line-diagnostics.md)

## 目的

`List.fold` の適用を複数行へ書いたときの E0407/E0402/E0401 連鎖について、専用診断への畳込み、既存診断への括弧/pipeline hint 追加、現状維持を比較し、実装候補を 1 方針だけ decision へ記録する。lane では実装しない。

## 現状確認

次の形を一時 fixture で確認した。

```gom
fn total (xs: List Int) -> Int = {
  List.fold
    (fn acc x -> acc + x)
    0
    xs
}
```

現行 diagnostics は 7 件だった。

1. `List.fold` の mid-block E0402（関数型を捨てている）
2. `List.fold` の型引数 E0407 ×2
3. lambda 行の mid-block E0402
4. lambda result の E0407
5. `0` 行の mid-block E0402
6. 末尾 `xs` の `expected Int, got List Int` E0401

本因は NL-2 により `List.fold` の行で式が切れることだが、最初の E0402 には `ignore` fix だけがあり、pipe/parens の修正方向は見えない。wave44 の既 decision は NL-2 維持と「`|>` か全体を括弧で書く」ことを確定済みなので、文法変更は検討対象外である。

## 比較

| 方針 | 効果 | 実装見通し / リスク | 判断 |
| --- | --- | --- | --- |
| 専用診断への畳込み | 連鎖を 1 件へ減らし、本因を直接示せる | block 内の独立式と継続行を区別する heuristic、連鎖 suppression、末尾式の独立エラー保全が要る。誤判定すると別のエラーを隠す | 見送り |
| 既存診断への hint 追加 | 最初の E0402 に pipe/parens への導線を付けられる | suppression が要らず、他診断や compile 結果を変えない。誤判定しても note が余計なだけでエラー自体は既に出ている | 採用 |
| 現状維持 | 実装不要 | `ignore` fix が前面に出て、ユーザー意図と逆方向へ誘導しやすい。連鎖も残る | 見送り |

## 判定条件の検討

採用案の false positive を抑える条件は次の通り。

- block の mid-block statement だけを対象にする
- 先頭 statement の型が `types.Func` のときだけにする
- 次の statement が後続行かつ先頭より深い列にあるときだけにする
- note を付けるだけで、診断 suppression・自動 rewrite・NL-2 変更をしない

この条件でも、深く字下げした独立式がたまたま関数値のあとに続く場合には note が付きうる。ただしその入力はすでに E0402 の erroneous input であり、hint によって compile 結果や他診断が変わらないため、専用診断 suppression より害が小さい。

## 結果

採用: **既存 E0402 へ継続行疑いの note を追加する**。

決定と red-first fixture 方針は [decision](../decisions/2026-11-27-continuation-line-diagnostics.md) に記録した。専用診断への畳込みは lane では見送り、採否は closeout の台帳整理で扱う。NL-2 文法変更と字下げ rewrite は既 decision どおり対象外である。

## 検証コマンド

文書・台帳のみの変更。最終検証は `gofmt -l .`（出力なし）、`go vet ./...`、`go test ./...`。
