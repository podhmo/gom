# DMMF OrderTaking を基準にした gom 適合性レポート

- 日付: 2026-11-26
- 目的: Scott Wlaschin 『Domain Modeling Made Functional』（DMMF）付属コード、特に `OrderTaking` / `OrderTakingEvolved` を写すとき、gom で各パターンが直接書けるか、代替で書けるか、不足かを判定する
- 対象コミット: `09c7fa3a`（wave48 lane G マージ後の main）
- 関連: [spec](../../spec.md)、[open-questions](../../open-questions.md)、[implementation-gaps](../../implementation-gaps.md)、[sandbox-explorer2-sumtypes](2026-11-24-sandbox-explorer2-sumtypes.md)

## 確認方法

添付コード `src/Chapters/Ch04`〜`Ch13`、`src/OrderTaking/*`、`src/OrderTakingEvolved/*` を読み、提示された受け入れチェックシートの各項目を gom の仕様・stdlib・生成コードに照らした。不明な点は最小 `.gom` probe で `gom check` / `gom build` / `gom run` を実行して確認した。主要な probe は [experiments/dmmf-fit](../../../experiments/dmmf-fit/) に残してあり、本文中のスニペットはその抜粋である。

検証手順:

```sh
# repo root でコンパイラを作り、probe module を build/test する
go build -o /tmp/gom ./cmd/gom
cd experiments/dmmf-fit
/tmp/gom build domain main/main.gom
go test ./...
```

2026-11-26 時点ではこの一連が成功した。生成された `domain.go` / `main.go` は rebuild できるため commit していない。

なお、このレポートは調査記録であり規範ではない。言語仕様の正は [spec.md](../../spec.md)。

## 総合結論

**DMMF の中心パターンはかなり書ける。** sum type、網羅的 `match`、式指向、カリー化、`|>`、コンストラクタの第一級関数化、`try`、`|?>`、`(T, error)` の `Result` 読み替え、`.gomi` による抽象型、`Scope` による後片付け、Go の `encoding/json` / `database/sql` への直接接続は揃っている。`validateOrder` のような fail-fast ワークフローは、F# の `result { let! ... }` より少し明示的だが、直線的に書ける。

一方で、DMMF をそのまま移植するときの摩擦・不足は次の 9 点に集約される。

1. **段階型 record の型推論が弱い**: `UnvalidatedOrder` / `ValidatedOrder` / `PricedOrder` のように同じフィールド名を持つ record を並べると、record literal が E0407 になることが多い。`Ok { ... }` の中では戻り値の注釈だけでは足りず、`let x : T = { ... }` を挟む必要がある。
2. **F# の型名モジュールに相当する namespace がない**: `String50.create` / `String50.value` のような型に紐づくモジュールは、パッケージ分割または `string50_create` のような命名で代替する必要がある。
3. **`internal` 相当の可視性がない**: `.gomi` に書くか書かないかの公開/非公開だけなので、bounded context 内では共有しつつ外部には隠す型を素直に表せない。
4. **stdlib に `Result.sequence` / `traverse` などがない**: ユーザー定義で書けるが、DMMF の `List.map ... |> Result.sequence` は毎回 helper が要る。
5. **panic を gom 側で `recover` できない**: Go の普通の `error` は `Result` に入るが、panic をドメインエラー化するアダプタは手書き Go の `@extern fn` が要る。
6. **JSON は record 用の struct tag 経由では強いが、sum type / `Option` の自然な表現は自動では出ない**: tagged union や nullable 文字列は DTO / `Ptr` / `Go.Map` で明示変換する。
7. **金額向けの exact decimal がない**: `Float` は binary64。DMMF の `decimal` に近づけるには integer minor units か外部 decimal 型 + メソッド呼び出しが要る。演算子オーバーロードはない。
8. **バリアントコンストラクタ名はパッケージ内で一意**: `Validation` のような汎用的なケース名を複数の sum type で使い回せない。型名付き参照 `PlaceOrderError.Validation` は使えても、宣言側の衝突は残る。
9. **不透明型でも zero value / reflection 経由の bypass は残る**: `.gomi` は通常の constructor/literal/conversion を防ぐが、`Ptr.new` や `encoding/json` など Go 境界経由では未検証値を作れる。スマートコンストラクタの保証は「普通の gom コードでの構築を塞ぐ」レベルである。

