# 数値リテラルの宣言型への代入可能性

- 状態: adopted
- 日付: 2026-11-24

## 背景

`Clamp64(u, 0, 100)`（`u: UserId`、`type UserId = {v: Int}` → Go の defined
type `type UserId int64`）は Go では untyped constant の代入可能性で通るが、
gom はリテラル引数の conform が先に「expected a numeric type, got UserId」の
E0401 で落ちていた（implementation-gaps、wave40 lane D で発見）。

## 決定

数値リテラルの meta が解く先の宣言型が Go の defined type に emit される
（1 フィールド record、フィールドが裸の型パラメータでない）とき、その
フィールドの underlying の型クラスで制約を判定する。これは Go の untyped
constant 規則と同型で、以下をまとめて導く:

- リテラルはその宣言型に代入できる（`Clamp64(u, 0, 100)`、
  `let x : UserId = 0`）。生成コードは `UserId(0)` と materialize する —
  bare `0` は Go のデフォルト `int` を束縛して型を失うため、lower が
  `core.Conv`（`UserId(0)`）を敷く。named Go 型のリテラル
  （`time.Duration(30)`）と同じ仕組み。
- 同じ剥がれで宣言型どうしの算術・比較も underlying が許す演算に受理される
  （`u + 0`、`u < 100` は `UserId` / `Bool` を返す — Go の `u + 0` が
  defined type を返すのと同じ）。
- struct emit の宣言型（複数フィールド record、フィールドが裸の型
  パラメータの record）は剥がれず E0401 のまま。named 型と組み込み数値型
  の混在（`u + intVar`）も Go と同じく E0401。
- リテラル範囲判定（E0417）は `litRange` が既に `definedUnderlying` を
  剥がすので変わらない — `{v: Int32}` への大きなリテラルは int32 幅で判定。

## 却下案

- **引数位置だけで受理する（untyped constant 相当の限定）**: Go と同じ意味
  論を型クラス判定の剥がれ 1 か所で自然に得られるのに、位置限定のほうが
  例外面になる。宣言型どうしの算術も Go が許すので分ける理由がない。
- **String / Char / Bool リテラルも宣言型へ代入**: それらは gom では
  concrete 型で meta 制約の経路を通らない（untyped constant ではなく型付き
  リテラル）。`type Name = {n: String}` への `"x"` の代入は等価 unify の
  peel が別途要る別規則 — 台帳には別 gap として残すか不要かは別途検討。

## 検証

- `testdata/golden/go_generics.gom`（`clamp_id_lit`・`bump_id`・`id_bound`
  — `goiface.Clamp64[UserId](u, UserId(0), UserId(100))`、`u + UserId(0)`、
  `x := UserId(0)` の emit）
- `testdata/spec/ok_literal_declared.gom`（`Clamp64`・`+`・`<`・`*`・let 束縛）
- `testdata/spec/E0401_literal_declared.gom`（struct emit の record への混在は拒否）
