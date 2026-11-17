# `Go.FuncN` 値を Gom 側から適用する(逆変換 `Go.fnN`)

- 状態: 採用（実装・検証した — `Go.fn2`/`fn0`/`fn1` の変換+呼出、int/int64 境界、`func() error` と `func(A) (T, error)` の `Result` materialize が `gom check`/`build`→`go build`→実行まで通る。testdata/spec/ok_go_fn_adapter・E0510_go_fn_{arity,non_fn}、testdata/golden/go_fn_adapter で固定。spec.md §8.8 へ反映済み）
- 日付: 2026-11-18
- 問い: [open-questions](../open-questions.md)「`Go.FuncN` 値の Gom 側からの適用」

## 問題

[decisions/func-interop](2026-11-17-func-interop.md) で Gom fn → `Go.FuncN` の変換 `Go.funcN` は決着したが、逆方向 — Go から受け取った `Go.FuncN` 値を Gom で呼ぶ — は「将来の問い」として残した。callback を返す API(`http.Client.CheckRedirect` を受け取る側、func を返す factory)に必要になる。現状 pin: `f : Go.Func2 Int Int Int` に対して `f 1 2` と書くと、callee 位置がフレッシュな関数型を要求する通常の適用経路で `E0401: type mismatch: expected Go.Func2 Int Int Int, got ?1 -> ?2` となる(コードは出るが、なぜ適用できないかは読み取れない)。

## 選択肢

- **(a) 直接 apply**: `f a b` が `Go.FuncN` 値に効く。callee が `*types.Con`(Qual "Go", `FuncN`)のとき check が型引数から合成した `Func` で引数を検査し、call は flat な `f(a, b)` に落とす。**不採用**(podhmo 採択, 2026-11-18 — (b) の変換 API のみ)。コスト見積としては中程度: emit 側は本決定の `Go.fnN` と同じ変換機械(Gom view 引数 → raw view の conv、結果の shape 別 consumer)を各 callsite に inline する形で足せるが、check の適用経路(CallInfo に flat-apply 種別、produced-vs-want の位置ごとの比較、`f 1` 部分適用を許すかの意味論)にも手が入る。Con は `Func` でないので評価順・部分適用の既存経路をそのまま通せず、「`Go.FuncN` は値だが直接は呼べない」という今の規則を崩す読み替えが必要になる。
- **(b) typed adapter(採用)**: `Go.fnN` という明示変換だけが `Go.FuncN` 値を Gom fn へ変換する。`Go.funcN` の鏡で、変換は 1 箇所に明示される。
- **(c) 何もしない**: func を返す Go API はホスト側でラップする。func-interop (c) と同じ理由で Gom だけで stdlib を試す価値が失われる。**不採用**。

## `Go.fnN` の形

`Go.fnN f` — `f : Go.FuncN p'... r'...` の値を Gom の関数値に変換する明示変換。型引数の読みは `Go.funcN` の seed と同じ写像を使う(goFuncGomView / goFuncSeedRet):

- パラメータ `p'_i` は Gom ビューへ(`Go.Int` → `Int`、境界位置の `Go.Slice T` → `List T`、それ以外はそのまま)
- 結果なし → `Unit`、結果 `error` 1 個 → `Result Unit GoError`、結果 `(T, error)` → `Result T GoError`、単一結果 `T` → Gom ビューの `T`
- `Go.fn0` は `func() R` を `Unit -> R` のサンクに対応させる(`Go.func0` の逆)

`func() error` の戻りは **`Result Unit GoError`** に読む — 素の `GoError`(never-nil claim)は `Go.fnN` では表せない。honest な側に倒し、呼び出し側に必ず unwrap を強いる(`goFuncSeedRet` と同じ規則)。

arity の不一致(`Go.fn2` に `Go.Func1`)、`Go.FuncN` でない引数、ラベル付き引数は E0510。

## 生成コード

Gom 関数値の canonical なカリー化表現(n 段の `func(p) ...` の入れ子)を emit し、最内の本体が flat な `f(a0, a1)` を呼ぶ — 形としては Go 関数参照の adapter 経路(`goFuncRefOrAdapter`)と同じで、callee がシンボルでなく FuncN 型の値という点だけが違う。`core.CallGo` に `Fn` フィールドを足して値 callee を表す(Args は Gom view → raw の conv 済み、結果は `ResultFromGo` など既存の consumer が raw → Gom view を担う)。`Go.funcN` が `core.GoFunc` という専用ノードを要したのは Go 側の呼び出し規約が flat だからで、逆方向は canonical な Closure チェーンで足りる — 新しい値表現は要らない。

```go
// Go.fn2 (f : Go.Func2 Int Int Int)  ->
func(p0 int64) func(int64) int64 {
    return func(p1 int64) int64 {
        return f(p0, p1)
    }
}
```

`func(int) int` のような `int`/`int64` の境界では引数・結果の両側に `int64(...)` / `int(...)` の conv が入る。

## conformance point

変換は `Go.fnN` の適用点だけにある — `Go.FuncN` 値はそのままでは適用できず(現状の E0401 は維持)、ユーザーが必ず `Go.fnN` を書く。`Go.funcN` と `Go.fnN` を往復すると薄い二重 wrapper が生成されるが、意味は変わらない。

## 影響

- spec 8.8 に `Go.fnN` を追記(「Gom 側から適用はできない」の記述を変換経路ありに更新)
- `Go` 名前空間に `fn0`..`fn3` を追加、`core.CallGo` に `Fn` フィールド
- `docs/decisions/2026-11-17-func-interop.md` の「そのまま呼び出せないのか?」はこの決定で解消

## 追記(PR #35 review findings への対応, 2026-11-18)

Devin Review の指摘 3 件はいずれも実際の不整合で、以下を修正した:

1. `snapshotCapturedParams` / `tailrec` の走査が `CallGo.Fn` を読んでおらず、ループ内で seed 値が capture されないケースがあった — 両 walker に `Fn` の走査を追加。
2. seed した Gom ビューと raw ビューの間に変換可能性の検査がなく、`func([]int) int` のようなコンテナ内部で `int`/`int64` が割れる型が `gom check` を通って `gom build` で E0901 になった — `checkGoFnCall` に viewsCompatible(import の Hidden 判定)と同じ規則の `fnViewCompatible` を追加し E0510 で拒否。コンテナ内部の要素ごとの変換が将来書けるようになったら緩和できる。
3. `(error, error)` 等のメソッドを持つ interface ペイロードに Option wrap がなく `(nil, nil)` が `Ok nil` になった — import mapping と同じ規則で `Result (Option T) GoError` を seed する(`GoError` は既知、`types.Go` の ref は新設の `check.NilableSource` optional protocol を `internal/goimport` が `l.named` + `methodIface` で実装)。

4. (2nd review round) 上記 3 の `Option` wrap は `Go.funcN` の inline lambda seed にも共有され、`Ok payload` なコールバックが unification に落ちる回帰と、`Result (Option T) GoError` 宣言の fn が `Go.funcN` で往復できない既存 gap が露呈した — `Go.funcN` 側の seed は verbatim（`Result GoError GoError`、payload が nil になり得ない honest な読み）に戻し、`goFuncRawResults` が `Option` payload を受けるようにし、Gom→Go 方向の `ConvOptToNil`（`option.NilOr(v)` — Some→payload, None→nil）を新設して `(I, error)` 宣言の往復を完成させた。
