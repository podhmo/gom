# LSP は未対応に戻す — `gom lsp` と `internal/lsp` を除去

- 状態: adopted
- 日付: 2026-11-19
- Supersedes: [lsp-v1-scope](2026-11-06-lsp-v1-scope.md)、[lsp-package-unit](2026-11-06-lsp-package-unit.md)、[lsp-gotool-invocation](2026-11-17-lsp-gotool-invocation.md)
- 問い: LSP 面を v1 の提供面に置き続けるか — wave24 lane C

## 決定

LSP を **v1 の提供面から取り下げて「未対応」に戻す**。不要コードの
除去ではなく「本当に必要な状況が来るまで提供しない」という
機能面の撤回であり、`gom lsp` サブコマンドと `internal/lsp`
（stdio JSON-RPC、hover、publishDiagnostics、`-go` の go tool
fold）を除去する。需要が具体化した時点の復帰は新しい decision
での再実装として扱う — この決定は将来の再導入と矛盾しない。

根拠は podhmo の方針（2026-11-19）:「削除というよりは、本当に
必要な状況が来るまで未対応という体で削除」。hover・即時 push の
価値は残るが、単一ユーザーの現状で保持する面としては重い。

## 残るもの（LSP 由来だが CLI が現役の consumer）

- `SourceMap`/`GoPosIndex` の go tool 診断逆引き — `gom test` と
  `gom build -go`/`-vet` が使う
- `go list -export` の file cache（CLI・LSP 共通だった基盤）
- `editor/emacs/gom-mode.el` の flymake + `gom check` 経路
  （保存時診断。hover・即時 push は失われる）

## 影響

- 除去: `cmd/gom/lsp.go`、`internal/lsp/`（doc/gotool/hover/
  jsonrpc/protocol/server + テスト、計 ~3.9k 行）、usage 列挙
- docs: spec のコマンド表、design の lsp 項とディレクトリ図、
  open-questions の LSP 前提 2 問（アンカー粒度・session-scoped
  shared importer）、TODO の LSP 拡張タスクを除去
- 棚卸しの全量と削除順序は
  [sketch](../sketch/2026-11-19-wave23-lsp-removal.md) どおり。
  lsp 以外から lsp への依存は `cmd/gom` 経由のみで除去は完結する
