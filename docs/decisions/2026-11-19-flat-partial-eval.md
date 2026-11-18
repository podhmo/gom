# flat 宣言パラメータの部分適用 — eval 側を遅延させて divergence を消す

- 状態: 採用（wave22 prompts で podhmo 採択済み。実装・検証した — `TestEagerFnChunkTiming` を新仕様に更新、spec.md §1.3 の注記を解消）
- 日付: 2026-11-19
- 関連: [flat-fn-params](2026-11-18-flat-fn-params.md)（flat 呼出規約本体。残存差として本件を記録していた）
- 出典: wave22 lane3 prompts（`devin/wave22-flat-partial`）

## Context

flat 呼出規約（`fn g (f: A -> B -> C)` の `f` は生成コードで
`func(A, B) C` で渡る）には、生成コードと eval の間に発火
タイミングの差が残っていた:

- **生成コード**: `f a` の部分適用は residual（`func(b) { f_(a, b) }`）
  を返すだけ — 被呼側の本体は flat arity を満たす適用まで走らない
- **eval（canonical 表現）**: `f a` は被呼側の最初のチャンクを直ちに
  発火していた。`f` に副作用を持つ関数を渡すと観測可能な差になる

podhmo 採択（wave22 prompts）: 「生成コードの発火タイミングを正として
eval を合わせる」— eval 側に residual partial の機構を移植する。

## Decision

eval では、flat パラメータ枠に束縛される関数値を **facade** で包む
（`internal/eval/call.go` の `flatSlot`）:

- どのスロットが flat かは型ではなく **宣言由来のマーカー** で決める:
  `fn` 宣言のパラメータ型の flat arity が 2 以上なら `fnValue` が
  VFunc の `flatArity` にスロットごとの arity を記録する。lower が
  `l.flat` を `funcDecl` でしか立てないのと同じく、lambda リテラル・
  builtin・ctor の VFunc はマーカーを持たず canonical のまま —
  `let stg = fn f -> f 20` のような lambda パラメータに渡した関数は
  従来どおり最初のチャンクを即座に走らせる（生成コードと一致）
- マーカー付きスロットに束縛される VFunc は `fnBuiltin` facade で包み、
  そのシグネチャをパラメータの flat 型全体（`evalFlatSig` — lower の
  `flatSig` と同じ flat 化規則）にする
- 部分適用は facade のスロットが埋まるまで partial VFunc を返し続ける
  （= 生成コードの `closureChain` residual 相当）
- flat arity を満たした時点で builtin が内側の canonical 値へ
  引数を 1 本ずつ `applyOne` する — 内側は自身のチャンク刻みを
  保ったまま、外側からは一度の flat 呼出として発火する

束縛点は `applyArgs` の直接スロット埋めと、label 適用の eta 残尾部
の 2 箇所。`applyArgs` を通る全経路（位置引数・ラベル引数・deep arg
経由）が同じ規則を共有する。

## 効果

- `let g = stage make` — `f 20` は partial のまま作用を走らせない
- `(stage make) 0` — flat arity を満たし make の第 1 チャンクが発火
- facade の入れ子（flat スロットから flat スロットへの受け渡し）は
  そのまま動く: 内側 facade が partial を返すので、実体の関数値には
  全引数が揃ってから届く

## 検証

`internal/build/eager_test.go` の `TestEagerFnChunkTiming` — eval 側を
新仕様へ更新: `answer`（residual 未適用）は `"survived"` を返し、
`probe`（`(stage make) 0` で flat arity 充足）は todo の panic を出す。
生成バイナリ側の発火位置は従来どおり（docs/sketch
[flat-partial-eval-verify](../sketch/2026-11-19-flat-partial-eval-verify.md)）。
