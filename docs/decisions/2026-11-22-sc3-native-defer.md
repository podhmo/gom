# SC-3: `Scope` 登録を native Go `defer` へ lowering

- 状態: adopted
- 日付: 2026-11-22

## 決定

`Scope.run` / `Scope.run_result` の本体関数が **scope ハンドル `s` を登録引数（`Scope.defer` / `Scope.defer_err` / `Scope.on_err` の第 1 引数）以外で使わない** ことを Core IR で検出したとき、lower 段で次の書き換えを行う:

- 呼出し側: `Scope.run f` → `let s2 = rt.NewScope() in f s2`（`run_result` も同形）
- 登録点: `Scope.defer s f` / `Scope.defer_err s f` / `Scope.on_err s f` → `core.Defer` ノード（元の式位置に残る）
- `run_result` で `defer_err` / `on_err` があるとき、本体関数の結果を `Body.ResBind` で名付け、`var errs []error` を持つ **DeferErrFold** を本体先頭に prepend する

条件に当てはまらないもの（`s` をクロージャ・spawn・他呼出しに渡す、`s` を結果に含める等）は従来どおり `rt.ScopeRun` / `result.ScopeRunResult` のランタイム scope を残す。

## 判定

- `uses` = `walkTailrec` で数えた `s` の全 Var 出現（入れ子の Closure / Spawn 内を含む）
- `regs` = 構造上の登録点（inline body の let 位置または tail 位置の、第 1 引数が `s` の `CallGo`）
- `uses != regs` のときは変換しない。入れ子クロージャ内の `s` 使用は自然に不適格になる — Go の `defer` は囲む関数の終了時に走るので、内側クロージャの登録が外の関数の defer に化けると意味が変わるため

## 意味の対応

| Gom | Go |
| --- | --- |
| `Scope.defer s f` | `defer f(struct{}{})` |
| `Scope.defer_err s f`（`run` 内） | `defer f()`（エラーは捨てる — runtime と同じ） |
| `Scope.defer_err s f`（`run_result` 内） | `defer func(){ if r := f(...); !r.IsOk() { errs = append(errs, e) } }()` |
| `Scope.on_err s f` | `defer func(){ if p := recover(); p != nil { f(...); panic(p) } else if !res.IsOk() { f(...) } }()` |
| `defer_err` の fold | 先頭に `var errs []error` + `defer` で `res.IsOk()` なら `res = Err(errors.Join(errs...))` |

- 登録が `if` / `match` の arm 内にあるとき runtime と同じく条件付き登録になる
- panic は `on_err` が `recover → f → panic(p)` と再送出して defer チェーンを降りるので、runtime の LIFO Close と一致
- `res.IsOk()` ガードで「本体が Err を返したら cleanup エラーで上書きしない」の runtime 挙動を保存
- `run` scope の `defer_err` は Go の `defer` でエラーを捨てる（runtime `ScopeDeferErr` は run で捨てるのと同じ）

## 却下案

- **呼出し側で IIFE に包んで本体を差し替える**: 宣言済み関数を複数呼出し側が共有すると本体を抜き取る形になり他の呼出しが壊れる。in-place 書き換えにして宣言 body 自体を変換する形にした
- **`s` の使用が登録以外にある場合も部分的に defer 化する**: 実行順序が変わるため見送り — 全部かなしか

## 参照

- `internal/lower/scopedefer.go`（書き換え本体）、`internal/codegen/defer.go`（emit）
- SC-2 静的検査: docs/decisions/2026-11-22-scope-escape-check.md（`s` を外部へ持ち出せる構文がそもそも無いことをチェック側で保証）
- 調査 sketch: docs/sketch/2026-11-22-sc3-lowering-design.md
