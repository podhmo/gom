# Prototype 検証: `Go.Any` の綴りを認める最小パッチ

- 日付: 2026-11-06
- 決定: [decisions/2026-11-06-go-any-spellable](../decisions/2026-11-06-go-any-spellable.md)
- 関連: [sketch: P3-7](2026-11-05-p3-7-realworld-interop-audit.md)、[nilable-iface-pair-ok-payload](../decisions/2026-11-06-nilable-iface-pair-ok-payload.md)

## 目的

`Go.Any` を `.gom`/`.gomi`/extern の型式で書けるようにする最小
変更を試し、下流（boxing・Option 宣言ビュー・`Go.conv`・
`.gomi` 生成面）がどこまで届くかを測る。裸の `any` が型変数の
ままか、named `any`（`json.Token`）との区別が保てるかも確認。

## prototype（production パスには未反映 — 検証後に revert 済み）

最小パッチは 2 行。検証後に `git checkout` で revert した
（本レーンは production 変更を land しない）:

```diff
--- a/internal/resolve/scope.go
@@ builtinGoMembers @@
 		"conv":  {Name: "conv", Kind: KindBuiltin, Package: "Go"},
 		"Slice": {Name: "Slice", Kind: KindType, Package: "Go"},
 		"Map":   {Name: "Map", Kind: KindType, Package: "Go"},
+		// PROTOTYPE(wave-12 lane 4): `Any` admits the `Go.Any`
+		// spelling — decision 2026-11-06-go-any-spellable.
+		"Any": {Name: "Any", Kind: KindType, Package: "Go"},
--- a/internal/check/typenv.go
@@ goTypeArity @@
 var goTypeArity = map[string]int{
 	"Slice": 1,
 	"Map":   2,
+	// PROTOTYPE(wave-12 lane 4)
+	"Any": 0,
 }
```

コンパイラは `go build -o /tmp/gom ./cmd/gom` でこのパッチ適用済み
の worktree からビルド。`go test ./...`（パッチ適用状態）全緑 —
既存フィクスチャに `Go.Any` 不可を前提としたものは無い。

## prototype の場所とコマンド

scratch（未コミット）: `/tmp/gom-w12/{anyiso,anygomi}`、Go 側
probe は `/tmp/gom-w12/probeapi`（`LoneAny(bool) any`、
`AnyOr(int64) (any, error)`）。`encoding/json` の
`Decoder.Token`（named `any` を返す実 API）も使った。

```
cd /tmp/gom-w12/anyiso && /tmp/gom check -format text <file>.gom
cd /tmp/gom-w12/anyiso && /tmp/gom build <file>.gom && go build -o x x.go && ./x
cd /tmp/gom-w12/anygomi && /tmp/gom build <pkg>.gomi
```

## 受理されるようになったもの（全て生成コード確認・実行済み）

| 位置 | 例 | 結果 |
| --- | --- | --- |
| パラメータ・結果（同一型） | `fn id (x: Go.Any) -> Go.Any = x` | ✓ 生成 `func id(x any) any` |
| 型注釈ボックス | `let x : Go.Any = 42` | ✓ — emit は `x := int64(42)`（ローカルの Go 型は値側の具体型を取り、注釈はスロットを作らない。`any(...)` を emit するのは `Go.conv` の明示経路だけ — ギャップ 4 参照） |
| extern lone `any` | `fn LoneAny (b: Bool) -> Option Go.Any` | ✓ `option.FromGo(v, v != nil)` |
| extern `(any,error)` | `fn AnyOr (n: Int) -> Result (Option Go.Any) GoError` | ✓ Ok payload を `FromGo(v, v != nil)` で物質化 |
| extern verbatim | `fn AnyOr (n: Int) -> Result Go.Any GoError` | ✓ `(nil,nil)` → `Ok <nil>` |
| コンテナ要素 | `List Go.Any`/`Go.Slice Go.Any`/`Go.Map String Go.Any`/`Ptr Go.Any` | ✓ `[]any`/`map[string]any`/`*any` |
| sum ペイロード・record フィールド | `type B = \| Mk Go.Any`、`{v: Go.Any}` | ✓ 構築位置でボックス |
| `.gomi` メンバー関数 | `fn Passthrough (x: Go.Any) -> Go.Any` | ✓ 生成 `func Passthrough(x any) any` |
| `==` | `r.v == r.v`（`r.v : Go.Any`） | ✓ `rt.Equal` |
| `Go.conv` → `Go.Any` | `Go.conv 42 : Go.Any` | ✓ `any(42)`、実行 `42` |
| パターン位置 | `match LoneAny x { Some v -> …; None -> … }`（`Option Go.Any`） | ✓ |
| Go `any` 引数へのボックス | `fmt.Println` / probe の `any` 引数 | ✓ 既存挙動そのまま |

## 拒否が保たれたもの / 変わらないもの

