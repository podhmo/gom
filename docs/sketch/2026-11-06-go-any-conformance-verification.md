# 検証: `Go.Any` 適合の materialize — 残ギャップ 1–4 の解消

- 日付: 2026-11-06
- 決定: [decisions/2026-11-06-go-any-conformance-materialize](../decisions/2026-11-06-go-any-conformance-materialize.md)、[decisions/2026-11-06-go-any-spellable](../decisions/2026-11-06-go-any-spellable.md)
- 前提: [sketch: go-any-spellable-prototype](2026-11-06-go-any-spellable-prototype.md)（残ギャップ 1–4 の計測）

## 目的

admission（`builtinGoMembers` の `Any` + `goTypeArity` の
`"Any": 0`）に加え、spellable 決定が impl lane に委ねた 4 件を
一経路で解く: 宣言結果の E0901、exported let の型忠実度、
`Option Go.Any` の `None` E0407、コンテナ payload の型/表現
乖離。

## 実装スケッチ（production に land 済み）

- `internal/resolve/scope.go` — `builtinGoMembers` に
  `Any`（KindType、Package "Go"）。
- `internal/check/typenv.go` — `goTypeArity["Any"] = 0`。
- `internal/check/unify.go` — `unify` を値スロット判定と構造
  判定 `unifySub` に分ける。値スロットでは `Go.Any` want が
  meta-bind せず受理（従来どおり）。`unifySub` は Con/Tuple/
  `types.Go` の型引数を再帰し、`Go.Any` 位置では got 側未解決
  meta を `Go.Any` に bind、concrete は一致要求。`unifyFunc`
  はパラメータを値スロット判定（`unify`）、結果を構造判定
  （`unifySub`）で pair する。
- `internal/check/expr.go` — `conformExpr` が unify 成功後に
  `isGoAny(want) && !isGoAny(out)` なら
  `Ascribe{Conv: true}` を返す（宣言結果・let 注釈・引数・
  レコードフィールド・sum ペイロードを一括でカバー）。
  `checkApply`/`checkTupleLit` は want を受け取るシグネチャに
  なり、`checkListLit` は `List T` want の要素を先行 bind する。
- `internal/check/apply.go` — `checkApply` は want が callee
  ret と同じ ctor の Con のとき、want 側 `Go.Any` 引数に対応する
  ret の未解決 meta を先行 bind（`Ok v` under
  `Result Go.Any GoError` → `Ok` の `a` が `Go.Any`）。
- `internal/check/pat.go` — ctor-pattern 用にあった手動 pin は
  `unifySub` が包含するので撤去。
- `internal/lower/expr.go` — `ascribe` の `Conv` emit に
  `convBoxSrc` を通す（`ConvBoxAny` のとき数値を `ConvNum` で
  先に物質化 — `any(int64(42))`。明示 `Go.conv` 経路の
  `ConvNum`/`ConvNamed`/`ConvIface`/`Slice` 系は no-op）。
- `internal/eval/eval.go` — `Ascribe{Conv}` が `Go.Any` ボックス
  のとき evaluator は `x.X` をそのまま評価（eval 値は動的型付き
  で box は表現差なので no-op）。`Iface`/`Slice`/他の `Conv` は
  従来どおり E9001。

## 検証結果（全て `gom check`/`gom build` + 生成 Go 目視）

| ケース | 期待 | 結果 |
| --- | --- | --- |
| `let x : Go.Any = 42` | `var x any = func() any { tmp := int64(42); return any(tmp) }()` | ✓（golden `go_any`） |
| `let n : Option Go.Any = None` | `option.Option_None[any]{}` — E0407 解消 | ✓ |
| `let o : Option Go.Any = Some 42` | `option.Option_Some[any]{V0: any(int64(42))}` | ✓（残ギャップ 4 解消） |
| `let r : Result Go.Any GoError = Ok 3` | `result.Ok[any, error](any(int64(3)))` | ✓ |
| `let l : List Go.Any = [1, 2]`、`takes [5]` | `list.List[any]{any(int64(1)), …}` | ✓ |
| `fn f () -> Go.Any = 42` | `func f(...) any { return any(int64(42)) }` — E0901 解消 | ✓ |
| `fn -> Result Go.Any GoError` 本体 `try dec.Token()` | `Ok t` の payload `any` — `result.Ok[any, error](any(t_))`、Err 側は `result.Err[any, error](err)` | ✓（残ギャップ 1 解消 — named `any` の `json.Token` が verbatim Result に乗る） |
| `fn id (x: Go.Any) -> Go.Any = x` | 二重ボックスなし `return x` | ✓ |
| `.gomi` `let x : Go.Any` → 生成 | `var X any`（初期化子の具体型ではなく宣言型） | ✓（残ギャップ 2 解消 — `d.Body.Tail.Type()` が `Go.Any` を拾う） |
| `extern` に `Go.Any` パラメータ | `fn ValueOf (i: Go.Any) -> reflect.Value` conform | ✓ |
| 裸の `any` | 変わらず型変数 — `fn id (x: any) -> any` は `func id[Any any](x Any) Any` | ✓（fixture `ok_any_lowercase_typevar`） |

