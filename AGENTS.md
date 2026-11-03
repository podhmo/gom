# CLAUDE.md

Gom は Go へトランスパイルする関数型言語。コンパイラは Go で実装する。

## ドキュメントの正

| ファイル | 内容 | 正となる範囲 |
| --- | --- | --- |
| `docs/spec.md` | 言語仕様 | 利用者から見える振る舞い（構文・型・エラーメッセージ形式） |
| `docs/design.md` | 実装方針と理由 | コンパイラ内部、コード生成、ディレクトリ構成 |
| `docs/decisions/` | 決定の記録（1 件 1 ファイル） | なぜそう決めたか |
| `docs/open-questions.md` | 未決事項と future work | まだ決まっていないこと |
| `TODO.md` | 作業一覧（`.claude/skills/todo-md` の規則に従う） | 決まっていて手を動かすだけのこと、明らかなバグ |
| `docs/sketch/` | 調査・検証のレポート | 規範ではない記録 |
| `docs/problems/` | 懸念事項（1 件 1 ファイル） | まだどちらとも言えない懸念 |
| `docs/implementation-gaps.md` | 仕様・実装・文書のずれの台帳 | 発見したずれと解消状況の記録。規範ではない |

仕様・実装・文書の記述のずれを見つけたら、大小や原因の確定を問わず `docs/implementation-gaps.md` に短い説明と該当箇所を追記する。既存項目があればそこへ情報を足す。勝手に実装に合わせて spec を書き換えない。解消時は台帳のチェックを付け、対応内容と検証へのリンクを追記して履歴を残す（書式は台帳冒頭）。

## `.gom` を書くとき

Gom のコード（`.gom` / `.gomi`）を書く・読む・直すときは、**最初に `$gom-agent-guide` を読む**こと。`skills/gom-agent-guide/` に canonical なスキルがあり、`.devin/skills/` に vendor してある（同期は `make vendor-skills`、一致は `skills/snippets_test.go` が検査する）。LLM の事前知識に Gom はほぼ存在しないため、OCaml / F# / Go の習慣で書くと構文を間違える — スキルに言語ツアーと「ありがちな間違い」集、ツールチェーンの使い方がまとめてある。この AGENTS.md はコンパイラ開発者向けの文書であり、言語知識はスキル側に置く（設計: [docs/sketch/2026-11-02-agent-skill-for-gom.md](docs/sketch/2026-11-02-agent-skill-for-gom.md)）。

## ドキュメント更新のルール

- 未決事項が決着したら: `docs/decisions/YYYY-MM-DD-<slug>.md`（日付は作成日。以後ファイル名は変えない）を追加 → spec.md / design.md を更新 → open-questions.md から消す → 作業が生じたら TODO.md に追加
- 決定を覆すときは、元の decision を消さず、新しい decision から「Supersedes」で参照する
- 「ユーザーのコードの書き方や意味が変わるなら spec、変わらないなら design」
- 実装中に決めたことは `状態: draft` の decision として追加し、prototype で検証して結果を `docs/sketch/` に書いてから状態を更新する（docs/decisions/README.md）
- 遠い将来のタスクは TODO.md ではなく open-questions.md の Future work に書く

## 実装のルール

- Go は 1.27 以上。完了前に `gofmt -l .`（出力が空）、`go vet ./...`、`go test ./...` を通す
- フェーズ間の依存は一方向: `syntax` → `resolve` → `check` → `pattern` → `lower` → `codegen`。`diag` と `types` は共通部品
- `codegen` に言語の意味を判断させない。評価順、arity と呼び出し規約、Go 相互運用の変換、脱出境界、型の具体化は Core IR で確定させる（docs/design.md 2.1 節）
- 型付き AST の適用ノードは呼び出し情報を構造化して持つ。シンボル名や AST の見た目から呼び出し方法を判定し直さない
- 内部 AST は sealed interface＋type switch で表す。type switch には `default: panic("unreachable")` を付ける
- 診断は必ず `internal/diag` の内部表現として作る。出力形式（JSON / text / LSP）はコンパイラ本体から見えない。位置の正は UTF-8 のバイトオフセット。`message` は 1 行の英語、詳細は `data` に構造化する。エラーコードは再利用しない
- 仕様の規則を実装したら、`testdata/spec/` に規則ごとの `.gom` と期待する診断 JSON を足す
- 生成コードの変更は `testdata/golden/` を更新して差分を確認する
- Go 相互運用を変えたら、両方向（Gom → Go、Go → Gom）のテストを足す。`nil`、スライス、named type、interface を忘れない
- 生成コードは「読めて、デバッグでき、Go から使える成果物」。人が直接保守するソースとしての書き味のために表現方式を縛らない
