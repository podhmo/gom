# `CtorApp`/`CtorRef` の `TypeArgs` を廃止し `Typ` の `Con.Args` を唯一の正にする

- 状態: 採用（wave24 lane A で実装）
- 日付: 2026-11-19
- Supersedes: [2026-11-18-ctor-typeargs-authoritative](2026-11-18-ctor-typeargs-authoritative.md)

## 問題

wave21 で採択した optional-but-authoritative は「存在するが必須で
ない」中途半端な状態を残した —— validator が `TypeArgs` と結果
`Con.Args` の一致を毎回検査していたのは、フィールドが冗長である
ことの裏返しだった。棚卸し（[sketch](../sketch/2026-11-19-ctor-targs-b-inventory.md)）
で lower の 8 経路すべてが `TypeArgs` を `Typ` と同一の具現化で
転記するだけと判明し、codegen 側も `ctorTArgs` のフォールバック
経路（`Typ` の `Con.Args` から派生）が既に稼働していた。

## 採択（podhmo 判定 (b) 採用 2026-11-19）

`CtorApp` / `CtorRef` から `TypeArgs` フィールドを撤去し、ADT の
具現化は `Typ`（CtorApp は結果 `Con` そのもの、CtorRef は矢印型を
chase した終着 `Con`）の `Args` を唯一の正とする。

- **core**: フィールド削除。`v.ctorTArgs` 検査関数と CtorApp/
  CtorRef への `v.targs` 呼出を撤去（検査対象が消える）。
- **lower**: 8 箇所の `TypeArgs:` 転記を削除。`Typ` は従来どおり
  具現化済み型を置く。
- **codegen**: `ctorTArgs` は結果型からの派生のみに単純化。
  `variantParams` / `ctorLit` / `ctorGoType` の `targs` 引数を削除
  （呼出側は受け側型の `Con.Args` を既に解決済みだった）。
- **treedump**: ctorapp/ctorref 行から `targs=` 表示を削除。
- **`FuncRef`/`CallDirect`/`CallGo`/`GoFuncRef` の TypeArgs**:
  多相 callee の具現化情報で別物 — 対象外。

## 却下した案

- **(a) 現状維持（optional-but-authoritative）**: 元 decision の
  採択案。「書くが読まない」回帰余地が残り、validator の一致検査が
  冗長性の証明になる形は解消されない。
- **`TypeArgs` を残して値を見ない派**: 未検査フィールドとして残る
  と再び drift の温床になるため却下（元 decision で同趣旨の懸念が
  既に挙がっていた）。

## 影響

- 利用者から見える振る舞い・生成コード: 変わらない（emit される
  Go は従来フォールバックが返していた派生値と同一、golden 不変）。
- ~150 行削除中心の差分。validator の検査面積が減り、lower の
  emit が単純化する。
- リスク: `Typ` の終着が `Con` でない経路は検出不能になるが、全
  emit 経路で `Typ` は具現化済みであり、TypeArgs 欠損を許す経路
  （imported/builtin ctor）は今日も `Typ` だけで動いているため
  後退はない（棚卸しの評価どおり）。