## 拒否（仕様どおりの維持/新規の固定）

| ケース | 診断 |
| --- | --- |
| `let bad : Option Go.Any = oi`（`oi : Option Int`）、`takes oi` | E0401 `expected Go.Any, got Int` — 型不変の拒否、要素差分を名指し |
| `let x : Go.Any = None`、`let x : Go.Any = []` | E0407 — スカラ受理は ctor インスタンス化を選ばない制限（合成の答えは `Option Go.Any` 注釈） |
| `Go.Any Int` | E0401 arity（`Any` takes 0 type argument(s)） |
| `let x = Go.Any` | E0201 `Any is a type, not a value` |
| `Go.conv v : Int`（`v : Go.Any`） | E0507 — `any`→`T` はアサーションであり変換ではない |
| extern `Option Go.Any` パラメータ | E0509 — param 側 `Option` は `*T` nil チャネルに限る |

## 回帰観測（`go test ./...` で検出→修正）

1. **pipe/hole の deferred replay** —
   `"b" |> fmt.Println("a", _)` が E0401。`unifyFunc` の
   パラメータ pair を `unify`（値スロット）に戻して解決 —
   `any -> R` が `String -> ?r` に適用されるのは param
   conform の規則。
2. **eval の E9001** — `fmt.Println("init:a")` など `...any`
   引数の `Ascribe{Conv}` が evaluator に到達。`Go.Any`
   ボックスの Ascribe を no-op として exempt。
3. **golden `float_int_literal`** — `any` ボックスの一時変数が
   引数の原子化に乗り tmp 順が変わるだけ（評価順・形は同一）。
   `-update` で regen。

## audit — 他の dangling-meta 経路

`unifySub` は Con/Tuple/`types.Go` の全引数位置を再帰するため、
`List Go.Any`/`Go.Slice Go.Any`/`Go.Map k Go.Any`/`Ptr Go.Any`/
sum ペイロード/record フィールドの同形ケースは一本の規則で
裁く。値スロット側（`let x : Go.Any = None`/`[]`/`Ok 1`）は
制限として残し、合成の注釈または named let で書く形を spec
§8.3 に明記した。

## fixtures

- spec: `ok_go_any_positions`、`ok_go_any_gomi/`（`.gomi`
  conformance）、`ok_extern_go_any_views`（`Option Go.Any` over
  `(any, bool)`、`Result (Option Go.Any) GoError` over `(any,
  error)` — `testdata/goiface.AnyLookup`/`AnyPair`。emit は
  `option.FromGo(v, ok)` /
  `result.FromGo(option.FromGo(v, v != nil), err)`）、
  `ok_any_lowercase_typevar`、
  `E0401_go_any_invariant`、`E0407_go_any_scalar_ctor`、
  `E0401_go_any_arity`、`E0201_go_any_value_position`、
  `E0507_go_any_conv`、`E0509_extern_option_go_any_param`。
- golden: `go_any`（新規 — 上表の emit 形状を固定）、
  `float_int_literal`（tmp 順 regen）。

## 再レビュー（関数型スロット）— 2026-11-06

admission/conformance の初回検証後の再レビューで、関数型を
通る `Go.Any` の型等価判定に穴が残っていることが判明した。
全ケースを検証・対応した。

### F1: `unifyFunc` のパラメータ対が値スロット判定を使っていた（実害 — 修正済み）

