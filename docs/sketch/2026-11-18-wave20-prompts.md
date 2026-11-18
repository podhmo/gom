# Wave 20 orchestration prompt drafts

State as of 2026-11-18: Wave 19 lanes all up —
cons-recursion 特殊化 (#46), `gom run` (#47), `concat` @extern
化 (#48), hygiene sweep (#49). podhmo merges (chained — bottom
up). This file drafts the prompts for the next wave, following
`2026-11-18-wave19-prompts.md`. Always append the "Common
instructions" block to each prompt.

## Execution model (unchanged from wave 19)

Quota constraints: **no child sessions** — one Devin session
executes every lane sequentially. Review is delegated to Devin
Review on each PR. podhmo merges.

- **One lane = one branch = one PR**, chained on the previous
  lane's branch while earlier PRs are unmerged.
- **Ledger edits ride in the lane's own PR** (TODO / gaps /
  open-questions check-offs for that lane's work only).
- **LSP verification: no Emacs/Eglot** — in-process JSON-RPC
  tests asserting publishDiagnostics payloads are sufficient
  (podhmo ruling, wave-15).
- **Sequential order below.**

## Orchestration notes (Wave 19 lessons)

- **`go run` ラッパーは exit code を潰す** — 子プロセスの
  終了コードが価値になる経路（`gom run`）は `go build -o`
  + 直接 exec にする。signal kill は `ExitCode()==-1` が
  os.Exit で 255 に化けるため `syscall.WaitStatus` から
  128+signal を返す。
- **index/oracle 系は空マップを渡さない** —
  `NewGoPosIndex` に空の source map を渡すバグは fold を
  沈黙させる。経路を新設したら「実データが流れる」回帰
  テスト（z.go の //line エラー → .gom span）を同時に置く。
- **spec-visible な変更は supersession decision が要る** —
  先行 decision が「X は変えない」と明記している箇所を変え
  たら Supersedes 参照の新 decision を起こす（concat で
  Devin Review flag）。台帳の未決記述も同じ PR で解消する。
- **ledger は PR ごとに腐る** — 「残件: X はない」行は
  実装 PR の中で消す。wave19 は review flag で拾われた。

## Lanes

### 1. N-ary Go 結果 (3+) — `devin/wave20-nary`

```
[decide+impl] open-questions の未決: 3 個以上の戻り値を持つ
Go 関数は現在 E0501 で非公開（spec §8.3/§8.4 に上限明記済
み — `strings.Cut` 等が実質使えない）。admission を広げる:

  (a) 採択済み: 3+ 結果を `Tuple a b c` に写し、末尾が
      `error` の形（`(T, U, error)`）は `Result (Tuple T U)
      GoError` に畳む — 既存の `(T, error)→Result` 規則の
      自然な拡張。エラー位置規則（末尾のみ or 任意）・
      `Option` 畳み込みの有無・`Go.FuncN`（spec §8.4 の
      可変長記述、現在結果 0..2）の上限撤廃の整合は spec で
      確定させる。
  (b) record 生成に写す — 不採用（podhmo 採択）。
  (c) 現状維持を pin — 不採用。
Ownership: internal/goimport (mapping/admission),
internal/check (E0501/E0509), internal/codegen (境界変換),
internal/eval, docs/spec §8.3/§8.4, testdata/spec, golden.
```

### 2. curried HOF の flat 二面公開 / 呼出側 uncurry — `devin/wave20-hof-flat`

```
[decide+impl] open-questions「生成コード特殊化ポリシー
（curried HOF 側）」の残り半分。`Fold` のような HOF が
要素ごとにクロージャを確保する — wave18 で cons 構築側は
buffer 化したが、呼び出し規約側の特殊化は未着手:

  採択済み: (c) まずベンチ計測して価値を判定してから実装
  形を決める — `Fold` の要素ごとクロージャ確保を wave18
  の計測形（確保数・ns/op）で pin し、価値が出たら (a)
  flat 二面公開 か (b) 呼出側 uncurry を選ぶ。計測結果が
  実害なしなら記録だけで閉じる。
Ownership: internal/lower, internal/codegen, stdlib
(member table 生成), docs/design, testdata/golden.
```

### 3. `gom lsp -go` の vet 半分 + §5-6 並列化評価 — `devin/wave20-lsp-vet`

```
[impl] 台帳 open gap「LSP 側の `go build`/`go vet` 実起動
経路」の vet 半分を閉じる: `gom lsp -go` は現在 `go build`
のみ — `go vet` も opt-in で走らせ `GoToolDiagnostics`
（tool="go vet"）で `.gom` span へ fold する（`gom build
-vet` と同じ経路・同じ tool 名）。フラグ形は採択済み:
`gom lsp -go` に vet を含める（1 フラグで build+vet 両方
実行）。spec の lsp 節と台帳を同じ PR で更新する。

あわせて design §5-6 の並列化ルール（P4-7 残件: 独立 dir
の並列コンパイル）を評価 — 実装するほど小さくないなら
残件であることを台帳に明記して lane を閉じる。
Ownership: cmd/gom/lsp.go, internal/lsp, internal/build,
docs/implementation-gaps (open gap 閉じ), spec の lsp 節.
```

### 4. hygiene — `devin/wave20-hygiene`

```
[impl] 恒例の低重心掃除:

(a) problems/ 未消化項目の台帳化判定 — generic-go-fns-
    hidden / ctor-typeargs-unvalidated / rune-char-
    correspondence など、implementation-gaps か
    open-questions への登録要否を 1 件ずつ判定。
(b) wave19 review 起因のパターン sweep: 「index/oracle
    に空マップ」「exit code 伝播の欠落」「spec-visible
    変更の supersession 欠落」の同型残りを grep。
(c) LSP shared importer は shelf 送り確定 — open-questions
    の記述は wave19 lane4 で更新済みだが、shared-importer
    案と per-change コストの数字が一貫しているか確認。
(d) implementation-gaps / TODO / open-questions の
    check-off 漏れ sweep（wave19 分: cons-spec・gom run・
    concat・hygiene 各 PR マージ後の閉じ忘れ確認）。
Ownership: docs/*, internal/lsp, internal/check.
```

## Lane candidates left on the shelf

採用しなかった候補（順位づけは podhmo 次第で後 wave に送れる）:

- **LSP shared importer + member table 共有** — wave19 で
  経路設計済み（長寿命 `goimport.Loader` + dep fingerprint
  invalidation）。podhmo 後送り指定。実装時は per-change
  コスト計測（≈48–50ms のうち export parse+member table が
  数 ms）の before/after を pin。
- **design §5-6 並列ビルドルール** — P4-7 残件。独立 dir
  の並列コンパイル。lane 3 で評価だけ残る。
- **共有スライス View 型** — `problems/shared-slice-view-type`
  のコピーしない部分ビュー。言語面の新構文・新メンバー。
- **`Result.to_option` / `Option.to_list`** — Go 循環参照
  禁止（stdlib layering 制約）。別モジュール化が要るかの
  検討。
- **LSP completion/definition/references** — v1 scope 外の
  機能群（TODO）。大規模なので専用 wave 向き。
