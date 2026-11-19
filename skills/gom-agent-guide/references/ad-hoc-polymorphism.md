# 型クラス的なことがしたいとき（辞書渡し）

Gom に型クラスはない（採用しない —
[decision](../../docs/decisions/2026-11-19-ad-hoc-poly-dict-passing.md)）。
「型ごとに振る舞いを差し替えたい」は **辞書渡し**: 操作を束ねた
record 値を関数に渡す。言語機能の追加はなく、record と普通の引数
だけで書ける。

動作確認済みの実物は `experiments/w23/typeclass/dict/`。

基本形は toolchain 同梱の `cmp` モジュールとして stdlib に収録済み
（`import "cmp"`）: `cmp.Eq a` / `cmp.Ord a` の辞書型、スカラー型
ごとのインスタンス（`cmp.int_ord` など、byte は eq のみ）、そして
辞書を第 1 引数・コレクションを末尾に取る
`cmp.sort` / `cmp.min` / `cmp.max` / `cmp.member`、辞書合成の
`cmp.desc` / `cmp.eq_of`。`xs |> cmp.sort cmp.int_ord` の形で
pipe に乗せられる。

## 基本パターン

```gom
// 辞書型: 操作を束ねた record（型変数を残して多相にする）
type Ord a = { lt: a -> a -> Bool }

// 内部補助は辞書ではなく部品の関数だけを受け取る
fn insert (lt: a -> a -> Bool) (x: a) (xs: List a) -> List a = match xs {
  [] -> [x]
  [y, ..rest] -> if lt x y { [x, ..xs] } else { [y, ..insert lt x rest] }
}

// 使う側は辞書を第 1 引数に取る
fn sort (ord: Ord a) (xs: List a) -> List a = match xs {
  [] -> []
  [x, ..rest] -> insert ord.lt x (sort ord rest)
}

// 具体的な辞書は普通の let 値。同じ型に複数あってよい
let int_ord : Ord Int = { lt = fn a b -> a < b }
let desc_ord : Ord Int = { lt = fn a b -> b < a }

test "sort by dictionary" {
  assert_eq (sort int_ord [3, 1, 2]) [1, 2, 3]
  assert_eq (sort desc_ord [3, 1, 2]) [3, 2, 1]
}
```

- record の field アクセスで操作を取り出す（`ord.lt`）。必要な
  操作だけを渡したい内部補助は、辞書ではなく生の関数を取る
  （上の `insert (lt: a -> a -> Bool)`）
- 辞書は型付きの普通の値なので、関数から返す・record に入れる・
  別パッケージから import するのもそのままできる（公開は `.gomi`
  に値として書くだけ）

## 複数の「instance」を持つ型

同じ型に辞書はいくつでも立てられる — これが型クラスの
coherence（型ごとに instance 一意）との一番の違い。

- 順序: `int_ord` / `desc_ord` / 長さ順の `string_by_len` など
- Monoid 相当: `type Monoid a = { empty: a, append: a -> a -> a }`
  を `sum_monoid` / `product_monoid` / `list_monoid` のように
  複数定義。newtype 包みは要らない
- テスト用の差し替え実装も別名の辞書を定義して渡すだけ

```gom
type Monoid a = { empty: a, append: a -> a -> a }

let int_sum : Monoid Int = { empty = 0, append = fn a b -> a + b }
let int_prod : Monoid Int = { empty = 1, append = fn a b -> a * b }
let list_cat : Monoid (List a) =
  { empty = [], append = fn xs ys -> List.append xs ys }

fn concat (m: Monoid a) (xs: List a) -> a =
  List.fold m.append m.empty xs
```

## 命名と置き場所

- 辞書名は `<型>_<役割>`（`int_ord`, `string_ord`）または役割名
  （`desc_ord`）で揃える。推奨の「デフォルト辞書」があれば
  `<型>_<抽象名>`（`int_ord`）をその名にする
- その型の標準的な辞書は**型と同じパッケージ**に置く。用途別の
  辞書は使う側のパッケージで定義してよい
- 関数は辞書を第 1 引数に取る（`sort ord xs`）— カリー化で
  `let sort_int = sort int_ord` のような部分適用済み API が
  作れる

## 書けないこと・注意点

- **呼び出し側が毎回辞書を指名する**。暗黙解決はないので、
  深く入れ子になったジェネリック関数は辞書をずっと運ぶ必要がある
  （`sort` 内の `insert` も `lt` を受け取る例のように、途中で
  record から部品だけ取り出して運ぶと浅く保てる）
- 辞書の中身は呼び出し側が自由に作れるので「この型の正しい辞書」
  の保証はない。規約（`int_ord` は `<`）はコードレビューと
  テストで守る
- `==`/`assert_eq` の構造的等価は型変数には使えない（E0409）。
  等価が要る抽象は `Eq a = { eq: a -> a -> Bool }` 辞書として
  引数に取る

## deriving がほしいとき

等価・表示はコンパイラが型ごとに生成コードを mint する
（`gomEqualN`/`gomShowN`）のでユーザが書く必要はない。辞書の
自動生成（deriving 相当）はまだない — 複合 record の辞書を
手で合成する場合は、部品の辞書を引数に取るコンストラクタ関数に
するとネストが浅い:

```gom
type Ord a = { lt: a -> a -> Bool }

fn pair_ord (o1: Ord a) (o2: Ord b) -> Ord (a, b) =
  { lt = fn p q -> match (p, q) {
      ((x1, x2), (y1, y2)) ->
        if o1.lt x1 y1 { true } else {
          if o1.lt y1 x1 { false } else { o2.lt x2 y2 }
        }
    } }
```
