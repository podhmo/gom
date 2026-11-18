# TypeArgs (b) — `Typ` の `Con.Args` を唯一の正にする案の棚卸し

- 対象: wave22 lane4 `devin/wave22-ctor-targs-b`（調査のみ — 実装しない）
- 背景: [decision 2026-11-18-ctor-typeargs-authoritative](../decisions/2026-11-18-ctor-typeargs-authoritative.md) で採択した (a) optional-but-authoritative に対し、(b) ctor ノードの `TypeArgs` フィールドを廃止する案。対象は core IR の `CtorApp` / `CtorRef` のみ（`FuncRef`/`CallDirect` の TypeArgs は多相 callee の具現化情報で別物 — ここでは触れない）

## 構造の要点

- `CtorApp` は常に saturated（部分適用は `Closure` で包む）— `Typ` は結果 `Con` そのもの
- `CtorRef` の `Typ` は ctor の矢印型 — 終着 `Con` は `resultCon` の chase で取れる
- したがって両ノードとも「ADT の具現化引数」は `Typ`（の結果 Con）の `Args` から一意に派生できる。wave21 lane1 の validator 不変条件（`TypeArgs == resultCon(Typ).Args`）はこの派生可能性を強制する側だった

## TypeArgs emit 経路（lower — 8 箇所）

| 箇所 | 由来 | Typ との関係 |
| --- | --- | --- |
| expr.go:203 (CtorRef) / :208 (CtorApp) | `e.TypeArgs`（check.Var） | `e.Type()` と同じ具現化 |
| expr.go:348 (CtorRef) / :351 (CtorApp) | 同上（パターン側 Var 経路） | 同左 |
| expr.go:1050 (CtorApp) | `ci.TypeArgs`（callInfo） | `resTyp` と同じ具現化 |
| expr.go:1445 (CtorApp) | `ci.TypeArgs` | `retAfter(calleeSig, arity)` と同じ |
| expr.go:1821 / :1833 (CtorApp) | `f.TypeArgs`（CtorRef から飽和化） | `resTyp` / `retAfter` と同じ |

全経路で TypeArgs は Typ と同一の具現化を転記するだけ — 独立の情報を持たない（validator が一致を検査しているのもその裏返し）。合成ノード（expr.go:2406 の `Ok`、:2720 の `Err`）は当初から TypeArgs なしで emit しており、codegen は既にその経路を fallback で処理している。

## TypeArgs 参照側

| 箇所 | 用途 | TypeArgs なしでの代替 |
| --- | --- | --- |
| `internal/codegen/expr.go:185` | `ctorTArgs(x.TypeArgs, fn.Ret)`（CtorRef の closure 化） | `ctorTArgs` は既に nil 時 `ret` の Args にフォールバック — 呼び出しを結果型のみに寄せるだけ |
| `expr.go:459, 472`（CtorApp → `ctorLit`） / `:537` | 同上 | 同左 |
| `expr.go:196-197` | `variantParams(fn, a.TypeArgs)` / `ctorLit(..., a.TypeArgs, ...)` | 引数を `resultCon` 由来に差し替える（シグネチャ変更 or 呼出側で解決） |
| `expr.go:478-486` `ctorTArgs` 本体 | nil フォールバック付きアクセサ | フィールド除去後は fallback 部だけが残る（`resultCon(Typ).Args`） |
| `internal/core/validate.go` | `ctorTArgs`（578-613）＋ `targs` 呼出 2 箇所（CtorApp:1049, CtorRef:840/846） | 検査対象が消えるので撤去 — 代わりに残るのは「CtorApp.Typ は Con」という既存の構造前提 |
| `internal/treedump/core.go` | 表示のみ | 行削除 |
| `internal/core/call.go:211` / `expr.go:97` | フィールド宣言 | 除去 |

check 側（typed AST）の `CtorApp`/`MemberExpr` の TypeArgs は診断・型情報として残る想定 — (b) の範囲は core IR フィールドのみ。

## 撤去の規模見積もり

- core: フィールド 2 箇所削除（CtorApp/CtorRef）＋ validator の ctorTArgs 関数と呼出（約 60 行）
- lower: 8 箇所の `TypeArgs:` 引数を落とすのみ（Typ は既に正しい）
- codegen: `ctorTArgs` を `resultCon`-only に単純化、2 関数のシグネチャから targs を落とす（約 15-25 行）
- treedump: 表示行
- テスト: golden 出力は不変（emit される Go は派生値なので同じ）。validator の TypeArgs 違反検査テストは対象が消えるため撤去・書き換えが要る（`internal/core/validate_test.go` の lane1 追加分）

規模: **小〜中**（1 ファイルあたり数十行、合計 ~150 行前後の削除中心の差分）。flat params との相互作用はない — flat 規約は関数型パラメータの話で ctor ノードは関係しない。

## 利得

- 「書くが読まない」状態の恒久排除 — optional-but-authoritative はフィールドが残る限り「存在するが必須でない」中途半端さがあり、validator の検査自体が「フィールドが冗長である」ことの証明になっている
- lower の emit コードが簡素化（CtorApp 生成時に instantiation を二重に持ち回らなくて済む）
- validator の検査面積が減る

## コスト・リスク

- 情報の単一化により、`Typ` の結果型が Con でない経路がもし存在すれば検出不能になる — ただし emit 全経路で Typ は具現化済みで、現行 validator も「TypeArgs present ⇒ 結果は Con」を要求済み。TypeArgs 欠損を許す経路（imported/builtin ctor）は今日も Typ だけで動いているので、後退はない
- check 側の TypeArgs は残るので「全部消える」わけではない — 正の一本化は core IR 内の話
- 差分は削除中心だが validator テストの撤去は lane1 で足したばかり（往って戻る形）

## 判定材料まとめ

| | (a) 現状維持 | (b) Typ 一本化 |
| --- | --- | --- |
| 変更規模 | 0 | ~150 行削除中心 + テスト整理 |
| 不変条件の表現 | validator が冗長性を検査 | 冗長フィールド自体がない |
| リスク | 「書くが読まない」回帰余地が残る | Typ 由来の具現化が唯一経路 — 欠落時は Con.Args なしでコンパイルエラーに出る（既に fallback で稼働中） |

移行しても行動上の差分は出ない見込み（golden 不変）。コストは lane1 の検査テストを書き直す手間が主。
