# wave52 prompts

- 日付: 2026-11-27
- 状態: 採択済み（podhmo interview、2026-11-27）
- 入力: [wave51 round](2026-11-27-wave51-round.md)、[TODO](../../TODO.md)、[open-questions](../open-questions.md)、[implementation-gaps](../implementation-gaps.md)

wave51 は match narrowing、record pipe 診断、診断 coverage、agent skill、module 外 run/init、labeled lambda の設計を処理した。wave52 は確定済みの型推論実装を先に処理し、module/workspace 境界と診断基盤の精度を小さく固め、最後に現在の機能で DMMF example を更新できるかを実コードで確かめる。

採用 lane は **A → B → E → C → D**。D は A の labeled lambda seeding を含む現行機能を再評価するため最後に置く。E を C より先に置くのは、診断コードの live/retired 分類を先に確定させてから coverage scanner の規約を強化するためである。

## lane A. label-erased contextual seeding 実装（中）

wave51 lane E で採択した LS-1 を実装する。label を含む文脈関数型について、label を inference contract に入れず parameter 順序と型だけを残した view で positional lambda の record parameter を seed する。

- labeled callback 文脈の positional lambda、同名 field set probe、speculative propagation の clone/rollback 接続を red-first で固定する
- erased view は推論だけに使い、accept/reject の意味判定には使わない。最終 conformance の unlabeled→labeled introduction は変えない
- labeled run の reordering は推論中に正規化しない。parameter 順序を変えない
- spec §3.3 の label 付き文脈型 propagation 禁止を今回の規則へ更新し、decision の状態も更新する
- structural record / anonymous record 型は導入しない

## lane B. `gom init` standalone checksum 必須化（小〜中）

`gom init` は新しい project を standalone module として成立させる。caller の `go.work` で依存を解決できても、init 先の module だけでは checksum や replace が足りない状態を成功扱いにしない。

- init 時の toolchain dependency 解決は `GOWORK=off` 側で行い、caller workspace の replace だけに依存する成功を避ける
- init 直後の project が workspace 外でも `gom run` / build できることを fixture にする
- workspace local replace、release module、既存 `go.sum` あり/なし、既存 go.mod への additive edit を fixture で固定する
- standalone に解決できない場合は actionable error にし、explicit な `replace` または利用可能な release version を促す
- module 外 `gom run` の scratch module 自体の寿命/cache contract は変えない
- 方針は decision へ記録し、`gom init` workspace checksum の implementation gap / open question を解消へ進める

## lane E. E0204/E0704/E0503 の診断方向を実装側へ固定する（小〜中）

podhmo 採択方針: E0204/E0704 は実装側へ寄せ、E0503 は retired にする。

- E0204: unknown attribute は parser が専用メッセージを出しているので、E0101 ではなく E0204 を production diagnostic として出す。phase 表記を現実に合わせ、spec/error-codes/explain/fixture を同期する
- E0704: parser が destructuring 形を generic E0101 で捨てず、checker の E0704 へ到達する形へ直す。`Some job <- jobs` のような入力が E0704 になる fixture を red-first で追加し、合法な `r <- jobs` / `_ <- jobs` を変えない
- E0503: production emitter を増やさず retired にする。implicit numeric conversion 違反は現行どおり E0401 の generic mismatch で報告する方針を decision/error-codes/coverage 分類へ反映する
- 実装したコードは reserved 分類から外し、対応する implementation gap を解消する

## lane C. 診断 coverage meta-test の positive assertion 規約（小〜中）

podhmo 採択方針: 承認済み positive assertion 形を scanner で要求する。

- positive unit / recovery coverage の例外表が指名した test 内に diagnostic literal があるだけでは足りず、承認済み assertion 形の引数としてその code が使われることを要求する
- 承認形は `requireCodes(t, diags, "E0502")` のような直接の positive assertion helper 呼出しを中心に決める。現行 evidence test が helper 経由・複数診断・data 検査を持つものは、scanner が追える最小の構造規約へ寄せる
- negative assertion、コメント中の literal、positive coverage と無関係な recovery literal を evidence として登録した場合に red になる meta-test fixture を先に追加する
- すべての Go test assertion を意味解析する scanner は作らない。例外表が指名した coverage evidence だけを対象にする
- 規約と承認形は decision/design へ記録し、open question を解消する

## lane D. DMMF fit example の現行機能での更新調査（中）

podhmo 指定: match narrowing の次段 precision 調査は見送り、代わりに DMMF example が現在の機能で更新できるかを調べる。`experiments/dmmf-fit` を実際に更新しながら、残課題をレポートへまとめる。

- wave49 以降に入った short-circuit 修正、record type literal / contextual inference、`Result.sequence` / `List.traverse_result`、E0301 text 改善、wave51 の nested match narrowing、lane A の labeled lambda seeding を使って書き直せる部分を更新する
- 以前の fit report で不足だった項目が解決したか、よりよい書き方ができるようになったか、まだ残る課題は何かを分類する
- 言語機能・compiler の新規実装はしない。更新中に見つかった仕様・実装・文書のずれは implementation-gaps に即時登録し、scope 外の改善要望は検証レポートへ残す
- DMMF boundary guide の全面改訂は lane に入れない。example 更新から明らかに stale な一文だけ直す場合は検証レポートに記録する

## 採否インタビュー結果

- A/B/C/E は採用。match narrowing の次段 precision 調査は見送り、D は DMMF fit example の更新調査へ差し替えた
- lane 順は A→B→E→C→D
- D は `experiments/dmmf-fit` を実際に更新しながら調査し、残課題はレポートへまとめる
- B は standalone checksum を必須にする
- C は承認済み positive assertion 形を scanner で要求する実装まで入れる
- E は E0204/E0704 を実装側へ寄せ、E0503 は retired にする

## 見送り候補（少なくとも現時点では lane にしない）

- match narrowing の次段 precision 調査 — wave51 で主ユースケースを解消済みで、DMMF example の再評価を優先する
- checker の状態分割、rt 表示一元化、benchmark 基盤 — 有効な内部改善だが、wave52 は wave51 直結の残件を優先
- `gom explain` fragment example、expected type provenance、details 昇格基準、snippet 統合 — 診断系の設計論点は多いため、C/E の結果を見て別 wave で束ねる
- stdlib `str` 拡張、Option traverse、panic capture adapter — 需要確認が弱い
- `gom install`、Go 定数セット、cgo/unsafe/reflect、セルフホスト — future work 維持
