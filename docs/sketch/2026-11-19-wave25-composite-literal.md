# wave25 lane B: composite literal (a) 再判定 — 初期化ブロック + `tap` で代替できるか

- 日付: 2026-11-19
- 状態: 調査完了・採否は podhmo に委ねる
- 関連: [struct 構築の選択肢](2026-11-19-wave23-struct-construction.md)、
  [decisions/struct-construction-options](../decisions/2026-11-19-struct-construction-options.md)、
  [implementation-gaps](../implementation-gaps.md) の「`let` アノテーション付き
  `fn` 本体の内部で期待型伝播が弱い」項

## 問い

wave23 で採用された (b) field write（`Ptr.new` + `v.Field = x`）に対し、
保留された (a) composite literal `http.Client { Timeout = ... }` は不要か。
判断材料として「初期化ブロック + `tap`」で (a) の主要用途（式位置での
初期化済み値生成）を代替できるかを実測する。

## 実測（`/tmp/w25b/` — `gom check`）

### 通る形（今日の慣用形 — gist-cli `bce91d6`）

```gom
let client : Ptr http.Client = {
  let c : Ptr http.Client = Ptr.new ()
  c.Timeout = 30 * time.Second
  c.CheckRedirect = Go.func2 (fn _req _via -> Err http.ErrUseLastResponse)
  c
}
```

E0 エラー・警告ゼロで型検査を通る（2026-11-19 再確認）。「束ね側の
`let` に型を書き、ブロック内で `Ptr.new` → field write → セルを返す」
というこの形が (a) の `&T{...}` とほぼ同じ意味を持つ — 生成される Go も
`new(http.Client)` + 代入の列で、読み味は同等。

### 壁 1: `fn` パラメータにアノテーションを書けない（E0101）

```gom
let g = fn (x: Int) -> x    -- E0101 ×3（構文エラー）
```

`fn` の param は裸の名前・`_`・`()` のみ。`tap` 経路で「初期化関数」を
外だししたい場合に最初にぶつかる構文側の壁。

### 壁 2: 注釈付き `fn` 本体の入れ子 lambda で期待型が届かない（E0407/E0510）

```gom
let init : Ptr http.Client -> Unit = fn c -> {
  c.CheckRedirect = Go.func2 (fn _req _via -> Err http.ErrUseLastResponse)
}
```

ブロック直下では通る同一式が、`fn` 本体に入ると `_req`/`_via`/`Err`
が未確定型のまま残り E0407、`Go.func2` が E0510。原因の局面:
`checkGoFuncCall` は `want` が `Go.FuncN` のとき lambda に seed を渡す
（internal/check/gofunc.go:76–）が、field-write の右辺から lambda 本体
内部へ `want` が届いていない — `let` アノテーションの期待型伝播が
`fn` 本体内部で切れる（implementation-gaps の当該項の再現確認）。

### 壁 3（確認済み・副次的）

`30 * 1000000000` は `Int` 型になり `time.Duration` の field には
E0401 — named int 型の算術はリテラルでは推論できず `time.Second` 等の
定数式が要る。これは (a) 採用時も同じ制約。

## 判定材料

### `tap` 経路の修復コスト（推論強化）

- 壁 2 の修正は `internal/check` 局所: field-write の value 検査と
  `fn` 本体検査の間で `want` をつなげればよい（`checkGoFuncCall` 側の
  seed 機構は既存）。`fn` param 注釈（壁 1）を足せば `tap init` の
  宣言型も書けるが、壁 2 さえ直れば `init` を局所 `let` に残す形で
  `Ptr.new () |> tap init` は動く — **壁 1 は必須ではない**
- ただし `tap` は「生成してから渡す」イディオムであり、式指向の利得は
  ブロック形式の糖衣レベル — 得られるのは書き味の改善のみ

### (a) を要する残りの用途

ブロック形式で**代替できない**ケース:

1. **値として直接置く場面**: `[]T{...}` の要素、`Foo (http.Client {...})`
   のような inline 引数、map 値 — ブロック形式は「束ねてから使う」形に
   強制するので、式位置の一発値は `let` 中間変数が必須になる
2. **`T` 値（非 `Ptr`）の構築**: (b) は `Ptr T` 経由のみ。`T` を値で
   返す API には `Ptr.make` が要るが copylocks 問題（wave23 調査）で
   sync 含有型には使えない — (a) でも素の `T` 生成を許すと同じ罠が
   開くので、(a) を採るなら `&T{...}`（`Ptr T`）専用形に制限するのが
   wave23 の結論 → その場合 **(a) の対応域も `Ptr` に限られ、この差分
   は縮む**
3. **部分初期化の表現力**: Go の composite literal は unexported を除く
   任意部分集合 — ブロック形式でも同じ部分初期化が書けるので機能差は
   なし。書き味の差のみ

### (a) を採る場合のコスト（wave23 から据え置き）

- spec 新面: literal 構文 + embedded/unexported/positional 規則 +
  レコードとの見分け規則（型名修飾必須）
- 実装: syntax → resolve → check → codegen の全層 + golden/spec 試験
- 対称: レコード literal と同形異規則（全列挙 vs 部分初期化）の混乱リスク

## 結論（材料の整理）

- 「初期化ブロック」は今日既に (a) の主用途をカバーし、追加実装ゼロで
  動く。`tap` はブロック形式の糖衣であって新しい表現力ではない
- 壁 2 の修正は局所的で、直せば `tap` イディオムも使えるようになる —
  ただしこれは (a) 採否と直交する推論強化の改善（別 lane 化も可能）
- (a) が残るのは「`Ptr` 以外の値・式位置の一発 literal」用途で、
  wave23 の安全側結論（`&T{...}` 専用）を取るなら対応域はブロック形式と
  ほぼ重なる — **(a) を採る唯一の強い理由は書き味（record literal との
  一貫性・Go 慣習そのままの読み物）**