| ケース | 結果 |
| --- | --- |
| `Go.conv x : Int`（`x : Go.Any`） | E0507 — `any`→`T` は Go でも変換でなくアサーション。境界の一貫性として正しい拒否 |
| `Option Go.Any` の **パラメータ** 宣言 | E0509 — param 側の nil チャネルは `*T` 同様認めない |
| `Go.Any` を式位置に書く | E0201「is a type, not a value」 |
| `Go.Any Int`（arity 違反） | arity エラー |
| 裸の `any` | 変わらず型変数 — `fn f (x: any) -> any` は多相関数のまま |
| `type X = Go.Any` | parse エラー — そもそも type 別名の構文が無い（既存の制約、`Go.Any` 特有ではない） |

named `any` の区別: `json.Decoder.Token` は `json.Token` 型に
派生するまま（`Result json.Token GoError`）— `Go.Any` とは nominal
に別型。nominal なのは `type Token any` の**定義型**の側だけ —
`type T = any` のエイリアスは `any` に展開されて `Go.Any` に写る。
`let v : Go.Any = <json.Token 値>` の want 位置ボックスは入るが、
extern で named `any` を `Go.Any` と書き換える宣言は拒否される —
probeapi の `type NamedAny any` + `TokenOr(n) (NamedAny, error)`
で検証: verbatim `Result probeapi.NamedAny GoError` は受理・実行
（`3` を出力）できたが、`Result Go.Any GoError` 宣言は
`E0509 … declare Result T GoError or Result (Option T) GoError,
not Result Go.Any GoError`。宣言ビューの正直性が保たれた。

## 発見された残ギャップ（採用外スコープ — impl lane へ）

1. **宣言結果位置の materialize 未実装 — E0901**。
   `fn token_of () -> Result Go.Any GoError = try 経路の
   json.Token ボディ` や `fn box (x: a) -> Go.Any = x` は check
   を通るが lower が invariant violation:
   `E0901 body type Result encoding/json.Token GoError differs
   from residual type Result Go.Any GoError` / `body type a
   differs from residual type Go.Any`。仕様の答えは「want が
   `Go.Any` ならボックス」（他の want 位置と同じ）— impl lane
   で residual 位置の `any()` materialize、または check 側を
   厳しくするかを決める。workaround は `let v : Go.Any = x; v`。
2. **exported `let` の declared-type 忠実度** — `.gomi` の
   `let x : Go.Any` は受理されるが生成 Go が
   `var X string = "v1"`（具体型）を出す。Go 側 API 面の型が
   `.gomi` の広告とずれる。codegen の var 型に declared 型を
   届ける必要。
3. **`Option Go.Any` の `None` が式位置で書けない** —
   `let o : Option Go.Any = None` → E0407（meta を `Go.Any`
   want に bind しない any-hole 規則の副作用）。`Option Go.Any`
   の値は Go 呼び出し結果・パターン経由では得られる。
4. **ジェネリックコンテナ payload の Gom 型 / Go 表現の乖離 —
   check と `gom build` を通って `go build` で落ちる**（fix-round
   レビュー指摘の追試で確認）。probe:

   ```gom
   fn takes (o: Option Go.Any) -> Unit = ignore (fmt.Println(o))
   fn probe () -> Unit = {
     let o : Option Go.Any = Some 42
     takes o
   }
   ```

   `gom check`・`gom build` とも成功し、emit は
   `o := option.Option[int64](option.Option_Some[int64]{V0: 42})`
   と `func takes(o option.Option[any])` — `Some` の payload meta
   が引数から `Int` に bind され（want 側の `Go.Any` 受理は meta
   を bind しない）、ローカル変数は値側の具体型を取る。Gom 型は
   `Option Go.Any` なので param への引き渡しに変換ノードは出ず、
   `takes(o)` は `go build` で
   `cannot use o (variable of interface type option.Option[int64])
   as option.Option[any] value in argument to takes:
   option.Option[int64] does not implement option.Option[any]
   (wrong type for method isOption)` — Go ジェネリクスの型不変性
   に刺さる。`let l : List Go.Any = [1]` も同型
   （`l := list.List[int64]{1}`）。`Some`/`Ok` を fn の宣言結果に
   置く形（`fn -> Option Go.Any = Some 42`、`fn -> Result Go.Any
   GoError = Ok 42`）はギャップ 1 の E0901 で先に止まる —
   サイレントに通るのは let 注釈経由だけ。スカラの
   `let x : Go.Any = 42`（`x := int64(42)`）は使われる位置で
   再ボックスされるので同じ乖離でも害はない — 壊れるのは型引数
   を持つコンテナ表現だけ。`Some`/`Ok`/list リテラルを `Go.Any`
   要素スロットへ物質化する経路（または check で payload の
   concrete-bind を拒否する）が impl lane の判断材料。

## 結論

2 行の admission で `Go.Any` はほぼ全域で書けるようになり、
wave-10 が用意した `Option`/`Result (Option …)` 宣言ビューが実際に
綴れる（named-interface workaround 不要化）。残る 4 件は
spelling の決定とは別の materialize/忠実度のギャップ — 採用の
上で implementation-gaps.md と TODO.md へ送る。
