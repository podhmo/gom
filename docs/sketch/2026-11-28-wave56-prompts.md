# wave56 prompts 草案

状態: 採択済み（A+B+C+D+E、順 A → B → C → D → E）

入力は wave55 round、TODO、open-questions。wave55 は診断の root cause 表示を進めたので、wave56 は decision 済みの 2 件を実装しつつ、残る表示方針を調査で絞る案を置く。

## lane A: E0407 ambiguous record candidate display の実装

TODO E0407-1。E0407 に候補 record declaration label（5 件まで）と deterministic な `data.candidates` を付ける。local cross-file、import `.gomi`、overflow truncate、text/JSON、E0407 explain/error-codes を含める。decision 済みで実装のみ。

## lane B: E0201 module/operation member typo candidates の実装

TODO E0201-1。E0201 `namespace: "member"` で member table 内の optimal string alignment 距離 1 の候補を最大 3 件、note と `data.candidates` に出す。通常 module に加え `Go.Map` / `Go.Slice` operation table を対象にし、`fixes` は付けない。positive/negative/truncate/text/JSON fixture を含める。decision 済みで実装のみ。

## lane C: E0401 provenance の call argument / record field 拡張可否調査

調査のみ。現状は direct let/return 注釈と block tail の if/match 腕に限定している。呼出し引数や record field で expected type provenance を出すと便利な一方、腕内の独立 constraint mismatch へ注釈を漏らしたときの誤誘導が大きい。具体 fixture を並べ、どの経路なら label が事実として正しいか、逆にどこで止めるべきかを decision へ落とす。実装は lane に入れない。

## lane D: diagnostic details 昇格基準と snippet 統合の調査

調査のみ。`data` のうち何を人間向け `details` へ出すか、text renderer が近接した同一ファイル span を 1 つの snippet にまとめるべきかを比較する。過剰出力・重複 snippet・JSON consumer との役割分担を fixture 例で評価し、採用/見送りを decision へ落とす。実装は lane に入れない。

## lane E: bare-name typo / 未 import Gom stdlib candidate の第二段調査

調査のみ。E0201 bare name typo は visible scope 走査、shadowing dedup、kind filter、false positive 制御が要る。未 import module candidate は Gom stdlib に限定し、goimports 的な Go package 探索は対象外。両者を比較し、次に実装する面を 1 つだけ decision へ落とす。note/structured data の候補表示を上限とし、safe fix は検討しない。実装は lane に入れない。

## 候補外

- `gom explain` fragment examples の formalize: 台帳衛生には有効だが利用者価値が薄め。今回は見送り候補
- match narrowing の残り精度化: 重要だが scope が広く、diagnostic 中心の wave と混ぜると大きい。別 wave で guard / literal path narrowing などを分割して扱う方がよい
- stdlib `str` 拡張、Option/Result helper: 独立したユーザー価値はあるが、wave55 からの連続性は弱い
- goimport export data decode 共有・benchmark 基盤: 内部性能・基盤寄り。benchmark 基盤は前回議論で優先度低と確認済み

## 採択結果

採用: A+B+C+D+E。順: A → B → C → D → E。

A/B は decision 済みで独立しており先に実装する。C/D/E はいずれも調査のみで、A/B の実装結果と review 発見を次段の表示設計へ反映できる。