## 判定の凡例

| 記号 | 意味 |
| --- | --- |
| ◎ | 直接書ける |
| ○ | ユーザー側の定型コード・パッケージ設計で代替できる |
| △ | 書けるが、保証や DX が DMMF/F# より弱い |
| × | 現状では書けない、または実質的に不足 |
| — | gom の対象外として割り切れる |

## 1. 型システム・データモデリング

| # | 判定 | 結果 |
| --- | --- | --- |
| 1-1 ゼロペイロードとペイロード混在 sum type | ◎ | `type PaymentMethod = \| Cash \| Check Int \| Card CreditCardInfo` の形で直接書ける。再帰・型パラメータ・named/labeled payload も仕様済み。 |
| 1-2 単一ケースラップ型の型区別 | ◎ | `type CustomerId = \| CustomerId Int` は nominal で `OrderId` と区別される。1 フィールド record（`type UserId = { value: Int }`）も nominal で、Go 側には defined type として出る。 |
| 1-3 再帰的 sum type | ◎ | `type Employee = \| Worker String \| Manager (List Employee)` を probe で確認。`List` やバリアント経由の再帰は合法。 |
| 1-4 相互再帰型・定義順 | ◎ | トップレベルは前方参照でき、`Payment` / `PaymentMethod` のような相互参照も probe でコンパイルできた。ただし record が直接互いを値として含む無限サイズ構造は E0413。DMMF の例は sum/list 経由なので問題になりにくい。 |
| 1-5 ジェネリック record / sum type | ◎ | `type Box a = { item: a }`、`type Validation a e = ...` を確認。Go 1.18+ の generics に落ちる。 |
| 1-6 `Option` | ◎ | builtin `Option a` と `Option.map` / `and_then` / `to_result` がある。JSON 表現は 7 章の注意が必要。 |
| 1-7 record の非破壊更新 | ◎ | `{ order with shipping_info = updated }` が書ける。ネスト更新は `{ order with shipping_info = { order.shipping_info with cost = zero } }` のように段階を書く。`with` は既存フィールドの更新専用で、別型へのフィールド追加はできない。 |
| 1-8 同名フィールドを持つ別 record | △ | 型定義自体はできるが、literal の持ち主推論が弱い。関数の戻り値位置では通ることがある一方、`Ok { ... }` や `let x = { ... }` では E0407 になりやすい。現状は `let x : T = { ... }` で回避。型名前置 literal は既に open question。 |
| 1-9 値オブジェクトの構造的等価性 | ◎ | `==` は関数を含まない型について構造的に効き、`List` を含む record や ADT でも probe が通った。Go の slice 制約は気にしなくてよい。型変数の `==` は v1 では不可。 |
| 1-10 エンティティの等価性制御 | △ | 既定の構造等価を型ごとに禁止・上書きする仕組みはない。`cmp.Eq` 辞書やキー抽出関数で代替できるが、誤って `==` を使うことは防げない。 |

## 2. カプセル化・ドメインの完全性

| # | 判定 | 結果 |
| --- | --- | --- |
| 2-1 不透明型とスマートコンストラクタ | △ | `.gomi` に `type String50` だけ書くと、import 側は通常の literal/conversion/constructor では作れず、公開した `string50_create` 経由にできる。probe では `let x : domain.String50 = "x"` が E0401、`Go.conv` が E0507 になった。ただし `Ptr.new` や `encoding/json` 経由では未検証値を作れるため、F# の private constructor と同じ強さではない。 |
| 2-2 型とモジュールの同居・公開/内部の分離 | △ | F# の `module String50` 相当はない。`string50_create` / `string50_value` 命名、または型ごとに小さな package を作って `string50.create` と呼ぶ代替になる。`.gomi` の公開/非公開はあるが、assembly internal 相当はない。 |
| 2-3 状態マシンとしての型遷移 | ◎ | `UnvalidatedOrder` → `ValidatedOrder` → `PricedOrder` のような段階型は普通に書け、不正な段階の関数適用は型エラーになる。実装上の摩擦は 1-8 の record literal 推論。 |

