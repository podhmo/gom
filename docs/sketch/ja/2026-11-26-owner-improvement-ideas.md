# オーナー視点の改善アイデア集 — 2026-11-26

「自由に変えてよい」前提で、コード改善・機能の追加と洗練・UX・性能の 4 方向について改善案を出したメモ。規範ではない。採否は wave の採否インタビュー（[wave-process](../../ja/wave-process.md)）で決める前提で、各案に「観測」「提案」「効果」「コスト感」を付けた。

観測は 2026-11-26 の main（`56a7e89`）で `go build ./cmd/gom` したバイナリを、手書きの小さな `.gom` に対して実際に走らせて取った。既存の台帳（[TODO](../../../TODO.md)・[open-questions](../../open-questions.md)・[implementation-gaps](../../implementation-gaps.md)・[problems](../../problems/)）にすでにあるものは、重複登録せずリンクで示す。

## 優先度の一覧

| # | 案 | 方向 | 効果 | コスト | 推奨 |
| --- | --- | --- | --- | --- | --- |
| U1 | 継続行の誤パースを 1 件の専用診断＋fix にまとめる | UX | 大 | 小 | ◎ 次の wave |
| U2 | text 診断に「詳細行」と「ラベル付き副 span」を通す | UX / コード | 大 | 中 | ◎ |
| U3 | 端末なら text を既定にする | UX | 中 | 小 | ◎ |
| U4 | E0201 に「もしかして」候補と import 補完 fix を付ける | UX | 中 | 小 | ○ |
| U5 | エラー回復を広げる（resolve エラーで止めない・宣言単位の網羅性検査） | UX | 中 | 中 | ○ |
| U6 | 型表示の `?1` を読める形にし、期待型の出どころを示す | UX | 中 | 小〜中 | ○ |
| U7 | `gom run` をモジュール外でも動かす＋`gom init` | UX | 大 | 中 | ◎（TODO の推移 build バグとセット） |
| U8 | `gom lsp`（最小構成） | UX | 大 | 大 | ○ 段階導入 |
| U9 | `gom eval` / REPL（既存の evaluator を利用者に開く） | UX / 機能 | 中 | 中 | △ |
| U10 | 型シグネチャで stdlib を引く `gom search` | UX | 中 | 小〜中 | △ |
| F1 | 組み込み `String` モジュール | 機能 | 大 | 中 | ◎（open question の決着案） |
| F2 | 型名付き record literal `T { ... }` | 機能 | 中 | 小 | ○（open question の決着案） |
| F3 | `.gom` 側の `@abstract` / `@private` を `gom info` が引き継ぐ | 機能 | 中 | 中 | ○（open question の決着案） |
| C1 | `checker` の状態を関心ごとの部分構造体に分ける | コード | 中 | 中 | ○ |
| C2 | 値の表示を rt の単一実装に寄せきる（eval と compiled の重複を減らす） | コード | 大 | 中 | ◎ |
| C3 | 診断コード表を軸にしたメタテスト | コード | 中 | 小 | ○ |
| C4 | `implementation-gaps.md` の解消済み項目を archive に分ける | 文書 | 小 | 小 | ○ |
| P1 | stdlib の export data decode をプロセス内で共有する | 性能 | 中 | 中 | ○ |
| P2 | テストスイートを 31s から短くする（compile-only 検証の go/types 化） | 性能 | 大 | 中 | ○ |
| P3 | 生成コードのベンチマーク基盤 | 性能 | 中 | 小 | ○ |

「◎」は次の 1〜2 wave で取りたいもの、「○」は価値がはっきりしているが急がないもの、「△」は需要が見えてから。

---

## UX

### U1. 継続行の誤パースを 1 件の専用診断にまとめる

**観測**: selfish 移植でも報告された「カリー適用を複数行に書くと崩れる」問題（[selfish-port](2026-11-24-selfish-port.md)）を再現すると、エラーが 6 件連なる。

```gom
  let s = List.fold
    (fn acc x -> acc + x)
    0
    xs
```

