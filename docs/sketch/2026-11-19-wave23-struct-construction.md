# Go struct 構築機構の比較設計（wave23 lane1）

2026-11-19。A テーマ（examples/gist-cli の host パッケージ根絶）の調査 lane。
decision [2026-11-17-struct-construction](../decisions/2026-11-17-struct-construction.md)
の再検討 — 当時の採用 (c) factory-extern の前提だった 2 つの障害は
解消済み:

- `Timeout: 30 * time.Second` — named int 算術（decision
  2026-11-17-named-int-arith）で `time.Duration` 型の式が綴れる
- `CheckRedirect` — Go.func2 / Go.FuncN（decision 2026-11-17-func-interop、
  8.8 節）で Gom の関数値を Go の func 型へ渡せる

残る唯一の理由は `http.Client{Timeout: ..., CheckRedirect: ...}` の構築。
podhmo 方針により本 lane は単一案を採ばず、複数候補の比較材料を残す
（採否は wave24 以降）。

## 不変条件の棚卸し（仕様拡張の前提確認）

「現仕様で守られているはずのものが守られている」ことの確認が先、
という lane1 の手順に従い、拡張が侵し得ない不変条件を列挙して
それぞれの検証機構を点検した。

| 不変条件 | 内容 | 検証機構 | 状態 |
| --- | --- | --- | --- |
| 値の不変性 | `List` の境界コピー（8.5）、record `with` は新値、組み込み型は値 | testdata/spec の W0501 fixture（in-place 変更契約への警告）、golden | 検証あり |
| 可変性はハンドル型経由のみ | 変更は `Ptr.set` / `Go.Map.set` / `Array.set` / `Chan` に閉じ、素の値型は書けない | spec fixtures（`ok_ptr_ops` 等）、`Array.set` 範囲外 panic 規則 | 検証あり。`Array.to_list` の凍結契約（書き戻し不可）は spec が「診断しない」と明記する契約違反 — 検査機構は意図的に無い |
| Go 境界の nil/不正値 | `Result` の nil-lie 境界検査、match 時 `InvalidVariant`、`*T` は `Ptr T` | spec fixtures + golden の境界検査 emit | 検証あり |
| 8.3 の型対応の一貫 | 公開されない型は理由付き E0501、extern 署名は生 Go シグネチャ照合 E0509 | spec fixtures。E0509 は `gom check` CLI 経路でも発火することを本 lane で実地確認（`/tmp` で誤署名 extern を検査） | 検証あり |
| eval ↔ 生成コード parity | 意味論オラクル eval と生成 Go が同じ意味 | `internal/build/compare_test.go`（`fn answer` を eval とバイナリで比較）。**Go 側に出る値は eval が観測できないので parity 対象外** — struct 構築値も同じ扱いになる | 範囲限定で検証あり |
| 呼び出し規約 | 型付き AST が呼び出し情報を構造化して持つ（VFunc.flatArity 等） | golden + `eager_test.go` + spec fixtures | 検証あり |
| 生成物の可読性 | 「読めてデバッグでき Go から使える」— `//line` アンカーで .gom 位置へ戻れる | golden 差分の目視レビューのみ。機械検証はない（主観品質） | レビュー慣行のみ |
| 診断の形式 | `internal/diag` 内部表現・UTF-8 byte offset・コード不重用 | spec fixtures（expected.json が形式を固定） | 検証あり |
| `.gomi` 適合 | 公開面と実装の一致（E0601 等） | spec fixtures | 検証あり |

穴として見つかったもの: 新しい構文面を足す場合、**eval は Go struct
値を表現できない**ので新機構は eval ↔ 生成コードの parity
（`compare_test.go` の `fn answer` 比較）の検証対象外になる —
`gom test` 自体は生成 Go の `go test` を走らせるので手書きテストで
埋められるが、parity スイートの範囲外に落ちることを容認するかが
設計判断になる。これは既存の `Ptr.set`/`Go.Map.set`/Go 呼び出し全般と同じ。