不透明型の probe 形:

```gom
// domain/domain.gom
package domain

type String50 = { value: String }

fn string50_create (s: String) -> Result String50 String = ...
fn string50_value (x: String50) -> String = x.value
```

```gomi
// domain/domain.gomi
package domain

type String50

fn string50_create (s: String) -> Result String50 String
fn string50_value (x: String50) -> String
```

注意点: この保護は「通常の gom 構文での構築を塞ぐ」もので、zero value や Go 境界まで塞ぐものではない。実際、abstract export した型でも次はコンパイルできた。

```gom
fn zero_bypass () -> domain.String50 = {
  let p : Ptr domain.String50 = Ptr.new ()
  Ptr.get p
}
```

`Ptr.new` は `database/sql` の scan 先や `errors.As` の out-pointer のための primitive なので、abstract な gom 型でも zero value を作れる。`encoding/json` も同じく検証を通さない。したがって外部入力は必ず専用 DTO にし、ドメイン型へはスマートコンストラクタ経由で入る運用が必要。より強く守りたい場合は、`Ptr.new` を abstract な gom 宣言型に対して制限する言語側の検討が要る。

## 3. パターンマッチと式指向

| # | 判定 | 結果 |
| --- | --- | --- |
| 3-1 `match` が式 | ◎ | `let x = match ...`、関数本体の末尾式として直接使える。 |
| 3-2 網羅性検査 | ◎ | ケース追加時に不足 arm は E0301。probe では `BillableOrderPlaced` を忘れると compile error になった。ただし text 出力は不足パターン名を出せない既知 gap があり、詳細は JSON の `data.missing_patterns` を読む必要がある。 |
| 3-3 ペイロード・ネスト分解 | ◎ | constructor / record / tuple / list rest / or / as / guard が揃っている。ネスト constructor は `Add (Num 0) r` のように括弧が要る。 |
| 3-4 関数引数での直接分解 | ◎ | `fn process_customer_id ((CustomerId inner): CustomerId) -> Int = inner` がコンパイルできた。F# より括弧は多いが1行で書ける。 |
| 3-5 複数値同時 match・ガード | ◎ | `match (a, b)` と `p if cond` が書ける。guard は網羅性に数えないので、catch-all か後続 arm が要る。 |

進化テストの観点では、広い `_ ->` を置いた `match` は新ケース追加時に E0301 にならない。網羅性を進化検知に使いたい場所では catch-all を避ける必要がある。

## 4. 関数・部分適用・パイプライン

| # | 判定 | 結果 |
| --- | --- | --- |
| 4-1 `|>` の型推論と連鎖 | ◎ | 仕様どおり。改行ルールだけ注意が必要で、複数行適用は pipe か全体の括弧で書く。 |
| 4-2 コンストラクタの第一級関数化 | ◎ | `s \|> String50 \|> Some`、`o \|> Option.map Event.AcknowledgmentSent`、`r \|> Result.map_err Validation` を確認。 |
| 4-3 部分適用 DI | ◎ | gom 関数はすべてカリー化されている。`validate_order check_product check_address` を部分適用して 1 引数関数として渡せる。Go 関数はカリー化しないので、依存が Go 関数なら薄いラッパを置く。 |
| 4-4 汎用高階関数アダプタ | ◎ | `predicateToPassthru` 相当や `tee` 相当は HM 型推論の普通の generic fn として書ける。 |
| 4-5 関数を返す高階関数・クロージャ | ◎ | `get_pricing_function` の「標準価格表を一度だけ評価して closure に捕捉」を probe でコンパイルできた。 |
| 4-6 関数値のコレクション・型エイリアス | ◎ | `type alias ValidateOrder = ...` に実装を束縛する形を確認。関数値は record/list に入る。関数を含む値には `==` は使えない。 |
| 4-7 関数合成・無名関数 | ◎ | `>>` / `<<` と lambda がある。Result 用の Kleisli 合成 `>=>` は組み込みではないが、generic fn として定義できる。 |

