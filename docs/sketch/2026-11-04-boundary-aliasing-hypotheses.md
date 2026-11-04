# 境界での別名と可変性 — 仮説（未検証）

- 状態: 仮説のみ。検証は [TODO](../../TODO.md) の「境界での別名と可変性の検証」
- 日付: 2026-11-04
- 関連: [decision: goconv-list-slice-copies](../decisions/2026-11-04-goconv-list-slice-copies.md)、[problems: shared-slice-view-type](../problems/2026-11-04-shared-slice-view-type.md)、[decision: go-interface-assignment-and-fields](../decisions/2026-11-03-go-interface-assignment-and-fields.md)

## 問い

`Go.conv` の `Go.Slice T ↔ List T` がコピーしない（pun）問題は、スライスに限った話か。struct やレコードでも同じことが起きるか。struct 自体の可変性（open-questions「Go の struct 型の構築・書き込み」）とどう関係するか。

整理の軸は、値の **持ち主**（Gom / Go / 借りているだけ）× **可変性**（不変 / 読み取り専用で共有 / 可変）× **可視性**（抽象 / 読み取り専用 / 全公開）。

## 仮説

以下はコードと文書を読んだうえでの推測で、どれも動かして確かめてはいない。

### Gom → Go の struct を読む向き

- **H1**: 値型の Go struct から exported field を読むと、読んだ時点の値を得るだけで別名は生じない（spec 8.3、field は読み取り専用）
- **H2**: `Ptr T` 越しに読むと、Go 側の後からの書き換えが次の読み取りで見える。`Ptr` は W0701 の可変リストに入っているので、これは意図どおり
- **H3**: Go struct の `[]T` 型の field を読んだときの扱いが決まっていない。decision go-interface-assignment-and-fields は「container copy conversion は入れない」としているので、E0501 になるか、`List` へコピーするか、`Go.Slice` で返るかを確かめる必要がある
- **H4**: field への代入や struct の構築（open-questions）を許すと、`Go.Slice` に書き込みを許すのと同じ問いになる — 「読み取り専用で借りる」と「可変で共有する」をどう区別するか

### Go → Gom のレコード・`List` を受け取る向き（spec 8.7）

- **H5**: `List` は `list.List[A] = []A` なので、生成された Gom の API から `List` を受け取った Go のコードは、元の配列を書き換えられる。その `List` が Gom 側のトップレベルの `let` や他の値と共有されていれば、Gom から見た不変性が破れる
- **H6**: Gom のレコードは Go の struct（値）として生成される。Go 側が field に代入しても、変わるのは Go 側のコピーだけ。ただし field に `List` があれば、H5 と同じく元の配列は共有されている
- **H7**: sum type のバリアント（sealed interface を実装する struct）も、ポインタで渡っていれば H6 より強く共有される可能性がある（表現を要確認）

つまり、「`List` は不変」という保証は Gom の内側でしか成り立っておらず、境界は Go 側の行儀に頼っている、という仮説。

## 他言語との比較（記憶ベース。ドキュメントで未確認）

- **MoonBit**: field は `mut` を付けない限り不変。struct の可視性は、修飾なし（外からは抽象）、`pub`（読み取り専用、外から構築・変更できない）、`pub(all)`（外から構築・変更できる）の 3 段階。Gom がいま Go の struct を扱う形は `pub` 読み取り専用に近い
- **OCaml**: レコードは `mutable` 指定の field 以外は不変で、更新は作り直し。C の FFI では値のブロックへのポインタをそのまま渡し、C 側が `Store_field` などで書き換えることもできる — 不変を守るのは C 側の責任（言語は防がない）

## 検証で決めたいこと（方向の候補。どれも未決）

1. OCaml 型: 不変は Gom の内側だけの保証とし、spec 8.7 に「Go 側は受け取った `List` を書き換えてはならない」と契約として明記する
2. 境界でコピー: 生成した API の入口と出口で `List` をコピーする（spec 8.5 の Go 呼び出し側と対称になる。コストがかかる）
3. 型で防ぐ: `List` を Go から見て抽象（メソッド経由の読み取りのみ）にする — MoonBit の `pub` 相当。生成コードを Go から使う書き味（[generated-code-goals](../decisions/2026-11-01-generated-code-goals.md)）とのトレードオフ

## 検証の手順（案）

- H1–H3: `testdata/gocall` 風の fixture に、値型の struct・ポインタ・`[]T` 型 field を持つ Go 型を置き、Gom から読んで Go 側で書き換えた後の値を観測する
- H5–H7: Gom で `List` / レコード / バリアントを返す API を生成し、Go のテストから書き換えて、Gom の関数を再度呼んだときに変化が見えるかを観測する
- MoonBit・OCaml の記述は公式ドキュメントで確かめてから残す
