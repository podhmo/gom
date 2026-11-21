# `--strict` / 診断フィルタ — 調査と採否

- 状態: adopted（2026-11-22 調査結果 — wave30 lane D）
- 日付: 2026-11-22

open-questions の「書きかけコードで一部エラーを一時フィルタしたい用途」について、`--strict` 相当のオプションを入れるか調査した。

## 現状の土台

- 診断 severity は 3 段（error / warning / note）。`Report.ExitCode` は error-severity を持つかテスト失敗時のみ `ExitError` を返し、warning は advisory で失敗しない
- warning レイヤは既に存在する（W0501/W0502 相互運用系、W0701/W0702 可変共有 lint、W0801 性能）— 「黙らせない」機構として diagnostic が常に報告に残る構造は整っている
- ファイル単位の制御コメントは `//gom:` ディレクティブの前例がある（`//gom:noescape`、SC-2 の `.gomi` 注釈）

## 論点と見つけたこと

**「エラーをフィルタする」の向きが 2 方向ある。** open-questions の動機はリラックス方向（途中の match 穴・冗長腕を抑えて書き進めたい）。一方 `--strict` と名の付く機構は普通、警告をエラーに引き上げる厳格方向を指す — 同じ旗で逆方向を表すと紛らわしいので分けて考える。

**厳格方向（warnings → errors）は安い。** 出口コードの計算を HasError → HasErrorOrWarning に変えるだけの小さな変更。CI で lint 残置を禁じたい用途には筋がよい。ただし動機の用途（書きかけの救済）には答えていない。

**リラックス方向は「コードを warning に格下げ」として実現できるが、対象は限られる。** コンパイルはエラー後も後段フェーズへ進むので、下げられるのはローカルに回復可能な診断だけ:

- E0301 non-exhaustive match — 欠けた腕は生成コードで panic に落ちるので、型付けは成立したまま
- E0302 unreachable match arm — 腕を落としてコンパイル可能
- それ以外（E01xx parse、E02xx resolve、E04xx type、E05xx boundary）は下流の lower/codegen が健全な型付き AST を前提にするため格下げできない — suppress しても生成物が出せないか嘘のコードが出る

つまり「severity 一括フィルタ」は設計として成立せず、**許可コードのホワイトリスト方式**が必要になる（codes.go に relaxable フラグを足す形）。

## 決定

採用 — ただし旗は 2 本に分ける（実装は次 wave 以降）。

1. **`//gom:allow E0301` のようなファイル内ディレクティブ**をリラックス用に入れる。対象は relaxable フラグを持つコードのみ（初期は E0301・E0302）。指定したコードはそのファイル内で warning へ格下げされ、報告にはコード付きで残る（silent にならない）。理由: 抑制が負債のそばに置かれ・grep でき・`.gomi` や CLI の引数回りを汚さない。CLI 版（`gom check -allow=...`）は umbrella 的な一時抑制が欲しくなったら追加する余地を残す
2. **`--strict`（warnings を error 扱いの exit code）**は小さいので同時に入れてよいが、動機上の主眼ではない — CI 固定用途

変更しない: severity 単位のフィルタ、型エラー系の格下げ。

## 影響

- 実装 lane の対象: `internal/diag/codes.go`（relaxable フラグ）、`internal/syntax` or directive scanner（`//gom:allow` の収集）、`internal/build`/`cmd/gom`（報告への適用と `--strict` の exit code）、`docs/spec.md` §12（CLI）とエラーコード文書
- open-questions の該当項目は本 decision へ差し替え
