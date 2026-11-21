# Wave 28 orchestration prompt drafts

State as of 2026-11-21: wave27 の 4 lane（#84–#87）は全件マージ済み。
別系統の scoped-cleanup チェーン（SC-1..3、#91/#93/#94/#96、
`devin/1795208106-*`）が別セッションで進行中 — lane 選定はそこと
被らない言語ギャップ埋めに絞る。linkcheck（#92）と rgrep（#95）の
example 実装で新たに見つかったギャップは台帳登録済み。
このファイルは wave28 の prompts。実行モデルは wave26/27 と
同じ前提（単独実行・Devin Review・podhmo がマージ）。
lane 順は下からチェーンする。

**採択（podhmo、2026-11-21）: A〜E の 5 lane 全部。**

## Lanes

### A. named func 型の呼出し・受け渡し — `devin/wave28-named-func-call` — **採択済み**（2026-11-21 チャット）

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

### B. `Array.make` の ADT/`Option` 要素ゼロ値 — `devin/wave28-array-make-init` — **採択済み**（2026-11-21 チャット）

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

### C. 予約語一覧と `fn send` の診断 — `devin/wave28-reserved-word-list` — **採択済み**（2026-11-21 チャット）

```
[impl] `send`/`select`/`default` などのキーワードは lexer が持つが
spec に予約語一覧がなく、`fn send (x: T)` は "a function
declaration needs at least one (param: Type) group, found send"
という理由の分からない E0101 になる。token.go のキーワード集合を
正として spec に予約語節を足し、束縛名位置でのキーワード出現は
専用診断（「`send` は予約語」）にする。既存の E0101 規則を壊さない
範囲で: parser の ident 期待位置で keyword を拾って診断を出し分ける
か、lexer で keyword→ident 変換せず専用エラーを出すか、構造を見て
選ぶ。testdata/spec に `fn send`/`let select` の fixture。
implementation-gaps の該当項を解消に進める。
Ownership: internal/syntax, docs/spec.md, testdata/spec.
```

### D. 共有 lint の「共有前提の Go 型」allowlist — `devin/wave28-shared-go-types` — **採択済み**（2026-11-21 チャット）

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

### E. type alias 構文の調査 — `devin/wave28-type-alias` — **採択済み**（2026-11-21 チャット）

```
[investigate] `AliasBody{Type}` は AST にあるが grammar の
type-body が variant-list|record-type しかないため unreachable —
`type Pred = String -> Bool` は E0101、`type X = Int` は**バリアント
`Int` として静かに受理**される意味罠（problems 台の記録どおり）。
構文案: `type X = T`（既存の曖昧を裁く — `Int` は variant か alias
かの切り分けが必要）、`type X alias = T`、別キーワード。
曖昧の扱い（大文字始まりは今も variant か、単一 variant と alias
の文脈分離）と、エイリアスが解決される位相（resolve 早期展開か
名前として残すか — .gomi への現れ方、`Go.FuncN` 型・record 型への
適用）を測る。結論として採用・延期・却下の判断材料を出す。
Ownership: grammar.ebnf, internal/syntax+resolve, problems,
docs/spec.md.
```

## 申し送り

- CI は push-to-main / PR-to-main synchronize でしか走らない —
  チェーン PR は Devin Review のみ。マージは podhmo が
  retarget → `gh pr merge --merge` を下から順に。
- lane A は examples 候補 G（Go 並行プリミティブの相互運用例、
  `docs/sketch/ja/2026-11-21-example-candidates.md`）の前提にも
  なる — named func が通れば WalkDirFunc 系の例が書ける。
- named func の観測事実（/tmp プローブで確認済み）: `f ()` は
  E0401「expected context.CancelFunc, got Unit -> ?1」、
  `Go.fn0 stop` は E0510「expects a Go.Func0 value」、
  `Go.conv stop : Unit -> Unit` は型検査を通るが生成コードが
  `func(struct{}) struct{}` で `go build` 失敗 — Go.conv の
  func 型変換は互換性を検査していないバグでもある。
- unnamed func 型はすでに双方向で動く（errgroup 実証済み:
  `Go.func0` で `Unit -> Result Unit GoError` → `func() error`、
  `context.AfterFunc` の戻り値 `func() bool` を `Go.fn0` で
  `go {}` 内から呼べた）。
- spec fixture の再生: `go test ./internal/build -update -run
  TestSpec`。golden の再生: 同コマンド -run TestGolden。
  gocall は `cmd/gom/gocall_test.go` のパッケージ列挙に登録。
- gomtest のディレクトリ case は bare `expected.json` /
  `golden.go` / `case.json` のどれかが無いと各ファイル個別
  ケースになる — 複数ファイルを 1 ケースにするなら
  `case.json {}` を置く（wave27 round）。
- lane C/E が grammar を触る場合: tree-sitter-cli は 0.27.1/0.26.3
  がこの VM では GLIBC 不足で動かない。
  `npm exec --yes --package=tree-sitter-cli@0.25.10 --
  tree-sitter generate` で再生成（ABI 15 は不変、wave27 round）。
- scratch チェック環境: `/tmp/gomctx/`（go.mod `module tmp` +
  `replace github.com/podhmo/gom => ~/repos/gom`、
  `/tmp/gom check .`）。`.gom` は `package` 節から始める。
  `gom build -o dir/` は dir を `/` で終わらせる。
- コミットメッセージにバッククォート/`$()` を入れない —
  `-m` を複数回に分けて書く（shell が喰う）。
