# Wave 31 orchestration prompt drafts

State as of 2026-11-22: wave30 の 5 lane（#112–#117、SC-2 脱出
検査 / SC-3 native defer / `@method` Go iface impl / `--strict`
設計 / defined-type 境界調査）は全て PR 化済み、Devin Review
反復・podhmo マージ待ち（stack #115）。

このファイルは wave31 の prompts 草案。テーマは診断系の仕上げ
+ 小粒の境界欠損。lane 順は下からチェーンする前提。採否は
prompts 確定時のインタビューで決める（`docs/ja/wave-process.md`
の流儀）。

## Lanes

### A. `--strict` + `//gom:allow` の実装 — `devin/wave31-strict-impl`

```
[impl] 採択済み decision docs/decisions/2026-11-22-strict-mode.md
の実装 lane。(1) `//gom:allow E0301` のようなディレクティブ行
（コメント位置の解析、対象診断の whitelist 判定）で局所的に
relaxable なエラーを警告へ下げる。(2) `--strict` CLI フラグで
警告をエラー化（exit 非ゼロ）。初期 whitelist は E0301
（非網羅 match）・E0302（到達不能 arm）のみ — pattern フェーズ
の局所回復可能な診断だけが対象。severity 全面の filtering は
decision で拒否済み。
Ownership: internal/diag, internal/pattern, cmd/gom,
docs/spec.md, docs/error-codes.md, testdata/spec, decision.
依存: wave30 lane D（設計のみ、コード依存なし）
```

### B. `Go.conv` の defined-type 変換 — `devin/wave31-goconv-defined`

```
[impl] 台帳の未解消項目: `Go.conv` が単一フィールド record
（`type UserId = { value: Int }` → Go defined type）と要素型の
相互変換を E0507 で拒否している。Go では `int64(u)` /
`UserId(7)` が合法なので、convOK の変換規則に defined-type
unwrap/wrap の経路を足すか調査・実装。named Go 型の既存 conv
規則（litRange・underlying kind）との一貫性が論点 — 許すなら
spec §8.5 に追記、許さないなら decision で棄却理由を記録して
台帳を [x] にする。小粒 lane。
Ownership: internal/check, internal/gomimport, docs/spec.md,
docs/implementation-gaps.md, testdata/spec, decision.
依存: なし
```

### C. `|?>` 自動ラップ文言のカスタマイズ調査 — `devin/wave31-pipe-wrap-frame`

```
[investigate] open-questions の未決項: `|?>` の wrap 文言
（関数名・ソース位置・任意文言）をパッケージ単位で指定する
方法か、ステップごとに文言を書ける構文か。フレーム
`rt.StepError{Step, Err}`・`step: message` 文言・`strconv`
二重名は provenance として容認済み
（decisions/2026-11-05-result-pipe-wrap-frame.md）。選択肢の
比較（属性・プラグマ・専用構文）を decision draft まで。
Ownership: docs/decisions, docs/sketch, internal/check（調査のみ）。
依存: なし
```

### D. scoped cleanup 拡張の調査 — `devin/wave31-scope-extend`

```
[investigate] open-questions の SC 拡張 2 件のうち優先する側を
決める: (a) `GoError` 以外のエラー型で cleanup 失敗を合流させる
版（結合関数を明示的に受け取る）、(b) 取得と登録を一度に書く
糖衣（`s.closing` 等）・専用ブロック構文。SC-3 native defer
着地後の構文面・lowering 面のコストを踏まえて decision draft
まで。「関数とメソッドだけの面で困ってから」という保留条件に
対して現状の不便度も評価する。
Ownership: docs/decisions, docs/sketch, stdlib/scope（調査のみ）。
依存: wave30 lane B（native defer 着地済み前提の評価）
```

### E. `Char` ↔ `Int32` / `rune` 境界の再検討 — `devin/wave31-rune-bridge`

```
[investigate] open-questions の保留項: Go の rune 引数 API には
`Int32` が要り `Char` は `Go.conv` 経由。problems/
2026-11-06-rune-char-correspondence.md のユースケース棚卸しと
「暗黙橋渡し・専用組み込み・現状維持」の比較を decision draft
まで。ユースケースが薄いなら現状維持で棄却記録。
Ownership: docs/decisions, docs/problems, internal/check（調査のみ）。
依存: なし
```

## 採択待ちの問い

- lane B は「許す実装」か「棄却記録」か — 実装コストが小さければ
  許す側が自然だが、defined type の nominal 性を破る入口にも
  なるので判断が要る
- lane C/D/E は全部 investigate-only でよいか、実装 lane にする
  ものがあるか
