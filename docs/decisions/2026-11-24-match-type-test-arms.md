# `Go.Any` への `match` 型テスト腕 — `p : T` 腕 + `_` 必須

- 状態: draft
- 日付: 2026-11-24
- 関連: [go-any-extraction](2026-11-24-go-any-extraction.md)（単一取り出しは `Go.try_as` で採択済み — 本 decision は多型ディスパッチの面）、調査 [sketch/2026-11-24-match-type-test-arms](../sketch/2026-11-24-match-type-test-arms.md)

## Context

`Go.Any`（および named Go interface 型）に入った値を動的型で複数
分岐したい需要 — JSON の union デコード、plugin 的な値の受け渡し
など — に対し、`Go.try_as` の入れ子では default 腕の位置が不自然
になり読みにくい。`match` に型テスト腕を足す案は lane F で「開い
た型への網羅性設計が新規」を理由に先送りされた。その設計をここ
で固める。

## Options

- **A: `match` に型アスクリプション腕 `p : T`（推奨）** — 既存の
  腕形を拡張するだけで新構文を足さない。scrutinee の型が
  `Go.Any` 系（`types.Chase` 後に `Go.Any` または named Go
  interface 型）のときのみ合法。
- **B: 専用式（typeswitch 的な別形）** — `match` の意味論と Go
  type switch の意味論の混線は避けられるが、構文が増える。
- **C: 現状維持** — `Go.try_as` 入れ子。ゼロコストだが多腕分岐で
  読み書きが劣化する。

## Decision（draft）

**案 A** — 次の形で採用したい。

- scrutinee が `Go.Any` 系の `match` の腕に型テスト形 `name : T`
  （および `_ : T`）を許す。`T` は `Go.try_as` と同じく具象型式 —
  `Go.Any`・型パラメータへのテストは拒否。
- 腕の意味は Go の `case T:` と同じ: 動的型が `T` のとき
  `name` を `T` で束縛して本体を評価する。
- **`Go.Any` 系 scrutinee の match は `_` 腕を必須**とする（式と
  して値を返す以上、暗黙 default は許さない）。違反は専用診断
  （E0301 の亜種または新コード — 実装時に決める）。
- 通常パターン腕との混在は禁止: `Go.Any` 系 scrutinee では腕は
  型テスト形・`_` のみ、sum scrutinee では型テスト形は書けない。
- nil は `_` 腕が受ける。明示的な nil 腕の綴りは実装時の論点と
  して残す。
- emit は Go `switch v := x.(type) { ...; default: ... }` への
  専用 lowering（Maranget の decision tree は通らない）。eval は
  腕順の内部断言。

### 型テスト腕の順序と到達不能

Go type switch の first-match 意味論をそのまま採る — 腕は書かれた
順に試される。逐語に同じ型の重複（`n: Int` と `m: Int`）は構文的
に E0302 相当で拒否する。subtype 包含による隠蔽（interface
ターゲットが先行の具象腕を覆う等）を静的に指すかは**実装時の
論点**: 指すには go/types の包含判定 oracle が要り、pattern 行列
に乗らない新規検査になる。許すなら到達不能はユーザー責任で
warning なし。

## Rejected alternatives

- **panic 形・comma-ok の再定義** — `Go.try_as` が既に単一取り出
  し面として確定済み。型テスト腕は分岐形であり代替ではない。
- **型テスト腕で網羅を証明させる**（列挙した `T` の直和で Any を
  閉じる）— 開いた型に閉じた集合を被せる嘘になる。`_` 必須で
  誠実に扱う。
- **`Int n` 形（ctor 風の綴り）** — sum ctor パターンと見分けが
  つかず、`x : Go.Any` 限定でも読み手の誤解を招く。

## 採択時の実装メモ

- syntax: パターンに型テスト形を追加（`pat := name : Type`）。既存
  の `pat`/`->`/guard 構造の中に収めるか、腕構文を分けるかは
  grammar への影響が最小の方を選ぶ。
- check: scrutinee 型で腕形を切り分け（`Go.Any` 系 ⇔ 型テスト腕
  のみ、それ以外 ⇔ 通常腕のみ）、腕束縛は `name : T`。`nil` の扱い
  と `_ : T`（束縛しない型テスト）もここで確定。
- pattern: `Go.Any` 系 scrutinee は Maranget 入力を構築せず、`_`
  腕の有無と型テスト腕の逐語重複だけを検査する別経路。
- lower/codegen: `switch v := x.(type)` 直写し。bound 変数の絞りは
  Go の型スイッチ束縛に載せる。
- spec: match 節に型テスト腕・`_` 必須・混在禁止を記述、spec
  fixture（受理/拒否・混在エラー・重複エラー）を追加。
- 実装は wave40 以降の lane（規模: 中〜大）。
