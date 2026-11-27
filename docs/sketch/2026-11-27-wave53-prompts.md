# wave53 prompts（草案）

- 日付: 2026-11-27
- 状態: 草案
- 入力: [wave52 round](2026-11-27-wave52-round.md)、[TODO](../../TODO.md)、[open-questions](../open-questions.md)、[implementation-gaps](../implementation-gaps.md)

wave52 は labeled lambda seeding、standalone init checksum、診断コード方向、coverage evidence、DMMF fit example を処理した。wave53 は wave52 で表面化した推論境界と init failure atomicity を先に決め、診断 UX の残件を小さく束ねる候補を置く。採否は lane 実行前に podhmo と確定する。

## lane A. generic higher-order pipe の record literal 推論を決める（小〜中）

`input.lines |> ... |> Result.map (fn lines -> { ... })` の無名 record literal が E0407 になり、二次 E0401 が分かりにくい問題について、実装拡張・spec 文言狭窄・診断改善のどこまでを入れるか決める。

- implementation-gaps のずれを解消する方向を決める。spec §3.3 が約束する範囲を現実へ合わせるか、実装を spec へ寄せるかを最初に固定する
- 実装する場合でも後続使用からの逆推論は入れない。pipe / generic application の静的に分かる result expectation に限定する
- bare literal が解決できない場合の二次 E0401 を抑制できるかは別 fixture で確認する

## lane B. `gom init` failure atomicity（小）

standalone validation に失敗したとき、新規 init で作成した `go.mod` / `main.gom` / `go.sum` を残すか rollback するかを決め、採用側だけ実装する。

- 既存 module の additive edit rollback は wave52 で済んでいる。今回は新規作成ファイルだけを対象にする
- directory 自体やユーザー既存ファイルの non-overwrite 契約は変えない
- 方針を decision/spec/test で固定する

## lane C. E0401 provenance / details 昇格の次段調査（中、調査）

wave50/52 で溜まった診断 UX の open questions を束ね、次に実装すべき 1 面を decision まで絞る。

- if/match 腕・呼出し引数・record field への expected type provenance 伝播
- structured data の details 昇格基準
- 近接 span の text snippet 統合
- `gom explain` fragment example の検証方針

実装は lane に入れず、採用する 1 面と fixture 方針だけを決める。

## lane D. checker 内部状態の分割（中）

TODO にある内部改善。`typeTables` / deferred revisit queue / taint / lint state を関心ごとの部分構造へ分け、`expr.go` の責務を下げる。振る舞いは変えない。

## lane E. benchmark 基盤（小〜中）

TODO にある生成コード benchmark 基盤を `bench/` に作る。代表ペアと記録方法だけを決め、CI 実行や回帰警告は導入しない。

## 見送り候補（少なくとも現時点では lane にしない）

- match narrowing の次段 precision — DMMF 再評価でも主用途は解消済み。guard/literal/field path は需要が出るまで保留
- stdlib `str` 拡張、Option traverse、panic capture adapter — 需要確認が弱い
- rt 表示一元化 — 有効だが checker 分割より後でよい
- `gom install`、Go 定数セット、cgo/unsafe/reflect、セルフホスト — future work 維持
