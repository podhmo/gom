# 外部モジュールの Gom パッケージの import

- 状態: 採用
- 日付: 2026-11-18

## 背景

spec 7 節は `import "path"` をメインモジュール内に限定していた: `gomimport.Loader.dirOf` はパスがモジュールパス自身かそのプレフィックスのときだけディレクトリを返し、それ以外は E0203 で拒否した。外部モジュール（`testdata/gocall` など consumer 側の Go module）からは、ツールチェーン同梱の Gom stdlib さえ import できず、`stdlib/list` が提供する操作を手書きの再帰で代替していた。

## 決定

1. メインモジュール外のパスは `go list -f '{{.Dir}}'` を import ファイルのモジュールコンテキスト（Config.Dir）で実行してディレクトリを得る。依存モジュール・`replace`・ワークスペースの兄弟に `.gomi` があれば Gom パッケージとして読み込む。Go 側の可視性と同じ解決規則に揃うので「生成コードが import できるなら Gom 側も読める」。
2. ツールチェーン同梱の Gom stdlib は短名を許す: `list` / `option` / `result` は `github.com/podhmo/gom/stdlib/<name>` に展開する。束縛名は従来どおりパス末尾要素（`list` など）、`.gomi` のメンバと生成される Go import は完全パスを記録する。短名は単一要素のパスなのでユーザのメインモジュールパスと衝突しない（衝突しうるのはモジュールパスが裸の `list` という異常な場合だけ）。
3. `.gom` 実装側の import 走査（E0606 循環検出）も同じ解決を通すので、外部パッケージを介した循環も検出できる。

## 却下した案

- **(a) stdlib だけ特別扱い（embed で同梱 .gomi を読む）**: gomstdlib.Gomis に .gomi は埋め込み済みだが、生成コードは結局 `github.com/podhmo/gom/stdlib/list` の Go パッケージを import するので consumer の go.mod に gom への依存は必須 — embed 版を読んでも build には意味がなく、`go list` 解決に一本化したほうが一貫する。stdlib 固有の分岐は短名展開だけに留めた。
- **`go list -m` でモジュール単位に解決してからパスを切り出す**: ワークスペース・replace を考慮すると `-f {{.Dir}}` のパッケージ解決のほうが直接で、そのまま oneGoMi に渡せる。
- **メインモジュール外の解決を opt-in（フラグ）にする**: spec の v1 限定を破る拡張だが、生成 Go import がそもそも同じ可視性を要求するため言語レベルで別スイッチを置く意味がない。

## 影響

- spec 7 節の import 解決規則を更新（メインモジュール限定を撤廃、`go list` 解決と短名を記述）。
- `testdata/gocall` の `total` / `isEmpty` は手書き再帰から `list.fold` / `list.is_empty` へ置き換え、外部 module からの `import "list"` が check/build/`go test` まで通る end-to-end の fixture とした。
