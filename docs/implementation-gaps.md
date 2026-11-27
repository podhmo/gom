# 仕様・実装・文書のずれ

このリポジトリで見つけた「こことここがずれている」を残す台帳。
セッションをまたいで使う。仕様と実装だけでなく、文書同士、評価器と
生成コード、説明とテストのずれも含む。この台帳自体は規範ではない。

## 使い方

- 見つけたら末尾に `- [ ] **短い説明**` と該当箇所へのリンクを足すだけでよい。
  原因・優先度・修正方針・担当・番号付けは登録の条件にしない。
  未確認なら「未確認」と書いて登録してよい。既存項目があれば追記する。
- 解消したら `[x]` にし、「何を合わせたか」と検証・変更へのリンクを1行足す。
  作業中という理由だけでは解消扱いにしない。`[x]` 項目は定期的に
  [implementation-gaps-archive.md](implementation-gaps-archive.md) へ移し、live 台帳には未解消だけを残す。
- ここで正しい側を決めない。規範の所在は [AGENTS.md](../AGENTS.md) に従う。
  決定が必要なら decisions の draft → prototype → 検証の手順を使う。
- 実装する作業は [TODO.md](../TODO.md)、詳しい懸念・検証は problems / sketch に置き、
  台帳からリンクする。同じ説明を長く複製しない。台帳だけのメモでもよい。
- 未解消だけを探すときは `rg '^- \[ \]' docs/implementation-gaps.md`。

追記例: `- [ ] **短い説明**: A にはこう書かれているが、B ではこうなっている。`
次の行に `[仕様](spec.md)`、`[実装](../internal/goimport/mapping.go)` などの参照を足す。

## 記録

以下は 2026-11-03 の実装・実行検証で発見、または関連資料から確認したもの。
未解消項目は網羅的な未実装機能一覧ではない。


- [ ] **stack を持つ error 値の生成が書けない（`pkg/errors` 系）**: `fmt.Errorf("ctx: %w", e)` のユーザーコードは生成される Go-call で問題なく通る（[errwalk fixture](../testdata/realworld/errwalk/main.gom)）が、`%+v` で stack trace を出す `pkg/errors.Errorf` 相当の「stack を持つ error 値」の生成には `@extern` shim が要った — `%+v` 出力自体は生成コードで動くが、stack を持つ値の作成側だけ shim（selfish 移植の観測 — [sketch](sketch/ja/2026-11-24-selfish-port.md)、wave43 lane F）

- [x] **静的な期待型のない無名レコードの `List.fold` acc は型推論が通らない（E0407）**: wave50 lane E で戻り値など静的に分かる record 期待型がある場合は解消（[検証](sketch/2026-11-27-wave50-lane-e-record-context-verify.md)）。named type がどこにもない ad hoc shape は named type（`type GistAcc = {...}`）が必須のままだが、wave51 lane E で structural record は不採用・named record 必須を仕様の正として維持すると決着した（[decision](decisions/2026-11-27-labeled-lambda-record-seeding.md)、[調査](sketch/2026-11-27-wave51-lane-e-labeled-records-verify.md)）

- [x] **match の到達不能 arm は `todo ()` に頼る**: catch-all `action ->` の内側で sub-match する形になると網羅性チェックの恩恵が薄れる（selfish 移植 — 同上 sketch）。wave51 lane B で spec/decision の採択範囲どおり same-scrutinee root constructor narrowing を実装し、E0301 のみへ適用することで解消（[decision](decisions/2026-11-27-match-nested-narrowing.md)、[検証](sketch/2026-11-27-wave51-lane-b-match-narrowing-verify.md)）。guard 条件解析や contextual E0302 は仕様上の対象外として残る

- [x] **E0204/E0503 は live code table にあるが production emitter がない**: `gom explain` / [error-codes](error-codes.md) はユーザー向けエラーとして説明するが、E0204 の unknown attribute は現状 parser が E0101 を出し、E0503 implicit numeric conversion の emitter は存在しない。wave52 lane E で E0204 を parser production diagnostic とし、E0503 を retired にした（[decision](decisions/2026-11-27-diagnostic-direction-e0204-e0704-e0503.md)、[検証](sketch/2026-11-27-wave52-lane-e-diagnostics-verify.md)）

- [x] **E0704 の例は parser では E0101 になる**: [error-codes](error-codes.md) / `gom explain` は destructuring select receive arm（`Some job <- jobs`）を E0704 の例として示すが、現状の parser は receive arm の左辺を identifier / `_` だけに制限して先に E0101 を出す。E0704 は checker recovery / typed-AST 経路の診断なので、現行では利用者ソースから到達しない。wave52 lane E で parser が receive arm 左辺の pattern を保持し、checker の E0704 へ到達するようにした（[decision](decisions/2026-11-27-diagnostic-direction-e0204-e0704-e0503.md)、[検証](sketch/2026-11-27-wave52-lane-e-diagnostics-verify.md)）

- [x] **agent guide の `Ref` / `Array` 説明が実装とずれていた**: `references/language-fundamentals.md` は `Ref.*` / `Array.*` の member table が空で E0201 になると説明していたが、現行は compiler-registered helper として提供される。wave51 lane F で compiler-registered helper の意味制約を書く形へ修正した（[検証](sketch/2026-11-27-wave51-lane-f-agent-skill-verify.md)、[guide](../skills/gom-agent-guide/references/language-fundamentals.md)）

- [x] **`gom init` が workspace 解決した依存の checksum を project 側へ残さない**: 既存 Go module への `gom init` は caller の workspace で `go mod download` できれば成功するが、checksum は `go.work.sum` 側へ入り、init 先に `go.sum` が残らないことがある。その project を workspace 外で build すると read-only mode の Go tool が checksum 不足で失敗する。wave52 lane B で init 時の toolchain 解決を `GOWORK=off` へ固定し、standalone に解決できない場合は actionable error にした（[decision](decisions/2026-11-27-init-standalone-checksum.md)、[検証](sketch/2026-11-27-wave52-lane-b-init-checksum-verify.md)、[spec](spec.md)、[実装](../cmd/gom/scratch.go)）

- [ ] **pipe 経由の `Result.map` lambda 内の無名 record literal は戻り値期待だけでは推論できない**: spec §3.3 は generic な適用・pipe で結果の期待型を speculative にパラメータへ伝えると読めるが、`input.lines |> List.traverse_result validate_line |> Result.map (fn lines -> { id = input.id, lines = lines })` は record literal が E0407 になる。同時に `expected ValidatedOrder, got Unit` の E0401 が出て、record 未解決に伴う二次診断が分かりにくい。`ValidatedOrder { ... }` の型名前置 literal、または直接 `Ok { ... }` を返す形では回避できる。wave52 lane D で実験更新中に確認（[spec](spec.md)、[experiment](../experiments/dmmf-fit/main/main.gom)、[lane D 検証](sketch/2026-11-27-wave52-lane-d-dmmf-fit-verify.md)）
