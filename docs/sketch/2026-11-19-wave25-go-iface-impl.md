# wave25 lane D: Gom 型が Go interface を実装できるかの調査

- 日付: 2026-11-19
- 状態: 調査完了・採否は podhmo に委ねる
- 関連: open-questions「Gom の型が Go の interface を実装できるようにするか」、
  spec §8（「v1 ではできない」明記）、[wave23 struct 構築](2026-11-19-wave23-struct-construction.md) の (c') host パッケージ

## 問い

`fmt.Stringer`・`error`・`io.Writer`・`io.Reader` 相当を最小セットに、
Gom で宣言した型が Go interface を満たすようにできるか。宣言形・
method 値生成・`.gomi` への現れ方を整理する。

## 実現可能性の整理

### emit 形状（方法を生やせる型は record）

- record → `type Name[A any] struct { ... }` — **named 型なので
  メソッドを生やせる**
- sum（ADT）→ sealed interface + `Name_Ctor[A]` struct — variant
  struct にメソッドを生やしても、値は sealed interface 型で流れる
  （宣言メソッドは `sealed()` のみ）ので **Go interface を満たさない**
  → 対応範囲は record（とその alias）に限定される
- `type T = ...` の alias は展開されるので、named record への alias
  経由では生やせる

### 今日既に動く経路: host ファイル（ゼロコスト）

生成物は普通の Go パッケージなので、wave23 の (c') 方式 —
生成パッケージと同じディレクトリに hand-written `.go` を置く —
だけで `func (t MyRec) String() string` が書け、`fmt.Stringer` を
満たす。**今日、コンパイラ変更なしで達成可能**。差分は:

- `Result T GoError` → `(T', error)` の unwrap や `Ptr T` 引数の
  扱いを手で正しく書く責任が利用者側に残る
- コンパイラが method set の充足を検査しない（Go ビルドまで
  気づかない）

### Gom 側で宣言する形（3 案）

| 案 | 宣言形 | spec 変更 | 実装規模 |
| --- | --- | --- | --- |
| (i) メソッド属性 | `fn (t: MyRec) write (p: Go.Slice Byte) -> Result Int GoError` + 属性で「Go method `Write` として emit」 | 中（属性 + 命名対応） | check（fn→method 検査）+ codegen |
| (ii) `impl` 宣言 | `impl io.Writer for MyRec = { write = my_write }` | 大（新構文） | resolve/check/codegen 全面 |
| (iii) `@implements` 属性 | `type MyRec = {…} @implements(io.Writer)` + 命名規約で fn を引き当て | 中 | check（goimport の signature と照合）+ codegen（転送 method emit） |

いずれの案でも機械的な困難は小さい:

- **レシーバ**: 先頭パラメータが `T` → 値レシーバ、`Ptr T` →
  `*T` レシーバ。`io.Writer`/`io.Reader` は状態を持つので実質
  `Ptr T` 専用になる（値レシーバで宣言しても mutations が
  コピーに消える — 検査 or 診断で `Ptr` を要求する設計が要る）
- **返り値の橋渡し**: `Result Int GoError` → `(int, error)` の
  unwrap は `Go.funcN` / import mapping と同じ境界規則で、
  `Result` の `Tag()`/`Unwrap()` を使う method 本体を emit
  すればよい — 新しい意味機構は要らない
- **命名**: method 名は CamelCase・Gom fn は snake_case。属性に
  明示名を書くか、規約（`write` → `Write`）にするか
- **検査**: interface の method set は goimport が既に signature
  を読めるので「`io.Writer` に対し `Write([]byte) (int, error)`
  を満たす fn が引き当てられたか」の適合検査は作れる

### `.gomi` への現れ方

メソッドは Gom の member 空間（E0201 のドットアクセス）ではなく
Go 側の表面なので、`.gomi` に書く必然はない。ただし「この型は
`io.Writer` を実装する」という宣言自体を `.gomi` に残すかは
選択肢 — 残すと import 側で `w : MyRec` を `io.Writer` 引数に
渡せるかどうかの検査材料になる。出さなくても生成物の Go 型は
実際に interface を満たすので実行は動く（宣言情報として消える
だけ）。

### 副次的な影響

- W0502（`errors.As` lint）は「宣言された record は error を
  実装し得ない具象形状」を仮定している — impl が入った record
  はこの lint の対象から外す対応が要る
- sum 型は対象外なので「`error` を返したい Gom の独自エラー型」
  は record で書く必要がある — ADT エラー型に `Error()` を
  持たせたい需要が出たら別問題（sealed interface に method
  を足す = interface の宣言メソッドを増やす話で、生成物の形を
  変える）

## 判定材料

- **host ファイルで足りる**のが本質的な論点: 宣言→検査→emit の
  全部を言語に入れても、買うものは「method set 充足のコンパイル
  時診断」と「`Result` unwrap の自動化」だけ。host 方式のコスト
  は「手で Go を書く」だけで、生成物は同じ
- (i)/(iii) を採る場合の実装コストは中規模 — 構文は属性で済むが、
  fn→method の emit 形（curried fn 値ではなく flat method）と
  receiver/Result の橋渡しに codegen の新経路が要る
- 採るならまず `fmt.Stringer`/`error`（値レシーバ、引数なし、
  `String`/`String` 返し）にスコープを絞るのが最小 —
  `io.Writer`/`io.Reader` は `Ptr` レシーバと `Result` unwrap
  が要るので 2 段目
- 対案「入れない」: open-questions の項目を「host ファイルで
  書けるので言語機能としては見送り」で閉じる判断も成立する

## 結論（材料の整理）

- 実現可能性: record 型については実装可能。sum は生成物の形を
  変えない限り対象外
- 既に host ファイルで同じことが書けるので、言語機能の価値は
  「検査」と「宣言の単一ソース化」。podhmo の価値軸で、生成物の
  Go 面を言語側から完全に統制したいなら採用、host で許容する
  なら open-questions 閉じ
