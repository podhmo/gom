# sandbox 探索 run 2（PR #196）からの不足機能の整理

- 日付: 2026-11-24
- 関連: [PR #196](https://github.com/podhmo/gom/pull/196)（sandbox/ の探索者
  run 2 — 未マージのアーカイブとして残る）、
  [観測ログの転記](ja/2026-11-24-sandbox-explorer2-log.md)、
  [解消確認の転記](ja/2026-11-24-sandbox-explorer2-recheck.md)、
  [前回の整理](2026-11-23-sandbox-missing-features.md)（PR #163 の棚卸し）、
  [TODO.md](../../TODO.md)、[open-questions.md](../open-questions.md)、
  [implementation-gaps.md](../implementation-gaps.md)
- 状態: 調査記録（規範ではない）

## 目的

PR #196 の「探索者」run 2 — CSV・日時・ネットワーク系の普段使いツールと
DMMF 風ドメインパッケージを gom で書き、`sandbox/log.md` に観測を、
`sandbox/recheck.md` に run 1（PR #163）の指摘の解消確認を残したもの —
から、新たに見つかった不足・摩擦を抽出して対応先を決める。

根拠はすべて転記済みの `docs/sketch/ja/2026-11-24-sandbox-explorer2-*.md`
（log・ideas・recheck・sumtypes・gotchas）。`.gom` のコードは未マージの
PR #196 側に残る。

## 前回指摘の解消状況（recheck.md の要約）

run 1 の「不得意・危険」の大半が wave35–41 の実装で解消したことを
`sandbox/recheck/` の最小 probe と実ツール内で確認済み:

- **解消**: ビット演算子（`|||` 等）、stdlib `List` 補強、
  `@method(ServeHTTP)` verbatim 名、`//gom:embed` + `@extern let`、
  record field の struct tag、`Go.Any` の match 型テスト腕
- **概ね解消**: `Go.nil`（List 面のパラメータには `let` 束縛が要る）、
  部分適用 lint（W0901 — `any` 引数経由は残る）
- **部分的解消**: `~[]E` の要素一致（`slices.*Func` の読み取り系に
  record リストを渡せる。in-place 変更系はコピーなので W0501 の警告どおり
  無意味 — `Go.Slice` を gom 側で作る面が依然ない）
- **未検証のまま**: `Go.conv` の `List`→named slice（TODO 上は wave38
  lane E で解消済みの扱い）

## 抽出した不足（対応先別）

### TODO に載せたもの

1. **`gom run` / `gom build` が gom の `import` 先を推移的に build しない**
   - 観測: `gom run sandbox/shop` は `domain.gom` を emit せず `go build`
     が `no required module provides package .../domain`（E0001）で落ちる。
     `gom check` は `.gomi` を読むだけなので通るのに、リンクで初めて
     失敗する。`gom build dirA dirB` と全依存を層順に列挙すれば emit
     されるが、`gom run` は単一ディレクトリ限定（使用エラー）なので
     依存を持つプログラムは「依存を `gom build` → `gom run`」の2段が要る。
   - spec §12 は `gom run` の単一 `package main` ディレクトリ制約を
     明記しているが、依存の推移 emit がないことは明記されていない —
     未実装の機能として tracker に載せる。
   - → TODO「Bugs」

2. **spec §7 に record の abstract 宣言を明記する**
   - 観測: `.gomi` で `type ValidatedOrder` と bare 宣言（本体を書かない）
     すると、import 側からの record literal 構築が E0401 で拒否される
     （sumtypes.md で実検証）。DMMF の「`place_order` を通さないと
     `ValidatedOrder` を作れない」が型で強制できる。
   - spec §7 の例は `type Canvas` を抽象型として見せているが、ルール 3 は
     「sum type はコンストラクタまで書けば透明、名前だけ書けば抽象」と
     sum type にしか言及していない — record にも同じ規則が効くことが
     明記されていない（implementation-gaps に登録）。
   - → TODO「ドキュメント」

### open-questions に載せたもの（提供形が未決）

3. **`Go.Slice` の gom 側コンストラクタ**
   - 観測: `slices.SortFunc` のような in-place 変更系には `Go.Slice` の
     実体が要るが、gom 側から `Go.Slice` を作る面がない
     （`Go.conv xs : Go.Slice Entry` は gom 要素型で E0507、`Array` も
     `~[]E` を満たさず E0401）。読み取り系は `~[]E` の要素一致で `List`
     のまま渡せるが、書き込み系は行き止まり。
   - → open-questions「Go 相互運用」

4. **文字列の操作面（len・部分列・添字）**
   - 観測: `strings.Join`/`Split` は Go の `strings` が使えるが、
     `len(s)`・`s[i]`・`s[lo:hi]` 相当がない — 文字集合の走査には
     `strings.Split (s, "")` + `List.get` の迂回になる（passgen）。
   - → open-questions「言語機能」（組み込み `String` 面か stdlib
     パッケージか）

5. **フィールド名の共通する record を並べると literal ごとに E0407**
   - 観測: `UnvalidatedOrder` → `ValidatedOrder` のような DMMF 的な
     段階型パターンでは、同じフィールド名を持つ record が自然に並び、
     literal のたびに `let v : T` 注釈が要る。曖昧さ解消の緩和
     （フィールド型・文脈での絞り込みなど）を考えるか。
   - → open-questions「言語機能」

6. **符号なし整数（`byte`/`uint8` 以外）**
   - 観測: `text/tabwriter` の `NewWriter`/`Init` は `flags uint` で
     E0501 — uint 系が非対応なので tabwriter 相当は手書きパディングに
     なった（tabl）。`uint`/`uint16`/`uint32`/`uint64` は spec §8.3 で
     v1 非対応と明記済み。
   - → open-questions「Future work」に 1 行追加

### PR #196 ブランチ側で tracker に登録済みのもの（本 PR で main に反映）

- `gom info` が stdlib 短名を解決しない → open-questions「ツール」
- `.gomi` を `gom info` の生成物として管理する場合の機能不足 →
  open-questions「ツール」
- `gom info` の再生成が `//gom:noescape`/`//gom:result-notaint` を落とす
  → implementation-gaps
- `gom explain E0513` の nil-able 一覧に `Go.FuncN` がない →
  implementation-gaps

### 既存 tracker 項目への追記

- **`any` 引数経由の関数値の静かな罠**: open-questions の部分適用 lint
  項目の残問として既に open。run 2 の recheck/partapp でも `fmt.Println f`
  が無警告で `0x...` 表示になることを再確認 — 既存項目に追記済み。

### 対応不要・仕様済み（tracker に載せない）

- **匿名 func 値の二重アダプタ**（`bufio.ScanWords` は import 面で
  カリー化 Gom fn、`sc.Split` の `bufio.SplitFunc` param は `Go.Func2`
  生ビュー → `Go.func2` で再ラップ）— spec §8.8 の「暗黙変換なし・
  `Go.funcN`/`Go.fnN` 明示変換のみ」という設計どおり。直感とのずれは
  DX の話であり、エラーメッセージ（E0401）がすぐ答えを出す。
- **Go の `int64` は gom の `Int`**（`Int64` は別のサイズ付き型）—
  spec §8.3 の対応表に明記済み。`big.NewInt` には `Go.conv x : Int` で
  通る。名前の直感に反するが仕様どおり。
- **`match` 腕の `,` 区切り・`let` ラムダの非再帰・`Go.conv` の
  ascription が式を閉じる・`{ }` 内の改行区切り** — spec/grammar 済み。
  gotchas.md の「自己解決できるもの」表に記録済み。
- **`Char` → rune 系フィールドの `Go.conv`** — 既存 open-question
  （`Char` ↔ `Int32` の橋渡し）。
- **1 ディレクトリ 1 パッケージ・`Go.Map.to_list` の順序不定・
  `List.range` の上端排他・`h.Sum` の List 面パラメータへの `Go.nil`
  （E0513）** — いずれも仕様済みの設計（List は nil-able でない）。
- **`iota` 定数セット・unsafe/reflect/cgo・E0506 の generics 制約** —
  Future work / spec 済みの既存項目。
- **`Go.conv` の `List` → named slice の抜け穴** — wave38 lane E で
  解消済み（run 2 では未検証だが修正が入っている）。
- **`fmt.Println` の `Option` 表示・`Go.Map` の `Map K Unit` 不在** —
  軽微（`Map K Bool` で代用可）。
- **Go 側ライブラリの仕様**（`regexp` の `$2_` 吸収、`csv` の
  `FieldsPerRecord = 0` の意味、`os.Rename` の上書き、`json.Number`、
  `time` layout）— gom の問題ではない。gotchas.md §4 に記録済み。

## 所感

run 1 と同じく言語の核（ADT/match/try/並行 fan-out）に破綻はなく、
今回の壁は「uint 非対応 API」「`Go.Slice` の生成面」「`gom run` の
非推移 build」という端の慣用句と CLI の挙動に寄っている。DMMF 的な
型遷移ワークフローは得意領域であることも確認できた（shop/ の
`.gomi` 付き domain パッケージ + abstract 宣言でコンストラクタを
封じる形まで到達）。むしろ新しい発見は「`.gomi` の abstract 宣言が
record にも効く」という仕様化されていない便利な挙動のほう。