```
error[E0407]: type annotation required: the type argument for `a` could not be determined
error[E0407]: type annotation required: the type argument for `b` could not be determined
error[E0402]: a mid-block expression must have type Unit, got ?1 -> ?1 -> ?1
error[E0407]: type annotation required: the lambda's return type could not be determined
error[E0402]: a mid-block expression must have type Unit, got Int
error[E0402]: a mid-block expression must have type Unit, got List ?1
```

どれも本当の原因（「改行で式が切れている」）を言っておらず、`help: discard the value with ignore` は逆方向の誘導になっている。

**提案**: ブロック内の文が、直前の文の開始列より深いインデントで始まり、かつ直前の文が改行で終わっている場合（= 書き手は継続のつもり）を parser か check の入口で検出し、次の 1 件にまとめる。

```
error[E0xxx]: this line is parsed as a separate statement
  --> m2.gom:8:5
   |
7  |   let s = List.fold
   |           --------- the application ends here
8  |     (fn acc x -> acc + x)
   |     ^^^^^^^^^^^^^^^^^^^^^ indented like a continuation
   = help: wrap the whole application in parentheses
```

`(List.fold ... xs)` で囲む fix は意味を変えないので safe にできる。検出された文は後続の E0402/E0407 を抑止する（連鎖を出さない）。

言語側で offside 規則による継続を入れるかは別の問い（open-questions に「複数行適用の継続規則」として起こす）。まずは診断だけで体験の大半が直る。

**コスト**: 小。インデント情報は lexer がすでに持っている。spec に新コードを足し、`testdata/spec/` に fixture を足す。

### U2. text 診断に「詳細行」と「ラベル付き副 span」を通す

**観測**: E0301 の JSON は `data.missing_patterns: ["Rect _ _"]` と fix の本文 `Rect _ _ -> todo ()` を持っているのに、text 出力には出てこない。

```
error[E0301]: non-exhaustive match on `Shape`
   --> main.gom:9:31
    |
 9  | fn area (s: Shape) -> Float = match s {
    |                               ^^^^^^^^^
10  |   Circle r -> 3.14159 * r * r
    | ^^^^^^^^^^^^^^^^^^^^^^^^^^^^^
11  | }
    | ^
    = help: add the missing arms (placeholder)
```

人間が一番知りたい「何が足りないか」が見えない。match 全体に下線を引いているのでノイズも多い。これは [problems/text-format-detail-block](../../problems/2026-11-02-text-format-detail-block.md) が 2026-11-02 から未解決のまま指摘している「spec の text 例をモデルが出せない」問題そのもの。

**提案**: format 層に `data` を解釈させる（problem が退けた案）のではなく、`diag.Diagnostic` に 2 つの概念を足す。

- `Labels []Label`（副 span ＋短い文言）: rustc の secondary label。E0401 の「期待型の出どころ」（U6）や U1 の「適用はここで終わる」に使う
- `Details []string`（本文の下に出す詳細行）: E0301 の「patterns not covered:」の一覧

作るのは各フェーズ（pattern が missing patterns から Details を作る）なので、「コンパイラ本体は出力形式を知らない」という design の原則は崩れない。JSON wire には `labels` / `details` を追加フィールドとして足す（既存フィールドは不変、`gom schema` を更新）。あわせて E0301 の primary span を `match s` までに縮め、`help:` には placeholder fix の本文も 1 行で出す。

**効果**: spec §4 の text 例がそのまま出るようになり、problem を閉じられる。LSP（U8）の `relatedInformation` にもそのまま写る。

### U3. 端末なら text を既定にする

**観測**: `gom check` / `gom explain` は既定で JSON Lines を出す。エージェントと CI にはこれが正しいが、人が手で叩くと毎回 `-format text` が要る。`gom explain E0301` を人が読むのは JSON の一行で、ほぼ読めない。

**提案**: `-format` の既定を `auto` にし、出力先が端末（`ModeCharDevice`）なら text、パイプ・ファイルなら json にする。`GOM_FORMAT=json|text` で固定もできるようにする。パイプ先では今と同じ出力になるので、エージェントや CI から見た互換性は保たれる。

**コスト**: 小。ただし出力形式の既定は利用者から見える振る舞いなので、decision と spec の更新が要る。

