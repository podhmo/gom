# wave23: cons 非適用形の蓄積 quadratic — 実測と残件の整理

- 日付: 2026-11-19
- 種別: 調査（podhmo 採択済み 2026-11-19: **β イディオム誘導 + γ 警告 lint 採用、α/δ は先送り** — [decision](../decisions/2026-11-19-noncons-accum-policy.md)）— lane4 は実測 fixture + 候補の整理 + γ の実験実装
- 実測 fixture: `experiments/w23/noncons-accum/`（生成物 `noncons.go` もコミット）
- 対象 problems: `docs/problems/2026-11-05-stdlib-cons-built-combinators-quadratic.md`

## 背景

cons-recursion 特殊化（[decision](../decisions/2026-11-18-cons-recursion-specialization.md)）が match-cons 自己再帰を buffer loop 化したので、「cons で積む」素直な形は解決済み。残るのは cons 位置に自己呼び出しが**立たない**蓄積形。本 lane はそれらの実測値を取り、救済候補を整理する（単一推薦はしない）。

## 測った形と結果

`experiments/w23/noncons-accum/noncons.gom` に 5 形を定義し、`gom build` の実出力（`noncons.go`）を `go test -bench` で計測。`ConsMap` だけ buffer loop に変換され、他 4 形は素の再帰のまま残ることを生成コードで確認済み。

| 形 | 外れ方 | n=500 | n=2000 | n=8000 | 増分（4×） |
| --- | --- | --- | --- | --- | --- |
| ConsMap | 被覆形（対照） | 6.8 µs | 23 µs | 78 µs | ~3.4× — 線形 |
| ChainAppend | append 左辺が蓄積 acc | 345 µs¹ | 1.78 ms | 26.6 ms | ~15× — O(k²·chunk) |
| FoldConcat | folder 内 append | 309 µs | 3.30 ms | 48.7 ms | ~10.7/14.7× — O(n²) |
| Mutual | Callee ≠ 自分（相互再帰） | 317 µs | 4.29 ms | 65.2 ms | ~13.5/15.2× — O(n²) |
| ReadTwice | 呼び出し結果 2 度読み（要件 4 除外） | 330 µs | 4.41 ms | 70.7 ms | ~13.4/16× — O(n²) |

¹ ChainAppend は総要素 1000/4000/16000（k=125/500/2000 × chunk 8）。

- n=8000 で対照形の **~600–900 倍**。除外 3 形（FoldConcat/Mutual/ReadTwice）はほぼ同一プロファイル — 同じ機構（成長する acc の全コピー）に由来。なお ReadTwice は cons コピーに加えて `List.length rest` の線形走査が各レベルで乗るので 2 つの quadratic 源が混ざる（実測は上振れ側の推定として読む）。
- ChainAppend は `List.append` 自体が buffer loop 化済みでも救えない: コストは append 内部ではなく**左辺 acc のコピー**にあり、レベルごとに acc が育つ。
- Go 再帰はスタックが伸びるので n=8000 でも完走するが、深度制限が実質ない分、quadratic が最初の症状になる。

## 救済候補（複数提示、採否は podhmo）

### (α) 検出範囲の拡張 — 相互再帰・多関数 SCC

`markConsCalls` を「自己呼び出し」から「同じ強連結成分（SCC）内の呼び出し」に広げ、相互再帰も 1 つの共有 buffer loop に畳む。

- **買うもの**: Mutual 形の救済。言語意味は変わらず、検出ポリシーの内部拡張
- **コスト**: 複数関数を 1 ループに統合する制御構造（状態変数 + 分岐）が必要で現行の単関数変換より一段複雑。対象 3 形のうち Mutual のみを救う — FoldConcat/ReadTwice/ChainAppend には効かない

### (β) Array イディオムへの誘導 — 言語は変えず「書き方」で解く

`Array` builder + `to_list`（wave18 採択済み）が既にユーザーに届いている。蓄積形は `Array.make` + `set`/`append` 相当で書き直すイディオムを doc / agent-guide に示す。

- **買うもの**: 実装ゼロで全形を手で書き直せる経路が今日存在する
- **コスト**: ユーザーが自分で気づいて書き換える前提。現状 `Array` の member 面・freeze 契約はまだ新しく、実測の前にイディオムの書き味確認が必要（fold 内の反復書き込み等）

### (γ) 診断ヒント — quadratic 形の検出と警告

