# トップレベル宣言の `init`/`main` 予約名は codegen で `_` 退避

- 状態: adopted
- 日付: 2026-11-19
- 問い: `let init`/`fn init`/`fn main`/`let main` が型検査を通るが、
  生成コードがそのまま `func init(w ...)`/`func main(...)`/`var init`
  を吐き、`go build` が失敗する（FuzzCompile seed 検出、
  implementation-gaps 唯一の open 項）— wave27 lane A の対応方針

## 決定

(b) codegen で束縛名を退避する — `init`/`main` は pkgReserved として
package-scope 宣言では `_` 接尾辞にマングルする（`init_`/`main_`）。
参照側は同一シンボルの emit 名解決（declSymName/claimBase）を通るので
自動的に揃う。エントリ `fn main () -> Unit`（`package main`）だけは
`func main()` を契約通り出力するため `main` を維持する — sibling
ファイルからの参照のため UnitDecl.Entry を core IR に載せる。

- `init`: `var init` はどのパッケージでもビルド不可
  （"cannot declare init - must be func"）、`func init(引数)` も不可、
  `func init()` は合法だが import 時に勝手に走る — 全ケース退避。
- `main`: `package main` では `func main()` 以外の `main` は不可、
  それ以外のパッケージでは合法だが一律に退避して規則を単純化。
- ローカル束縛（`let init` 内側など）は合法なので触らない —
  退避は package-scope 宣言のみ。

## 却下した代替

- (a) check で予約名を E0xxx として拒否 — 診断は早く明快だが、
  `let init`（`Ptr` 初期化イディオム）のような既に動くはずのコードを
  エラーに変える。生成物の名前付け規則として codegen で解決する方が
  ユーザーに制約を課さない（podhmo 採択、b）。

## 実装メモ

- `pkgReserved`（names.go）: `init`/`main`。`declSymName` で
  `!g.entry[sym]` のとき `base += "_"`。`claimBase` は
  claim の base 明示版 — declBase（sort key・binder stem）を汚さない。
- `g.entry` は indexDecls で集める: 自ファイル `FuncDecl.IsEntry` +
  sibling `UnitDecl.Entry`（lower.unitScope が isEntryDecl で判定 —
  isEntryPoint の check-level 相当: 1 param・束縛なし・Unit・残部 Unit）。
- golden: `testdata/golden/reserved_names.gom` — `fn init`/`let`/`fn`
  ヘルパー + entry main の混在で退避と参照 rename を固定。

## 影響

- spec section 5: 入口以外の `main`/`init` トップレベル宣言は
  `_` 接尾辞と明記。
- implementation-gaps の該当項目を解消済みに更新。
