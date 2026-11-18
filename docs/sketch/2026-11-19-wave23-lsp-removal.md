# `gom lsp` 全削除の影響棚卸し（wave23 lane2）

2026-11-19。podhmo の意向メモ「LSP の機能全部消しても良い気がする」
（B テーマ・削減）に向けた準備調査。削除可否の決定は podhmo — 本書は
「消すなら何が吹き飛ぶか」の全量と削除順序。

## スコープの確定

`internal/lsp` を import しているのは `cmd/gom/lsp.go` のみ。
**lsp 以外から lsp への依存はゼロ** — 削除は 2 ファイル + 1 ディレクトリで
完結する。

逆方向（lsp が使う共有部品）はすべて CLI 経路と共有なので残る:

- `internal/build` — compile unit・SourceMap/GoPosIndex（`gom test` の
  go test -json fold と `gom build -go`/`-vet` が使う。P4-5 アンカー表と
  gotool position inverse は LSP 由来だが現役の consumer は CLI 側）
- `check`/`resolve`/`syntax`/`types`/`diag`/`goimport`/`gomimport`/`iface`/
  `stdlib` — 全コンパイル経路の共通部品

`editor/emacs/gom-mode.el` は `gom check` ベースの flymake で Eglot/LSP
配線を持たない — 無関係。

## 削除対象の全量

### コード（~3.9k 行）

| 対象 | 内容 |
| --- | --- |
| `internal/lsp/` 全体 | doc.go, gotool.go(+test 532), hover.go, jsonrpc.go(+test), protocol.go, server.go(+test 1322) — 計 3802 行 |
| `cmd/gom/lsp.go` | `gom lsp` サブコマンド実体 |
| `cmd/gom/main.go` | doc comment (l.4, l.9)、usage 行 `lsp serve ...` (l.38)、dispatch `case "lsp"` (l.80-81) |
| `cmd/gom/main_test.go` | usage 列挙の `"lsp"` エントリ (l.56) |

### ドキュメント

| ファイル | 箇所 | 削除時の処理 |
| --- | --- | --- |
| `docs/spec.md` | l.734 `gom lsp` コマンド表の行 | 行を除去（構文・型の仕様本体には LSP 項なし） |
| `docs/design.md` | l.152 `gom lsp -go` 記述、l.204 CLI 一覧、l.227 `lsp/` ディレクトリ項 | 各記述を除去。go tool fold の記述は `gom test`/`-go`/`-vet` 側が残るので fold 自体の節は維持 |
| `docs/open-questions.md` | l.28 LSP アンカー粒度の問い、l.41 session-scoped shared importer | 両項とも LSP 前提の問い — 削除決定なら項ごと除去（#41 の importer 共有の洞察は LSP 文脈なので残しても参照先がない） |
| `TODO.md` | l.66 `[ ]` LSP 拡張タスク（completion/definition/incremental sync） | 削除（決まった作業ではなくなる）。l.63-65, l.89 の `[x]` は完了済みの履歴なので GC 任せ |
| `skills/gom-agent-guide/SKILL.md` | l.757 コマンド一覧、l.764 `gom lsp` 行 | 同期（ユーザ向け記述のため） |

### decisions — Supersede 対象

削除を決めたとき新しい decision から Supersedes で繋ぐ対象:

- `2026-11-06-lsp-v1-scope` — LSP v1 の採用そのもの
- `2026-11-06-lsp-package-unit` — パッケージ単位コンパイル
- `2026-11-17-lsp-gotool-invocation` — `gom lsp -go`

言及するが削除しない（機構は CLI で現役）:

- `2026-11-06-gotool-position-inverse` — SourceMap 逆引きは `gom test` fold が使用
- `2026-11-17-cli-gotool-invocation` / `2026-11-18-build-vet-flag` — `-go`/`-vet` は残る
- `2026-11-18-golist-export-cache` — file cache は CLI・LSP 共通だったが CLI 側が残る

### 履歴として残るもの

- `docs/sketch/2026-11-06-lsp-v1-verification.md` 等の検証記録 — sketch は
  規範ではない記録なので削除しない
- wave 記録（wave11-16 の lsp lane 記述）— 同上

### 検証・CI

- LSP 専用の testdata fixture・CI ジョブ・Makefile ターゲットは存在しない
- `gom lsp` 固有の golden/spec fixture もない（LSP の検証は server_test /
  gotool_test の in-process テストのみ — ディレクトリごと消える）

## 削除順序

1. `cmd/gom/lsp.go` 削除 + `main.go` の usage/dispatch/comment、
   `main_test.go` の列挙から lsp を外す
2. `internal/lsp/` をディレクトリごと削除
3. spec/design/open-questions/TODO の上記箇所を更新
4. 削除の decision を 1 件立て、3 件の LSP decision へ Supersedes を張る
   （decisionsdoc で README 再生成）
5. gom-agent-guide のコマンド一覧を同期
6. 検証: `gofmt -l .` / `go vet ./...` / `go test ./...` / check-sumtype

削除後に残る LSP 由来の資産: SourceMap/GoPosIndex 逆引き（`gom test`・
`gom build -go` が現役）、`go list -export` file cache、emacs mode
（flymake 経路）。いずれも lsp パッケージを経由しない。

## 付記: 削除しても失われないもの

- go tool 診断の `.gom` 位置 fold — `gom test`/`gom build -go`/`-vet` に
  ある（lsp/gotool.go は lsp 専用の起動点で、機構の正は internal/build）
- エディタでの診断表示 — flymake + `gom check` が残る（保存時診断。
  hover・即時 push は失われる）
