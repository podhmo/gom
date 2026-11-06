# `Go.Any` への適合はボックスを物質化する — 宣言結果・exported let・コンテナ要素まで一経路

- 状態: 採用（検証済み — [検証](../sketch/2026-11-06-go-any-conformance-verification.md)。spec.md §8.3 へ反映済み）
- 日付: 2026-11-06
- 関連: [go-any-spellable](2026-11-06-go-any-spellable.md)（綴りの採用と残ギャップ 1–4 への本決定の委任）、[nilable-iface-pair-ok-payload](2026-11-06-nilable-iface-pair-ok-payload.md)（`Result Go.Any GoError` の派生ビューが返る実 API）

## Context

[go-any-spellable](2026-11-06-go-any-spellable.md) は `Go.Any` の
綴りを採用しつつ、実装で解くべき 4 件の materialize ギャップを
impl lane に送った:

1. `fn -> Go.Any` / `-> Result Go.Any GoError` の本体が
   `json.Token` や型変数だと check は受理するのに lower が
   E0901（body type ≠ residual type）で止まる。
2. `.gomi` の `let x : Go.Any` 広告に対し生成 Go は初期化子の
   具体型（`var X string`）を出す。
3. `let o : Option Go.Any = None` は ctor の要素 meta が
   `Go.Any` want に bind せず E0407。
4. `let o : Option Go.Any = Some 42`（`Ok 42`、`[1]` 同型）は
   check/build を通るのに emit は `Option[int64]` — Go
   ジェネリックの型不変性で `go build` が落ちる。

判断材料として sketch が残した分岐は「payload materialize
するか、check で concrete-bind を拒否するか」と「宣言結果に
ボックスを生成するか check を厳しくするか」。

## Decision

**`Go.Any` への適合は値スロットで常にボックスを物質化する。**
checker の `conformExpr` は `unify(Go.Any, got)` が通った式を
`Ascribe{Conv: true}` で包み、lower は既存の `ConvBoxAny`
経路（`any(x)`、数値は `convBoxSrc` で Gom 数値表現へ先に
物質化）で emit する。これで 1・2 は追加の経路なしに解く —
`-> Go.Any` の本体は `any(body)` で residual type に合い、
`var X` の宣言型は `d.Body.Tail.Type()` = `Go.Any` を拾って
`var X any` になる（型忠実度は Core の型物質化として運ぶので
codegen に表面情報を持ち込まない）。

**合成型の型引数位置では `Go.Any` は受理規則ではなく
インスタンス化の選択になる**（Go ジェネリックは型不変 —
`Option[int64]` は `Option[any]` ではない）。unify を
「値スロット判定」と「構造判定（`unifySub`）」に分け、
Con・Tuple・`types.Go` の型引数位置は後者で裁く:

- `unifySub(Go.Any, 未解決 meta)` は `Go.Any` に bind —
  `Option Go.Any` のもとでの `None`/`Some`/`Ok`/list リテラルは
  `any` 要素の合成を直接組み立てる（`None` の E0407 = 残
  ギャップ 3 はこの規則で解く）。
- `unifySub(Go.Any, concrete)` は一致要求 — `Option Int` の値は
  `Option Go.Any` スロットに合わず、要素差分を名指しする
  E0401（`expected Go.Any, got Int`）。
- apply の先行 pin は want 側の `Go.Any` 要素に対応する ctor の
  未解決 meta だけを bind し、concrete の不一致は apply 自身の
  conform に残す（診断の二重化を避ける）。

**関数型は全位置で不変 — 値スロット受理は適用サイトの conform
だけが持つ。** 2 つの関数型の比較（`unifyFunc`）はパラメータも
結果も `unifySub` で裁く — `any -> Int` と `String -> Int` は
emit される Go が `func(any) int64` ≠ `func(string) int64` で
型不変なので、注釈・パラメータ・match などのスロットで互いに
置けない（どちら向きも E0401）。`Go.Any` を含む関数型を期待して
`fn x -> …` を書いた場合は期待位置の materialize が働き、
`x` は `any` に bind されて `func(x any) int64` が emit される。
一方「`any -> R` の callee に `String` 値を渡す」は型の比較では
なく引数ごとの conform — 値スロット判定が `any` へのボックスを
物質化する（pipe/over-application/composition も同じ経路）。
`unifySub` が `Go.Any` を含むかの判定は型を深く歩く
（`Option (Go.Any -> Int)` や `Result (List Go.Any) GoError`
の内側も捕捉し、`unifyFunc` の入口で `unify` ではなく
`unifySub` を選ばせる）。

