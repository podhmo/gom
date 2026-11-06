# `Go.Any` の綴り — 型式の書けるすべての位置で `Go.Any` を認める。裸の `any` は型変数のまま

- 状態: 採用（検証済み — [検証](../sketch/2026-11-06-go-any-spellable-prototype.md)。spec.md §8.3 へ反映済み。下流の残ギャップは implementation-gaps.md / TODO.md へ）
- 日付: 2026-11-06
- 関連: [nilable-iface-pair-ok-payload](2026-11-06-nilable-iface-pair-ok-payload.md)（`(any,error)` の派生ビュー）、[sketch: P3-7](../sketch/2026-11-05-p3-7-realworld-interop-audit.md)（`Go.Any` が書けない実害の実測）

## Context

Go の `any`（`interface{}`）は内部では `types.GoAny` として
表現され、`(any, error)` は `Result Go.Any GoError` に派生し、
Go `any` へのボックス機構（`ConvBoxAny` — `Go.conv` が目標の
`any` を emit する経路を含む）は既に存在する。しかし
`.gom`/`.gomi` のソースから `Go.Any` を**綴る**道がなかった:

- 裸の `any` は小文字識別子として lex され、シグネチャでは
  暗黙の型変数になる（`fn f (x: any) -> any` は多相関数）。
- `Go.Any` は `builtinGoMembers` が `conv`/`Slice`/`Map` しか
  公開せず E0201、checker の `goTypeArity` も `Slice`/`Map` のみ。

P3-7 で `json.Decoder.Token` が `Go.Any` しか返さない唯一の
stdlib decode 経路と実測された。`try` の推論位置には `Go.Any`
が流れるが、宣言された `(any, error)` ビュー
（`Result (Option Go.Any) GoError`）や `let t : Go.Any` の注釈は
書けず、workaround は named 空 interface の別名宣言だけだった。

## Decision

### `Go.Any` は型式の書けるすべての位置で認める

`Go.Any` を `builtinGoMembers`（resolve）と `goTypeArity`
（check、arity 0）に追加し、型式の現れるすべての位置で受理する:

- `.gom` / `.gomi` / `extern` のシグネチャ（パラメータ・結果）
- `let x : Go.Any` などの型注釈
- コンテナ・複合位置 — `List Go.Any`、`Go.Slice Go.Any`、
  `Go.Map k Go.Any`、`Ptr Go.Any`、`Option Go.Any`、sum ペイロード、
  record フィールド
- extern 宣言ビュー — `Option Go.Any`（lone `any` 結果）、
  `Result (Option Go.Any) GoError`（`(any,error)` — nil = absence
  の API）、verbatim `Result Go.Any GoError`

`Go.Any` が want 型の位置には任意の Gom 値がボックスされて入る
（既存の `ConvBoxAny` 経路）。`Go.Any` から具象型への取り出しは
引き続き `Go.conv` ではできない（E0507 — Go でも `any` → `T` は
変換ではなく型アサーションなので、変換面の一貫性として正しい）。

### 裸の `any` は綴りにしない — 型変数のまま

`any` は keyword ではなく ident で、`fn f (x: any) -> any` は
今日も valid な多相シグネチャ。ここで `any` を予約すると既存の
型変数が黙って別の意味に変わる（silent re-meaning）。`Go.` 修飾の
慣習は `Go.Slice`/`Go.Map` と同じ — qualified 名だけを認める。

### named `any`（`json.Token` 等）と `Go.Any` は別名として扱う

`type Token any`（定義型 — `json.Token` がこの形）のような
named 空 interface は従来どおり `types.Go` ハンドル
（`json.Token`）に写る — `Go.Any` とは nominal に別型（Go 表現
は同じ `any`）。`type T = any` のエイリアスは `any` そのものに
展開されて `Go.Any` に写る — nominal なのは定義型の側だけ。
extern 宣言で `json.Token` を `Go.Any` と書き換えることは認め
ない（宣言ビューの正直性 — probe で `(probeapi.NamedAny, error)`
への `Result Go.Any GoError` 宣言が E0509 で拒否されることを
確認済み）。`let v : Go.Any = <json.Token 値>` のような want
位置へのボックスは入る。

