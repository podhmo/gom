# `@extern let` と `//gom:embed` の提供形

- 状態: verified（実装済み — spec §8.10）
- 日付: 2026-11-23
- wave37 lane E

## 課題

`//go:embed`（アセット同梱）と、Go の directive comment 全般・blank
import（`import _ "x"`）の3題が未提供だった（open-questions）。
Gom は生成コード中に任意の `//go:` コメントを書く術を持たず、
`let` は必ず `= e` を要求し、import のエイリアスに `_` が書けなかった。

## 選択肢

### 案 A: `//go:embed` をそのまま書ける

directive をユーザーが書くとコード生成がそれを通す — 生成ソースへの
自由なコメント注入は `gom fmt`・`//line` 対応・emit 台帳の保証を
壊し得る。拒否。

### 案 B: `@extern let` + `//gom:` directive（採用）

`@extern` を let に拡げて「宣言のみ・実体は Go 側」の変数を宣言し、
`//gom:embed <patterns>` を直上に付けると `//go:embed` +
`var name <T>` を生成する。

- directive は `//gom:` 接頭の Gom 側語彙 — 生成コード注入ではなく
  「embed 変数を生成してほしい」という宣言の一部として扱う
- 付着規則は `///` doc コメントと同じ「直上の連続コメント行」—
  宣言付着の新機構を新設せず File.Comments + span で実現できる
- `@extern let` 単体は「手書き Go が var を供給する」宣言として独立の
  意味を持ち、struct タグや他の directive を必要とする将来の用途も
  同じ面に載る

### 案 C: `let name : T = embed "pat"` のような値式

`let` の意味（値を束ねる）と矛盾する — embed var は式を持たない。
採用しない。

## 決定

案 B。合わせて `import go _ "path"`（blank import — Go パッケージ
のみ）を文法に追加する。`embed` パッケージの `_` import は
`//go:embed` を emit した側で自動補完する（ファイルが embed を
他に import していなければ）。型の制約（`String`・`Go.Slice Byte`・
named Go 型）は check 側で E0512、パターンの実在は `go build` に
委ねる。

## 実装メモ（検証済み）

- `//gom:embed` の付着: parser の `embedBefore` — docBefore と同じ
  連続コメント行スキャンで `gom:embed` 行を拾い `LetDecl.Embed` に
  載せる。check がパターンを分解し `check.LetDecl.Embed []string` →
  `core.VarDecl{Extern, Embed, Typ}` → codegen が
  `//go:embed` 行 + `var` を emit
- `@extern let` は core.VarDecl の Body を持たない — validate は
  Extern 時に nil Body を許容（Typ 必須）
- `//gom:embed` を non-extern let に付けると E0512。`@extern let`
  に `= e`・注釈なし・非 VarPat は E0101（parse）
- blank import: resolve は名前を束ねず loadPackage だけ行う
  （E0203 は生きる）、check.Import/core.Import に Blank を通し、
  codegen は `g.imports[path] = "_"` で `_ "path"` を emit。
  embed 自動補完は `needBlankEmbed` フラグ + render 時に
  「`embed` が未登録なら `_` で登録」— 既存 import との衝突なし
