# 型クラス vs 明示的辞書渡し（wave23 lane3）

2026-11-19。open-questions「型クラスの代替」（D テーマ v2 設計の
先取り）の設計空間棚卸し。podhmo 方針により一意には定めず、
候補と判定材料を残す（採否は podhmo / wave24 以降）。

## 起点: 現状の等値・比較

- `==`/`!=`/`assert_eq` は構造的等価だが**型変数には使えない**
  （E0409）。実体はコンパイラが mint する `gomEqualN` 生成比較器と
  `internal/eval` の `eqValue`（同じ読み）。名前のない Go 型は
  `rt.Equal` の反射比較
- 比較演算 `<` は数値型と named 数値型（3.3 節 named 算術）のみ。
  `String` にも `<` はない
- stdlib に比較器渡しの API はまだない（`List.map`/`filter`/`fold`
  系のみ — `sort`/`min`/`member` 等は未提供）
- deriving 相当は「コンパイラが型ごとにコードを mint する」機構
  （等価 `gomEqualN`・表示 `gomShowN`、budget 128）であって、
  ユーザが拡張できる仕組みではない

つまり「型をパラメータに挙動を変える」唯一の現行手段は
**素の関数引数**（`fn sort (lt: a -> a -> Bool) (xs: List a)`）。

## 設計空間: 3 案

### (1) 明示的辞書渡し — `Eq a`/`Ord a` record を引数で渡す

```gom
type Ord a = { lt: a -> a -> Bool }
fn sort (ord: Ord a) (xs: List a) -> List a
let int_ord : Ord Int = { lt = fn a b -> a < b }
sort int_ord xs
```

**実験で確認: 今日の Gom のまま書ける**（generic record + fn field は
実装済み）。`experiments/w23/typeclass/dict/a.gom` が `gom check`・
`gom test`（eval）・`gom build` を通り、生成 `a.go` も `go test` 緑。
生成形は `type Ord[A any] struct{ Lt func(A) func(A) bool }` +
`Min[A any](ord Ord[A], xs list.List[A])`。

### (2) 型クラス風の暗黙辞書

```gom
class Ord a = { lt: a -> a -> Bool }
instance Ord Int = { lt = fn a b -> a < b }
fn min [Ord a] (xs: List a) -> Option a
min [3, 1, 2]            // 辞書は解決されて挿される
```

mock と elaboration 後の手書き Go は
`experiments/w23/typeclass/typeclass/`。**生成 Go は (1) と同型** —
型クラスは「辞書レコード + 暗黙の引数合成」であり、差は codegen
ではなく言語面（解決・coherence）に集中する。

### (3) 現状維持 — 個別の関数引数

`fn sort (lt: a -> a -> Bool) (xs: List a)`。辞書を record に
束ねないだけで、(1) の部分集合と見なせる。複数操作が要る抽象
（`Ord` が `eq`/`lt`/`compare` を持つ等）で引数が散らばる。

## 影響の整理

| 軸 | (1) 辞書渡し | (2) 型クラス風 | (3) 現状維持 |
| --- | --- | --- | --- |
| 新しい言語面 | **ゼロ**（generic record + fn field は実装済み — 実験で確認） | 大: `class`/`instance` 宣言、constraint 構文、メンバー名のスコープ規則 | ゼロ |
| 推論・型検査 | 既存のまま（辞書は普通の引数） | instance 解決（backchaining 検索・overlap/coherence・曖昧性診断・orphan 規則）が新しいフェーズ級の仕事。value restriction との絡みも要検討 | 既存のまま |
| `.gomi` | そのまま（record 型と let を公開するだけ — 実験の dict.gomi） | instance の公開規則・instance を返す/使う関数の表現が要る | そのまま |
| 生成コード | 辞書 record + 明示引数（実測形は a.go） | **(1) と同型**（elaboration で同じ構造に落ちる） | 関数引数 |
| 呼び出し側 | 辞書を名指す（`sort int_ord xs` — 明示的で追いやすい、非標準順も自然: `desc_ord`） | 何も書かない（標準的な1経路 — ただし非標準順には newtype か別 class が要る: coherence が同型2 instance を禁じるため） | 関数を毎回渡す |
| deriving 相当 | コンパイラ mint をそのまま辞書値として emit する拡張で到達可能（`Ord` instance を auto-derive するだけ — 言語面不要の経路がある） | class/instance があれば自然だが同じことが deriving 宣言でもできる | mint のまま |
| 失敗時の診断 | 型不一致として素直に出る | 「instance が見つからない/曖昧」は新しい診断族が要る | — |