`unifyFunc` はパラメータ対を `unify`（値スロット）で比較して
いたため、`let f : Go.Any -> Int = g`（`g : String -> Int`）が
受理された。実際の emit は宣言型ではなく初期化子の型に倒れて
`func f(x string) int64` になり、本体では `g` が
`func(any) int64` 呼び出し規約で参照される不整合コードが
生成された。対応: パラメータ対を `unifySub` に揃え関数型を
全位置不変に — 両方向とも E0401
（fixture: `E0401_go_any_fn_param`、`E0401_go_any_fn_contra` —
注釈スロットとパラメータスロットを固定）。

`any -> R` の callee へ具象値を渡す適用サイトは従来通り —
型等価ではなく引数 conform が値スロットで働く区別を spec
§8.3 に明記した。

### F2: `unifySub` の `Go.Any` 検知が浅かった（実害 — 修正済み）

「`Go.Any` を含むか」の判定が `*types.Con` の直下の型引数だけを
見ていたため `Option (Go.Any -> Int)` のような入れ子は検知
自体が漏れ、`Option Go.Any = Some None` のように値スロット受理
（`isGoAny`）で通った `Some` の要素が dangling meta で
`?a` のまま残るケースがあった。対応: `containsGoAny` として
型を深く歩く判定に差し替え（Con 型引数・Tuple・Func・Go
型引数を再帰）。`Option (Go.Any -> Int) = Some (fn x -> 1)`
は `option.Option_Some[func(any) int64]{V0: func(x any) int64{…}}`
を emit する（実測）。

### F3: 期待位置の materialize が関数型を見なかった（実害 — 修正済み）

`let g : Go.Any -> Int = fn x -> 1` — レビュー指摘は
「`x` が具象型に推論される」。実測では `x` は未解決のまま
ジェネリックに昇格して `func f[A any](x A) int64` が emit
されていた — 型は合う有効なコードだが宣言の `any` 忠実度を
失い、`go build` は通るため静かなドリフトだった。対応:
`conformExpr` の want が `Go.Any` を含む関数型のとき、ラムダの
binder に want 側パラメータ型を流して `x := any` に bind —
`func(x any) int64` が emit される（`fn x -> 1` を `Go.Any ->
Int` の関数値スロットへ渡すパスも同じ — `h (fn s -> 1)` で
`func(x2 any) int64` を確認）。

### F4: list/tuple リテラルの要素 want 伝搬（前回対応済み）

`checkListLit`/`checkTupleLit` への `bw` 伝搬は前回の変更で
入っていた。`List Go.Any = [1, 2]` → `list.List[any]` で固定。

### その他の観察（挙動の確認 — 仕様どおり・修正なし）

- match 観察「`let o : Option Go.Any = Some 1; match o { Some a
  -> a }` は `?a` が残る」→ 実測では `a` が `Go.Any` に bind
  されず E0407（`a` の型が決まらない）。暗黙の型スイッチを
  emit するのではなく、`let x : Go.Any = None` と同じクラスの
  正直な拒否 — 合成の注釈が答えという規則と一致するため据置。
- extern `(Go.Any -> Int) -> Int` シグネチャ — 実測では
  extern の関数型パラメータ自体が E0509 で未対応
  （`ok_extern_go_any_views` の結果ビューとは別面）。`any` の
  適合を問う以前に上流で拒否されるため穴ではない
  （fixture: `E0509_extern_go_any_fn_param`、`goiface.TakesAnyFn`）。
- `let f : String -> Int = (g : Go.Any -> Int)` の逆方向 —
  既存の `unifySub` が双方向で不変性を強制していた
  （E0401 `expected String, got Go.Any` — `E0401_go_any_fn_contra`
  で fixture 化）。

### 検証結果（再レビュー後）

- `ok_go_any_fn.gom`（fixture 化）— 直接適用・pipe・
  over-application・`>>`/`<<` composition・`|?>`・期待位置
  ラムダ・`Option (Go.Any -> Int)`/`Result (Option Go.Any)
  GoError`/`Result (List Go.Any) GoError` の入れ子、全て
  `ok:true` で emit は `func(x any) int64`・`ga(any(int64(42)))`・
  `func(tmp17 any) int64`（compose）・`Option[any]`/`any` 要素。
  生成 Go は `go build` 通過。
- `go test ./...` 全パス、`gofmt -l .` 空、`go vet ./...` 通過、
  `check-sumtype.sh` exit 0（canary 検知確認）。
- `containsGoAny` の型スイッチは sealed `types.Type` の
  exhaustive 規律に従い `Meta`/`Var` を明示ケースに
  （`panic("unreachable")` の default 付き）。
