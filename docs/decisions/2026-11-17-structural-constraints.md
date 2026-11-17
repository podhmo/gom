# Go ジェネリック関数の構造制約（`~[]E`・`~map[K]V`・基本型 union）を公開する

- 状態: 採用
- 日付: 2026-11-17

## 背景

P3-3 で `any` / `comparable` 制約のジェネリック関数は呼べるようになったが、
`slices` / `maps` / `cmp` のほぼ全関数が使う `S ~[]E`・`M ~map[K]V`・
union 制約は E0506 で非公開のままだった。調査の結果、3 クラスを認めれば
66 の公開関数が解放される（[survey](../sketch/2026-11-17-wave15-structural-constraints.md)）。

## 決定

次の制約を持つ Go ジェネリック関数を公開する。型引数は宣言順に、呼び出し側が
解いた Gom 型から推論する（`any`/`comparable` と同じ経路）。

- `S ~[]E` — `S` の出現位置は `Go.Slice E` として読む（パラメータ・戻り値を問わない）
- `M ~map[K]V` — `M` の出現位置は `Go.Map K V` として読む
- 型パラメータを含まない union（`~int64 | ~string` など、`cmp.Ordered` 相当）—
  項の判定は出力する Go の型で行う。`Int` の repr は `int64` なので `~int64` を
  満たし `~int` は満たさない。`interface{ cmp.Ordered }` のような named union
  interface を包んだ綴りも同じ型集合として扱う
- union の項が slice / map を指すとき（`~[]byte`、`[]byte | string`）`List`
  型引数は E0508 — 出力が named な `list.List[T]` で項に属さず、素通しは
  callee の書き込みが backing に届く。`Go.Slice Byte` は匿名に綴れるので認める

構造制約を「パラメータの表層を広げる」として扱う（`instantiateSig` が
`S` を `Go.Slice ?E`・`M` を `Go.Map ?K ?V` に置き換える）ことで、2 つの
性質を既存機構からそのまま得る:

- `List` 引数は境界のコピーを通る — 呼び出し先がスライスを書き換えても
  `List` の不変性は保たれる。in-place 変更の形（`slices.Sort xs`）は
  W0501 の対象になり、警告側は `CallInfo.TypeArgs` 経由で `S` の実体を見る
- 制約だけに現れる型パラメータ（`maps.Equal` の K/V）は普通の引数適合で
  推論される — Go の constraint type inference を別パスで実装しなくてよい

公開しないもの（E0506、理由は診断に出す）: メソッドを持つ制約
（`errors.AsType` の `T interface{ Unwrap() []error }` など、interface の
埋め込みが 1 つでもメソッドを持つ場合を含む）、近似項・union 項の交叉
（`interface{ ~int64 | ~string; ~int64 | ~bool }` — 平坦化すると型集合を
緩めるので union の包みは 1 個に限る）、型パラメータを含む union 項、
そして制約自身を項に参照する再帰形（`S ~[]S` — Gom で綴れない形状を
メタが再帰的に満たすのを防ぐ）。

検証: [sketch](../sketch/2026-11-17-wave15-structural-constraints.md) の
prototype 節。`ok_go_structural_generics.gom`、W0501 の構造パラメータ版
fixture、goimport の session テスト（両方向）。

## 残る制約

- ジェネリックな Gom 関数の中からの転送（`fn f (xs: List a) = slices.Contains(xs, v)`）は
  `a` が Go repr を持たないため E0508 — `comparable` の `Var` 拒否と同じ正直な残差
- `maps.Keys` など結果が `iter.Seq` などのインスタンス化ジェネリック型の関数は
  引き続き E0501 — 制約クラスとは無関係の別残差
