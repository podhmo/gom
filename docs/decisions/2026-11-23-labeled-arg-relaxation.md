# ラベル付き引数の制限緩和の調査

- 状態: adopted
- 日付: 2026-11-23

wave35 lane D。open-questions の Future work「ラベル付き引数の制限の緩和」
（型が未知の関数へのラベル付き適用、ラベル付き／なし関数の暗黙変換）を調査し、
緩和する範囲を決める。

## 現状の制限（probe で確認）

| 書きたいこと | 今日の結果 |
| --- | --- |
| `apply (fn x -> x)`（expected `~x: Int -> Int`） | E0401 + E0407。ラベルの有無は型の一部で一致しない |
| `f ~x: 1`（`f: Int -> Int`、型は既知） | E0406「no parameter labeled `~x`」 |
| `f ~x: 1`（`f` が meta のまま、型が未知） | E0408「labeled call requires the callee's type to be known」 |
| `transfer 1 2`（位置引数を `~amount` に充てる） | 過剰適用として meta に流れ、離れた位置で E0401 |
| `fn ~x -> x`（ラムダが自分でラベルを宣言） | E0101（構文が存在しない） |
| `(fn x -> x) : ~x: Int -> Int`（ascription で名付け） | E0401（一般 ascription は存在しない） |

## 論点

ラベルは**呼び出し側の契約**であって関数値の実行時属性ではない — `f ~b: 2 ~a: 1`
は check が `~label` → パラメータ位置に解決したうえで宣言順の位置引数として emit
する。したがって「値の型にラベルがない」のと「ラベル付きで呼ばれる」のは
共存可能で、制限は主に unification がラベルの有無を型の同一性に含めている点と、
パーサがラムダにラベルを書かせない点に集約される。

### 案 A: unlabeled → labeled の introduction（採択）

expected 型が `~x_i` を宣言する位置で、produced 型の同じ flat 位置にラベルのない
パラメータがある場合に限り、produced 側へ expected のラベルをそのまま順序対応で
貸す（`Int -> Int` ≤ `~x: Int -> Int`）。ラベル付き呼び出しは位置に解決して emit
するので値レベルの shim は不要。

- 効く例: `apply (fn x -> x)`、`takes (fn x y -> x + y)`、record field や変数に
  入れた unlabeled 関数を labeled 文脈へ渡す。HOF が labeled コールバックを要求
  してもラムダをそのまま書けるようになる — 今日は文法的に書きようがない。
- 方向は片方だけ。labeled → unlabeled（`fn ~x ->` を `Int -> Int` 文脈へ）は
  変換すると以後の呼び出しが位置引数のみでラベル契約を破れるため従来どおり
  拒否（eta 展開 `(fn x -> f ~x: x)` で明示）。
- ラベルはカリー化鎖の flat パラメータ列に対して位置対応で貸す（spec §2 規則 6
  の flat 解釈と同じ）。途中にラベル付きが混ざる produced 型（`Int -> ~y: Int -> Int`
  を `~x: Int -> ~y: Int -> Int` に合わせるなど）は貸さず E0401 のまま — 複雑さに
  見合わない。

### 案 B: meta へのラベル制約の後送り（reject）

`f ~x: 1` で `f` が meta のままのとき、「この meta は `~x` を持つ関数型」を
unification まで保持するにはラベル制約の postpone 機構（構造的レコード相当）が
要る。HM への侵食が大きく、得られるのは `fn f -> f ~x: 1` のような無注釈ラムダ
中の呼び出しだけ — E0408 に注釈の書き方を添える現行で十分。

### 案 C: 位置引数の labeled パラメータへの充填（reject）

`transfer 1 2` で `2` を `~amount` に充てる緩和は `~` の「呼び出し側で名指し必須」
という契約自体を崩す。現行の遠回しな E0401（過剰適用経由）の分かりにくさは
エラーメッセージ改善で扱う余地があるが本 lane の範囲外。

### 案 D: `fn ~x ->` ラムダラベル構文（採択・wave36 lane F）

案 A があればラムダが自前でラベルを名乗る必要は薄い（expected から借りられる）。
構文追加は小さいが必要性が生じてから。`fn` 宣言の `(~x: T)` と対称の形を取る
想定で open-questions に残す。

追記（2026-11-23）: wave36 prompts インタビューで採択 — lane F として
`fn` 宣言の `(~x: T)` と対称の構文を実装する
（[prompts](../sketch/2026-11-23-wave36-prompts.md)）。

## 決定

- 採択: 案 A。unification（または expected 伝播）で「produced のラベルなし
  パラメータに expected のラベルを位置対応で貸す」規則を追加。spec §2 の
  「暗黙変換はしない」をこの一方向の introduction を許す形に書き換える
- 実装は別 lane: check の unify/expected 伝播に触る。実施時は
  `unfilled_labels` の E0401 診断が introduction 経路では消えること、
  `.gomi` の fn 型記述（ラベルはインターフェースの一部のまま）に影響しない
  ことを確認する

## 検証

- 上表の probe すべて `gom check` で現行診断を確認（E0401/E0406/E0407/E0408/E0101）
- 案 A の安全性: ラベル解決が check で位置へ写像し emit は位置引数のみ、という
  現行パイプラインの確認（internal/check/apply.go の `paramByLabel` →
  `argToParam`）
