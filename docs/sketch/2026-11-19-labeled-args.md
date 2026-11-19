# wave26 lane D: ラベル付き引数の制限緩和・省略可能引数の調査

- 日付: 2026-11-19
- 状態: 調査完了・採否は podhmo に委ねる
- 関連: open-questions「ラベル付き引数の制限の緩和」「省略可能
  引数とデフォルト値」、spec §2（ラベル付き必須引数）、
  [label-args-into-returned-functions](../decisions/2026-11-06-label-args-into-returned-functions.md)

## 問い

open-questions の 2 項目の設計空間を測る:

1. 型が未知の関数へのラベル付き適用（現行 E0408）
2. ラベル付き／なし関数の暗黙変換（現行不許可）
3. 省略可能引数とデフォルト値（現行なし — OCaml の `?x` 相当）

それぞれについて、現行規則の置き場所、導入時の構文・推論
コスト、`.gomi` / Go 境界への影響を整理する。

## 現行規則の置き場所

| 規則 | spec | 実装 |
| --- | --- | --- |
| ラベル付き適用は callee の型が既知を要求 | spec §2 注 5 | `checkGomApply`（internal/check/apply.go）— callee 型が未解決メタか平坦化パラメータ 0 件なら `~l:` に E0408、既知で未定義ラベルなら E0406、重複は E0405 |
| 位置引数はラベル付き仮引数に充てられない | spec §2 注 3 | 同上の `posIdx` 走査 + 超過分は `overApply`（unifyFunc のラベル run 規則が不一致を報告） |
| 型同値は連続ラベル run を名前順に整列比較 | spec §2 注 7 | `unifyFunc` の labeled-run ペアリング（unify.go:912–1010）、`types/equal.go` の `normalizeLabels` |
| ラベル付き／なしの暗黙変換なし | spec §2 注 8 | 同じ unifyFunc 規則 — ラベル run の先頭が位置パラメータなら別型 |
| 省略可能引数は持たない | spec §2 冒頭 | `types.FuncParam{Label, Type}` — Optional/Default の場がない |

引数→パラメータの対応表は `CallInfo.ArgToParam`（flat 位置への
写像）として `checkGomApply` が確定し、分類は `ClassMappedCall`
（filled prefix が宣言 arity を覆えば CallDirect、さもなくば
CallPartial）。下流の lower/codegen はこの写像だけを見る。

## 設計空間

### (1) 型が未知の関数へのラベル付き適用（E0408 緩和）

- **案 A: 遅延（defer）**。`~l:` を含む適用で callee 型が未解決
  メタなら、wave26 lane A が引き延ばしたのと同じ枠組み —
  `pendingApplies` に「ラベル制約 + 引数メタ」を記録し、callee
  型が bind された時点で `paramByLabel` を再試行。推論コストは
  低〜中（既存の revisitSelects/pendingApplies ループに乗る）。
  落とし穴: ラベルが結局どの flat 位置にも対応しない場合の
  E0406 は最終ラウンドで出す。`let f = g` のような値経由の
  呼び出しが主な受益者（宣言済み fn は今日既に既知）。
- **案 B: 構造的推測**。`?f ~l: x` で `?f := ~l: ?a -> ?r` と
  即座に束縛。実装は最小だが、後から来た制約（同じ f への
  位置引数適用、`~l` とは別ラベル）と衝突したとき覆せない —
  初期の「メタへの即時 bind は order-dependent」という
  unification の方針と相性が悪い。**非推奨**。
- **案 C: 現状維持（注釈要求）**。コストゼロ。E0408 の出る
  頻度が実用上どれほどかは未計測（エラー件数は集計していない）。

### (2) ラベル付き／なし関数の暗黙変換

- **方向を分けて考える**:
  - `~l: T -> R` の値を `T -> R` の位置に渡す（labeled →
    unlabeled）は**安全な消去**: 呼び出し規約の情報が落ちるだけ
    で、生成コードの形は同じ。unifyFunc で「提供側のラベル run
    を期待側が位置パラメータで受ける場合はラベルを捨てて比較」
    に変えるだけ。逆方向（`T -> R` を `~l:` 期待に渡す）は
    eta-expansion が要るので同じ規則では扱えない — やるなら
    呼び出し側が `fn ~l: x -> f x` と包む明示的な変換のまま
    が誠実。
  - なお「部分適用の結果型」はラベルを残して良い: 残る
    パラメータのラベルは callee 宣言由来なので `ArgToParam` の
    前提（宣言ラベル）を壊さない。
- **推論コスト**: labeled→unlabeled の一方向だけなら unifyFunc
  の run 先頭分岐の追加で小さい。型同値（types.Equal）側も
  normalizeLabels に「一方が無ラベルなら緩める」非対称版が必要
  — equal.go は同値判定専用で方向を持たないので、unify 側だけ
  で閉じられる。
