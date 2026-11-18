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

### B. `Ptr T` field 書き込みの実装 — `devin/wave24-struct-field-write`（podhmo 採択済み 2026-11-19: (b) 採用 + host 撤去、(a) 先延ばし）

```
[impl] TODO G-1: `ptr.Field = v` 代入構文と addressability
規則を実装。`Ptr T`（T は named Go struct）経由に限定し、
record/`Ref` への field 代入は開かない（decision
2026-11-19-struct-field-write）。spec 8.3 節「field 代入と
コンポジットリテラルは提供しない」を更新 → testdata/spec +
golden → tutorial/examples の host パッケージ（NewClient
factory）を撤去して Ptr.new + field write で書き換え検証。
composite literal (a) は対象外（要検討で先延ばし）。
Ownership: syntax/resolve/check/lower/codegen + testdata +
examples。
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

### D. 辞書渡し / 型クラス（採択済み — podhmo 判定 2026-11-19）

辞書渡しを採用、型クラスは不採用（[decision](../decisions/2026-11-19-ad-hoc-poly-dict-passing.md)）。
decision 追加・open-questions 決着・イディオムガイド
（`skills/gom-agent-guide/references/ad-hoc-polymorphism.md`）は
wave23 lane3 のブランチで反映済み。

残る後続作業の候補（採否は podhmo）:

```
[docs-or-stdlib] 辞書渡し採用の後始末（残りがあれば）:
- stdlib への Eq/Ord 辞書追加の要否の判断と追加
  （experiments/w23/typeclass/dict を参考、辞書引数は先頭）
- deriving 相当の自動生成は open-questions の future work へ
  移行済み — 着手するなら別 lane
Ownership: wave24 以降、必要なら。
```

### E. 非 cons 蓄積の救済 — **wave23 で消化済み**

podhmo 採択（2026-11-19、[decision](../decisions/2026-11-19-noncons-accum-policy.md)）:
β イディオム誘導 + γ 警告 lint を採用し、γ は wave23 lane4 で実装済み
（`W0801`、lower の `lintQuadraticAccum`）。α SCC 拡張と δ 表現変更は
先送りで problems ファイルに残す。wave24 の lane としては不要 — α を
再検討するなら別途 lane を立てる。

## 申し送り

- CI は push-to-main / PR-to-main synchronize でしか走らない —
  チェーン PR は Devin Review のみ。マージは podhmo が
  retarget → `gh pr merge --merge` を下から順に。
- 採択されなかった候補の sketch/experiments は記録として残す
  （decision から却下理由つきで参照）。
