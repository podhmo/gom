# Gom record が Go interface を実装する宣言 — 設計

- 状態: adopted（2026-11-22 podhmo 採択 — wave30 lane C で実装済み）
- 日付: 2026-11-21
- 関連: open-questions「Gom の型が Go の interface を実装できるようにするか」、[sketch 2026-11-19-wave25-go-iface-impl](../sketch/2026-11-19-wave25-go-iface-impl.md)（実現可能性の調査）、decisions/2026-11-21-record-single-field-defined-type.md（lane A — 本稿はその着地後の世界を前提にする）

## 前提: lane A 着地後の生成物

- 複数フィールド record → `type Name[A] struct{...}`、単一フィールド record → `type Name F` の defined type。どちらも **named 型なので Go メソッドを生やせる**。
- sum（ADT）は sealed interface + variant struct のまま — 値が interface 型で流れるため Go interface を満たせず、**対象外**。
- alias（lane E の `type alias X = T` が入れば）は透明な別名 — 本体の record 側に宣言する。

## 宣言面の設計

### 推奨: `@method` 属性 + fn 宣言（案 (i) の確定形）

```gom
@method
fn (w: Ptr MyRec) write (p: Go.Slice Byte) -> Result Int GoError = ...

@method
fn (r: MyRec) string () -> String = ...
```

- 先頭パラメータをレシーバとみなす: `T` → 値レシーバ、`Ptr T` → `*T` レシーバ。
- Go メソッド名は fn 名の CamelCase 変換で導出（`write` → `Write`、`read_line` → `ReadLine`）。属性引数（`@method(WriteAll)`）は属性構文が引数を取れないので scope 外 — 規約で足りなければ別 decision。
- `@method` は `@extern`・`@tailrec` と同じ declAttrs の 1 枠を使う — 属性は fn/let のみなので構文変更はほぼゼロ。
- `impl` 宣言（案 (ii)）と型属性 `@implements`（案 (iii)）は退ける: 前者は新しい宣言構文（resolve/check 全面）で重すぎ、後者は「メソッドの出所」を命名規約の引き当てに依存して失敗時の診断が悪い。`@method` は fn 側に明示宣言があるので未充足は「interface の必須メソッドと宣言の照合」で機械的に検査できる。

### 充足宣言は要るか — 要らない（最小設計）

「`MyRec` が `io.Writer` を満たす」かは、型に紐づく `@method` fn の集合から**オンデマンドで計算できる**。`type R = {..} impl io.Reader` 系のリスト宣言は診断の早さ（宣言時に必須メソッドの抜けを検査）以外の価値がなく、意図表明としては後から足せる — v1 では持たない。

### `.gomi` の見せ方

`@method` 付き fn を `.gomi` に書き出す（属性付き fn シグネチャとして）。import 側のパッケージが「`x : MyRec` を `io.Writer` パラメータへ渡せる」判定をレシーバ型から再現できる。実装リストではなく fn リストを出すので「この型は I を実装する」は導出情報のまま — 宣言漏れは境界の照合で検出される。

## 呼び出し規約と境界

- **生成物**: `func (r *MyRec) Write(p []byte) (int, error)` — flat な Go メソッド。Gom fn 本体は既に flat 呼出し形を持つので、本体コードはそのまま移せる。gom 内からの呼び出しは従来どおり `write w p`（fn としても参照できる — 1 宣言 2 面）。
- **`Result T GoError` の unwrap**: `Go.funcN` / import mapping と同じ境界規則を method emit に適用する（`Tag()`/`Unwrap()` で `(T', error)`）。新しい意味機構は要らない。
- **method value**: `w.Write` を Go 側で取るのは生成物の普通の機能 — gom 内の `w.Write` は member 空間に入れない（v1）。gom 内で method value が要る場面（`io.WriterFunc` 相当）には対応する fn を `Go.funcN` 経由で渡す。
- **境界（record → Go interface パラメータ）**: extern の param が Go interface 型 `I`、引数が gom record 型 `R` のとき:
  - `R` の `@method` 集合が `I` の method set を値レシーバで満たす → `w` をそのまま渡す。
  - ポインタレシーバが要る場合 → `Ptr R` の引数を要求する（`Ptr.make`/`Ptr.new` は既存の綴り）。暗黙のアドレス取得はしない — record は値意味論で、`&w` を黙って取ると Go 側の書き込みが Gom 値を介さず共有される方向の契約違反（8.6 節）に繋がる。
  - 判定は goimport に新しい oracle `InterfaceMethods(I)`（go/types の method set 列挙）を足し、check が「`R` の `@method` 集合 ⊇ `I` の必須集合」を照合する。既存の `InterfaceSource`/`MethodSource` プロトコルに載せる。

## 副次的な影響（sketch から引き継ぎ）

- W0502（`errors.As` lint）: `@method` を持つ record は「error を実装し得ない具象形状」の仮定から外す。
- ADT エラー型に `Error()` を持たせたい需要は別問題（sealed interface の宣言メソッド追加 = 生成物の形を変える）。
- 単一フィールド record の defined type 化で、そのフィールド型が Go 型（`type X = {b: Go.Buffer}` → `type X bytes.Buffer`）でもメソッドは**継承されない**（defined type の仕様）。委譲メソッドを書きたい需要は `@method` fn で素直に書く（`fn (x: X) write p = x.b.Write p` 相当 — field アクセス経由）。

## スコープ提案（採用時）

1. `fmt.Stringer` / `error` — 値レシーバ・引数なし・非 Result 返り（`String`/`Error`）から始めるのが最小。
2. `io.Writer`/`io.Reader` — `Ptr` レシーバ + `Result` unwrap + interface パラメータ照合は 2 段目。

## 判定材料（wave25 sketch から、現状で再評価）

- host ファイル（wave23 (c')）で今日でも同じ生成物は書ける — 言語機能の価値は「method set 充足のコンパイル時診断」と「`Result` unwrap・レシーバ形状の自動化」と「宣言の単一ソース化」。
- 採用するなら本稿の `@method` + 規約命名 + `.gomi` の fn リスト + `InterfaceMethods` oracle が構成要素。open-questions の項目は採否確定まで開いたままにする。
