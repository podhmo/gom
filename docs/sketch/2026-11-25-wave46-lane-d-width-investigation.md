# wave46 lane D — eval の width 伝播の残り調査

- 日付: 2026-11-25
- lane: wave46 lane D（[prompts](2026-11-25-wave46-prompts.md)）

## 結論

width 伝播自体に残件はなかったが、調査中に boxed-any equality が
foreign named scalar の nominal type を落とす実装ずれを見つけたため、
lane D で修正した。残る width-32 の入れ子表示検証は foreign named scalar
の括弧 canonical（writeOperand の型駆動再設計、wave46 lane A）に
ブロックされている、という既存の台帳判断は変わらない。

## 調査項目

### 1. `Go.conv` 経由の int→float

eval は `Go.conv` ascription をサポートしない。`internal/eval/doc.go` の
scope 記述どおり unsupported construct で、`internal/eval/eval.go` は
`Go.conv conversions are not supported by the evaluator` を返す。したがって
eval 内部に runtime conversion 経由の int→float width 伝播は存在しない。

constant conversion の width は eval ではなく checker の const-fold 側の責務。
こちらは `foldFloat` / `narrowFloat` が conversion target の float32 幅で
丸め、`testdata/spec/E0414_div_f32_conv.gom` が `SmallRatio` 経由の
float32 丸めを固定している。lane D で追加する残件はない。

### 2. width-32 算術の W 伝播

wave45 lane B の下地どおり、width-32 の算術は float32 精度で実行し、結果に
`W: 32` を付ける。unary negation は既存の canonical 値の W を保存し、
literal materialization は width-32 位置で canonical float32 image を
構築する。`float32_width_literal_rounding` /
`float32_width_arithmetic` / `float32_width_int_spelled_literal` が
eval/compiled 一致を固定している。

### 3. cross-width boxed-any 比較

直接の `VFloat` equality が W を見ないのは意図的でよい。通常の数値比較
では static type が同じなので、W は dynamic type を表す比較種別ではなく
表示・演算の canonical 情報にすぎない。

一方、`Go.Any` 同士の equality は compiled では `rt.Equal`
（reflect.DeepEqual 近似）を通るため dynamic type を区別する。eval は
width-32 の `VFloat` だけが `W=32` を持つ invariant
（`TestFloatWidthTagInvariant`）によって cross-width box を区別する。
width-64 側は W ゼロなので、同じ width どうしは余計なタグ差で割れない。

compare ケース `float32_width_boxed_any_dynamic_width` は次を
eval/compiled の両方で固定する。`SmallRatio` 側は `16777217.0` /
`16777216.0` が float32 構築丸めで同値になることを使い、oracle 未注入では
同型比較が false に落ちる negative control も効く:

- 数値的には同じでも `SmallRatio` box と `Float` box の比較は false
- `SmallRatio` 同士の box は float32 丸め後に true
- `Float` 同士の box は true

### 4. 調査で見つけた実装ずれ: foreign named scalar の nominal type

cross-width の probe を立てたとき、width とは別のずれを再現した:

```gom
let d : time.Duration = 1
let t : goiface.Ticks = 1
let x : Go.Any = d
let y : Go.Any = t
x == y
```

compiled は `time.Duration` と `goiface.Ticks` の dynamic type が違うため
false。しかし eval は両方を payload `VInt 1` + static type tag 付きの
`VHost` として保持し、equality が payload だけを `reflect.DeepEqual` で
比較していたため true になっていた。

修正: `VHost` equality は payload 比較の前に、box に記録された static
type の emitted-Go-type 名を比較する（`hostBoxRepr`）。opaque host 値や
static type を引けない box は従来どおり payload comparison の best-effort
に残る。これで nominal type を落とさず、`Int`/`Int64` のように同じ
`int64` に emit される型どうしは従来どおり一致する。

compare ケース `go_any_boxed_named_type_identity` は次を固定する:

- `time.Duration` vs `goiface.Ticks` は false
- `time.Duration` 同士は true
- `Int` vs `Int64` の box は同じ `int64` なので true

### 5. 入れ子表示の残件

width-32 の値自体は canonical で、トップレベルの splice 表示も
`displayTyped` が float32 precision で描く。残っているのは foreign named
scalar を composite 内で描くときの括弧 canonical であって、width 伝播の
欠損ではない。compiled は opaque 値を括弧で包むが、eval は値レベルで
foreign 性を見られない。したがってこの検証は lane A（writeOperand の
型駆動再設計）にブロックされている。

## 台帳同期

- `docs/implementation-gaps.md` に boxed-any equality の nominal type 脱落を
  登録し、同項目を解消済みとして記録
- foreign named scalar の既存項目に、width 伝播側の残件がないことと
  lane A ブロックであることを追記
- `TODO.md` の wave46 lane D を完了に更新
