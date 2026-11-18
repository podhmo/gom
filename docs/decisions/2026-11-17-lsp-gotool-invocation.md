# `gom lsp` の go-tool 診断は opt-in フラグ + per-change 同期起動

- 状態: 採用
- 日付: 2026-11-17

## 背景

wave-14 lane 2 で `gom build -go` が着地した（[decision](2026-11-17-cli-gotool-invocation.md)）。
残った TODO「LSP: go-tool 診断の取り込み」は `gom lsp` 側の起動点の話:
サーバのコンパイルは CheckOnly で生成物を持たないので、`go build` の
出力が fold できる位置に出てこない。

## 候補

- (a) per-change: 各コンパイル後に temp dir へ生成物を書き出して
  `go build`。`gom lsp` の CheckOnly は ~48–50ms（内部は `go list
  -export` 支配）。実測（本 lane、単一ファイル main pkg、
  `import go "fmt"` あり）: 全面コンパイル ~44–47ms + ステージング
  `go build` ~158–166ms = change あたり ~205–210ms — opt-in なしに
  既定経路へは乗せられない
- (b) debounced/idle: バッファが静かになってから `go build`。read loop が
  単一スレッドで `$/cancelRequest` 未実装の現状では、世代カウンタと
  古い publish の retract 設計が要る
- (c) opt-in サーバフラグ（`gom lsp -go`）: 既定の動作を変えない

## 決定

(c) + (a): `gom lsp -go` が立っているときだけ、ユニットのコンパイルが
**全面コンパイル（CheckOnly なし）**になり、診断の publish の前に
`go build` が read loop 上で同期的に走る。非同期待ち・世代管理は入れない
— フラグなしの既定経路は振る舞い・コストとも据え置きで、go tool のコストを
払うのは明示したユーザだけ。

同期起動を選ぶ理由: `gom lsp` の publish 経路は unit ごとの差し替えで
retract が保証されている。publish 直前に走らせれば go-tool 診断は gom
診断と同じ差し替え単位に乗り、stale publish の管理が要らない。`$/cancelRequest`
未実装のまま fork すると retire ロジックが新規の設計になる。

折り畳みは wave-14 の機構を再利用する（`RunGoBuild` +
`GoPosIndex` + `GoToolDiagnostics`、`source: "go"` は LSP の
convertDiagnostic がすでに区別する）。

ステージング: パッケージ自身のディレクトリ配下に一時ディレクトリを
作り、生成した `.go` を並べて `go build` する — モジュール内なのでユーザーの
go.mod（`require github.com/podhmo/gom`）がそのまま効き、実ディレクトリと
同じ `internal` 可視性を持つ。兄弟の `.go` ファイルとその `//go:embed` 資産も
併せてステージする（generated 名が衝突時に勝つ）。Gom パッケージ同士の
import（`import "other"` → `"modPath/other"`）は生成物がそのモジュール内に
存在しないため `go build` を skip する — 判定は import 先の解決後ディレクトリで行い、
`.gom`/`.gomi` を含む（＝ Gom パッケージ。手書き `.go` helper があっても
生成実装はディスクにない）または `.go` がない場合に skip。同一モジュール内の
実在 Go パッケージ（gom 自身の `rt` など）は skip しない。その形の診断は
gom 側が出す。

skip 条件: ユニットに error 診断がある / いずれかの member が Output を
持たない / 生成物が自モジュール import を含む。いずれの場合も go-tool
診断は出さない — gom 診断が既に状況を説明している。go.mod が
見つからないときだけ位置なしの E0001（`source: "go"`）をユニットの先頭
member に出して、opt-in が働いていないことを沈黙にしない。

## セッションキャッシュの件

~48–50ms の支配項は `go list -export`（compileOptions が goimport セッションを
コンパイル毎に立てるため）。`-go` ありなら全面コンパイル＋`go build` の上に
さらに乗る。ローダキャッシュは unsaved buffer とディスク更新の混在を
invalidate する設計が要り、本 lane の範囲外 — 実測を sketch に記録して
deferral を決定に残す。

検証: [sketch](../sketch/2026-11-17-lsp-gotool.md)（計測と検証手順）。
`internal/lsp` の JSON-RPC テスト 3 件（fold / silent / skip）と
Emacs+Eglot の実起動確認。

## 追記（wave20 lane3）: `-go` に `go vet` を含める

採択: フラグ形は「`gom lsp -go` に vet を含める（1 フラグで build+vet
両方実行）」。`NewGoTool` が同じ staging・同じ `GoToolDiagnostics`
fold で `go build` のあと `go vet` を実行する（`runGoTools` の独立
doBuild/doVet と同じく build 失敗時も vet を試行）。tool 名は
`"go vet"` — `gom build -vet` と同じ。台帳の「LSP 側の go build /
go vet 実起動経路」の vet 半分はこれで閉じた。
