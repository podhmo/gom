# result-taint flag + .gomi export（案 B 拡張）

- 状態: adopted
- 日付: 2026-11-23

wave35 lane E。wave34 lane A の条件付き採択（「案 B に拡張する可能性を残す」
— [decision](2026-11-23-scope-result-leak.md)）に対し、result-taint flag を
`.gomi` にエクスポートする設計を調査して決める。

## 背景

案 A の `retainedProducers` はローカル宣言の本体を構文的に走査して
「結果が引数の生産物を運ぶ」を近似する。精度の残課題は2つある:

1. **imported callee は常に保守的 true**: `fnDeclOf` が nil の callee（外部
   パッケージ・Go・CallUnknown）には情報がなく、結果型が関数を含む呼び出しは
   引数ラムダの生産物を常に danger で walk する — `f` の生産物を捨てる
   外部関数にも E0419 の偽陽性が残る
2. **ローカルの transitive が拾えない**: `w f = lib_wrap f` で `lib_wrap` が
   引数の生産物を返すとき、`retainedProducers` は saturated call の callee
   シンボル（`lib_wrap`）だけを数え、引数位置の `f` を数えない — `w` の
   param `f` は producer に載らず false negative

## 設計（採択）

`Escapes` と対称な第2の flag ベクトルを `resolve.Symbol` に持たせる。

- `Symbol.ResultTaint []bool` — `ResultTaint[i]` が true のとき「結果は
  param i（flat 化されたパラメータ位置）の生産物を運び得る」。`Escapes[i]` が
  「callee が引数そのものを保持する」なのに対し、こちらは「引数の**生産物**が
  結果経由で漏れる」。`f 3` を結果に持つ `run_cb` は `Escapes[0]=false` で
  `ResultTaint[0]=true` — リークの本命パターン
- 推論: 各 `FnDecl` について `retainedProducers(body)` に含まれる param sym の
  位置を true とする。推論は fixpoint — saturated call の callee に対しては
  その callee の `ResultTaint` を引き、`lib_wrap f` の `f` が
  `lib_wrap.ResultTaint[0]` を見て伝播する（上記2の穴を閉じる）。相互再帰は
  「全部 true」の保守解で底を打つ
- `.gomi` export: `//gom:result-notaint <name> <i> ...` — `//gom:noescape` と
  同じ行形式で、taint **されない**位置の index を列挙する。`Escapes` が
  「列挙された位置のみ escape=false、欠落は escape=true」とするのと同じ
  極性 — 列挙されない位置と directive 欠落は taint=true（運び得る）を
  既定にする。**欠落時は保守方向に倒れる**ため、false を表現するには
  「taint しない位置を列挙する」形が要る — taint 位置を列挙する素直な形
  （`//gom:result-taint <name> <i>`）だと列挙・未列挙の両方が true になり
  false が表せず、imported callee の偽陽性解消という目的自体が果たせない
- 後方互換: 古いコンパイラは未知の `//gom:` 行をコメントとして無視する
  （`//gom:` 行頭プレフィックス以外は無関係）。directive を持たない古い
  .gomi は全位置 true で現行の保守動作と一致。手書き .gomi の注釈誤りは
  noescape と同じ信頼モデル（作者の約束）
- 消費側: `resultCarriesArg` / `resultCarriesPipe` が `fnDeclOf` 不成立の
  callee に flag を参照し、flag があれば正確・なければ従来どおり true。
  ローカル body があるときは従来どおり本体を読む（fixpoint 済みの flag と
  同値だが、body のほうが常に最新）

## 却下した選択肢

- **`.gomi` にエクスポートしない（ローカルのみ伝播）**: transitive の穴だけは
  閉じるが、imported callee の偽陽性 — 案 A の主な残課題 — が残る
- **型レベルの taint（関数型にフラグを埋め込む）**: 値として渡される関数の
  taint も追える最も強い形だが、types.Func の構造と unification を変える大
  工事。Symbol flag では first-class な関数値の taint は CallUnknown で保守的
  true に落ちる — 現実のリークは名前のあるライブラリ関数経由が主で、
  flag ベクトルで十分
- **detect しない・文書化のみ**: `Scope` 系 HOF はまさにこの形を取るため
  見逃しが苦い（wave34 lane A 調査済み）

## 残課題（採択後も残る近似）

- first-class 関数値（変数・record field・引数）の taint は追えない —
  CallUnknown は常に保守的 true
- `retainedProducers` の構文近似はそのまま引き継ぐ: `let` alias 経由・
  `Ref` 経由の保持は bindOf/aliases の近似精度に依存
- directive の index は flat パラメータ位置 — `~label` 部分適用で残る
  パラメータ位置との対応は ArgToParam で解決する（`resultCarriesArg` の
  現行経路と同じ）

## 実装スケッチ（別 lane）

- `resolve.Symbol.ResultTaint` + 推論 fixpoint（escape.go の Escapes 推論に
  隣接）
- `internal/iface/noescape.go` と対称の `ScanResultNoTaint` /
  `ApplyResultNoTaint`、`draft.go` に `resultNoTaintLine` writer
- `resultCarriesArg`/`resultCarriesPipe` で flag 参照
- spec 6.1 のアノテーション節に directive 追記 + spec fixture +
  gomi 書き出しの往復テスト

## 検証

- `run_cb`/`const_f` 型の probe: ローカルでは `retainedProducers` が正確に
  分ける現行動作を確認済み（wave34 lane A）
- 案 B の恩恵例: imported `lib_wrap`（`f` の生産物を捨てる）に引数ラムダを
  渡したとき flag なし→E0419、flag あり→無診断 — 実装 lane で fixture 化する
