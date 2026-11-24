# String / Char / Bool リテラルの宣言型への代入可能性

- 状態: adopted
- 日付: 2026-11-24
- Supersedes: [2026-11-24-literal-declared-assignable.md](2026-11-24-literal-declared-assignable.md) のうち 却下案「String / Char / Bool リテラルも宣言型へ代入」

## 決定

String / Char / Bool のリテラルも、defined type に emit される宣言型
（1 フィールド record でフィールドが対応する builtin）へ代入できる
—— Go の untyped constant 規則と同じく。

- 代入可能なのはリテラルノードだけ: 型付きの `String` 変数を
  `type Name = {n: String}` へ渡すのは Go と同じく E0401。
  `conformExpr` は `*Lit` ノード（`LitStr` / `LitChar` / `LitBool`）
  だけを剥がれの照合に回す。
- 生成コードは数値リテラルと同じく `Name("x")` と materialize する
  （`Ascribe{Conv: true}` → `core.Conv`）。
- builtin の照合はファイルローカル宣言の shadow 規則に従う — 宣言側の
  record がファイルローカルなら、そのフィールド綴りの `String` は
  このファイルの `type String` で shadow されうる。
- `type Deep = {inner: Name}` のような chained peel も効く。
- 数値リテラルとは経路が違う: 数値は meta の型クラス制約で判定する
  （untyped 相当が言語に組み込まれた経路）のに対し、String / Char /
  Bool は concrete 型なので値レベルの conform で「リテラルかどうか」
  を見る。これは Go の「untyped constant はリテラルから生まれる」
  という規則の写像で、非対称ではなく同一規則の 2 つの表れ。

## 検証

- `testdata/spec/ok_scalar_lit_declared.gom`（引数・let ascription・
  return・ネスト peel）
- `testdata/spec/E0401_scalar_var_declared.gom`（変数は拒否）
- shadowed `type String` + `type Name = {n: String}` の手動確認
  （"x" は E0401）