**スカラの `Go.Any` スロットは受理するだけで ctor の
インスタンス化を選ばない。** `let x : Go.Any = None` は引き続き
E0407 — 値スロットの受理は「どの具体値も `any(x)` に包める」
という規則で、`None` の要素型を `Go.Any` で決めると `let l =
[]; f l` のように後で具体型へ bind する使い方を先に食う
（受理側で環境共有 meta に `Go.Any` を書き込む副作用の方が
E0407 より扱いにくい）。合成の注釈 `Option Go.Any` がこの形の
答え。

## 検証まとめ

- `fn -> Go.Any = 42` / `-> Result Go.Any GoError = Ok 9` /
  `try dec.Token()` 経路 → E0901 解消、`any(int64(42))` /
  `result.Ok[any, error](any(t))` emit。
- `.gomi` `let x : Go.Any` → `var X any`（golden
  `go_any`・spec `ok_go_any_gomi/`）。
- `Option Go.Any = None` → `option.Option_None[any]{}`；
  `Some 42`/`Ok 42`/`[1]`/`takes [5]` は全て `…[any]` emit。
- `Option Int` 値の `Option Go.Any` スロット供給 → E0401
  `expected Go.Any, got Int`（spec `E0401_go_any_invariant`）。
- 回帰: `x |> fmt.Println`、`"b" |> fmt.Println("a", _)`
  （`any -> R` のパラメータ受理を保持）、eval の
  `Ascribe{Conv}` 到達 — `Go.Any` ボックスを evaluator の
  no-op として exempt。
- 全詳細: [sketch](../sketch/2026-11-06-go-any-conformance-verification.md)。

## Alternatives

- **宣言結果・コンテナ payload は check で厳しく拒否する** —
  不採用: `try dec.Token()` → `Ok t` のような実害パターン
  （named 空 interface の結果を verbatim `Result` へ流す）を
  書けなくし、「`Go.Any` want にはボックス」と他の want 位置と
  規則が分かれる。spec の読みも単純な方（ボックスが生成される）
  に倒した。
- **スカラ `Go.Any` 受理時に got 側の未解決 meta へ `Go.Any`
  を流す**（`let x : Go.Any = None` を `Option[any]` に）—
  不採用: 環境と共有される meta への早い bind で後の具体型使用
  が壊れる順序依存を生む。制限は E0407 として残し、合成の答え
  は `Option Go.Any` 注釈（検証 sketch の audit 節）。
- **emit を `any(x)` にせず `var X` の型だけ declared 型にする**
  — 不採用: 型忠実度を codegen の判断にすると規範は Core に
  載らない（design.md 2.1）。`Ascribe`/`Conv` の物質化は
  Core の型として伝わるので `d.Body.Tail.Type()` が自然に
  `Go.Any` を返す。

## Consequences

- spec §8.3 の `Go.Any` 段落を規範通りに更新（受理→ボックス
  materialize、型引数位置のインスタンス化選択、スカラの制限）。
- 残ギャップ 1–4 は解消 — implementation-gaps.md を `[x]` に。
- pat.go の ctor-pattern `Go.Any` pin（nilable-iface 決定時の
  workaround）は `unifySub` が包含するので撤去 — 規則は unify の
  構造判定一本になる。
- `unify` の契約が「値スロット判定」に明確化 — 合成の中では
  `unifySub` を使う規律はコメントと呼び出し点で分かる。
- codegen の一時変数順が golden で変わる（`any` ボックスが
  引数の原子化に乗る）— `float_int_literal` を regen。
