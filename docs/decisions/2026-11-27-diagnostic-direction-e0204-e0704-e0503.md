# E0204/E0704/E0503 の診断方向

- 状態: 採用（実装・検証した — [検証](../sketch/2026-11-27-wave52-lane-e-diagnostics-verify.md)）
- 日付: 2026-11-27

## 決定

- E0204 unknown attribute は production diagnostic とし、parser が出す。attribute の語彙は `@test` / `@tailrec` / `@extern` / `@method` に閉じ、未知の attribute を AST へ運ばず parse 時に拒否する。phase は syntax、spec 参照は §1 とする
- E0704 destructuring select receive arm は checker の production diagnostic とする。parser は receive arm の左辺を pattern として受理し、変数・`_` 以外の形を checker が E0704 で拒否する。合法な `x <- ch` / `_ <- ch` の意味は変えない
- E0503 implicit numeric conversion not allowed は retired とし、番号を再利用しない。numeric conversion に関する型不適合は引き続き E0401 の generic mismatch で報告し、明示変換は `Go.conv` で書く

## 理由

E0204/E0704 は利用者が遭遇する構文・制約を具体的に説明できる。E0503 だけを正確に区別する emitter は境界変換の判定を複雑にする割に、E0401 の mismatch で必要な情報が足りる。

## 検証

`E0204_unknown_attribute` / `E0704_select_recv_pattern` spec fixture、coverage meta-test、explain example 分類を同期した。