### U4. E0201 に「もしかして」候補と import 補完 fix を付ける

**観測**: `aera x` は `undefined name 'aera'` だけを出す。resolve / check に候補提示の仕組みはない。

**提案**:

- 同じ名前空間のスコープにある名前から編集距離（Damerau–Levenshtein、名前長に応じて閾値 1〜2）で候補を出す。`List.lenght` のようなモジュールメンバーでも同じことをする
- 未 import の修飾名（`strings.Split` で `strings` が未定義）で、標準ライブラリにその名前のパッケージがあれば `import go "strings"` を足す fix を付ける。Go パッケージは実在するので safe にしてよい
- 候補は `data.candidates` と（U2 の）`help:` に出す

**効果**: LLM が書いたコードの典型的な失敗（Go / OCaml の名前で書く）に直接効く。`gom fix` との相性もよい。

### U5. エラー回復を広げる

**観測**: resolve エラーが 1 つでもあると型エラーも網羅性エラーも出ない。また型エラーがあるファイルでは網羅性検査（pattern）が走らず、型を直したところで初めて E0301 が出る。1 回の check で見える情報が少なく、直す → 待つ → 次のエラー、という往復が増える。

**提案**:

- 未定義名を「エラー型」（どこへでも unify して、追加の診断を出さない型）として check に通す。check はすでに部分的に回復しているので、入口を広げるだけで済む
- pattern を宣言単位で走らせる。型エラーを含まない宣言の match は検査できる

**コスト**: 中。連鎖エラーを出さないことを fixture で固める必要がある（`testdata/spec/` に「エラー 1 件だけを期待する」ケースを足す）。

### U6. 型表示の `?1` を読める形にし、期待型の出どころを示す

**観測**: `got ?1 -> ?1 -> ?1`、`got List ?1` のように内部のメタ変数番号がそのまま出る。E0401 は `expected Shape, got Float` だけで、なぜ `Shape` が期待されたのか（`area` の引数 `s` の宣言）は自分で探すことになる。

**提案**:

- 診断ごとにメタ変数を `_a`, `_b`… に振り直して表示する。番号は 1 診断の中で安定していればよい
- E0401 には U2 の副ラベルで「expected because of this」を付ける。check の conform 系はすでに want 側の由来（宣言パラメータ・注釈・分岐の先頭腕）を知っているので、それを `Label` として載せる

### U7. `gom run` をモジュール外でも動かす＋`gom init`

**観測**: README の例をそのまま空のディレクトリで `gom run main.gom` すると、`go: go.mod file not found` が E0001 として返る。生成コードが `github.com/podhmo/gom/rt` を import するので go.mod が必須になるが、その説明はどこにもない。さらに TODO 末尾の「`gom run` / `gom build` が gom の `import` 先を推移的に build しない」バグもあり、「とりあえず動かす」までの壁が 2 枚ある。

**提案**:

- `gom init [module-path]` を足す: `go.mod`（gom の require、開発中は `replace`）と `main.gom` の雛形を書く。tutorial の各章にある go.mod（D-4）もこれで生成できる
- `gom run` がモジュール外で呼ばれたら、一時ディレクトリにスクラッチモジュールを合成して実行する（`go run file.go` がモジュール外でも動くのと同じ体験）
- 推移 build バグを直す: `gom run` / `gom build` が、`import` した gom パッケージの dir を Kahn 層の compile グループに自動で加える（並列 compile と増分キャッシュの仕組みはすでにある）

**効果**: 初回体験の失敗がなくなる。sandbox 探索 run 2 でエージェントが嵌った点（[gotchas §2](2026-11-24-sandbox-explorer2-gotchas.md)）も同時に解消する。

### U8. `gom lsp`（最小構成）

**観測**: design は LSP を診断の出力先の 1 つに数えているが、実装はない。エディタ支援は Emacs の `gom-mode` と tree-sitter 文法だけ。一方で、LSP の土台はほぼ揃っている。

