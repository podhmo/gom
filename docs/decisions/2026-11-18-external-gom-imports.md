# 外部モジュールの Gom パッケージの import

- 状態: 採用
- 日付: 2026-11-18

## 背景

spec 7 節は `import "path"` をメインモジュール内に限定していた: `gomimport.Loader.dirOf` はパスがモジュールパス自身かそのプレフィックスのときだけディレクトリを返し、それ以外は E0203 で拒否した。外部モジュール（`testdata/gocall` など consumer 側の Go module）からは、ツールチェーン同梱の Gom stdlib さえ import できず、`stdlib/list` が提供する操作を手書きの再帰で代替していた。

## 決定

1. メインモジュール外のパスは `go list -m -f '{{.Path}}|{{.Dir}}' all` を import ファイルのモジュールコンテキスト（Config.Dir）で 1 回だけ実行し、モジュールグラフの最長プレフィックス + module-relative remainder でディレクトリを得る。依存モジュール・`replace`・ワークスペースの兄弟に `.gomi` があれば Gom パッケージとして読み込む。Go 側の可視性と同じ解決規則に揃うので「生成コードが import できるなら Gom 側も読める」。
2. ツールチェーン同梱の Gom stdlib は短名を許す: `list` / `option` / `result` は `github.com/podhmo/gom/stdlib/<name>` に展開する。束縛名は従来どおりパス末尾要素（`list` など）、`.gomi` のメンバと生成される Go import は完全パスを記録する。短名は単一要素のパスなのでユーザのメインモジュールパスと衝突しない（衝突しうるのはモジュールパスが裸の `list` という異常な場合だけ）。
3. `.gom` 実装側の import 走査（E0606 循環検出）も同じ解決を通すので、外部パッケージを介した循環も検出できる。

## 却下した案

- **(a) stdlib だけ特別扱い（embed で同梱 .gomi を読む）**: gomstdlib.Gomis に .gomi は埋め込み済みだが、生成コードは結局 `github.com/podhmo/gom/stdlib/list` の Go パッケージを import するので consumer の go.mod に gom への依存は必須 — embed 版を読んでも build には意味がなく、`go list` 解決に一本化したほうが一貫する。stdlib 固有の分岐は短名展開だけに留めた。
- **`go list -f '{{.Dir}}' <pkg>` でパッケージ単位に解決する**: 実装を進めると `-f {{.Dir}}` は「does not contain package」で Gom-only ディレクトリを拒否する（`gom build` 前には .go が 1 ファイルも存在しない）ことが判明 — .gomi があればこちらではパッケージなので、モジュール単位の `go list -m` + remainder 継ぎに切り替えた。モジュールグラフ側なら require/replace/workspace の可視性もそのまま反映される。
- **メインモジュール外の解決を opt-in（フラグ）にする**: spec の v1 限定を破る拡張だが、生成 Go import がそもそも同じ可視性を要求するため言語レベルで別スイッチを置く意味がない。

## 影響

- spec 7 節の import 解決規則を更新（メインモジュール限定を撤廃、`go list` 解決と短名を記述）。
- `testdata/gocall` の `total` / `isEmpty` は手書き再帰から `list.fold` / `list.is_empty` へ置き換え、外部 module からの `import "list"` が check/build/`go test` まで通る end-to-end の fixture とした。

## 追記（レビュー修正）

- vendoring 下は `go list -m all` が build list を返せないため、`<module root>/vendor/<import path>` を直接見る（vendor が module cache より優先なのは Go と同じ）
- `go list -m` はモジュールソースを download しないため、build list にあって Dir の無いモジュールは `go mod download -json` で取得する（Go パッケージを持たない純 Gom 依存は何も download しないケースがある）
- spec fixture 側: `internal/build` の spec ハーネスを cmd/gom と同じく gomimport 配線にし、`ok_import_stdlib_short`（短名）を pin。外部モジュール側は `internal/gomimport/loader_test.go`（replace 先・vendor）と testdata/gocall の e2e が pin — spec ハーネスの Dir はリポジトリのモジュール内なので外部モジュール fixture は表現できない
