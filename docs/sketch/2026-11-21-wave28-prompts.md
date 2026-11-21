# Wave 28 orchestration prompt drafts

State as of 2026-11-21: wave27 の 4 lane（#84–#87）は全件マージ済み。
別系統の scoped-cleanup チェーン（SC-1..3、#91/#93/#94/#96、
`devin/1795208106-*`）が別セッションで進行中 — lane 選定はそこと
被らない言語ギャップ埋めに絞る。linkcheck（#92）と rgrep（#95）の
example 実装で新たに見つかったギャップを台帳に登録済み。
実行モデルは wave26/27 と同じ前提（単独実行・Devin Review・
podhmo がマージ）。lane 順は下からチェーンする。

## 候補（採否は podhmo が lane ごとに決める）

ギャップの棚卸し（2026-11-21 時点の open）:

| ギャップ | 出所 | 重さ |
| --- | --- | --- |
| named func 型（`context.CancelFunc` など）を Gom から呼べない・named func 型パラメータへの `Go.funcN` 適合も未対応 | rgrep + errgroup プローブ | 大（相互運用全体に波及） |
| `Array.make` が ADT/`Option` 要素で Go nil を生成し `Array.get` が panic | linkcheck | 中（API/仕様の決定事項） |
| 予約語の一覧が spec にない + `fn send` が意味不明な E0101 | gist-cli | 小 |
| `Ptr sync.WaitGroup` 等「共有前提」の Go 型が W0701/W0702 を踏む（false positive） | rgrep + errgroup プローブ | 中 |
| type alias の構文が存在しない（`AliasBody` は unreachable。`type X = Int` がバリアント `Int` として**静かに誤解**される） | problems/2026-11-02-type-alias-body | 中（調査） |

### A. named func 型の呼出し・受け渡し — `devin/wave28-named-func-call`

```
[investigate → impl] `type CancelFunc func()` のような named func
型は spec 8.8 の `Go.FuncN` に読まれず、値は束縛・受け渡しできるが
`f ()` は E0401、`Go.fnN` は E0510、`Go.conv` は型検査を通るのに
`func(struct{}) struct{}` を emit して `go build` で落ちる（実害:
`context.WithCancel`/`signal.NotifyContext` の stop を呼べない、
`fs.WalkDirFunc` 型コールバックに Gom fn を渡せない恐れ）。
選択肢: (a) underlying が unnamed func で `Go.FuncN` の形に合う
named 型は `Go.FuncN` として読む — 双方向が一度に通るが型名と
メソッド面（`http.HandlerFunc` の `ServeHTTP`）を失う、
(b) named 型のまま `Go.fnN` と直接適用を許し、expected 側の
互換比較を named↔`Go.FuncN` で行う — 局所的だが変換側の規則を
増やす、(c) named func 値に組み込み `.Call` メンバーを見せる。
import mapping が named func 型をどう保持しているかを確認し、
(a) なら mapping の分岐1箇所、(b) なら check の apply/conv 両側を
測る。決まり次第 spec 8.8 更新 + 双方向テスト（named 戻り値を
呼ぶ、named param に `Go.funcN` 値を渡す、`Go.conv` の誤変換は
拒否側 fixture 化）。implementation-gaps の該当項を解消に進める。
Ownership: internal/goimport, internal/check, docs/spec.md,
testdata/spec + golden.
```

### B. `Array.make` の ADT/`Option` 要素ゼロ値 — `devin/wave28-array-make-init`

