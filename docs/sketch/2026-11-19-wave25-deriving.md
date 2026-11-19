# wave25 lane C: deriving 相当の調査 — ユーザー型の `Eq`/`Ord`/`Show` 辞書をコンパイラが合成するか

- 日付: 2026-11-19
- 状態: 調査完了・採否は podhmo に委ねる
- 関連: [decisions/ad-hoc-poly-dict-passing](../decisions/2026-11-19-ad-hoc-poly-dict-passing.md)、
  `stdlib/cmp`、`internal/lower/equal.go`・`show.go`（`gomEqualN`/`gomShowN` mint 経路）、
  open-questions「`deriving` 相当の自動生成」

## 問い

明示的辞書渡し（型クラス不採用）の残件: レコード型ごとに
`{ eq = fn a b -> … }` を手で書く手作業を、コンパイラ mint で
置き換える仕組みが要るか。

## 現状の整理

- コンパイラは既に `==`/`show` のために composite 型の比較・表示
  関数を mint する（`l.eqDeclPlan` → `core.EqDecl` → `gomEqualN`、
  `l.showSeq` → `gomShowN`）。フィールドを宣言情報つきで walk し、
  record/sum/tuple/`Go.Slice`/`Go.Map` ごとの plan を組み立てる
  機械は揃っている
- ただし mint されるのは `==`/`show` が**使われた**型のみで、
  生成物はユーザーに見えない内部シンボル
- ユーザー側の今日の書き方: スカラーは `cmp.int_eq` 等が stdlib に
  あり、複合は自分で `{ eq = … }` を書く。`Ord (Pair a)` のような
  多相フィールドを持つ辞書は「辞書を取って辞書を返す関数」
  （`fn (e: Eq a) -> Eq (Pair a)` 形 — プロンプトの `pair_ord` 形）
  で書けるが、全レコードに手作業が残る

## 設計空間

### 案 1: 据え置き — `cmp` に合成関数だけ足す

`option_ord (e: Ord a) -> Ord (Option a)`、`list_ord`、
`pair_ord (e1: Ord a) (e2: Ord b) -> Ord (a * b)` を `stdlib/cmp`
に追加するだけ。コンパイラ変更ゼロ、`.gomi` 面は通常の公開値。
record 型の `Eq`/`Ord` だけが手作業として残るが、record 辞書は
「その型だけの固有の比較」を書く場なので手作業自体は正当化できる
（構造等価でよい record は `==` を内部で再利用する `{ eq = fn a b -> a == b }`
の 1 行で済む — これがすでに mint の恩恵を受けている）。

### 案 2: `@derive(Eq)` / `@derive(Ord)` 属性で辞書値を mint

`type P = { x: Int, y: Int } @derive(Eq, Ord)` で
`p_eq : Eq P`・`p_ord : Ord P` をコンパイラが生成する形。

- **実装の足場**: eqDeclPlan を流用できるのは `Eq` のみ。
  `Ord`（フィールド辞書順比較）と `Show`（record 形の整形）の
  plan は新規。生成先は `lower` の EqDecl 系だが、「要求駆動で
  なく宣言駆動の mint」なので derive 要求を check → lower に
  運ぶ経路が要る（新しい中間表現）
- **多相フィールド**: `type Pair a = { fst: a, snd: a }` の
  `pair_eq` は値ではなく `fn (e: Eq a) -> Eq (Pair a)` を mint
  する必要がある — eqDeclPlan の meta 型の扱いが「rt.Equal
  fallback」なのに対し、ここでは「渡された辞書を呼ぶ」新 plan
  kind が要る。ここが設計の芯
- **`.gomi` / 公開面**: mint された辞書を自動で `.gomi` に出すと
  公開面が暗黙に増える。出さないなら他パッケージは自分で
  derive すればよいので「同じ型に複数パッケージで辞書が複製
  される」問題は辞書渡しでは無害（coherence が要らない）
- **`Ord` の限界**: Go-opaque な葉（named Go 型、`Go.Any`）は
  順序を mint できない。`Eq` には `rt.Equal` fallback があるが
  `Ord` 相当の fallback はない — derive 失敗時に診断を出す設計
  が要る
- **命名規則**: `<type>_eq` の自動 mint はユーザー定義の値と
  衝突しうる。`derive` 名前空間（`P.eq` のような member 形）に
  するか、衝突時にエラーにするか

### 案 3: 中間 — `Eq` だけ自動（`==` の再利用）

`{ eq = fn a b -> a == b }` が already 動くので `Eq` 辞書の
手作業コストはほぼゼロ → derive が買うのは実質 `Ord` と `Show`
のみ。`Ord` だけでも多相フィールド問題は残るので案 2 と
コストは近い。

## 判定材料

- 手作業の痛みが最大なのは「構造等価でよい多相 record/ADT の
  `Ord`」— 現状フィールドごとの辞書順比較を手で書く必要があり
  ADT の variant 順比較は面倒が大きい
- 案 2 は「型クラス不採用」と矛盾しない（instance 解決ではなく
  名前つき値の自動生成 — 呼び出し側で辞書を明示する性質は
  変わらない）
- 採用するならスコープを `Eq`+`Ord`+`Show` の構造 derive に
  絞り、「辞書を取る derive」（多相型）は v2 以降に分けると
  初期実装を小さくできる
- 残る未測定点: `Ord` derive の ADT ケース（variant 順 =
  宣言順か tag 順か）、`Show` derive の生成フォーマット仕様、
  mint 名の衝突規則

## 結論（材料の整理）

- 案 1（合成関数追加）は今 wave で実装可能な低コスト改善で、
  `Option`/`List`/tuple の辞書手作業はほぼ消える
- 案 2（`@derive`）が買うのは record/ADT の構造 `Ord`/`Show`
  で、コストの芯は「多相フィールドに辞書引数を渡す plan
  kind」と「宣言駆動 mint の経路」の 2 点。型クラス導入とは
  別次元の話で、採用しても辞書渡し方針と整合する
- podhmo の判断材料: 構造 `Ord` を record/ADT にどれだけ欲しいか
  （欲しい → 案 2、当面は合成関数で足りる → 案 1）
