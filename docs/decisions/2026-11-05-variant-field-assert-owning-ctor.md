# バリアントの名前付きペイロード読み取りは所有者バリアントをアサートする

- 状態: 採用（実装・検証した — [検証](../sketch/2026-11-05-variant-field-assert-owning-ctor.md)）
- 日付: 2026-11-05

## 決定

sum type の値 `s : Shape` に対するフィールド参照 `s.f` は、「`f` を宣言する
唯一のバリアント」を静的に決める型検査が許した読み取りであり、その
バリアントを値が保持していることを**実行時にアサートする**。値が別の
バリアントを保持している場合は契約違反として panic する — `match` の
DPanic 系や spec 8.6 節の「不正な値」と同じ失敗の族である。

つまり `type Shape = | Circle (r: Float) | Rect (w: Float) (h: Float)` で
`s.r` は、`s` が `Circle` 値ならペイロードを返し、`Rect` / `Empty` 値なら
panic する。位置（index）や同名フィールドへのフォールバックは意味に
含めない: `.f` が選ぶのはフィールド名ではなく「その名を持つバリアントの
宣言済みスロット」である。

panic メッセージは型名・期待するバリアント・実際の値を含めることを
推奨するが、panic であること自体が仕様で、文言は実装依存とする
（生成コードは Go の型アサーション `s.(Shape_Circle).R` の失敗、
評価器は E9003 系の診断）。

## 背景

`fn getr (s: Shape) -> Float = s.r` に `Rect 2.0 3.0` を渡すと 2 実装が
ずれた（[problem](../problems/2026-11-02-variant-field-wrong-variant.md)）:

- 評価器は実行時バリアント自身のフィールド列を index → sym → name の順に
  引き、index 0 の `w`（別のフィールド）の値 `2.0` を返していた
- 生成コードは所有者バリアントの具象 struct への型アサーション
  `s.(Shape_Circle).R` を出力し、panic していた

check は「そのフィールド名を持つバリアントがちょうど 1 つ」のときだけ
`.f` を受理するので、所有者は静的に一意だが、値が実際にそのバリアントを
保持するかは静的には分からない — 意味を spec が定めていなかった。

選択肢:

1. **アサーション意味（生成コードに揃える）** — 違約 = panic。`match` が
   網羅性を超えた不正値に DPanic で応えるのと同じ族で、sum type の
   「どのバリアントか」の情報を型検査が担保しきれない場面の既定と
   一貫する。eval の ctor 一致検査を足すだけの小さな変更
2. **位置意味（eval に揃える）** — `.f` を「何番目のスロットか」として
   読む。スロット型がバリアントごとに違うので静的な結果型を作れず、
   生成コードは全バリアントの type switch が必要になる。非現実的
3. **静的制限** — `.f` をバリアントが確定している場面だけに限ると、
   `fn f (s: Shape) -> Float = s.r` の素直な形が型検査を通らなくなる。
   spec の「名前を使える」の文言とも合わない

(1) を採る。`check.MemberExpr.Ctor` は既に所有者バリアントのシンボルを
運んでおり、Core IR の `FieldGet.Ctor` 経由で生成コード側は確定済み。
評価器側が `x.Ctor` と値の ctor を比較すれば揃う。

## 実装

`internal/eval` の `evalMember`: `x.Ctor != nil` のとき、受け取った
`VVariant` の ctor を `x.Ctor` と比較（matchPat と同じく、Ctor が nil の
手組み値は Name 比較にフォールバック）。不一致なら E9003
（CodePanic — 「invalid variant」の族）で「field read on the wrong
variant」系の診断。一致すれば従来どおり index → sym → name の順に
フィールドを引く。`x.Ctor == nil`（手組み AST）は従来のフォールバックを
維持する。

## 検証

- `docs/sketch/2026-11-05-variant-field-assert-owning-ctor.md` に検証記録
- `internal/eval/eval_test.go` に wrong-variant / 手組み値のケース
- `internal/build/compare_test.go` に eval とバイナリが同じ panic 族に
  なることを見るケース
