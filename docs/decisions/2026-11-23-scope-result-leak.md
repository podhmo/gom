# 結果経由の Scope リーク（non-escaping 引数の生産物がクロージャを運ぶ）

- 状態: 採択（案 A — 案 B への拡張余地あり、wave34 lane A で実装）
- 日付: 2026-11-23
- 問い: `f` が non-escaping でも `f` が返す値が `Scope` を捕まえた
  クロージャを運ぶ流路を、使い方の規則（値レベル脱出解析）で
  検出できるか — wave33 lane C の調査（implementation-gaps の
  未解消項目）

## 観測した穴（実証済み）

```gom
fn run_cb (f: Int -> (Unit -> Unit)) -> Unit -> Unit = f 3

fn leak (held: Ref Int) -> Unit -> Unit =
    Scope.run <| fn s ->
        run_cb (fn x -> fn () -> Scope.defer s (fn () -> Ref.set held 0))
```

`check` は無診断で通るが、`leak` が返すクロージャは `s` を
捕まえており、scope 終了後に呼ぶと `Scope.defer` が閉じた
scope に登録して panic する。型位置の規則は拾えない — 結果型
`Unit -> Unit` に `Scope` は表れず、capture は型に見えない。

## 機構

- `argEscapes(run_cb, 0)` は `Escapes[f]=false`（`f` は適用されて
  消費され、保持されない）→ 引数ラムダは non-danger で walk
- non-danger では内側ラムダの capture 判定（`capturedVars` →
  `markDanger` + E0419）が走らない → `s` は未マーク
- `retainedVars(run_cb の呼出し)` は full application なので空 →
  `let mk = run_cb ...` の `bindOf` にも載らず、本体結果経由でも
  拾えない

「non-escaping」の現在の意味は「callee が引数そのものを保持
しない」であって「引数の生産物が callee の結果に載らない」
ではない。flag モデルに結果経由の流路がそもそも無い。

## 検討した近似

### 案 A: result-danger（採択候補）

danger 位置の呼出しで結果型が関数を含むとき、**引数ラムダの
本体の結果位置だけ** danger で walk する（引数ラムダ自身では
なくその生産物が怪しい、という粒度）。内側ラムダの capture
判定が効いて上記の例は E0419 になる。

被呼出しがローカル宣言なら `retainedVars` に param sym が
含まれるかで絞れる（`f 3` を結果に持つ `run_cb` は true、
`f` を捨てる `const_f` は false → 正確）。imported /
CallUnknown な callee には情報がないので保守的に true。

偽陽性の面積: opaque な呼出しが関数を返し、引数ラムダの
生産物が scope capture を含むが callee は結果に載せない
ケースのみ。全体引数を danger にする荒い版（引数ラムダの
capture 全部を danger）と違い、`fn x -> Scope.defer s ...`
のような結果非 capture の引数は誤爆しない（検証済み）。

### 案 B: result-taint flag の拡張

「結果がその引数の生産物を運ぶ」をパラメータごとの第2の
flag として fixpoint 推論し、`.gomi` にもエクスポートする。
imported callee でも正確になるが、Symbol の flag ベクトル・
gomi ディレクティブ文法・apply.go の推論を全部拡張する — 
規模は大。案 A を先に入れて必要なら B に拡張するのが自然。

### 案 C: 現状維持

文書化して残す。実害は「ライブラリ関数が引数の適用結果を返す」
パターンでのみ起きるが、`Scope` 系の高階関数はちょうどこの
形を取りやすいので見逃すのは苦い。

## 実装（wave34 lane A）

案 A を実装済み:

- 脱出位置の呼出しで `containsFunc`（結果型に関数型を含む）が
  真なら、引数ラムダに `b.forward` を立てて本体の結果位置を
  danger で walk する（`escWalk` の danger は構造上、結果位置に
  だけ伝わる — Block の末尾・If/Match のアーム・Try のみ）
- ローカル宣言の callee は `retainedProducers`（`retainedVars` +
  結果位置の saturated call の callee 自身を producer と数える）
  でパラメータの生産物が結果に載るかを絞る — `run_cb` は true、
  `const_f` は false。opaque（imported / Go / 非シンボル）は
  保守的に true
- pipe 形式（`x |> f` / `f <| x`）にも同じ規則をかけ、
  `Scope.run <| fn s -> ...` の本体引数が返すクロージャも
  E0419 になる
- 検証: `testdata/spec/E0419_scope_result_leak/` — pipe/apply
  両形式のリークで E0419、`Unit` 結果の引数・生産物を捨てる
  callee では無診断

## 制限・補足

- 案 A でも imported の opaque callee には偽陽性が残る — 
  必要になったら案 B の flag エクスポートで閉じる
- `Scope.run` 自身の本体引数は `b.forward` で既に danger
  相当なので、run の結果が関数型でも従来どおり検出される
  （二重報告ではなく同じ漏れを別経路で見るだけ）
- 実装は wave33 の採択範囲外（調査 → decision まで）— 採択なら
  次 wave の lane に
