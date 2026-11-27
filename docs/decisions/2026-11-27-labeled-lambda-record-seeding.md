# labeled lambda の record seeding / structural record

- 状態: 採用（label-erased seeding を実装・検証した。structural record は不採用）
- 日付: 2026-11-27
- Supersedes: [record-contextual-inference](2026-11-27-record-contextual-inference.md) のうち「関数パラメータ label を含む文脈型は propagation しない」という除外

## 状況

wave50 lane E で record literal の文脈推論を入れたが、文脈型が関数パラメータ label を含む場合は propagation を行わない境界にした。一方で型等価には unlabeled → labeled の一方向 introduction があり、positional lambda は最終 conformance では labeled callback 要求を満たせる。推論時だけその文脈を使えないと、同じ positional lambda が最終的には合法なのに record update を推論できず、あいまい record literal の E0407 へ連鎖する。

また、静的に named record 型がどこにもない ad hoc accumulator shape は依然として作れない。selfish 移植では `List.fold` accumulator が代表例として観測されていた。

## 調査結果

検証記録: [sketch/2026-11-27-wave51-lane-e-labeled-records-verify](../sketch/2026-11-27-wave51-lane-e-labeled-records-verify.md)。

- `type Acc` / `type Other` が同じ field set を持つ probe では、unlabeled callback 要求の generic helper は結果の `Acc` 期待から record literal を推論できるが、同じ形の labeled callback 要求では positional lambda だけが E0401/E0407 へ落ちる。明示的に `fn ~acc ->` と書いた lambda は通る
- positional lambda は spec 上 `~acc` を位置対応で借りられるので、これは型を広げる要求ではなく、既に合法な値の推論が labeled 文脈で弱い問題である
- named record が存在しない ad hoc accumulator probe は従来どおり E0401/E0407 に落ちる。selfish 移植ではこの摩擦は実在したが、1 行の named accumulator 型で回避できる。DMMF 側は段階型・domain type を名前付きで設計するため、anonymous structural record の需要はむしろ低い

## 決定

### labeled callback 文脈: label-erased seeding / propagation を採用する

label を含む文脈関数型を positional lambda の推論へ渡すとき、label 自体は inference contract に入れず、parameter 順序と型だけを残した label-erased view を使う。

- unannotated positional lambda parameter の対応位置が record 型なら、その record 型で seed する
- erased view は推論だけに使い、accept/reject の意味判定には使わない。最終 conformance は従来どおり — produced 側が完全に unlabeled のときだけ位置対応で label を貸し、produced 側が自分の label を持つときは strict な run pairingで判定する
- label-erased view でも parameter 順序は変えない。generated Go ABI は positional なので、labeled run の reordering を推論中に正規化しない
- erase するのは positional lambda 自身が消費する parameter 位置だけに限る。残余の関数型に残る label は returned function の contract なので推論用 view でも落とさない
- speculative propagation の clone/rollback 構造は変えない。label 付き型は最終 conformance と明示 labeled lambda のために文脈型として運び、positional lambda の seeding へ入る時点で erased view を作る。positional lambda の inference variable に label を持ち込まないことが目的なので、merge 済みの型から後付けで意味判定を変えない

これは label introduction 全体の speculative propagation ではない。「合法な unlabeled 関数が借りられる label を、accept 前の型推論へ意味のない contract として持ち込まない」という限定規則である。

### structural record: 導入しない

匿名 record 型 / structural record は導入しない。named record 必須を仕様の正として維持する。

理由:

- gom の record は nominal な型 identity、generated Go struct/defined type、struct tag、`@method`、`.gomi` export、field-set 推論の曖昧さ判定と一体になっている。structural row typing を入れると、width subtyping または row unification、匿名型の Go 名、`.gomi` 表現、equality/display、recursive record 制限まで再設計が要る
- 主要な需要は ad hoc accumulator だが、selfish の実例では named accumulator 型 1 行で回避できる。DMMF の domain modeling では、段階・概念を名前付きにする方が意図に合う
- 同名 field set を持つ record を意図的に区別する今の nominal 性は、structural typing と直接衝突する

将来、ad hoc shape が named 化では説明できない頻度・形で観測された場合は、structural record 全般ではなく「型名を持たない accumulator 専用の推論」や「匿名 record 型の明示注釈構文」など、小さい設計から再検討する。

## 実装と検証

wave52 lane A で実装した。`checkLambda` は完全に positional な lambda に限り、その lambda が消費する expected parameter 位置の label を inference 用に落として対応させる。残余 parameter の label は残す。speculative propagation は clone/rollback を変えず、label mismatch を推論上の不一致にせず、callee 側の label を authoritative に残したまま parameter 型だけを merge する。

検証記録: [sketch/2026-11-27-wave52-lane-a-label-seeding-verify](../sketch/2026-11-27-wave52-lane-a-label-seeding-verify.md)。
