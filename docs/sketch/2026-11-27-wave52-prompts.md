# wave52 prompts

- 日付: 2026-11-27
- 状態: 草案
- 入力: [wave51 round](2026-11-27-wave51-round.md)、[TODO](../../TODO.md)、[open-questions](../open-questions.md)、[implementation-gaps](../implementation-gaps.md)

wave51 は match narrowing、record pipe 診断、診断 coverage、agent skill、module 外 run/init、labeled lambda の設計を処理した。wave52 は wave51 で決まった型推論実装を先に処理しつつ、merge 時レビューで残った module/workspace 境界と、診断基盤の精度を扱う。

## lane 候補

### A. label-erased contextual seeding 実装（中）

wave51 lane E で採択した LS-1 を実装する。label を含む文脈関数型について、label を inference contract に入れず parameter 順序と型だけを残した view で positional lambda の record parameter を seed する。labeled callback・positional lambda・同名 field set probe を red-first で固定し、speculative propagation の clone/rollback へ最初から erased view を作る接続も fixture で固定する。最終 conformance の unlabeled→labeled introduction は変えず、spec §3.3 も更新する。

### B. `gom init` workspace checksum の方針決定と修正（小〜中）

wave51 lane A の merge 時レビューで残った問題。`gom init` が caller の workspace 経由で依存を解決すると、checksum が `go.work.sum` 側へ入り、init 先を workspace 外で build できないことがある。workspace 内利用を優先するか、standalone module としての checksum を必須にするかを決め、必要なら小さい修正と fixture を入れる。workspace local replace・release module・既存 `go.sum` の有無を probe する。

### C. 診断 coverage meta-test の assertion polarity（小〜中）

wave51 lane C の meta-test は指名 test 内に diagnostic literal があることまでしか保証しない。negative assertion を positive evidence へ誤登録する事故をどこまで機械検知するかを決める。test helper 経由を含む静的判定が現実的かを調べ、編集者責任の維持・構造規約・scanner 強化のいずれかを decision にする。

### D. match narrowing の次段 precision 調査（調査 + decision、中）

wave51 lane B の残件。guard 条件解析、literal/field path narrowing、複数 partial payload alternative の合成、alias 解釈、interprocedural narrowing、contextual E0302 への移行を一度に実装せず、次に価値のある境界を probe して decision だけ行う。`todo ()` 回避と進化検知の緊張も整理する。

### E. E0204/E0503/E0704 の方向決定（小）

live code table / `gom explain` / parser-checker 境界が揃っていない 3 コードについて、production emitter を実装するか、docs/code table を現行診断へ寄せるかを決める。決まったものだけ小さい fixture/docs 修正へ落とし、大きい emitter 実装は別 lane へ分ける。

## 採否を確認したい点

1. A〜E の推薦候補をどこまで採用するか
2. B は方針決定だけにするか、決定後の修正まで同じ lane に入れるか
3. C は scanner 強化まで入れるか、decision だけにするか
4. D は調査 lane に留めるか、小さい precision 拡張まで入れるか
5. E で production emitter 実装が必要になった場合、wave52 で扱う範囲をどう区切るか

## 見送り候補（少なくとも現時点では lane にしない）

- checker の状態分割、rt 表示一元化、benchmark 基盤 — 有効な内部改善だが、wave52 は wave51 直結の残件を優先
- `gom explain` fragment example、expected type provenance、details 昇格基準、snippet 統合 — 診断系の設計論点は多いため、C/E の結果を見て別 wave で束ねる
- stdlib `str` 拡張、Option traverse、panic capture adapter — 需要確認が弱い
- `gom install`、Go 定数セット、cgo/unsafe/reflect、セルフホスト — future work 維持