- 診断の内部表現（バイトオフセットを正とする位置、fix の edit）
- 型付き AST（式ごとの型、適用ノードの CallInfo）
- 0.015s で返る `gom check` と `.gomi` 境界の増分キャッシュ
- `gom fmt`

**提案**: 段階的に入れる。

1. publishDiagnostics＋codeAction（safe fix をそのまま写す）＋formatting。ファイル保存ごとにパッケージを再 check するだけでよい
2. hover（型付き AST の位置検索で式の型を出す）と go-to-definition（resolve の Symbol が宣言位置を持っている）
3. completion（スコープ＋モジュールメンバー＋Go パッケージのメンバー）

position の UTF-16 変換は `internal/diag` の lineindex に足す。goimport の Loader をプロセス内で使い回す必要があり、これは P1 と同じ問題（open-questions に「LSP の Go import oracle の cache/再利用」として既出）。

**コスト**: 大。ただし段階 1 だけでも「保存するとエラーに波線が出る」体験になり、効果は大きい。

### U9. `gom eval` / REPL

**観測**: `internal/eval` は 8,000 行ある tree-walking evaluator だが、意味論比較テストのオラクルとしてしか使われておらず、利用者からは見えない。

**提案**: `gom eval -e 'List.map (fn x -> x * 2) [1, 2, 3]'` で式を評価して、型と値を表示する。純粋な式なら evaluator で即座に返し、Go 呼び出しを含む式は一時の `main` を合成して `gom run` の経路に落とす。行単位の REPL はその上に載せる。

学習用途に加え、エージェントが「この式の型は何か」「stdlib の関数がどう振る舞うか」を確かめる手段になる。eval の表示はすでに compiled 側と揃えてある（wave45〜47）ので、その投資が利用者に届く形にもなる。

**懸念**: eval のカバー範囲（Go 相互運用・並行）が compiled より狭いので、どちらの経路で評価したかを表示に出す必要がある。

### U10. 型シグネチャで stdlib を引く `gom search`

`gom info` で短名を引けるようになった（wave47 lane B）。その次の段階として、`gom search 'List a -> (a -> Option b) -> List b'` で、型が単一化できる stdlib / 組み込みモジュールの関数を一覧する（Hoogle 相当）。`filter_map` が stdlib にないと思い込んで `fold` で書いた、という selfish 移植の事例（実際は後から追加された）は、名前を知らなくても型から引ければ起きにくい。実装は既存の unify を使ったフィルタで足りる。

---

## 機能の追加と洗練

### F1. 組み込み `String` モジュール

open-questions の「文字列の操作面（len・部分列・添字）」に対する決着案。

**提案**: `Go.Slice` / `Go.Map` と同じ ctor-table 経路の組み込みモジュール `String` を rt バックで足す。

| 関数 | 型 | 備考 |
| --- | --- | --- |
| `String.byte_length` | `String -> Int` | Go の `len(s)`。名前でバイト単位であることを明示 |
| `String.chars` | `String -> List Char` | rune 単位の走査（`strings.Split (s, "")` の迂回を置き換える） |
| `String.get` | `String -> Int -> Option Byte` | 範囲外は `None` |
| `String.slice` | `String -> Int -> Int -> Option String` | バイト境界。範囲外は `None`（Go のように panic させない） |
| `String.fold_chars` | `(b -> Char -> b) -> b -> String -> b` | 中間 List を作らない走査 |

`strings` パッケージで足りるもの（`Split`/`Join`/`Contains`…）は重ねて提供しない。「Go にないもの、または Go では演算子・構文でしか書けないもの」だけを提供する、という線引きを decision に書く。

### F2. 型名付き record literal `T { ... }`

open-questions の「フィールド名の共通する record literal の曖昧さ（E0407）」と、gaps の「無名レコードの `List.fold` の acc が推論できない」は同じ根を持つ。

**提案**: 段階型パターン（`UnvalidatedOrder` → `ValidatedOrder`）のための推論を賢くするより先に、**型名を前置したリテラル** `ValidatedOrder { id = 1, ... }` を足す。

