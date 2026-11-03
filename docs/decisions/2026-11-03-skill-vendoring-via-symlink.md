# `.devin/skills` への skill 公開は symlink で行う

- 状態: 採用
- 日付: 2026-11-03
- Supersedes: [agent-skill-example-verification](2026-11-03-agent-skill-example-verification.md) のうち `.devin/skills/` への vendor を手動コピー + `make vendor-skills` で行うという方式

## 背景

P4-9 は `skills/gom-agent-guide/` を canonical として `.devin/skills/` へ実ファイル複製（vendor）し、`make vendor-skills` + 一致テストで二重管理を防ぐ設計だった。ユーザーの要求により、二重管理自体をやめて symlink に変更した。

## 案

- `.devin/skills/gom-agent-guide` → `../../skills/gom-agent-guide` の相対 symlink。複製が存在しないため drift は起きない。
- `Makefile` の `vendor-skills` ターゲットを廃止。
- `skills/snippets_test.go` の vendor 一致テストを `TestSkillSymlinks` に置き換え、`skills/<name>/` ごとに `.devin/skills/<name>` が canonical ツリーへ解決される symlink であることを検査する（skill を追加しても自動で検査対象になる）。

## 検証

`go test ./skills` が symlink 解決の検査を含めて pass。`.devin/skills` 経由の skill 発火は引き続き動作する（Devin が symlink を辿って SKILL.md を読むことを本セッションの available_skills で確認済み）。
