# `gom list -external` — 外部 Gom パッケージの列挙

- 状態: 採用（wave41 lane C で実装）
- 日付: 2026-11-24

open-questions の「`gom list` の列挙対象を外部 Gom パッケージまで
広げる」を解消する。外部パッケージ参照（`import "path"` →
`gomimport` の module 解決）は wave39 までに成立しており、列挙の
前提は揃っていた。

## 決定

`gom list` に `-external` フラグを追加し、列挙元を stdlib テーブル
から「import 可能な Gom パッケージ」に切り替える。列挙ルール:

- 候補モジュールは `go list -m all`（build list）＋ 最寄り
  `go.mod` の module 自身（workspace が `use` しない module でも
  dirOf が自分の go.mod で解決するのと対応させるため）
- 各モジュールの Dir 配下を walk し、`.gomi` がちょうど 1 つの
  ディレクトリを「その module path + 相対パス」の import パスと
  して列挙する
- 導出した import パスが dirOf の解決規則（importing module 優先、
  次に最長 prefix 一致）で別モジュールの Dir に向くものは載せない
- `vendor/`、`.` 始まり、nested module（go.mod あり）は walk から
  除外する

vendor 列挙（module 側 `vendor/` 配下の .gomi）は範囲外で future
work として残る（open-questions の vendoring 項目に記録）。

参照: [調査](../sketch/2026-11-24-gom-list-external-enumeration.md)、
[実装](../internal/gomimport/loader.go)（`ExternalPackages`）、
[実装](../cmd/gom/list.go)
