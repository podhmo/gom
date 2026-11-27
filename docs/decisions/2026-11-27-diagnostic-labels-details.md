# diagnostic labels/details wire

- 状態: 採用（実装・検証した — [検証](../sketch/2026-11-27-wave50-lane-c-diagnostic-wire-verify.md)）
- 日付: 2026-11-27
- Supersedes: [e0301-text-missing-patterns](2026-11-27-e0301-text-missing-patterns.md) のうち E0301 限定で text emitter が `data.missing_patterns` を解釈する例外

## 決定

`diag.Diagnostic` に汎用の `labels` と `details` を足す。

- `labels` は `{ span, message }` の列で、主 span を説明する副 span を運ぶ。text 表示では副 span の snippet として描く
- `details` は `{ label, lines }` の列で、人間向けの詳細行を運ぶ。text 表示では `<label>:` の detail block として描く
- `data` は引き続き機械可読な構造データの正とし、text renderer は `data` の鍵を診断コードごとに解釈しない。人間向けに見せる情報は、生成側が `details` へ rendering-ready な形で添える

E0301 はこの wire を使い、`missing_patterns` を `patterns not covered` detail として出す。E0401 の代表利用として、let 注釈と関数 return 注釈が直接 supply した expected type との mismatch には、注釈位置を指す `expected type declared here` label を付ける。

## 背景

wave49 lane D では spec §4 の例に合わせ、E0301 だけ `data.missing_patterns` を text emitter が解釈する例外を入れた。これで実害は解消したが、format layer が診断コード固有の data key を知る構造だった。また E0401 では expected type が注釈由来でも、出力には mismatch 位置だけが出て、注釈そのものの位置を辿れなかった。

## 実装

- JSON wire と `gom schema` に optional な `labels` / `details` を追加する。既存 consumer を壊さない追加なので schema version は 1 のまま
- text renderer は generic に `labels` / `details` を描く。`data` は描かない
- pattern checker は E0301 に `missing_patterns` data と同じ反例を detail として添える
- checker は let 注釈・関数 return 注釈の直接の conformance mismatch に限り、注釈 span を secondary label として添える。if/match の腕や呼出し引数へ expected type が伝播した場合の由来追跡は、この lane の scope 外

## 検証

- `internal/diag`: labels/details の JSON wire、text rendering、validation
- `testdata/spec/E0301_nonexhaustive_match`: E0301 が data に加えて `patterns not covered` detail を出す
- `testdata/spec/E0401_expected_type_label`: let 注釈と関数 return 注釈が E0401 の secondary label になる