## 評価軸

- 嬉しい記述: `NewClient` 相当を Gom で書くゴールの記述例（下記各案の
  mock）。record literal `{ f = v }` との見た目の距離も見る
- 制約: partial init・unexported field・embedded・addressability・
  生成コードの Go 慣習
- 守るもの: 上の不変条件表（値の不変性・interop 一貫・spec の面の小ささ・
  生成物原則）

## 実験（experiments/w23/struct-construction/）

各案の「ゴールの記述例」を対で置いた。`go vet` + `go test` 緑
（CI の check-experiments が走る nested module）。3 パッケージとも
`host.NewClient` と同じ観測契約（`Timeout == 30s`、
`CheckRedirect == ErrUseLastResponse`）を検証する。

| dir | Gom 側記述 | 生成側 |
| --- | --- | --- |
| `acomposite/` | `mock.gom.txt` — `Ptr.make (http.Client { Timeout = ..., CheckRedirect = ... })` 提案構文 | `gen.go` 手書き — `&http.Client{Timeout: ..., CheckRedirect: ...}` |
| `bfieldwrite/` | `mock.gom.txt` — `Ptr.new ()` + `c.Timeout = ...` 提案構文 | `gen.go` 手書き — `new(http.Client)` + field 代入 |
| `cextern/` | `a.gom` — **今日コンパイルできる実物**（`@extern fn` + `Go.Func2` シグネチャ）。`a.go` は `gom build` の実際の出力 | `impl.go` 手書き body |

`cextern` は実物検証: `gom check`/`gom build` が通り、生成 `a.go` +
手書き `impl.go` が `go test` で動く。`Go.func2` の emit は
`func(*http.Request, []*http.Request) error` に adapter（`tmp6`）を
生成し、内部で `list.FromSlice` + `Result.Unwrap` する。

## 案の比較

### (a) named Go struct 限定のコンポジットリテラル

記述例: `http.Client { Timeout = 30 * time.Second, CheckRedirect = ... }`
（record literal と同じ `{ f = v }` 形、先頭に型名）。`Ptr.make` で
`*http.Client` を得る。

- spec の面: **大**。新しい literal 形式（型名 + keyed field）、
  partial init（未指定は Go ゼロ値）・unexported field 拒否・
  embedded field のキー名規則・positional 非対応、の規則設計が要る。
  syntax → resolve → check → lower → codegen の全面
- record literal との距離: 近い（同じ `{ f = v }` の見た目）。
  「Go struct だけ type 名が前置する」差は説明しやすい
- 値の不変性: 守れる — 生成物は値、`with` 相当の更新はなし
- 生成コードの読み味: 素直（Go の composite literal そのもの）
- addressability: 不要（値生成のみ）

### (b) field 書き込み + ゼロ値構築

記述例: `let c : Ptr http.Client = Ptr.new (); c.Timeout = ...; c`.
2 機能 — `ptr.Field = v` 代入面とゼロ値の綴り（`Ptr.new ()` は
**既にある**ので、実質の新規は field 代入だけ）。

- spec の面: **中**。`v.Field = x` は Gom 初の「束縛ではない代入」
  構文で、addressability 規則が要る。`Ptr T`（T は named Go struct）
  経由のみに絞れば、値の不変性は `Ptr.set`/`Go.Map.set` と同じ
  「ハンドル型経由の可変性」枠に収まる
- record literal との距離: 遠い（命令列）。ブロック内の手続き的記述
- 値の不変性: Ptr 経由に限定すれば既存枠内。`Ptr` でない値への
  field 代入を開くと不変性を侵すので、制限が設計の肝
- 生成コードの読み味: 素直（`new(T)` + 代入列）
- 二次効果: field 代入面は composite literal では書けない既存 API
  （`http.Transport` の細かい dial 設定等、段階構築する値）にも効く。
  逆に「中途半端な値を型付きで引き回せる」穴でもある

