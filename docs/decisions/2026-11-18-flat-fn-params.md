# >=2 本矢印の宣言パラメータは flat 表現（呼出側 uncurry）

- 状態: 採用（wave20 lane 2 で実装・検証した — ベンチと生成コード差分は [sketch/2026-11-18-flat-fn-params-verify.md](../sketch/2026-11-18-flat-fn-params-verify.md)、全ゲート緑）
- 日付: 2026-11-18
- Supersedes: [function-value-representation](2026-11-02-function-value-representation.md) のうち「関数値は常に canonical の 1 引数チェーン」——値の表現は canonical のままだが、宣言パラメータ位置の >=2 本矢印スロットだけ flat 表現を載せる例外を加える

## 問題

カリー化 HOF（`Fold` 等）の呼び出しは要素ごとに `f(acc)(x)` のクロージャ適用を繰り返し、毎回中間クロージャを確保する。計測（n=100000）で `Fold` は 1368870 ns/op・1600004 B/op・100000 allocs と、同等の手続きループ（117420 ns・0 allocs）に対して約 12 倍の実害が出ていた。

## 採択

**宣言 FuncDecl のパラメータで、型が >=2 本の矢印チェーンを持つもの**は、Go 側で `func(T1', ..., Tk') R'`（全チェーンを 1 関数に平坦化した形）をパラメータ型として emit する。それ以外の位置（関数値のリテラル、戻り値、Let 束縛の値としての関数）の表現は従来どおり canonical の 1 引数チェーン。

呼び出し規約は Core IR で確定する（codegen は `CallFunc.FlatArity` と `Uncurry` をそのまま emit するだけ）:

- フラット引数への**飽和呼出** `f a b`: 1 回の `CallFunc{FlatArity: k}` — `f(a, b)`
- フラット引数への**部分適用** `f a`: `func(q1, ..., q_{k-1}) { f(a, q1, ...) }` の eta — 戻り値は canonical 値ではなく残り arity の flat 値
- canonical 値を flat スロットへ渡す位置（引数・コンストラクタ引数等）: `Uncurry{X, k}` アダプタ — `func(p0, ..., p_{k-1}) { x(p0)(p1)... }` で、X が FuncRef/Closure のときは各水準の let を平坦リテラルへ畳み込む
- flat 変数を**値位置**で使う: `curryEta` で canonical チェーンへ包み直す。ただし Uncurry で束縛された一時変数は canonical ソースを覚えていて（`flatSrc`）、値位置ではソースをそのまま使い、部分適用もソースの canonical チェーンを段階適用する — `make_` のような宣言由来のソースでは各チャンクの let が canonical 順序で走る

`Go.funcN`/`Go.fnN`（canonical→flat の境界 builtin）自体はこの変換の対象外。

## セマンティクス上の差（残存）

宣言された flat パラメータ `f` への部分適用 `f a` は、flat 呼出全体を残余引数の到着まで遅延する。canonical 表現では `f a` の時点で被呼側の最初のチャンク（let 群）が走っていたため、`todo` 等の作用の発火タイミングが「residual を適用したとき」に移る — residual が一度も適用されなければ作用は走らない。eval は canonical 適用のままなので、この限定的なケース（flat 型パラメータの部分適用で residual 未適用）では eval と生成コードの発火タイミングがずれる。`TestEagerFnChunkTiming` を residual 適用形に更新してこの差を pin した。Uncurry で束縛された一時変数経由の部分適用は `flatSrc` 経由で canonical 順序を維持する。

## 実装位置

- `internal/lower/flat.go`: パラメータの flat arity 判定、`curryEta`、呼出引数の flat 化
- `internal/lower/expr.go`: `l.flat`（sym → arity）・`l.flatSrc`（sym → canonical ソース）の管理、`CallFunc{FlatArity}` の emit、`Uncurry` の導入
- `internal/core`: `CallFunc.FlatArity`、`Uncurry` ノード、`validate.go` の不変条件（Uncurry 直下の FuncRef/CtorRef は任意 arity を許す）
- `internal/codegen`: `flatFuncType`、`paramsFlat`、`uncurryLit`
- `rt/slice.go`: `SliceFold` は `func(B,T) B` の flat 形（member table の gom 側シグネチャは変えない — flat arity は矢印チェーンから導出する）
