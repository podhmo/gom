# `gom lsp -go` — verification report

Verifies [docs/decisions/2026-11-17-lsp-gotool-invocation.md](../decisions/2026-11-17-lsp-gotool-invocation.md):
`gom lsp -go` がユニットの全面コンパイル後に staged `go build` を
同期的に走らせ、`source: "go"` の診断を `.gom` span に fold する。

Method: (a) in-process JSON-RPC tests in `internal/lsp/gotool_test.go`,
(b) a wall-time measurement (`TestGoToolCost`),
(c) a real Emacs/Eglot session against the built `gom lsp -go`.

## In-process tests (`go test ./internal/lsp/`)

| Test | Covers |
| --- | --- |
| `TestGoToolFoldsBuildError` | gom check では通るが `go build` では落ちる package（兄弟 `.go` が `//line a.gom:2` で .gom 入力を名指す — wave-14 と同じ fixture class）で、fold された診断が `.gom` の line 2 に `source:"go"` で publish される。兄弟を修正した次のコンパイルで retract される |
| `TestGoToolSilentOnClean` | `go build` が通る package では何も publish しない |
| `TestGoToolSkipsOnGomError` | gom 診断で error があるユニットでは staging しない — partial output の go 診断で gom 診断を濁さない |
| `TestGoToolCost` | 壁時計のログ（下記）。`-short` では skip |

staged dir の `//line` 名解決はパッケージ dir に対して行う —
`NewGoPosIndex` の dir 引数は staging ではなく member の属する
ディレクトリを渡す（`//line` は生成先ディレクトリ相対の綴り）。

## 計測（`TestGoToolCost`、単一ファイル `package main` + `import go "fmt"`）

- 全面コンパイル（CheckOnly なし、`go list -export` 込みの fresh options）:
  open=47ms / change=44–44ms
- staged `go build`（temp dir への書き出し + RunGoBuild + fold）:
  open=161ms / change=158–166ms
- 合計: change あたり ~205–210ms — CheckOnly 経路の ~48–50ms の 4 倍
  以上。既定経路に乗せない判断の根拠として decision に記録

## Emacs+Eglot 実起動

GNU Emacs 27.1（`emacs-nox` + eglot 1.8 + project.el 0.9、batch セッション）
で `gom lsp -go` を LSP server として起動し、`/tmp/lsp-go-pkg/` —
`package main` の `a.gom`（`fn main () -> Unit = ()`）と兄弟 `z.go`
（`//line a.gom:2` で `var _ int = "not-an-int"`）の fixture — を
`find-file` → `eglot`（手続き呼び出し；batch では `eglot-ensure` が
interactive contact を要求するため）で開いた。

接続時点で flymake に届いた診断:

```text
(:line 2 :type eglot-error
 :text "go: cannot use \"not-an-int\" (untyped string constant) as int value in variable declaration"
 :source "go" :code "E0001")   ; LSP 診断の range.start.line = 1 → .gom の 2 行目
```

`//line` が指す `.gom` span に fold された `source:"go"` の診断が、
実際のクライアントにそのまま届く。severity は Error（gopls 等と同じ
`eglot-error` 表示）。

Harness note: Debian 系 `emacs-nox` 27.1 では eglot は同梱されないため
upstream の `eglot.el`（1.8）と Emacs 28 系の `project.el` を load-path
に足した（`project-root` は 27 同梱の project.el にない）。