```
[impl — draft decision から] `Array.make n` の「n 個のゼロ値」は
`Option`/ADT 要素では Go nil が入り、`Array.get` が "not a
declared variant" で panic する（linkcheck で実発生、`--timeout
1ms` 経路）。選択肢: (a) `Array.make n init` と初期値引数を必須に
変える（素直・全型で一貫、破壊的だが v1 前の API 安定性要求は
小さい）、(b) `Option` に限り `None` を生成し他の ADT 要素は
`Array.make` を診断拒否（部分修正、生成側の ADT 分岐が要る）、
(c) 現状維持して spec に「ADT 要素不可」を明記 + lint。
決定は draft decision として起こし、prototype で検証してから
sketch に結果を書いて状態を更新する（AGENTS.md の手順どおり）。
stdlib array impl.go と spec 8.3、利用側（stdlib 内部・examples）
の機械移行を含む。implementation-gaps の該当項を解消に進める。
Ownership: internal/stdlib/array, docs/spec.md, decision +
testdata/spec.
```

### C. 予約語一覧と `fn send` の診断 — `devin/wave28-reserved-word-list`

```
[impl] `send`/`select`/`default` などのキーワードは lexer が持つが
spec に予約語一覧がなく、`fn send (x: T)` は "a function
declaration needs at least one (param: Type) group, found send"
という理由の分からない E0101 になる。token.go のキーワード集合を
正として spec に予約語節を足し、束縛名位置でのキーワード出現は
専用診断（「`send` は予約語」）にする。既存の E0101 規規則を壊さない
範囲で: parser の ident 期待位置で keyword を拾って診断を出し分ける
か、lexer で keyword→ident 変換せず専用エラーを出すか、構造を見て
選ぶ。testdata/spec に `fn send`/`let select` の fixture。
implementation-gaps の該当項を解消に進める。
Ownership: internal/syntax, docs/spec.md, testdata/spec.
```

### D. 共有 lint の「共有前提の Go 型」allowlist — `devin/wave28-shared-go-types`

```
[investigate → impl] `Ptr sync.WaitGroup`・`Ptr errgroup.Group`・
`Ptr sync.Mutex`/`sync.Map`/`atomic.*` など、内部的に同期済みで
共有が前提の Go 型が W0701/W0702（mutable crossing）を鳴らす —
ユーザーがチャネル countdown に逃げるか警告同居になる実害は
rgrep/errgroup プローブで観測済み。選択肢: (a) builtin allowlist
（sync 系の構造体・errgroup.Group 等を lint の可変判定から除く —
W0701/W0702 の「閉じた builtin リスト」設計と同じ枠組み）、
(b) `@shared`/抑制属性で宣言側に線を引かせる、(c) 何もしない
（0 警告運用の例外として文書化）。(a) なら lint の mutable 判定に
named Go 型の allowlist テーブルを足し spec 10 に記載。採否を
判断して進める。testdata/spec に warn しないケースの fixture。
Ownership: internal/check/lint.go, docs/spec.md, decision.
```

### E. type alias 構文の調査 — `devin/wave28-type-alias`

```
[investigate] `AliasBody{Type}` は AST にあるが grammar の
type-body が variant-list|record-type しかないため unreachable —
`type Pred = String -> Bool` は E0101、`type X = Int` は**バリアント
`Int` として静かに受理**される意味罠（problems 台の記録どおり）。
構文案: `type X = T`（既存の曖昧を裁く — `Int` は variant か alias
かの切り分けが必要）、`type X alias = T`、別キーワード。
曖昧の扱い（大文字始まりは今も variant か、単一 variant と alias
の文脈分離）と、エイリアスが解決される位相（resolve 早期展開か
名前として残すか — .gomi への現れ方、GoFuncN 型・record 型への
適用）を測る。結論として採用・延期・却下の判断材料を出す。
Ownership: grammar.ebnf, internal/syntax+resolve, problems,
docs/spec.md.
```

## 補足

- A は implementation-gaps で最も広い波及面（CancelFunc 呼出し・
  WalkDirFunc 等コールバック・`Go.conv` の誤 emit まで同根）。
  examples の G 候補（Go 並行プリミティブの例）も A に依存する
- B は v1 凍結前の API 形に関わるので wave28 以内に結論を出したい
- C は小さい — 調整 lane として先にやってもよい
- E は言語表面の決定なので、採用するなら複数 lane のどれかと
  入れ替えてもよい（調査だけなら軽い）