### (c') `@extern fn` のユーザーモジュール運用

記述例: 実験の `a.gom` どおり — `@extern fn new_client (...)` +
同パッケージの `impl.go`。

- spec の面: **ゼロ**（実装済み）。`.gomi` が要る点だけが package
  main 以外での儀式
- record literal との距離: 関係なし — Gom 側は普通の関数呼び出し
- 値の不変性: 侵さない（不透明な Go 値のまま）
- 生成コードの読み味: a.go は読めるが、本質の構築は impl.go にあり
  「生成物が全部を語らない」— host 根絶というより host の同居化
- 別の見方: host パッケージを「別パッケージの hand-written Go」から
  「生成パッケージの隣の hand-written Go」に縮めるだけ。構築面として
  Gom で書けるわけではない

### 比較表

| 軸 | (a) composite literal | (b) field write | (c') @extern impl | 現状 (c) host pkg |
| --- | --- | --- | --- | --- |
| spec 変更量 | 大（新 literal + 規則群） | 中（代入構文 + addressability） | ゼロ | ゼロ |
| 実装規模 | syntax→codegen 全面 | check/lower/codegen + lvalue | ゼロ | ゼロ |
| NewClient が書ける | はい（値を Ptr.make で包む） | はい | Go 側に残る | Go 側に残る |
| 不変性への影響 | なし（値生成） | Ptr 限定なら既存枠内 | なし | なし |
| host 根絶 | できる | できる | 同居で縮小 | できない |
| 生成物の読み味 | Go 慣習そのまま | Go 慣習そのまま | 本体は手書き Go | 本体は手書き Go |
| 不変条件の検証との整合 | Go 値は eval 圏外（既存と同じ） | 同左 | 同左 | 同左 |
| 記述の嬉しさ | record literal に近い | 手続き的、後から値を組み立てる形 | Gom 側は見えない | Gom 側は見えない |

## 判定材料（一意に定めない — podhmo の価値軸で選ぶ）

- **構築面としての言語の完結性**を重視するなら (a)。(b) は「構築」
  ではなく「変更」で、(c') は Gom で書けることにならない
- **spec の面の小ささ・実装規模**を重視するなら (b) か (c')。
  (b) は機能が2つ要るが片方（`Ptr.new`）は済み。(c') は実装済みで
  今日の gom がそのまま host をパッケージ内に畳める
- **利用者の書き味**では (a) が record literal と一貫し、(b) は
  手続き的で Gom の式指向から外れる — ただし (b) は段階構築する
  Go 値（初期化が複文になる API）への汎用性がある
- (a)+(b) 排他ではない: (a) が構築、(b) が構築後調整を担う設計も
  ある。最小の host 根絶は (c') で既に可能、という事実も残る

## 検証済み・未検証の事実整理

- 確認済み: (c') は今日動く（本実験で `gom build` + `go test` 緑）。
  `Ptr.new ()` は `Ptr http.Client` を型注釈で作れる（(b) の
  ゼロ値側は実装済み）。named int 算術と Go.func2 は main.gom で
  実働している
- 未検証（採否時に要る設計作業）: (a) の embedded/unexported/
  positional の規則、record literal との構文衝突の有無、(b) の
  addressability を `Ptr` 限定にするか代入一般にするか、(a) で
  func-typed field に `Go.func2` がそのまま置けるか（mock では
  置いたが型検査の設計は未着手）

## 関連

- decision [2026-11-17-struct-construction](../decisions/2026-11-17-struct-construction.md)（本調査の再検討対象）
- draft decision [2026-11-19-struct-construction-options](../decisions/2026-11-19-struct-construction-options.md)（比較の固定）
- 実験: [experiments/w23/struct-construction/](../../../experiments/w23/struct-construction/)
- host: [examples/gist-cli/host/](../../../examples/gist-cli/host/)
