# 生成 Go の API 安定方針（Go ABI）

- 状態: 採択（案 B、2026-11-23 wave35 開始前インタビュー）
- 日付: 2026-11-23
- 問い: 生成される Go の API（型名・関数シグネチャ・バリアントの
  struct 名）を、Gom のバージョンをまたいでどこまで安定させるか
  （[open-questions](../open-questions.md) の Go 相互運用節・唯一の未決）

## 棚卸し: 名前の決まり方（現状）

codegen の emit 名は次の規則で決まる（`internal/codegen/gen.go`・
`names.go`・`decl.go`）:

- **決定的**: claim 順はユニット全体で一意 — (emit base, Gom 名)
  のソート順で、宣言順・ファイル順に依存しない。同じソース +
  同じパス + 同じコンパイラなら同じ名前になる
- **型名が先に取る**: 型宣言は `ExportName` した Gom 名
  （大文字化 + `_` 除去 — `Foo_bar` → `FooBar`）を claim し、
  値名はそれを避けて付く（exported `let foo` は
  `type Foo` より後に dedup される）
- **値名**: exported は `ExportName`（`is_some` → `IsSome`）、
  unexported は `valueName`（camel）。`main`/`init` 衝突は
  `_` エスケープ（entry point は `func main()` 契約）
- **衝突の suffix**: 同じ emit base に寄る名（`my_fn` と `myFn`）
  はソート順の先が素の base、後続が `x1`・`x2` …
- **sum のバリアント struct**: `Type_Variant`（`Option_Some`）。
  sealed interface の marker は `isType`（unexported）
- **record field**: `ExportName(field)` — ただし 1 フィールド
  record は Go defined type（`type UserId int64`）になるので
  フィールド名は Go API に現れない
- **`@method`**: `ExportName(fn 名)` の転送 wrapper が record に
  生える。Unit パラメータは wrapper から落ちる
- **test**: `Test<名>`、兄弟ファイル衝突時は fnv32a ファイルタグ
- **ローカル**: binder は宣言内でだけ dedup（`x`/`x1`）だが、
  生成 temp（`tmpN`）はファイル全体の `used` 集合で採番するので
  ある宣言の編集が別宣言の temp 名を変えうる。Go keyword 衝突は `_`

## 揺れどころ（既存コードの名前が変わる条件）

1. **同 base 衝突への追加**: `myFn` と `myFn1`（=`my_fn`）が
   同居するとき `myFn` に写る新 decl を足すと suffix 配分が
   ずれる — 新参者が `myFn1` を取り、元の `my_fn` は `myFn11`
   に追い出される。衝突していない名前は何を足しても動かない
2. **型名の追加・改名**: 型は先に取るので、新しい型名が既存の
   値名を追い出す。ただし `.gomi` に出ているメンバの衝突は
   E0607 で build が止まる（黙って `Foo1` にはならない）—
   揺れるのは非公開 decl だけ
3. **Gom 名の変更**: Go 名は Gom 名の `ExportName`/`valueName`
   写像 — Gom 側の改名はそのまま Go API の改名。ただし
   `is_some` → `isSome` のように同じ export 名に写る改名は
   Go API を変えない
4. **コンパイラの codegen 変更**: `genVersion`（build cache の
   キー）が上がる = 生成規約の変更。数値マッピング・Result の
   Go 形・Option 包装などシグネチャ面の規約変更はここに入る
5. **ソースパス**: `//line` アノテーションと test 衝突タグは
   ファイル名/パスを埋め込むので、同一ソースを別パスに置くと
   生成物が変わる（関数名は変わらない）

## 採択された方針（案 B — インタビューで draft の推奨案 A を越えて採択）

emit 名の衝突検出（生成名の一覧化・suffix 衝突・暗黙 member 名）
まで含めて採択。実装は wave35 lane C。

**「ソースが同じなら名前も同じ」を保証し、それ以上は約束しない。**

- 決定的 emit（ソート順 claim）は仕様として維持する — 同じ
  ソース・同じパス + 同じコンパイラで同じ Go が出ることは、
  diff レビューとビルド再現性の基盤なので緩めない
- **Gom 名が API**: `.gomi` メンバの Go 名は Gom 名の機械的写像
  であり、改名はソース側の変更として追跡される（export 名が
  同じままの改名は API 不変）。衝突 suffix でずれるのは
  同 base 衝突を持つ非公開 decl だけで、公開面の衝突は
  E0607 で止まる — 「export 名を重ねない」は利用者側の
  回避可能な条件
- **バージョン間の安定は約束しない**: `genVersion` の更新は
  生成規約の変更を意味し、Go API が変わり得る。手で書く
  Go 側の結合コードは `.gomi` シグネチャに対して書き、
  バージョンアップ時は再生成して差分を見る運用
- **内部名は約束の外**: temp・binder・`gomEqN`・`is` marker・
  test タグは Go API として参照されない前提で自由に変えてよい
- 将来「変更抑制レベル」を強めるなら（案 B の余地）: 衝突
  suffix の配分を宣言順にする、`@method` の alias 機構、
  `.gomi` に emit 名のピン留め欄を設ける、などは個別 decision で

## 選択肢

- 案 A: 決定的 emit のみ保証、Go API はソース追従 — draft の推奨
- 案 B: 衝突検出まで含める — 採択
- 案 B: 上に加えて「編集で既存名が変わらない」まで保証
  （衝突解決を宣言順・ピン留め機構などで強化）— 将来の余地
- 案 C: Go API を semver で管理（メジャーまで互換保証）—
  v1 の変更速度に対して過剰、棄却候補
