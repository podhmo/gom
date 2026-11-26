# wave49 prompts（草案）

- 日付: 2026-11-27
- 状態: 採択（podhmo、2026-11-27）。順 A→C→B→D→E→F。lane 実装は wave48 lane H/closeout merge 後
- 入力: [wave48 round](2026-11-26-wave48-round.md)、[TODO](../../TODO.md)、[open-questions](../open-questions.md)、[implementation-gaps](../implementation-gaps.md)、[problems](../problems/)、[DMMF fit](ja/2026-11-26-dmmf-order-taking-fit.md)、[lane H audit](2026-11-27-wave48-lane-h-dmmf-fit-audit.md)

wave48 は correctness bug、cache/emit、interface metadata、stdlib `str`、spec 整合、owner ideas / DMMF fit の台帳化まで終わった。wave49 は「明確な correctness 修正を先に潰し、DMMF で効く表面を小さいものから決める」構成を推薦する。

## 推薦 lane

### A. `&&` / `||` の生成コードを短絡評価に揃える（小〜中）

evaluator は `false && x` / `true || x` で右辺を評価しないが、lower は両 operand を先に atom 化し、生成コードでは副作用や panic が先に走る。correctness bug として lower/ANF の評価順を修正し、spec に短絡規則を明記する。副作用または panic を持つ右辺で eval/compiled の一致を fixture にする。guide の「generated Go では短絡しない」という現状注記も更新する。

### B. record literal E0407 の決着（中〜大）

`UnvalidatedOrder` / `ValidatedOrder` / `PricedOrder` のような段階型では同名フィールドが自然に並び、`Ok { ... }` や無名 record の `List.fold` accumulator で E0407 が頻発する。sandbox と DMMF の両方で再観測された最大級の摩擦。wave49 で正を決めて実装する。

推薦案: PR #256 F2 / DMMF fit にある型名前置 record literal（例: `ValidatedOrder { id = id, ... }`）を採用し、文脈推論の一般化は後続に残す。代替案は expected type / field type を使う推論強化。両方入れると意味論とテスト面が大きいので、まずどちらを正にするか採否時に決める。

### C. stdlib Result/Option traverse helpers（小）

DMMF の `List.map ... |> Result.sequence` が毎回ユーザー helper を要する。`Result.to_option` は既存なので対象外。不足している traversal 面を stdlib に追加する。

推薦案: `Result.sequence : List (Result a e) -> Result (List a) e` と `List.traverse_result (f: a -> Result b e) (xs: List a) -> Result (List b) e` に限定する。Option を Result へ畳む helper は命名と必要性が弱いため今回は入れない。配置には既存の制約がある: 両方を素直に所属パッケージへ実装すると `result` が `list` を、`list` が `result` を import して循環する。`List.traverse_result` は `list` 側に実装し、`Result.sequence` は [stdlib-conv-placement](../decisions/2026-11-18-stdlib-conv-placement.md) と同じ compiler-owned forwarding member として `list` 側実装へ寄せる。実装本体が `@extern` buffer-built Go か pure Gom + 既存最適化のどちらが既存 stdlib 流儀に合うかは lane 内で確認する。

### D. E0301 text 出力の不足パターン詳細（中）

spec §4 の text 例は `patterns not covered:` を示すが、現行 text emitter は `data.missing_patterns` を出せない。DMMF の進化テストでは「どの variant が不足か」が最重要情報。wave49 で人間向け出力を仕様へ寄せる。

推薦案: いきなり汎用 `labels` / `details` wire を設計せず、E0301 の `missing_patterns` を text emitter が構造的に描く小さな修正から入れる。ただし spec 例と同じ形にするか、汎用 details を先に導入するかは採否時に決める。E0401 の期待型由来など broader な副 span は対象外。

### E. DMMF boundary guide（小〜中、docs）

言語機能を足す前に、DMMF 型のドメインモデリングで必要な境界ルールを guide / spec 参照付きの文書として決める。対象は次の 3 点に絞る。

- JSON tagged union / nullable field の推奨 DTO 表現（`Go.Map String Go.Any`、専用 DTO record、`Ptr` nullable の使い分け）
- bounded context の package / visibility 設計（`internal/` layout、型ごとの package 分割、`string50_create` 命名）
- 金額表現（integer minor units 推奨か、外部 decimal 許容か）

abstract 型の zero value 制限や panic capture adapter のような言語/ランタイム変更は対象外。文書化の結論が open-questions のどれを close できるかも lane 内で台帳へ反映する。

### F. implementation-gaps archive 分割（小）

podhmo が PR #256 で肯定した archive 分割を実施する。`[x]` 項目を `docs/implementation-gaps-archive.md` へ移し、live 台帳は未解消だけにする。機械的な移動で済むか、残すべき文脈があるかを確認し、README/リンクを更新する。台帳が小さくなった後で wave49 後半の gap 管理をしやすくする。

推薦順: A → C → B → D → E → F。A/C は小さく先に終わらせ、B/D は設計の重い順、E/F は後半の締めに近い面として置く。

## 採否インタビュー結果

1. A〜F はすべて採用（podhmo、2026-11-27）
2. B は型名前置 record literal を第一案とし、文脈推論強化は後続に残す
3. C は `Result.sequence` + `List.traverse_result` に限定し、Option traverse は入れない。`Result.sequence` は compiler-owned forwarding member として `list` 側実装へ寄せる
4. D は E0301 限定の text 改善とし、汎用 `labels` / `details` wire は今回の前提にしない
5. E は JSON / bounded context / decimal の 3 点に絞る
6. module 外 `gom run` / `gom init` は wave49 に追加しない

## 採否を確認したい点

1. 推薦 6 lane（A〜F）をそのまま採用するか。落とす lane があれば指定する
2. B は型名前置 record literal を第一案にするか。それとも文脈推論強化を第一案にするか、wave49 でも見送るか
3. C の surface は `Result.sequence` + `List.traverse_result` に限定してよいか。Option traverse まで入れるか
4. D は E0301 限定の text 改善でよいか。先に汎用 `labels` / `details` wire を設計するか
5. E の docs laneを wave49 に入れるか。入れる場合、JSON / bounded context / decimal の 3 点に絞ってよいか
6. module 外 `gom run` / `gom init` を追加するか。追加するなら F の archive か E の docs どちらかを落とすことを推奨する

## 見送り候補（少なくとも現時点では lane にしない）

- panic capture adapter — spec は言語 `recover` を提供しない方針で、具体的な阻塞ケースがまだない
- abstract 型の zero value 制限 — E の boundary guide で推奨運用を決めてから、lint/制限の要否を見る方が分岐が小さい
- module 外 `gom run` scratch module / `gom init` — 価値はあるが wave49 が過密。確認質問 6 で追加余地だけ残す
- E0201 typo 候補、resolve/type error 後のエラー回復、`gom eval` / REPL、型シグネチャ search — いずれも設計が大きく、D の診断面が先
- `str` surface 拡張（`fold_chars`、panic しない slice、Unicode 一般処理） — 需要はあるが DMMF 主軸ではない
- checker 分割、rt 表示一元化、診断コード meta test、benchmark 基盤 — 有効な内部改善だが、今回はユーザー可視の correctness / DX を優先
- stack-carrying error、match 到達不能 arm、anonymous-record fold inference — B の record literal 決着で一部状況が変わるため、その後に再評価
- constructor 名の型スコープ化、custom equality、Applicative validation 糖衣 — lane H audit で需要不足として登録しない判断を維持
- Go 定数セット / `iota`、`gom install`、cgo/unsafe/reflect、セルフホスト — future work 維持