コンストラクタを受け取る generic factory もそのまま書けた。

```gom
fn create_string (field: String) (ctor: String -> a) (max_len: Int) (s: String) -> Result a String =
  if str.length s == 0 {
    Err "${field} must not be empty"
  } else {
    if str.length s > max_len { Err "${field} is too long" } else { Ok (ctor s) }
  }

type String50 = | String50 String

fn string50_create (field: String) (s: String) -> Result String50 String =
  create_string field String50 50 s
```

## 5. `(T, error)` と `Result` の統合

| # | 判定 | 結果 |
| --- | --- | --- |
| 5-1 `bind` / `map` / `mapError` 連鎖 | ◎ | stdlib に `Result.and_then` / `map` / `map_err` があり、Go の `(T, error)` は import 時点で `Result T GoError` に読まれる。`Result` は第一級の値なので `List (Result a e)` も作れる。 |
| 5-2 アーリーリターン構文 | ◎ | `try` が `let!` 相当。`toAddress` 型の直線的な検証を probe でコンパイルできた。ネストや `if err != nil` 連打は不要。エラー型は一致している必要があり、違う場合は `Result.map_err` を挟む。 |
| 5-3 型付きドメインエラーの sum type 化 | ◎ | `PlaceOrderError = \| Validation ValidationError \| Pricing PricingError \| OutsideBusinessHours` のような閉じた型を書き、constructor を `map_err` に渡せる。Go の `error` は `GoError` 値として payload/record に保持できる。 |
| 5-4 `Result.sequence` | ○ | stdlib にはないが、ユーザー helper で書ける。DMMF の頻出パターンなので stdlib 入り候補。 |
| 5-5 Applicative なエラー蓄積 | ○ | `Validation a e` と `map2` を自作できる。ただし専用構文や stdlib はなく、fail-fast の `try` よりかなり冗長。 |
| 5-6 `Option` と `Result` の相互変換 | ○ | `Option.to_result` は stdlib にある。`Result -> Option` と `bindOption` は小さな helper を書く。能力不足ではないが surface は薄い。 |
| 5-7 Go `(T, error)` とのシームレス連携 | ○ | Go → gom は直接。gom → Go は通常の export fn が curried + `Result` 値を返すためそのままでは `(T, error)` ではないが、`Go.funcN` に適合する `let` を export すれば Go 側に `func(string) (int64, error)` の形で見せられることを確認した。`@method` でも `Result T GoError` は `(T, error)` に unwrap される。 |
| 5-8 panic / 例外の Result 化 | △ | gom の言語内に `recover` はなく、panic は伝播する。`Scope` は panic 時 cleanup を走らせるが error 化はしない。アダプタが必要なら手書き Go 実装の `@extern fn protect` を用意する。Go の通常の失敗は panic ではなく `error` なので、DMMF の例外アダプタほど頻繁には要らない。 |

`Result.sequence` の動作確認済み実装例:

```gom
fn result_sequence (rs: List (Result a e)) -> Result (List a) e =
  rs
  |> List.fold (fn acc r -> acc |> Result.and_then (fn xs -> r |> Result.map (fn x -> [x, ..xs]))) (Ok [])
  |> Result.map List.rev
```

`bindOption` 相当:

```gom
fn bind_option (f: a -> Result b e) (o: Option a) -> Result (Option b) e = match o {
  Some x -> f x |> Result.map Some
  None -> Ok None
}
```

`toAddress` 相当の fail-fast 検証は次の形でコンパイルできた。

