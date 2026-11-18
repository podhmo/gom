# Wave 24 orchestration prompt drafts

State as of 2026-11-19: wave23 の 4 lane（#66–#69）を PR 済み・
チェーンでマージ待ち。wave23 は全調査 wave — lane ごとの採否は
podhmo がここで決める。このファイルは wave24 の prompts 草案。
実行モデルは wave23 と同じ前提（単独実行・Devin Review・
podhmo がマージ）。lane 順は下からチェーンする。

## 確定済み（判定不要）の候補

### A. `CtorApp`/`CtorRef` `TypeArgs` 廃止 — `devin/wave24-ctor-targs-remove`

```
[impl] open-questions の TypeArgs (b) 廃止は podhmo 採択済み
（2026-11-19）。wave22 lane4 の棚卸し（sketch
2026-11-19-ctor-targs-b-inventory）どおり、emit 8 経路の
TypeArgs 転記と core 型のフィールドを撤去し `Typ` の
`Con.Args` を唯一の正にする。~150 行削除中心・golden 不変見込み。
decision 追加（採択済み・Supersedes/実装記録）、該当
open-questions 項を閉じる。testdata/golden は差分を確認。
Ownership: internal/check (emit), internal/core,
internal/lower/codegen (TypeArgs 参照部), testdata/golden,
docs/decisions, docs/open-questions (閉じ).
```

## wave23 採否待ち（podhmo が lane ごとに採択/却下）

### B. struct 構築（lane1 の採択結果を反映）

```
[impl] wave23 lane1（PR #66）の候補から podhmo 採択分を実装。
(a) composite literal なら syntax→codegen の新構文、
(b) field write なら代入規則と不変条件（immutability lint・
handle 型の可変性契約）、(c') のみなら言語変更なしで
docs/examples への適用のみ。decision
2026-11-19-struct-construction-options（draft）を採択内容に
更新 → spec.md/design.md → testdata/spec + golden →
examples/gist-cli の NewClient を採択機構で書き換え検証。
Ownership: 採択候補に依存（構文系は syntax/resolve/check/
lower/codegen + testdata、c' は examples + docs）。
```

### C. LSP 全削除 — `devin/wave24-lsp-removal`（podhmo 採択済み: 「未対応」という体で削除）

```
[impl] LSP 面を v1 の提供から取り下げて「未対応」に戻す（podhmo
方針: 不要コードの除去ではなく、必要な状況が来るまで提供しない）。
棚卸し（sketch 2026-11-19-wave23-lsp-removal）の 6 ステップ順に
internal/lsp・cmd/gom/lsp.go・main.go・main_test・各 docs を
削除し、lsp-v1-scope / lsp-package-unit / lsp-gotool-invocation
を Supersede する decision を追加 — 文言は「需要が具体化したら
別 decision で再導入を検討する」体。gotool-position-inverse・
SourceMap/GoPosIndex・cli -go は残す。SKILL.md の toolchain
表と spec.md から lsp 記述を除去。
Ownership: internal/lsp, cmd/gom, docs/spec.md, docs/design.md,
docs/decisions, docs/open-questions, TODO.md,
skills/gom-agent-guide.
```

### D. 辞書渡し / 型クラス（lane3 の採択結果を反映）

```
[impl-or-docs] podhmo 採択分:
- 辞書渡し採用なら: 言語変更なし。stdlib への Eq/Ord 辞書
  追加の要否と gom-agent-guide のイディオム化だけを実施
  （experiments/w23/typeclass/dict を参考）。decision を採択に
  更新して open-questions を閉じる。
- 型クラス風採用なら: instance 解決・coherence 規則・.gomi
  表現の設計 lane が先（spec 設計 → 実装は wave25 以降）。
- 現状維持なら: open-questions の項を「辞書渡しで済ませる」
  決定として閉じる decision だけ追加。
Ownership: 採択に依存。
```

### E. 非 cons 蓄積の救済（lane4 の採択結果を反映）

```
[impl-or-docs] podhmo 採択分（sketch
2026-11-19-wave23-noncons-accum の α–δ、複数可）:
- α SCC 拡張: markConsCalls を相互再帰に広げる内部実装。
- β Array イディオム: guide/examples のみ、Array builder の
  書き味を experiments/w23/noncons-accum 上で確認してから。
- γ 診断ヒント: 除外形の検出 + 情報診断の診断族設計。
- δ 表現変更: future work のまま保留が既定。
problems ファイルの状態行を採択結果で更新。
Ownership: α→internal/lower/consrec.go + golden、β→docs +
skills、γ→internal/lower or check + diag 定義。
```

## 申し送り

- CI は push-to-main / PR-to-main synchronize でしか走らない —
  チェーン PR は Devin Review のみ。マージは podhmo が
  retarget → `gh pr merge --merge` を下から順に。
- 採択されなかった候補の sketch/experiments は記録として残す
  （decision から却下理由つきで参照）。
