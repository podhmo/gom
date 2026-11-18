# 型クラス相当の抽象は明示的辞書渡しで書く

- 状態: adopted
- 日付: 2026-11-19
- 問い: open-questions「型クラスの代替」— v2 で型クラスを入れるか、
  明示的な辞書渡し（`Ord a` レコードを引数で渡す）で済ませるか

## 決定

(1) 明示的辞書渡しを採用する。型クラス（instance 宣言 + 解決機構 +
coherence 規則）は導入しない。キーワードも構文も増えない。書き方の
ガイドは `skills/gom-agent-guide/references/ad-hoc-polymorphism.md`。

## 理由（調査の要約）

調査・実験は [sketch](../sketch/2026-11-19-wave23-typeclass-vs-dict.md) と
`experiments/w23/typeclass/`。

- 辞書渡しは現行言語のまま動作する（record + 値引数だけ）ことを
  実物で確認済み。生成コードは型クラス案とほぼ同型 — 差は
  「辞書を誰が選ぶか」だけ
- 型クラスが買うのは呼び出し側での記述省略と一貫性だが、coherence
  （同じ型に instance 一意）は「同じ型に複数の正当な振る舞い」
  （Monoid 型の抽象、テスト用差し替え）を newtype 包みなしに書けなく
  する表現力制約でもある。辞書渡しにはこの制約がない
  （`desc_ord` の実例あり）
- 解決機構・曖昧さ診断・`.gomi` 拡張は v2 級の投資。現段階では
  見送る対象に十分な差がある

## 影響

- spec 変更なし（辞書渡しは既存機能の組み合わせ）
- design.md「他言語からの取捨」の型クラス行を本決定に更新
- stdlib に比較器辞書を取る API（`sort`/`min`/`member` 系）を
  追加するかは別途検討 — 追加するなら辞書引数を先頭に取る形に
  揃える
- deriving 相当（コンパイラ mint の `gomEqualN`/`gomShowN`）は
  現状のまま。ユーザ拡張可能な deriving は別件
