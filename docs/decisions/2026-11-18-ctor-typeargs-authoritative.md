# `CtorApp`/`CtorRef` の `TypeArgs` は optional-but-authoritative

- 状態: 採用（wave21 lane 1 で実装・検証した — `TestCtorTypeArgs` が合法・違反の両形を pin。[検証](../sketch/2026-11-18-ctor-typeargs-verify.md)）
- 日付: 2026-11-18

## 問題

`CtorApp.TypeArgs` / `CtorRef.TypeArgs` は「ADT の型パラメータの
インスタンシエーション」を運ぶが、core validator は個数も `Typ`
との整合も検査しなかった（[problem](../problems/2026-11-02-ctor-typeargs-unvalidated.md)、
implementation-gaps 台帳済み）。codegen は `TypeArgs` があれば信用し、
なければ `Typ` の `Con.Args` にフォールバックする（`ctorTArgs`）ため、
不正形ノードは violation ではなく誤った instantiation を emit する。
両者が食い違うときどちらが正かも未規定だった。

## 採択（podhmo, wave21 prompts 作成時）

**TypeArgs は optional-but-authoritative**:

- 存在するとき（非空）は、ノードが最終的に辿る ADT インスタンシエーション
  と完全一致を要求する — CtorApp・CtorRef ともに `Typ` の矢印列を chase
  した終着 `Con` の `Args`。部分適用 CtorApp（`Typ` が Func）でも
  TypeArgs は ctor 自身の instantiation を綴るので、終着の ADT に
  アンカーするのは CtorRef と同じで正しい。不一致は violation。
- ADT 型パラメータ arity との個数不一致も violation — 既存の
  `targs` 検査（宣言がファイル内・unit の sibling にある場合）が
  そのまま担う。imported ctor は宣言 arity を引けないが、結果型との
  完全一致検査で個数の不整合も検出できる（結果型自身が正しい arity
  で綴られる前提は checker が保証する）。
- 欠損（nil / 空）は宣言 arity が見えない ctor（imported・builtin）では
  合法 — codegen の `ctorTArgs` フォールバックは TypeArgs 欠損時の経路
  として残し、validator が一致を保証した上で使う。ファイル内・unit の
  sibling に宣言がある generic ctor では arity が見えるため、従来どおり
  `targs` が個数不一致（欠損含む）を violation とする。

### 却下した案

- **(b) `Typ` の `Con.Args` を正とし TypeArgs を廃止する**: 正を一本化
  できるが、lower が emit する全経路の `TypeArgs` フィールドと codegen
  の参照を一斉に外す変更になり、フィールドを残したまま「書くが読まない」
  状態にもできない（再び未検査フィールドになる）。移行の規模に対し得る
  ものが小さいため future work として open-questions に残す。
- **ctor→ADT 所有者解決による arity 検査（imported も含む）**: 正確だが
  validator に ctor→type-owner 解決の新規配線が要る。結果型との一致
  検査で実用上の穴は塞がるため採らない（problem 文書の推奨案どおり）。

## 影響

- `internal/core/validate.go`: CtorApp/CtorRef で TypeArgs と結果型 Con
  の一致を検査する。
- spec/design への影響: 利用者から見える振る舞いは変わらない（内部 IR
  の不変条件の追加）— design.md の validator 記述に 1 行足す程度。
