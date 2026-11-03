# gom-agent-guide の例示コードを抽出テストで検証する方式

- 状態: 採用
- 日付: 2026-11-03

## Problem

`skills/gom-agent-guide/`（P4-9, 設計:
[docs/sketch/2026-11-02-agent-skill-for-gom.md](../sketch/2026-11-02-agent-skill-for-gom.md)）
に置く Gom のコード例は、コンパイラが変わるたびに静かに腐る
リスクがある。MoonBit の `moonbit-agent-guide` は `SKILL.mbt.md` を
CI で `moon check` して腐敗を防いでいる。Gom には literate
markdown の仕組みがないため、同等のことを自前で行う必要がある。

## Proposal

`skills/snippets_test.go`（`package skills_test`）が
`skills/gom-agent-guide/**/*.md` から fenced code block を抽出し、
`build.Compile` の `CheckOnly` 経路（`gom check` と同じ配線:
`cmd/gom/cli.go` の `compileOptions` と同じ goimport ローダ）で
コンパイルする。commit する抽出済み fixture は作らず、毎回
メモリ上で検証する（抽出物を二重管理しないため）。

フェンスの info string で期待結果を宣言する:

| 形式 | 意味 |
| --- | --- |
| ` ```gom ` | エラー診断ゼロでコンパイルできること |
| ` ```gom fail ` | エラー診断が 1 つ以上出ること |
| ` ```gom fail=E0301 ` | そのコードの診断を含むこと |
| ` ```gom skip ` | 抽出するがコンパイルしない（断片・未実装機能） |
| ` ```gomi ` | インターフェースファイルとして check する |

- `package` 節を持たない `gom` ブロックは `package main` で包んで
  コンパイルする。reference の小さな宣言例をいちいち完全な
  ファイルに書き起こさなくて済む。
- `fail` 形式は「NG 例」を正直に保つ: 間違い例が本当に今日
  エラーになることを検証し続け、将来言語が受理するようになったら
  テストが落ちて例の陳腐化を検出できる。
- `go` ツールチェインが PATH に無い環境では `import go` を含む
  スニペットのみ skip する（tutorial_test.go と同じ guard）。
- `.devin/skills/` への vendor は手動コピー + `make vendor-skills`
  で行い、canonical ツリーとバイト一致することを同じテストが検査
  する（moonbitlang/skills の skills.sources.json より軽い方法 —
  現状 consumer はこのリポジトリだけで、external sync は不要）。

## Verification result

- `go test ./skills` で 47 のスニペット subtest + vendor 一致
  subtest が全件 pass（2026-11-03 時点）。
- 負例として `fn oops () -> Int = "not an int"` ブロックを一時
  追加して FAIL（E0401 を検出）することを確認。`fail=E0xxx` の
  コード指定も NG 例 11 件で実働確認済み。
- 記録: [docs/sketch/2026-11-03-agent-skill-verification.md](../sketch/2026-11-03-agent-skill-verification.md)

## 関連

- タスク: TODO.md P4-9、設計正: sketch 2026-11-02-agent-skill-for-gom
- 実装: `skills/snippets_test.go`、`Makefile` の `vendor-skills`