除外形をコンパイル時に検出して「この蓄積は O(n²) になり得る。Array builder を検討」的な情報診断を出す。

- **買うもの**: 沈黙の取りこぼし（現行ポリシーは「診断を出さず従来どおり再帰」）を可視化。実装は検出だけで変換不要なので (α) より小さい
- **コスト**: 誤検出・過剰警告の設計が要る（O(n²) でも入力が小さい用途は無害）。診断族の設計が新規

### (δ) List 表現の変更（chunked/unrolled/hybrid）

P0-3 で実測理由から却下済み（problems ファイルの Options 欄も「much larger design」）。本 lane では再検討しないが、対象外形を構造的に全部救える唯一の候補として残す。

## 判定材料

- 計測の帰結: **cons 化のコストは一様に O(acc)**。自己再帰以外の 4 形はどれも生成コードが同じ機構（成長 acc のコピー）に落ち着くので、「誰がバッファを持つか」の問題として一括で見える
- 対象外形の頻度見込み: FoldConcat/ChainAppend は初級者が書きがちな形（fold で積む・部分結果を継ぎ足す）。Mutual/ReadTwice はより稀
- 前段との関係: cons-recursion 特殊化の決定文書が「外れた形は沈黙で従来どおり」を明記済み。(γ) はその「沈黙」を診断に変える最小拡張で、(α) と直交する（両方採れる）
- (β) は wave18 の `Array` builder が機能している前提で動く — イディオムの提示先は gom-agent-guide が自然（言語構文の話なので）

## γ の実験実装（2026-11-19 追加）

podhmo の「検出の実装を実験してみるというのはあっても良いかもな。warningとか出ると良い」で γ を vet 的警告として実装した（実装箇所に制限なしの指示で、除外形と同じ IR を眺められる `lower` を選択）。

- **仕組み**: `internal/lower/conswarn.go` の `lintQuadraticAccum` が Core IR 上で動く。ファイルローカル呼び出しグラフを作り、「呼び出し先が自分と同じサイクル（SCC）にいるか」で再帰蓄積かを判定（線形ラッパが再帰 sibling を 1 回呼ぶだけの場合は O(n) のままなので警告しない）、3 形を検出:
  - (A) 再帰呼び出しの結果を `[.., ..rest]` の rest に置く cons 末尾 — consLeaf が callee≠自分や read-twice で弾いた形
  - (B) 再帰呼び出しの結果へ `List.append`/`List.concat` — ChainAppend 形。concat の 1 引数は list-of-lists なので、let バインド越しの `[rec_result, [x]]` リテラルも見る
  - (C) accumulator（自パラメータ）に append/concat する folder を持つ `List.fold`/`foldl` — FoldConcat 形。名前付き folder は FuncRef 経由でファイル内 decl まで追う
- **診断**: `W0801`（warning severity、PhaseLint、「list accumulation shape is quadratic (cons-loop specialization does not apply)」）。`gom build`/`test`/`run` で発火（`gom check` は lower を走らせないので出ない — vet 的位置づけとして許容）。警告は (rule, site) で dedup — 同一関数の 2 箇所は 2 件出る。なお incremental build cache が効く再ビルドではコンパイル自体がスキップされ警告も出ない（manifest ベースのキャッシュ。vet 的運用として許容）
- **実測結果**: `experiments/w23/noncons-accum` で全除外形に発火（chain_append L20、fold_concat L26、mut_a L33、read_twice L43、concat_acc L57、fold_named L64、two_sites L70+L71 2 件）、被覆形 `cons_map` と線形ラッパ `wrap_once` は無発火。`go test ./...` 全件（testdata/spec・golden 含む）で誤検出ゼロ
- **設計上の注意**: 検出は let 変数経由のデータフローのみ追う（bound map）。`List.append(xs, elem)` のように再帰 acc 経由でない append は無発火 — 線形な append もあるので正しいが、複雑な別名経路や他ファイルの folder は取りこぼす（警告の取りこぼしは安全側）

## 未検証・残件

- Mutual を本当に 1 ループ化できるかの技術確認は未実施（試行なし — 設計見積もりのみ）
- `Array` builder の現行 surface で 4 形を書き直したときの書き味は未確認（wave24 で (β) 採用なら fixture に追加候補）
- Fold の要素ごとクロージャ確保（problems の Related open axis）は本 lane の範囲外のまま