- **影響**: spec §2 注 8 の書き換え（「ラベル付き→なしの消去は
  許す」）。`.gomi` / Go 境界への影響なし（Go 呼出しはラベルを
  持たない）。

### (3) 省略可能引数とデフォルト値

構文の核は 2 モデル:

- **案 α: OCaml `?x` 型**。`fn f (?x: Int) -> ...` の `?x` は
  本体では `Option Int`、呼び出し側は `?x: 3`（Some 3）か省略
  （None）。**カリー化と相性が悪い**: 「省略」を確定できるのは
  適用の終わりで、部分適用が常態の言語では「いつ None を補うか」
  が自明でない。実装は apply の `used` 管理・`applyResidual`・
  partialCall emit 全てに波及 → **コスト大**。
- **案 β: 呼出し側 fill のデフォルト値**（C++ 的）。宣言は
  `fn f (x: Int) (?size: Int = 16)` など、デフォルト式をパラ
  メータに保持。呼び出しで省略された `?size` はデフォルト式を
  その場で差し込む（ノードとしては `checkExpr(デフォルト式)` を
  emit）。**カリー化と両立**: `used` が未充足でも `ClassMappedCall`
  は「非オプションが埋まれば CallDirect」、埋まらなければ
  デフォルト適用済みの部分関数値（closure）を emit — 「飽和の
  判定」は呼出し規約の分類規則を 1 箇所変えるだけ。推論コストは
  中: `types.FuncParam` に `Default syntax.Expr`（または index
  参照）を足し、apply の未充足スロット処理、applyResidual、
  ClassMappedCall の飽和条件を更新。
- **構文**: `fn f (?size: Int = 16)` — `?` で省略可能を表し
  `= e` でデフォルト式。呼び出し側は `f 1 ~size: 32`（ラベル
  必須にして位置指定を禁止する方が「どの省略スロットに充てるか」
  の規則が要らず安全）。`~size` 省略形はそのまま punning と
  衝突しない（値なし `~size` は今も同名変数参照）。
- **`.gomi`**: デフォルト式は API の一部 — `.gomi` に
  `fn f (x: Int) (?size: Int = 16)` と書く。式の妥当性
  （pure・interface で型が閉じる）を checker が検査する
  コストはあるが、実装側 `.gom` のデフォルトと `.gomi` の
  照合は既存のシグネチャ照合の延長。
- **Go 境界**: Go 関数はラベルも optional も持たないので影響
  なし（`import go` 宣言の arity は従来通り）。

## 推奨の組み合わせ

- (1) は**案 A（遅延）が自然** — wave26 lane A の仕組みに載る。
  実施するなら internal/check のみで閉じる。
- (2) は**labeled→unlabeled の一方向消去のみ**が費用対効果
  あり（unify.go + types/equal.go の調整）。逆方向は明示的に
  書かせるままが健全。
- (3) は**案 β（呼出し fill デフォルト）**。案 α の `Option`
  セマンティクスはカリー化と相性が悪く実装も広い。案 β でも
  触るのは syntax（`?x:` パース）、types.FuncParam、apply.go
  のスロット fill・ClassMappedCall・applyResidual、spec §2、
  `.gomi` のシグネチャ形 — 境界や lower の呼出し規約は
  CallInfo 経由で既に抽象化されている。
- 優先度の印象: (1) > (2) > (3)。E0408 は値経由の高階関数
  呼び出しで実害が出やすく、(3) は「ほしい」より「あると便利」
  レベル（現状も `Option` 引数 + `Option.get_or_else` で代替
  可能）。

## 未決として残る論点

- (1) の遅延ラベル解決で「最終的にどの flat 位置にも対応し
  ない」場合の E0406 タイミングと、quiet 化の粒度。
- (3) のデフォルト式の評価時点（宣言時の const 化か、呼出し
  毎の再評価か — Go/C++ なら後者、参照型デフォルトの共有は
  罠）と、`.gomi` で式を晒すか名前だけ晒すか。

## 結論の追記（2026-11-19, podhmo と協議）

- **省略可能引数は言語機能としては持たない方向** — 「モジュールが
  defaults record を公開し、呼び出し側が `{defaults with x = v}`
  で潰して record 引数として渡す」パターンで十分。`with` 更新は
  既存構文で、省略した項目は defaults の値が残るので事実上の
  optional 引数になる。実装コストが高い `?x:` 構文・スロット
  fill・`.gomi` デフォルト式照合は見送り。
- **Go との対応整理** — labeled-unrequired は options record
  相当、unlabeled required は positionals、labeled required
  `~x:` は Go にない概念なので生成コードでは positional に
  潰れる（必須性は gom check が保証する呼出し契約）。
- **record→ラベル引数の spread（`f ~..opts` 的な TS `...` /
  Python `**` 相当）は未実装** — record 引数1個で受ける方式が
  あれば要らないが、labeled 呼び出し互換がほしくなったら
  future work として検討。
