# ビット演算子の提供形 — 三重字演算子 `&&&` `|||` `^^^` `<<<` `>>>`

- 状態: 採択（2026-11-24 wave38 インタビュー。実装は wave38 lane B）
- 日付: 2026-11-23

wave37 lane B（[prompts](../sketch/2026-11-23-wave37-prompts.md)）。出典は
[problem](../problems/2026-11-01-operator-coverage.md) と open-questions:
`|`/`&`/`^`/shift がなく、Go のフラグ合成（`os.O_APPEND|os.O_CREATE`）を
`+` で代用するしかない。sandbox 探索（jot）で再観測（
[sketch](../sketch/2026-11-23-sandbox-missing-features.md)）。

## 制約

- `|` は sum 型宣言と match 腕の区切り、`||`・`|>`・`|?>` は別トークン —
  中置 `|` は match 腕 `| A -> f x | g` の `|` が腕区切りか中置かを
  曖昧にする（`x | g` を式として読むと腕が終わらない）
- `<<`/`>>` は関数合成に使用済み
- `&&`/`||` は論理演算 — Go 式の `&`/`|` を論理と同じ字面で混ぜると
  Bool 演算と整数演算の区別が読み取りにくい
- 前置 `~` は labeled 引数 `~x:` が占有 — `^x` 相当の単項 NOT は別名が要る
- 被演算子は `intg` 制約（`%` と同じ機構）の届く範囲: builtin の
  `Int`/`Int32`/`Int64` と `fs.FileMode` 等の named Go 整数型
  （`namedIntegral`）— 専用の型名指定は不要。**builtin `Char`/`Byte`
  は `intg` に含まれない**（`isIntegral` は `Int`/`Int32`/`Int64`
  のみ）— `Byte` のマスク操作は需要が出たら `intg` を拡張するか
  bit 専用制約を足す。本草案では対象外とする

## 決定（案 B を推薦）

Elixir / F# と同じ三重字演算子を中置に追加する:

| Gom | Go | 優先度（Go の段に合わせる） |
| --- | --- | --- |
| `a &&& b` | `a & b` | mul-expr 級（`*`/`/`/`%` と同じ） |
| `a &&^ b` | `a &^ b` | mul-expr 級 |
| `a <<< b` | `a << b` | mul-expr 級 |
| `a >>> b` | `a >> b` | mul-expr 級 |
| `a ||| b` | `a | b` | add-expr 級（`+`/`-` と同じ） |
| `a ^^^ b` | `a ^ b` | add-expr 級 |

- どれも既存トークンと衝突しない新規トークン。`a ||| b` は現状必ず
  lex error か意味不明なので greedy lex で退行しない（`a ||` の後に
  `|` 開始の式は来られない。`<<<`/`>>>` も同様）
- 結果型は被演算子型 — `%` の `checkArith` と同じ経路で実装できる
- 単項 bit-NOT は持たない（`~` は labeled 引数が占有）。`x ^^^ -1`
  は `Int`/`Int32`/`Int64` では `^x` と同値だが、**unsigned な
  named 型（`fs.FileMode` 等）では `-1` が literal range check
  （`[0, 2^bits)`）で弾かれて使えない**。その系統では「ビットを
  落とす」形 `y &&^ x` が実用の代替。`Int.bnot` 級の builtin は
  必要が見えたら後で足す

## 検討した他案

- **案 A: Go 式のまま `&`/`|`/`^` + 代替 shift**: `|` は match 腕・
  sum 宣言との曖昧さが大きく、`<<`/`>>` は合成が占有。`&&`/`||`
  との見分けも付かない。棄却
- **案 C: `Int.band a b` 等の builtin/module 関数**: 構文リスクゼロだが
  `Int.bor (Int.bor a b) c` はフラグ合成の主用途で辛い。`Go.Int` 系の
  named 型まで関数が届くかの設計も要る。棄却（関数は補助として将来
  追加は自由）
- **案 D: 単語演算子 `band`/`bor`/`bxor`/`shl`/`shr`（OCaml 系）**:
  予約語・文脈キーワードを増やす。Gom のキーワード集合は意図的に
  小さいので棄却

## 実装メモ（採択時）

- lexer: `&&&`/`&&^`/`|||`/`^^^`/`<<<`/`>>>` を追加（最長一致順:
  `&&&`→`&&`、`<<<`→`<<`、`|||`→`||` の先に判定）
- parser: mul-expr 段に `&&&`/`&&^`/`<<<`/`>>>`、add-expr 段に
  `|||`/`^^^` を `syntax.BinOp` として追加。grammar.ebnf 優先度表も同期
- format: `precOf`（internal/format/print.go）の BinOp switch に
  新演算子を同じ優先度で追加 — 未登録は `panic("unreachable")` になる
- check: `checkArith` に `intg` 制約で追加（`BinMod` と同じ経路）
- core: `validate.go` の Binary operator allowlist に新演算子を追加
  — lower が Go emit 前に Core 検証を走らせ、未登録は invalid operator で落ちる
- codegen: `binOp` に `&`/`&^`/`|`/`^`/`<<`/`>>` の対応を追加
- spec: §1.5 の演算子一覧と優先度を更新（problem が指摘する通り
  演算子表自体が無い — 既存演算子も含めた表を足すのが自然）
- `testdata/spec/` に `ok_bit_ops`、golden に出力形を追加