```gom
fn to_address (input: UnvalidatedAddress) -> Result Address ValidationError = {
  let line1 = try (string50_create "line1" input.line1)
  let line2 = try (string50_create_option "line2" input.line2)
  let city = try (string50_create "city" input.city)
  let zip = try (zip_create "zip" input.zip)
  let address : Address = { line1 = line1, line2 = line2, city = city, zip = zip }
  Ok address
}
```

ここで `Ok { ... }` ではなく一度 `let address : Address` にしているのが重要。`UnvalidatedAddress` と `Address` が同じフィールド名を持つため、constructor 引数の中の record literal は E0407 になりやすい。

逆方向 interop の probe では、

```gom
fn parse_impl (s: String) -> Result Int GoError = Ok 1

let parse_go : Go.Func1 String Int GoError = Go.func1 parse_impl
```

が次の Go 値を生成した。

```go
var parseGo func(string) (int64, error)
```

## 6. コレクション・数値・ビジネスロジック

| # | 判定 | 結果 |
| --- | --- | --- |
| 6-1 iteration / aggregation / transformation | ◎ | `List.map` / `filter` / `fold` / `concat` / `sum` / `sum_int` / `sort_by` がある。DMMF の注文行集計は書ける。 |
| 6-2 `List.choose` | ◎ | `List.filter_map` が相当品。`ProductLine` だけ抽出する `make_shipment_line` を確認。 |
| 6-3 条件付きイベント list | ◎ | `list_of_option` と `List.concat` で F# の `yield!` 相当が書ける。`Option Event` の list なら `List.filter_map (fn x -> x)` でもよい。 |
| 6-4 ワークフロー全体の decorator | ◎ | `business_hours_only get_hour on_error on_success workflow` のような高階関数を普通に書ける。同期モデルなので async 版の色付けは不要。 |

注意は数値である。DMMF は `decimal` を使うが、gom の `Float` は binary64。`Price` / `BillingAmount` をそのまま `Float` にすると金額計算の丸めが変わる。代替は:

- セント等の integer minor units を `Int` で持ち、`Price` / `BillingAmount` で包む
- Go の decimal パッケージを使い、`Add` / `Mul` のようなメソッド呼び出しに落とす

後者は外部 Go 型なので `+` / `*` の演算子は使えない。ドメインモデルの核で頻繁に演算するなら integer minor units の方が gom らしい。

## 7. 境界・シリアライズ・永続化

| # | 判定 | 結果 |
| --- | --- | --- |
| 7-1 DTO ↔ ドメイン型の双方向変換 | ◎ | DTO record に Go struct tag を付け、`json.Marshal` / `json.Unmarshal` してから手書き converter でドメイン型へ検証する形が書ける。`Ptr.new` + `json.Unmarshal` の probe はコンパイルできた。 |
| 7-2 sum type の JSON シリアライズ戦略 | △ | 自動の tagged union codec はない。direct marshal は implementation shape（`{"V0":"a"}`、zero payload は `{}`）になるので外部 schema に使わない。DMMF と同じく、`PlaceOrderEventDto.fromDomain` 相当の手書き変換で `Go.Map String Go.Any` や専用 DTO record にする必要がある。 |
| 7-3 副作用境界とリソース管理 | ◎ | `database/sql` の呼び出しと `Scope.run_result` / `Scope.defer_err` の組み合わせをコンパイル確認した。純粋ドメイン関数と境界 I/O の分離は書ける。effect tracking はないが DMMF の F# 版も型レベルの効果分離は限定的。 |

record の JSON struct tag は実装済み。

```gom
type CustomerInfoDto = {
  name: String `json:"name"`,
  email: String `json:"email"`,
}
```

`json.Marshal` は `Result (List Byte) GoError` として見え、そのまま `try` / `|?>` に載る。

ただし `Option` は direct marshal で期待する nullable 値にならない。probe では次のようになった。

```gom
type Dto = { line1: String `json:"line1"`, line2: Option String `json:"line2,omitempty"` }
// {"line1":"a","line2":{"V0":"b"}}
```

