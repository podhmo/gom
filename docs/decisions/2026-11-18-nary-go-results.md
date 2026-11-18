# N-ary Go 結果（3 個以上）の写像 — Tuple および `Result (Tuple) GoError`

- 状態: 採用（wave20 lane 1 で実装・検証した — `testdata/goiface/nary.go` のフィクスチャと `testdata/golden/nary_results.gom` が全経路の生成コードを pin する）
- 日付: 2026-11-18
- Supersedes: [go-types-mapping](2026-11-03-go-types-mapping.md) のうち「>2 結果は hidden (E0501)」の行、[func-interop](2026-11-17-func-interop.md) のうち「結果は 0・1 個、または `(T, error)` 2 個のみ。3 個以上の結果・`(T, U, error)` は隠す」の制約

## 問題

`strings.Cut`（`(string, string, bool)`）や `runtime.Caller` 相当の形など、3 個以上の戻り値を持つ Go 関数は従来 E0501 で公開されず、`extern` による上書きも E0509 で照合に落ちていた（spec 8.3/8.4 に上限明記）。

## 採択

3 個以上の結果（上限 8 個 — `rt.Tuple8` の arity が上限）を次の規則で写す:

- 末尾が `error` の `(T, U, ..., error)` → `Result (T, U, ...) GoError` — 既存の `(T, error)` → `Result T GoError` 規則の自然な拡張で、Ok ペイロードがタプルに畳まれる
- それ以外の多値 → `(A, B, ...)` のタプル

エラー位置は**末尾のみ**を識別する（`(error, T)` のように先頭・中間の error は特別扱いしない — タプル要素として写る）。`(error, ..., error)` のように末尾が error なら先に error を含むペイロードもタプル要素として写る（`Result (Option GoError, GoError) GoError` — 末尾だけが失敗チャネル）。

### nil-able 要素の規則（`Result` ペイロード内）

ペイロード・タプルの各要素は `(T, error)` のペイロードと同じ規則を要素ごとに適用する:

- メソッドを持つ interface 要素（`error`、`net.Error`、ユーザ定義 interface）は型由来の既定で `Option` に包む — nil は `Ok None` に読む（`(I, error)` pair と同じ理由: メソッド呼び出しが nil で panic する値を `Ok` に素通ししない）
- `*T`・`any`・named 空 interface などその他の nil-able 要素は verbatim — nil はそのままの値として流れる
- `Option` 要素の物質化は要素位置の `option.FromGo(v, v != nil)`（`ResultFromGo.ElemConv`）

### extern 宣言の admission

`Result (T, U, ...) GoError` の宣言はタプル要素を照合する:

- verbatim 要素は `viewsCompatible` で照合
- `Option x` 要素は nil-able な raw 型に限り `x` と raw が一致すれば受理
- **verbatim 要素 over メソッドを持つ interface は拒否** — `(T, error)` pair が verbatim 宣言に付ける never-nil 境界検査（`if v == nil { panic }`）と等価なガードをタプル内では emit しないため、素直に `Option` を綴らせる形だけを認める（honest-by-construction）
- 末尾 error を持たない多値は `(A, B, ...)` のタプルのみ（`Option` 要素は書けない）

## `Go.FuncN` の結果上限

`Go.FuncN` の可変長型引数（パラメータ N 個 + 結果群）は結果 0..8 個までに拡げる（旧 0..2）。FuncN コンは引き続き生の Go ビューを並べるだけで、読み戻しは `Go.fnN` / `Go.funcN` の両方で同じ畳み込み規則を使う:

- `func(A) (T, U, error)` ⇄ `Go.Func1 A' T' U' GoError` — Gom 側では `Result (T, U) GoError`
- `func(A) (T, U, V)` ⇄ `Go.Func1 A' T' U' V'` — Gom 側では `(T, U, V)` タプル
- `func(A) (T, U)`（error でない 2 値）⇄ `Go.Func1 A' T' U'` — Gom 側では `(T, U)` タプル（comma-ok の `Option` 畳み込みは関数メンバー側の規則で、func 値の読み戻しには適用しない — `Go.funcN`/`Go.fnN` が互いの逆変換であり続けるため）

`Go.funcN` の Gom→Go 方向では、Gom の `Result (Tuple) GoError` 結果の関数は `(T', ..., error)` を返す Go func に、タプル結果の関数は多値を返す Go func に展開される（error arm は各要素のゼロ値を返す）。

`Go.funcN` 側の曖昧性・上限の規則（レビュー起因で確定）:

- 最終要素が `GoError`（`Option GoError` を含む）の裸タプル結果 — `(T, GoError)` — は `Result T GoError` と同じ `(T', error)` シグネチャを綴る。FuncN コンは両者を区別できないため、`Go.funcN` はこの形を E0510 で拒否し、`Result` 綴りを要求する（結果の arity 判定は emit 側と同じく fs.Ret の `Result` 性で行う — 末尾の raw が error でも Gom 側がタプルなら verbatim の `GoResultsPair`/`Nary` が対応する、のがフォールバックの意味）
- `Result (Tuple8) GoError`（ペイロード 8 要素 + error = 9 結果）と 9 要素超の裸タプルは `Go.FuncN` の結果上限（8）を超えるため check で E0510 — 綴るなら `Result (Tuple7) GoError` まで

## 実装上の選択

- 新しい `core.GoResults` 値 `GoResultsNary`（素の多値）と `GoResultsTupErr`（末尾 error の多値）を導入 — `GoResultsPair`/`GoResultsErr` の流用は「ちょうど 2 結果」を仮定する emit 地点（EscapeCall の `v, err := call` など）で誤 emit するため不採用
- `try` / `|?>` の EscapeCall 融合は TupErr に拡げない — タプル・ペイロードの per-element 変換を融合パス内で担うより、一旦 `Result` に物質化してから `EscapeResult` で受ける形が素直（融合は `(T, error)` のまま）
- `(T, U, error)` のペイロード内 Option 変換は `ResultFromGo.ElemConv []ConvKind` が要素ごとに持つ（既存の `ResConv` はペイロード単位）
- `core.GoFunc.ResConvs []ConvKind` が `Go.funcN` 生成 func の per-result 変換を保持

## 残件・非目標

- `Result (Tuple) GoError` のペイロード内の nil-able 要素への never-nil claim（verbatim over method-iface）は表せない — 上記のとおり `Option` を必須とすることで代替
- EscapeCall 融合の TupErr 拡張は行わない（物質化経路で十分）
- 9 個以上の結果は従来どおり E0501 で非公開
