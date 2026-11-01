# CLAUDE.md

Gom は Go へトランスパイルする関数型言語。コンパイラは Go で実装する。

## ドキュメントの正

| ファイル | 内容 | 正となる範囲 |
| --- | --- | --- |
| `docs/spec.md` | 言語仕様 | 利用者から見える振る舞い（構文・型・エラーメッセージ形式） |
| `docs/design.md` | 実装方針と理由 | コンパイラ内部、コード生成、ディレクトリ構成 |
| `docs/decisions/` | 決定の記録（1 件 1 ファイル） | なぜそう決めたか |
| `docs/open-questions.md` | 未決事項と future work | まだ決まっていないこと |
| `TODO.md` | 作業一覧 | 決まっていて手を動かすだけのこと |

spec と実装が食い違ったら、勝手に実装に合わせて spec を書き換えず、食い違いを報告する。

## ドキュメント更新のルール

- 未決事項が決着したら: `docs/decisions/NNNN-<slug>.md` を追加 → spec.md / design.md を更新 → open-questions.md から消す → 作業が生じたら TODO.md に追加
- 決定を覆すときは、元の decision を消さず、新しい decision から「Supersedes」で参照する
- 「ユーザーのコードの書き方や意味が変わるなら spec、変わらないなら design」

## 実装のルール

- フェーズ間の依存は一方向: `syntax` → `resolve` → `check` → `pattern` → `lower` → `codegen`。`diag` と `types` は共通部品
- `codegen` に言語の意味を判断させない。評価順、arity と呼び出し規約、Go 相互運用の変換、脱出境界、型の具体化は Core IR で確定させる（docs/design.md 2.1 節）
- 型付き AST の適用ノードは呼び出し情報を構造化して持つ。シンボル名や AST の見た目から呼び出し方法を判定し直さない
- 内部 AST は sealed interface＋type switch で表す。type switch には `default: panic("unreachable")` を付ける
- 診断は必ず `internal/diag` の内部表現として作る。出力形式（JSON / text / LSP）はコンパイラ本体から見えない。位置の正は UTF-8 のバイトオフセット。`message` は 1 行の英語、詳細は `data` に構造化する。エラーコードは再利用しない
- 仕様の規則を実装したら、`testdata/spec/` に規則ごとの `.gom` と期待する診断 JSON を足す
- 生成コードの変更は `testdata/golden/` を更新して差分を確認する
- Go 相互運用を変えたら、両方向（Gom → Go、Go → Gom）のテストを足す。`nil`、スライス、named type、interface を忘れない
- 生成コードは「読めて、デバッグでき、Go から使える成果物」。人が直接保守するソースとしての書き味のために表現方式を縛らない