nullable / omitted な外部 schema には `Ptr` を使うと近い。

```gom
type Dto = { line1: String `json:"line1"`, line2: Ptr String `json:"line2,omitempty"` }
// Some 相当: {"line1":"a","line2":"b"}
// nil:       {"line1":"a"}
```

これは型安全というより JSON 境界用の表現なので、DTO 側だけに限定し、ドメイン型では `Option` を使うのがよい。あるいは DMMF 側がやっているように optional string を空文字で表す割り切りもできる。

tagged union 相当も手書きで成立する。

```gom
fn event_dto (e: Event) -> Go.Map String Go.Any = {
  let m : Go.Map String Go.Any = Go.Map.make ()
  match e {
    Shippable dto -> Go.Map.set m "ShippableOrderPlaced" dto
    Ack order_id -> Go.Map.set m "OrderAcknowledgmentSent" order_id
  }
  m
}
```

DB 境界の probe:

```gom
fn count_users (dsn: String) -> Result Int GoError =
  Scope.run_result <| fn s -> {
    let db = try (sql.Open ("sqlite3", dsn))
    Scope.defer_err s (fn () -> db.Close ())
    let row = db.QueryRow ("select count(*) from users")
    let n : Ptr Int = Ptr.new ()
    try (row.Scan n)
    Ok (Ptr.get n)
  }
```

## 8. OrderTaking → OrderTakingEvolved の進化テスト

| 変更 | 判定 | 結果 |
| --- | --- | --- |
| 変更1: 送料計算の追加 | ◎ | `Address` に `state` / `country` を足すと、既存 record literal は missing field で compile error になる。配送地域分類は active pattern ではなく、「分類用 sum type を返す関数 + `match`」で書ける。`PricedOrderWithShippingMethod` の段階追加も可能。摩擦は同名フィールド record の literal 推論。 |
| 変更2: VIP 顧客 | ◎ | `CustomerInfo` に `vip_status` を足すと構築箇所がコンパイルエラーで洗い出せる。`freeVipShipping` の `with` 更新は直接書ける。 |
| 変更3: promotion / comment line | ◎ | `PricingMethod` と価格 factory closureは確認済み。`PricedOrderLine` を record から `ProductLine ... \| CommentLine ...` に変えると、`match` 不足や型不一致で影響箇所を辿れる。ただし E0301 の text 出力は不足ケース名を表示しない。 |
| 変更4: shippable event / business hours | ◎ | `List.filter_map` で `CommentLine` を落とせる。`PlaceOrderError` に `OutsideBusinessHours` を足したときの分岐漏れも E0301 で検出できる。decorator も高階関数で書ける。 |

## 9. 意図的にスコープ外でよいもの

提示された割り切りに同意する。gom でも次は代替または非目標でよい。

| DMMF/F# の機能 | gom での扱い |
| --- | --- |
| `AsyncResult` / 非同期モナド | Go の goroutine モデルでは同期風の blocking call でよい。`Task` / `Chan` はあるが、DMMF の写経には不要。 |
| 測定単位 `[<Measure>]` | `KilogramQuantity` / `UnitQuantity` のような opaque wrapper で代替。 |
| active pattern | 分類用の sum type を返す関数 + exhaustive `match` で代替。 |
| type provider | Go の `database/sql`、sqlc 生成物、または手書き boundary で代替。 |
| ユーザー定義 computation expression builder | `try` / `|?>` は言語組み込み。Applicative validation は自作できるが糖衣はない。 |

## 10. 追加で見えた不足・注意点

チェックシートの個別項目には載っていない、または項目またぎになる注意として、DMMF 移植では次も事前に決めておいた方がよい。

