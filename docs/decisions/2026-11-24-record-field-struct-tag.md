# record フィールドの Go struct tag — フィールド行の verbatim バッククォートタグ

- 状態: draft（wave39 lane B として実施）
- 日付: 2026-11-24
- 関連: [method-emit-name-verbatim](2026-11-24-method-emit-name-verbatim.md)
  （verbatim 名の escape という同種の問題意識）、open-questions の
  `{{.name}}` テンプレ参照

## Context

record フィールドは Gom 側が lowercase 強制・Go emit は PascalCase なので、
`json.Marshal` が `{"Tag":"x"}` を出力する。外部の JSON スキーマが
`{"tag":"x"}` を期待すると一致しない。Go では struct tag
（`Name string \`json:"name"\``）がこの問題の標準的な解だが、Gom
record のフィールド宣言 `name : String` には修飾面が一切ない。

フィールド単位で外部名を与える需要はこれに留まらない — `{{.name}}`
テンプレ参照（derived naming）や `xml`/`db` の複数タグも同じ宣言面の
問題。

## 修飾形式の比較（網羅）

既存の修飾面は 2 種ある。トップ宣言への属性ブロック `[ @name ]`
/`[ @name(UIdent) ]`（parser.go `parseAttrDecl`、`@method(GoName)` が
引数形の先例）と、宣言 lead コメントの directive（`gom:embed`、
`gom:allow`、`gom:noescape`、`gom:result-notaint`、`gom:mutable`）。
どちらも**宣言単位**で、フィールド単位の修飾点は存在しない。
wave37 の `AttrSp` 作業で判明した制約: decl 内部（record body を含む）の
コメントは formatter が dead trivia として lead の後ろに移す —
**宣言内部のコメントに意味を持たせる形式は formatter と衝突する**。

| 案 | 形 | 長所 | 短所 |
| --- | --- | --- | --- |
| A. フィールド行 directive | `name : String //gom:tag json:"name"` | directive 先例に合う | decl 内部コメントは formatter が移す（上記制約）— 行末コメントの帰属が壊れる |
| B. フィールド属性 | `name : String @tag("json", "name")` | 構造化・検査可能 | 属性 grammar はトップ宣言専用 — フィールド位置への拡張が新設計。複数タグで `…,omitempty` のような値は結局文字列になり構造化の旨味が薄い |
| C. 型宣言 directive テーブル | `//gom:tag name=json:n,x=xml:x` + `type T` | lead コメントは安全 | タグとフィールドが分離、追加・改名の同期が壊れやすい |
| D. doc コメント規約 | `/// json: name` | 新構文ゼロ | doc と意味が混ざる・decl 内部コメント制約と同根 |
| E. verbatim バッククォート | `` name : String `json:"name"` `` | Go の記法そのもの（`Name string \`json:"name"\`` と同位置）。複数タグ・`,omitempty` 等はそのまま書ける。lexer は backtick 文字列 1 種だけ | 生文字列なので形式検査は check が負う（`key:"v"` 対の形） |
| F. `{{.name}}` テンプレ値 | `@tag("json", "{{.name}}")` | 派生命名を宣言できる | 修飾位置の問題とは直交（値の仕組み）。B/E の値側オプションとして後から足せる |

複数タグの要求（`json:"n" xml:"n" db:"n"`）で B/C は属性・directive を
繰り返すか可変長引数にする必要がある — どちらも verbatim 1 本より重い。
D/A は formatter 制約で棄却。F は位置の決定に関係しない。

## 値の表現

Go のタグは `key:"quoted"` の空白区切り列で任意性が高い
（`json:",omitempty"`、`db:"name,pk"` 等）。構造化するなら key/value
対だが、value に gom の文字列リテラルを通すと `"` の二重 quote が
煩雑になる。verbatim バッククォートなら Go と同じ綴りで任意の並びを
書け、check 側は `key:"v"` 対の形だけ検査すればよい（誤形式は
reflect まで落ちる前に E コードで止める）。

## Decision

**案 E — フィールド行の verbatim バッククォートタグ**を採る。

```gom
type User = {
  name : String `json:"name" db:"name,pk"`
  mail : String `json:"mail,omitempty"`
}
```

- 位置は Go の struct tag と同じ（フィールド型の後）— 学習面が最も自然
- 複数タグ・オプション値を無変更で書ける
- 検査面: check で `key:"value"` 対の連なりかを検査（誤形式は新しい
  E コード） — emit は verbatim
- `.gomi` は同じ綴りで往復、`gom fmt` はフィールド行をそのまま保持

将来 `{{.name}}` のような派生値（案 F）が要れば、タグ文字列内の
プレースホルダ展開として後から足せる — 位置の決定とは直交する。
