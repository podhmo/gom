# `Ctor ({ ... })` の括弧が畳まれて位置引数と名前付きフィールドの区別が消える

- 状態: 未解決
- 発見: 2026-11-02, P1-3 の追加検証中

## 現象

`docs/decisions/2026-11-02-p1-3-parser-interpretations.md` は「`Ctor ({...})` は普通の引数として残る」と主張するが、`parsePatParen` (`internal/syntax/parse_expr.go`) がグルーピング括弧を畳むため、`*RecordPat` が Fields lift に到達し、`A2 ({name = n})` は `A2 {name = n}` と同じ `(ctor A2 name = n)` に dump される。括弧による明示的な曖昧性解消が文法上表現できない。

これが実害になるのは位置引数のレコード型フィールドとの衝突:

```
type X = | A {name: String}   // 位置引数として record 型を持つ variant のつもり
```

`{...}` は named-field lift に吸われ、`(sum |A (rectype ...))` という**位置引数のレコード型**として解釈される — つまり書いた通りには解釈されるが、意図した named-field バリアント `A {name: ...}` と紙一重で、ユーザが named fields を書こうとして `A {name = n}` パターンを書いた場合の意味が変わる。

## 選択肢

1. **仕様どおりに括弧を保持する**: `parsePatParen` でグルーピングを `ParenPat` として保持するか、Fields lift を「引数の `{` が直接書かれた場合」に限定する。AST に 1 ノード or フラグ追加。
2. **文書を実装に合わせる**: interpretations doc の記述を修正し、「ctor 引数の `{` は常に named-field lift」と明記する。位置引数のレコード型パターンは `A ({...})` では書けず、型注釈か別の形で書くしかないことを認める。
3. **spec 側で構文を分ける**: named-field ctor パターンの構文自体を再検討（例: `A.{name = n}` など）。spec 改定が要る大きめの変更。

## 影響範囲

`internal/syntax/parse_expr.go`（`parsePatParen`, Fields lift)、grammar.ebnf の ctor-pat 節、P1-6 の `checkCtorPat`（positional vs labeled の argIndex/fieldIndex 振り分けがこの区別に依存）。
