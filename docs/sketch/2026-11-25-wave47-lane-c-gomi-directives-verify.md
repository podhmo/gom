# wave47 lane C: `.gomi` directive 引き継ぎ検証記録

2026-11-25。[docs/decisions/2026-11-25-gomi-extern-directive-carry.md](../decisions/2026-11-25-gomi-extern-directive-carry.md)
の prototype 検証メモ。

## 再現

次の形を temp dir に置いて `gom info <dir>` を実行した。

- `@extern fn keep (f: Unit -> Unit) -> Unit`
- 本体が `f` を結果に返す `fn leak (f: Unit -> Unit) -> Unit -> Unit = f`
- 既存 `.gomi` には両者へ `//gom:noescape <name> 0` と
  `//gom:result-notaint <name> 0` を記述

修正前は再生成後に `keep` / `leak` 両方の directive が消えた。
`keep` は extern なので推論し直す本体がなく、ここが bug だった。

## 修正後の確認

同じ入力で、再生成後は次の形になった。

```gom
//gom:result-notaint keep 0
//gom:noescape keep 0
fn keep (f: Unit -> Unit) -> Unit

fn leak (f: Unit -> Unit) -> Unit -> Unit
```

- extern の `keep` は既存 directive を引き継ぐ
- 本体のある `leak` は再推論され、stale な directive は落ちる

関数型の `@extern let` も同じ引き継ぎ経路を通ることを
`TestInfoCarriesExternEscapeDirectives`（`cmd/gom/cli_test.go`）で固定した。
同じ test では、extern の 2 つのコールバック引数を入れ替えた signature では
`//gom:noescape moved 0` を引き継がないことも固定した — directive の index は
位置への主張なので、古い位置の保証を別の入力へ移さない。

`gom info -o <path>` の regression も
`TestInfoAlternateOutputSeedsItsOwnDirectives` で固定した。明示した出力
ファイルが同じ package directory に既にある場合は canonical `.gomi` ではなく
そのファイルを seed に読むため、alternate draft だけに書いた extern directive
が上書き後も残る。symlink 経由で同じ directory に届く経路も同じ package
として扱う。
ただし seed の package directory が違う場合は実装の保証を別 package へ移植できない（同名 package でも別 directory は別 package）
ため引き継がない。

formatter の regression も同じ lane で固定した。`gom fmt` は interface の
`let x: T` を colon 前に空白を置かない canonical で書く一方、`gom info` の
draft は `let x : T` を出す。raw text のまま比較すると同じ signature で
directive が落ちるため、比較前に両側を `gom fmt` の interface 構文で
canonical 化する。

## stdlib/list での確認

`stdlib/list/list.gom` と既存 `stdlib/list/list.gomi` を temp dir にコピーし、
`gom info <dir>` を実行した。修正前は extern な `take` / `filter_map` /
`find_map` / `sort_by` の directive が差分で消えたが、修正後は outputs が空、
つまり再生成 no-op になった。

## 範囲外

- abstract/private 化の意図保持
- directive の attachment（宣言直上の連続コメント run）を scan 側で
  厳密に解釈するか — spec と現行 scan のずれとして implementation-gaps に登録

- 2026-11-25 review round 3: 別 directory にある同名 `package lib` の `lib.gomi` を `-o` seed に使っても directive を引き継がないことを `TestInfoAlternateOutputSeedsItsOwnDirectives` に追加した。
- 2026-11-25 review round 4: `-o` が directory symlink 越しに同じ package directory の既存 draft へ届く場合は、その draft を seed に使い directive を保持することを `TestInfoAlternateOutputSeedsItsOwnDirectives` に追加した。
- 2026-11-25 review round 5: symlink test は unknown member の marker directive が再生成で消えることも確認し、successful no-op を検出できるようにした。
