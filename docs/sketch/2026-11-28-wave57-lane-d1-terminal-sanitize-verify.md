# wave57 lane D1: diagnostic snippet terminal control sanitize 検証

- 日付: 2026-11-28
- lane: D1（小さい実装修正）

## 実施内容

text renderer の source snippet 表示で、terminal-active な制御 rune を renderer-wide に sanitize した。C0 control は Unicode control picture（ESC は `␛`）、DEL は `␡`、C1 control は `�` に置き換える。tab は従来どおり 4 空白へ展開する。replacement を 1 rune に保つため、既存の code-point column accounting と caret alignment は変えない。

## red-first

`TestWriteTextSnippetSanitizesTerminalControls` 追加後、実装前は ESC (`\x1b`) と DEL (`\x7f`) が text output へそのまま残り red だった。実装後は同じテストを含む `go test ./internal/diag -run TestWriteText -count=1` が pass した。

## fixture

- `TestWriteTextSnippetSanitizesTerminalControls`: source snippet 内の ESC/DEL が control picture へ置き換わり、raw control rune が出力へ残らないこと、caret alignment が変わらないことを固定

## 文書同期

- `docs/spec.md` §12: text source snippet の制御文字 replacement を更新
- `docs/design.md` 2.3: renderer-wide sanitize の責務を更新

## 残件

message/details/note/fix title など診断テキスト自体の sanitize は対象外。今回の root cause は source snippet 経路で、review 指摘も renderer-wide の snippet handling だった。