## ユースケース（v1 の記述可能範囲から拾った需要）

- **型変数への等値・比較**: `member`/`nub`/`sort`/`min`/`unique` 系は
  現状書けない（`==` が E0409、`<` は数値のみ）。(1) なら今日でも
  `fn member (eq: a -> a -> Bool) (x: a) (xs: List a)` で書ける — 
  **需要は stdlib API の追加で満たせ、言語拡張なしで届く**
- **stdlib の比較器 API**: `List.sort` 等を足すなら引数の形を決める
  必要がある — (3) の `lt` 引数か (1) の `Ord` 辞書か。辞書にする
  動機は「1 型に複数操作を揃える」（`Ord` が `lt`+`eq`+`compare`）
- **deriving**: `==`/`assert_eq`/show はコンパイラ mint で済んでいる。
  ユーザ拡張の deriving は v2 話
- **HOF の境界**: dict 渡しは関数値を record に入れるだけ — 
  curried HOF（flat fn params 特殊化）との相性に新しい問題なし

## coherence の代償 — 同じ型に複数の振る舞い（podhmo からの指摘）

(2) の instance 解決が買う一貫性（同じ型には同じ instance が来る）は
裏返しで**表現力の制約**になる: coherence は「同じ型に 2 つの instance
を立てられない」ことを意味し、**Monoid 型の抽象**——同じ型を複数の
正当な振る舞いで扱いたい設計——が素直に書けなくなる。

- Haskell の例: `Int` の Monoid instance は加算と乗算で 2 つ正当な
  ものがあり、coherence のために `Sum`/`Product` の newtype 包みが
  要る — newtype は構文・unwrap の儀式・eq の分断を持ち込む
- 辞書渡しではこの自由度は**そのまま**残る: 実験の `dict/a.gom` の
  `int_ord` と `desc_ord` は同じ `Int` に対する 2 つの辞書で、
  `min desc_ord xs` のような非標準使用が newtype なしに書ける
  （実証済み）
- つまり「辞書の束ね方」だけの話ではない: (1) は instance の**選択を
  呼び出し側が持つ**設計、(2) は選択を**言語が一意化する**設計。
  Monoid・圏論的抽象・テスト用の別実装差し替えのような
  「同じ型に違う振る舞い」を中心に据えるコードベースでは (2) の
  coherence は純粋な足かせになり、newtype 経路を整備しないと
  その分の表現力を失う

## 判定材料（一意に定めない）

- **(1) は「すでに言語が持つものの組み合わせ」** — 実物が動くことを
  本 lane で確認した。すぐ使えるし、撤去コストもない（普通の record）
- **(2) が買うものは「書かなくてよさ」と一貫性**（同じ型に同じ
  instance が来る保証）。払うものは解決機構・coherence 規則・
  診断族・`.gomi` 拡張 — v2 の大きな投資。かつ coherence は
  表現力制約でもある: Monoid 型の「同じ型に複数の正当な振る舞い」は
  newtype 包みなしに書けない（前節）— (1) にはこの制約がない
- **(3) は (1) の辞書を束ねない版** — 1 引数で足りる API には
  これで十分であり、(1) と両立する（辞書を受ける API と素の関数を
  受ける API は共存できる）
- 移行経路: (1) で辞書の**形**を先に育てておけば、(2) を入れたとき
  `instance` は同じ record 値の自動供給になる — 生成コードの形が
  同じなので (1) → (2) は非破壊に拡張できる（呼び出し側は
  明示辞書を残してもいい設計にできる）
- 反対方向の事実: (2) の暗黙解決は「コードに書かれない引数」を
  生む — 読むときに instance テーブルへの目grepが要る。(1) は
  常に呼び出しに名前が出る

## 関連

- open-questions「型クラスの代替」（本 sketch を参照先として更新）
- 実験: [experiments/w23/typeclass/](../../../experiments/w23/typeclass/)
- 等価・表示の mint 機構: spec 4.9 節、design.md
- `Go.Map` の `comparable` 制約（E0508）も「型につく制約」の一種 —
  型クラス導入時の同居点として記録しておく