- 推論の改良（フィールド型での候補の絞り込み）は、決まる・決まらないの境界が利用者に読めなくなる。明示構文なら境界が明快
- `let v : T = { ... }` より短く、式の途中（`List.fold` の初期値など）にも書ける
- `gom fix` で、E0407 の候補が 1 つに絞れる場合に型名を前置する fix を付けられる

構文上は「大文字の型名の直後に `{`」なので、コンストラクタ適用との衝突は起きにくい。grammar.ebnf での確認は要る。

### F3. `.gom` 側の `@abstract` / `@private` を `gom info` が引き継ぐ

open-questions の「`.gomi` の abstract/private 意図を再生成に残す経路がない」に対する決着案。

**提案**: `//gom:mutable` の「著者が書き、再生成が引き継ぐ」モデルを `.gomi` 側に広げるのではなく、**意図は `.gom` 側の属性に置く**。

```gom
@abstract
type Order = { id: Int, items: List Item }   // .gomi では `type Order` になる

@private
fn helper (x: Int) -> Int = ...              // .gomi に出さない
```

`.gomi` は完全な生成物にでき、「draft を一度生成したら手で保守する」運用が要らなくなる。`.gomi` の手編集は従来どおり許し、適合検査（E0601 など）は手編集にも同じように効く。

---

## コード改善

### C1. `checker` の状態を関心ごとの部分構造体に分ける

**観測**: `internal/check` は空行を除いて 23,000 行あり、全パッケージで最大。`expr.go` は 4,549 行。`checker` 構造体のフィールドは 70 を超え、型表・lint 状態・taint 推論のメモ・遅延適用の待ち行列・可視性が 1 つの構造体に平置きされている。どのフィールドがどの段階で有効なのかは、コメントを読まないとわからない。

**提案**（振る舞いは変えない）:

- `typeTables`（tyCons / ctors / fieldSyms / records / foreignRecs）、`deferred`（pendingSels / pendingApplies と revisit の不動点）、`taint`（resultTaintMemo / Doing）、`lintState` に分けて埋め込む。finalize の各段階が触る部分構造体を引数で明示する
- `expr.go` を構文の種類ごと（record / literal / select / block / match）に分割する。`apply.go` はすでに分かれているので、その粒度に揃える
- 遅延適用の replay（`replayGoApply` / `replayGomApply`）は「レシーバ確定待ちの制約キュー」として小さな型にまとめ、不動点ループの終了条件をその型の不変条件として単体テストする

wave の lane が同じファイルで衝突する頻度も下がる。

### C2. 値の表示を rt の単一実装に寄せきる

**観測**: wave45〜47 の lane のうち少なくとも 6 本（45-B/C、46-A/C/D、47-A）が「eval と compiled で表示・値の幅がずれる」問題の修正だった。evaluator が rt と別に表示ロジック（`writeOperand` など）と数値幅の規則を持っていることが、ずれの温床になっている。wave47 lane A で runtime renderer の共通化が始まった。

**提案**: 方向を「eval は rt の関数を直接呼ぶ」に定める。表示（`gomShow` 系）、整数幅の演算・ラップアラウンド、float32 の丸め、structural equality を rt の公開関数に一本化し、eval は値を rt の表現（または rt が受け取れる形）に寄せて呼ぶ。eval 独自の実装は「評価順・パターンの振り分け・部分適用」という本来のオラクル対象に限定する。

**効果**: 意味論比較テストが「同じ表示コードを 2 回書いたか」ではなく「評価の意味論が同じか」を検査するようになる。テストの価値が上がり、保守対象は減る。

### C3. 診断コード表を軸にしたメタテスト

`internal/diag/codes.go`（1,756 行）を正として、次を 1 本のテストで機械検査する（未整備のものだけ足す）。

- すべての error / warning コードに `testdata/spec/` の fixture が少なくとも 1 つある
- `gom explain` の `example` が、そのコードを実際に出す（example を check して、期待コードが出ることを確かめる）
- `docs/error-codes.md` とコード表の一覧が一致する

implementation-gaps の「`gom explain E0513` の nil-able 一覧に `Go.FuncN` がない」のような説明文のずれは機械では取れないが、example を実行すると構造的なずれは取れる。

### C4. `implementation-gaps.md` の解消済み項目を archive に分ける

