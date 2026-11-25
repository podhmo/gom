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
ファイルが既にある場合は canonical `.gomi` ではなくそのファイルを seed に
読むため、alternate draft だけに書いた extern directive が上書き後も残る。

## stdlib/list での確認

`stdlib/list/list.gom` と既存 `stdlib/list/list.gomi` を temp dir にコピーし、
`gom info <dir>` を実行した。修正前は extern な `take` / `filter_map` /
`find_map` / `sort_by` の directive が差分で消えたが、修正後は outputs が空、
つまり再生成 no-op になった。

## 範囲外

- abstract/private 化の意図保持
- directive の attachment（宣言直上の連続コメント run）を scan 側で
  厳密に解釈するか — spec と現行 scan のずれとして implementation-gaps に登録