### スコープ — 発見された残ギャップは実装レーンへ

prototype で check が認めても lower/codegen が materialize
しない位置が見つかった（[sketch](../sketch/2026-11-06-go-any-spellable-prototype.md)）:

1. **宣言結果位置のボックス未実装** — `fn f -> Go.Any` で本体が
   `json.Token` や型変数 `a` だと check は受理するが lower が
   E0901（`body type … differs from residual type Go.Any`）。
   want 位置としては他と同じくボックスが仕様の答え — impl lane
   で residual 位置の `any()` materialize（または check 側を
   厳しくするか）を決める。
2. **exported `let` の declared-type 忠実度** — `.gomi` が
   `let x : Go.Any` を広告しても生成 Go は具体型
   （`var X string`）を出す。Go 側 API 面として `any` を出すには
   codegen の var 型に declared 型を届ける必要がある。
3. `Option Go.Any` の `None` は式位置で書けない（`let o :
   Option Go.Any = None` → E0407 — meta が `Go.Any` want に bind
   しない規則の副作用）。`Option Go.Any` の値は Go 呼び出し結果
   またはパターンからは得られる — impl lane が規則を精緻化する
   か判断する。
4. **ジェネリックコンテナ payload の Gom 型と Go 表現の乖離**
   （fix-round レビューの追試で確認）— `let o : Option Go.Any
   = Some 42` のような注釈では ctor の payload meta が `Int` に
   bind され、emit は `option.Option[int64]`。Gom 型は
   `Option Go.Any` なので check/build は通るが、`o` を
   `Option Go.Any` パラメータへ渡すと Go の型不変性で
   `go build` が落ちる（`Option[int64]` は `Option[any]` を
   満たさない）。ローカル変数の Go 型が値側の具体型を取るため
   注釈はスロットを作らない — `Some`/`Ok`/list リテラルを
   `Go.Any` 要素へ物質化する経路が要る。スカラの
   `let x : Go.Any = 42` は `x := int64(42)` で、使用位置で
   再ボックスされるので同じ乖離でも害はない — 壊れるのは
   ジェネリック実引数の位置だけ。

これらは「`Go.Any` を綴るか」の決定とは別の materialize の
ギャップなので、採用の対象は spelling 面のみとし、4 件は
implementation-gaps.md / TODO.md に送る。

## 検証まとめ

最小パッチ（`builtinGoMembers` に `Any`、`goTypeArity` に
`"Any": 0` の 2 行）で検証 — 詳細は
[sketch](../sketch/2026-11-06-go-any-spellable-prototype.md)。
`go test ./...` 全緑（既存フィクスチャに `Go.Any` 不可を
前提にしたものは無い）。extern の `Option Go.Any` /
`Result (Option Go.Any) GoError` 宣言は `option.FromGo(v, v != nil)`
で物質化され、named-interface workaround が要らなくなることを
確認。

## Alternatives

- **`Go.Any` だけ宣言位置に認める（結果・extern だけ）** —
  不採用: P3-7 の主たる実害は `let t : Go.Any` の注釈と
  `Option Go.Any` 宣言 — 「シグネチャだけ」では直らない。逆に
  残ギャップ（宣言結果の E0901）は spelling を狭めても消えない
  materialize の問題。
- **裸の `any` も綴りにする** — 不採用: 上記の型変数との衝突。
- **どちらも認めない** — 不採用: `Go.Any` が推論位置にだけ
  現れて綴れない現状は spec 記述不能・診断も不自然。
  workaround（named 空 interface の別名）は実害のある API ごとに
  Go 側 shim が要る。

## Consequences

- spec §8.3 の対応表に `any`/`interface{}` → `Go.Any` の行を
  追加し、「裸の `any` は型変数」を明記。
- open-questions.md の `Go.Any`/`any` 項目は解決済みとして消す。
- 実装は次 wave のレーン — 2 行の admission 変更 +
  上記 4 件の下流ギャップ（declared-result materialize、
  exported let の型忠実度、`Option Go.Any` の `None`、
  コンテナ payload の Go 表現乖離）。
- wave-10 が予約していた `Option Go.Any` / `Result (Option
  Go.Any) GoError` の宣言ビューが実際に書けるようになる。