台帳は 1,994 行あり、未解消は 10 件だけ。TODO.md と TODO-archive.md の関係と同じく、`[x]` の項目を `implementation-gaps-archive.md` に移すと、未解消のずれが一目で見える。台帳冒頭の書式（解消時に対応と検証へのリンクを残す）は archive 側で維持する。

---

## 性能

コンパイラ本体は十分に速い（手元の小さなファイルで `gom check` が warm 0.015s）。効くのは goimport と、テストと、生成コードの 3 か所。

### P1. stdlib の export data decode をプロセス内で共有する

[build-perf](2026-11-24-gom-build-perf.md) の残課題 4: `gcimporter.Import` がモジュールプールごとに stdlib を decode し直していて、108 dir 一括 build の 25% を占める。importer をまたぐと `*types.Named` の identity が切れるので、単純な process キャッシュはできない。

**提案**: 「stdlib は GOROOT とビルド条件が同じなら不変」を利用して、**stdlib 専用の importer を 1 プロセスに 1 つだけ持ち**、モジュール側の importer はその stdlib パッケージを親として参照させる（`importer.ForCompiler` の lookup で stdlib パスなら共有側の `*types.Package` を返す）。identity が切れるのはモジュール間だけになる。モジュールプールが分かれている現状では、モジュールをまたいで `*types.Named` を比較する経路はないはずだが、これは今回確かめていない。着手前にその確認が要る。

LSP（U8）では Loader を長命にすることになるので、同じ仕組みがそのまま要る。

### P2. テストスイートを 31s から短くする

[problems/slow-tests](../../problems/2026-11-05-slow-tests.md) は 10 倍を目標に継続中で、フル実行は約 31s。残りの大きな塊は `TestEvalCompiledDiff` の約 10.5s、直列の中規模群の約 10s、NativeBinaryCache の検証の約 2.8s。

**提案**:

- **compile-only の検証を `go build` から `go/types` の in-process 型検査へ移す**。golden や「生成 Go がコンパイルできること」だけを見るテストは、子プロセスの `go build` ではなく、生成 Go を `go/types`（importer は P1 の共有 importer）で検査すれば足りる。リンクと実行が要るのは意味論比較だけ
- `TestEvalCompiledDiff` は、すでに試して退けられた「単純な一括ビルド」ではなく、**1 バイナリに N プログラムを `switch os.Args[1]` で同居させ、1 回のビルドで N 回実行する**形を試す。パッケージ名の衝突は生成時に prefix を付けて避ける
- `go test -json` の所要時間を CI で集計し、上位 10 件を `docs/sketch` の計測表に定期的に貼る（どこから削るかを毎回測り直さずに済む）

### P3. 生成コードのベンチマーク基盤

flat fn params（約 12 倍）や `concat` の buffer 化（約 72 倍）のように、生成コードの最適化は実測で大きく効いてきた。しかし計測はその都度の sketch に散っていて、回帰を検出する仕組みはない。

**提案**: `bench/` に、examples の代表的な処理（行処理・JSON・fold 主体の集計・`x :: acc` の逐次構築）を gom 版と手書き Go 版のペアで置き、`go test -bench` の比率（gom / Go）を記録する。比率が一定以上悪化したら CI で警告を出す。Future work の「List のハイブリッド表現」を再検討するときの判断材料も、ここから取れる。

---

## やらないと判断したもの

- 型クラスや `deriving` の復活: 明示的な辞書渡しと deriving 見送りの decision を覆すほどの新しい材料は今回なかった
- 名前の安定性保証（Go ABI）: 案 B（決定的な emit＋衝突検出）で十分
- 生成 Go を慣用的にする整形: 生成コードは「読めてデバッグできる成果物」であって保守対象ではない、という原則に反する

## 次の一手

U1・U3・U7 は小さく、独立していて、効果が大きいので、次の wave の lane 候補にしやすい。U2 は U6・U8 の前提になるので、先に decision（診断モデルへの `labels` / `details` 追加）を起こしておくとよい。F1〜F3 は open-questions の決着案なので、採否インタビューで方向だけでも決めたい。