1. **コンストラクタ名の衝突**: 同一パッケージ内で `type E1 = | Validation String` と `type E2 = | Validation Int` は E0202。ドメインエラー設計では、ケース名を一意にするか package を分ける。
2. **zero value / direct JSON / reflection は不透明型の不変条件を守らない**: `Ptr.new` で abstract 型の zero value を作れる。1 フィールド record は scalar として marshal され、unmarshal でも検証を通さず値が入る。外部入力は必ず専用 DTO にし、ドメイン型へは smart constructor 経由で入る。
3. **Go 側から見た通常の export fn は `(T, error)` ではない**: `Go.FuncN` adapter か `@method` が必要。Go との厚い境界を持つなら、公開面を `Go.FuncN` にする規約を先に決める。
4. **E0301 の人間向け出力は弱い**: 網羅性エラー自体は出るが、text 出力に不足パターン名がない。AI/CI が JSON を読む分には問題ないが、人間向け DX としては改善候補。
5. **`&&` / `||` は生成 Go で短絡しない**: DMMF 写経とは直接関係しないが、境界ロジックでは `if` に書き分ける必要がある。

## 11. 優先度つきの提案

### 優先度 A: DMMF 移植の主軸に効く

1. **型名前置 record literal か record literal 推論の改善**  
   `ValidatedOrder { ... }` のような明示構文、または expected type / field type を使う推論。DMMF の段階型パターンでは最も頻繁に効く。既存 open question を具体化済み。
2. **stdlib に `Result.sequence` / `List.traverse_result` / `Result.to_option` / `bind_option` を足す**  
   いずれも小さいが、DMMF では頻出。仕様論点は名前と引数順だけ。
3. **bounded context のパッケージ設計ガイドを書く**  
   `public types` / `internal types` / `implementation` / `api` を 1 package に寄せるのか、sub package に分けるのか。`internal` 不可視性がない現状では、ここを決めないとカプセル化方針がぶれる。
4. **JSON tagged union と nullable DTO の推奨形を文書化する**  
   言語機能追加より先に、`Go.Map String Go.Any`、専用 DTO、`Ptr` nullable のどれを推すかを決めるとよい。
5. **abstract 型の zero value 生成をどう扱うか決める**  
   `Ptr.new` を abstract な gom 宣言型へ許す現状では、smart constructor の保証が Go の zero value まで届かない。制限する、警告する、あるいは DTO 境界規約に留めるのかを決める必要がある。

### 優先度 B: 不足を補う小さな基盤

6. **panic capture の `@extern fn protect` 相当を rt か実験 package に置く**  
   gom 言語に `recover` を入れる必要まではないが、外部サービス呼び出し用の一度きりのアダプタは定型化できる。
7. **金額表現の decision を作る**  
   integer minor units を推奨するのか、外部 decimal を許容するのか。DMMF 移植前に決めないと `Price` / `BillingAmount` の意味が揺れる。
8. **E0301 text 出力の不足パターン詳細**  
   既知 gap。進化テストでは「どのケースが不足か」が最重要情報なので優先度は高め。

### 優先度 C: 無理に言語機能化しなくてよい

9. **コンストラクタ名の型スコープ化**  
   あると F# 風だが、命名規約と package 分割で回る。衝突規則の文書化で十分かもしれない。
10. **等価性の禁止/カスタム equality**  
   `cmp.Eq` 辞書で代替できる。誤用防止まで欲しいかは需要次第。
11. **Applicative validation の糖衣**  
   fail-fast が主軸なら `Validation` type と `map2` helper で十分。DMMF でも発展項目。

## 最終判定

DMMF の「型で不正状態を潰し、ワークフローを `Result` の pipeline として組み、境界で DTO に落とす」という中心部分は、gom の現在の言語機能で十分に表現できる。特に `try` + `Result` + `(T, error)` の統合は、Go ベースの言語として強い。

一方で、**快適に写せるか**という観点では record literal 推論と namespace/visibility が最大の摩擦である。次に stdlib の Result helpers、JSON union/Option 表現、decimal 方針が続く。これらはコンパイラの根本的な穴というより、DMMF 型のドメインモデリングを繰り返し書くときに効いてくる「面」の不足である。
