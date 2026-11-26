# wave48 lane G: PR #256 owner improvement ideas の精査

- 日付: 2026-11-26
- 対象: [PR #256](https://github.com/podhmo/gom/pull/256) `docs/sketch/ja/2026-11-26-owner-improvement-ideas.md`（docs-only、20 件）
- コメント: Devin Review 6 件、podhmo 3 件を読み、本文の観測と突き合わせた
- 基準: wave48 lane F までの chain（`8597669`）。PR #256 の観測基点 `56a7e89` から wave48 lane A〜F 分だけ新しい

## 判定表

| ID | 判定 | 台帳への反映 |
| --- | --- | --- |
| U1 継続行の誤パース診断 | 観測は有効。ただし自動 fix は unsafe | open-questions へ登録 |
| U2 診断 labels/details | 有効。E0301 のずれは gaps 未登録だった | implementation-gaps + open-questions。既存 problem へ追記 |
| U3 端末なら text 既定 | podhmo が却下（AI first） | 登録しない |
| U4 undefined name の候補 | 方向は有効。候補提示・import fix の境界は未設計 | open-questions へ登録 |
| U5 エラー回復 | 方向は有効。error type と宣言単位回復の設計が未決 | open-questions へ登録 |
| U6 meta 型表示と期待型由来 | 有効。U2 の labels/details と一部同根 | open-questions へ登録 |
| U7 module 外 `gom run` / `gom init` | 一部 stale。transitive emit は wave48 lane C で解消済み | 残りを open-questions へ登録 |
| U8 LSP | podhmo が却下。既に withdrawn decision あり | 登録しない |
| U9 `gom eval` / REPL | 需要と eval/compiled 分担が未決 | open-questions へ登録 |
| U10 型シグネチャ search | 方向は有効。例の引数順は誤り | open-questions へ登録 |
| F1 組み込み `String` module | wave48 lane E の `str` package 決定で大部分は stale | 残余の surface 拡張だけ open-questions へ登録 |
| F2 型名付き record literal | 既存 open question を具体化 | 既存項目へ追記 |
| F3 `.gom` 側 `@abstract` / `@private` | wave48 lane D の `.gomi` directive 決定と競合 | 登録しない |
| C1 checker 分割 | 方向は有効。フィールド数の観測は過大 | TODO へ登録 |
| C2 表示実装の rt 一元化 | 有効。wave47 lane A の続き | TODO へ登録 |
| C3 診断コード meta test | 有効 | TODO へ登録 |
| C4 implementation-gaps archive | 有効。podhmo も肯定 | TODO へ登録 |
| P1 stdlib export data decode 共有 | 提案機序は未検証。Devin Review の指摘どおり lookup だけでは不足 | open-questions へ登録 |
| P2 テスト高速化 | 既存 problem の継続課題 | slow-tests problem へ追記 |
| P3 生成コード bench 基盤 | 有効 | TODO へ登録 |

## 再確認した観測

### U1: 連鎖は残るが、内訳は PR 本文と少し違う

現行 chain でも、`List.fold` の適用を字下げで複数行に書くと 6 件の診断が出る。再現では E0407 ×3、E0402 ×2、末尾の `xs` に E0401 ×1 だった。PR 本文は末尾も E0402 としているが、カスケードが原因を見えなくするという本体の観測は変わらない。

Devin Review の指摘は重要: インデントは Gom の意味論ではなく、深く字下げした独立式を継続行と断定すると正しいコードまで `f () ()` のような別意味に書き換えうる。したがって「字下げだけで safe fix」とする案は退け、少なくとも診断がすでに出ている連鎖に限定するか、fix を出さず専用診断へ畳み込む境界を決める必要がある。

### U2: E0301 の text 出力は spec とずれる

現行 chain で E0301 を出すと、JSON には `data.missing_patterns`（例: `["Empty"]`）と placeholder fix があるが、text 出力には不足パターン名が出ない。一方 spec §4 の例は `patterns not covered:` の詳細行を示す。これは既存 problem [text-format-detail-block](../problems/2026-11-02-text-format-detail-block.md) と同じ根だが、implementation-gaps には未登録だったため登録する。

### U7: transitive emit 部分は解消済み、module 外実行は残る

PR 本文が前提にしていた「`gom run` / `gom build` が module-local dependency を推移的に emit しない」は wave48 lane C で解消済み。残るのは module 外での scratch module 合成と `gom init` の雛形生成。現行 chain でも go.mod のない directory で `gom run main.gom` は `go.mod file not found` を含む E0001 になる。

また PR 本文は tutorial の章別 go.mod を `gom init` で生成する案に触れるが、章別 go.mod は削除済みで、現行 tutorial は scratch module 合成。TODO D-4 の古い前提もあわせて修正する。

### U10: 例の型では `filter_map` に届かない

`filter_map` の現行シグネチャは `(a -> Option b) -> List a -> List b` で、変換関数が先。PR の例 `List a -> (a -> Option b) -> List b` は data-last 検索例としては自然だが、単一化だけでは `filter_map` に一致しない。登録時はこの修正を残す。

### F1/F3: wave48 で前提が変わった

F1 の「組み込み `String` module」は、wave48 lane E で採用した stdlib `str` package とは別案。`str.length/get/slice/runes` は実装済みなので、組み込み module 案自体は open question へ戻さない。残るのは `fold_chars` 相当や安全な slice など、`str` の最小面を広げるかという問いだけ。

F3 の「意図は `.gom` 側属性に置く」は、wave48 lane D で採用した `.gomi` の `//gom:abstract` / `//gom:private` carry と競合する。現時点では新しい反証がないため open question へ戻さない。

### C1: checker の状態は大きいが、70 超ではない

`internal/check/expr.go` が 4,549 行で最大という観測は現行でも成立する。一方 `checker` struct の非コメント field 宣言は現行で 49 件で、「70 を超え」は過大。分割方向自体は有効なので、誇張された件数は auditor 側で修正して TODO に登録する。

### P1: 共有 importer の提案機序は未成立

Devin Review の指摘どおり、`importer.ForCompiler` の lookup は export data の `io.ReadCloser` を返し、`*types.Package` を返さない。このため「stdlib path なら共有側の package を返す」だけでは decode 済み型 identity を共有できない。P1 は implementation-ready な TODO ではなく、型 identity を保つ decode/import 構成の調査課題として扱う。

## podhmo コメントによる採否

- U3: 「要らない。gomはAI firstなので。」— JSON 既定を変える案は採らない（[comment](https://github.com/podhmo/gom/pull/256#discussion_r4704480172)）
- U8: 「lspは不要になったので消した。」— [lsp-withdrawn](../decisions/2026-11-19-lsp-withdrawn.md) を維持する（[comment](https://github.com/podhmo/gom/pull/256#discussion_r4704484918)）
- C4: 「これはやって良いかも。」— implementation-gaps archive 分割を TODO に登録する（[comment](https://github.com/podhmo/gom/pull/256#discussion_r4704493270)）

## この lane での台帳反映

- TODO: C1/C2/C3/C4/P3 を追加。wave48 lane G を追加。D-4 の stale な前提を現行 scratch module 方式に修正
- open-questions: U1/U2+U6/U4/U5/U7/U9/U10/F1 residual/P1 を追加。F2 の既存項目へ `T { ... }` 案を追記
- implementation-gaps: E0301 text が `missing_patterns` を表示しないずれを登録
- problems: text-format-detail-block に `Details` 案、slow-tests に go/types 化と EvalCompiledDiff bundled 実行の仮説を追記
