# experiments/w23/struct-construction

wave23 lane1 の実験: Go struct 構築機構の各案の「ゴールの記述例」。
調査・比較は `docs/sketch/2026-11-19-wave23-struct-construction.md`。

- `acomposite/` — (a) composite literal 案。`mock.gom.txt` が提案構文、
  `gen.go` が生成しうる手書き Go、`use_test.go` が契約検証
- `bfieldwrite/` — (b) field write 案。同じ構成
- `cextern/` — (c') `@extern fn` 案。`a.gom` は今日コンパイルできる
  実物で `a.go` は `gom build` の実出力、`impl.go` が手書き body
- `dsync/` — sync 含有・pointer-receiver 型の確認。`a.gom` は実物で
  `Ptr.new` → `new(http.Server)`/`new(sync.Once)`/`new(sync.WaitGroup)`
  とメソッド呼び出しが動く。`mock.gom.txt` は (b) の `srv.Addr = ...`
  記述例

3 案とも `host.NewClient` と同じ観測契約（30s timeout + 呼び出し側の
CheckRedirect）を `use_test.go` で検証する。`dsync` は Once が 1 回だけ
走ること・WaitGroup が回ること・`new(Server)` の sync 内部が無傷な
ことを検証する。
