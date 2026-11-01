# Core IR validator — verification report

- 日付: 2026-11-02
- 対象: `internal/core`（P1-2b）、[draft decision](../decisions/2026-11-02-core-ir-node-inventory.md)

## 検証方法

実機検証はユニットテストで行う（`go test ./internal/core`）。

- `core_test.go`: 全ノード族を含む valid な `*core.File`
  （`buildOKFile` — type decl + match decision tree + EscapeCall +
  CtorApp + CallFunc + partial-application Closure + FuncRef +
  list/tuple/select/go/for + test decl）を構築し、sealed family の
  網羅 switch（go-check-sumtype で検査される）、atom ⊂ expr の
  関係、`Span()`/`Type()`/`BaseOf()` を確認する。
- `validate_test.go`: `Validate(okFile)` が空を返すことを確認した
  うえで、不変条件ごとに壊れたファイルを1件ずつ組んで拒否を
  確認する（34 ケース）。

## カバーした不変条件

| 分類 | 拒否ケース |
| --- | --- |
| span/type | ノードの span なし、nil type、binder type ≠ expr type、body type ≠ sig result |
| ANF/scope | 未定義 Var、シンボルの二度束縛、束縛前の使用 |
| call | CallDirect 非飽和・Go callee、CallFunc 部分適用・既知 callee、CallGo arity 不一致・非 variadic spread・裸の多結果呼び出し、FuncRef arity 不一致、GoFuncRef adapter 必要 |
| 演算子 | sugared op (`|>`) の残留、if 条件が非 Bool、else なし if が非 Unit |
| decision tree | type switch の default が DPanic でない、value 用 literal test が type switch に混入 |
| escape | 関数外、`To` が最内関数でない、境界が非 Result、`|?>` wrap が非 GoError、`go{}` 越え、EscapeCall が single-result call |
| interop | ResultFromGo が ok-pair call、Conv slice-to-go の operand が非 List |
| tail call | 非 tail 位置、TailLoop なし |
| select | default 2 つ、recv bind が非 Option |
| その他 | `go{}` 本体が非 Unit、ForChan が非 Chan、Closure sig 不一致、record field 二度初期化、tuple index 範囲外 |

## 結果

- `gofmt -l .` 出力なし、`go vet ./...`、`go test ./...`、
  `make check`（go-check-sumtype を含む）すべてパス。
- 当初 `CallFunc.Fn` の再分類チェックを `switch e.Fn.(type)` で書いて
  いたが、Atom の exhaustive ではない partial switch として
  go-check-sumtype に検出された。「2 種類を拒否する」意味論は
  dispatch ではないので if 型アサーションに書き換えた — 内部
  type switch = `default: panic("unreachable")` の規約とも整合する。

## 残る未検証点（機械的に検査できないもの）

- ケースの網羅性そのもの（variant 集合は TypeDecl 側、tree 側は
  ctor 名だけを持つ）。
- 呼び出し側の引数 *型* の一致（symbol に型 scheme がない。P1-5 の
  symbol→scheme テーブルが必要）。
- `Conv` の num/named/box-any/iface の合法性（check 時に確定済み）。

## Review fixes（同日・レビュー指摘対応）

レビューで見つかった 2 件の過剰拒否（false positive）と、
nice-to-have 指摘を反映した。テスト数は 34 ではなく **52 ケース**
に増えた（負荷テーブル 39→52、＋ arity split / go{}+lambda の
独立した正例テスト 2 件）。

### arity split を誤って拒否していた（must-fix ①）

`Func.Sig` は canonical flat 形（全矢印を平坦化）で `Func.Params`
は*宣言された* arity を持つ。P0-2 rule 2 の `fn mk_add (a: Int)
-> (Int -> Int)` は `Sig = Int -> Int -> Int`・`Params` 1 個となり、
3 箇所が不正に拒否していた。

- `len(fn.Sig.Params) != len(fn.Params)` の等価比較を
  `len(fn.Params) > len(fn.Sig.Params)`（宣言 arity が矢印長を
  超える場合のみ拒否）に変更。
- body tail 型との比較を `Sig.Ret` から *residual type* へ。
  residual = `len(Params) == len(Sig.Params)` なら `Sig.Ret`、
  それ以外なら `Func{Sig.Params[len(Params):], Sig.Ret}`。
  `Func.ResidualType()` として node.go に追加し、validator 側で
  一貫して使う（`Func.ResultType()` は「全適用の結果型」である
  旨をドキュメントで明確化）。
- `escapeTarget` の境界 `Result _ e` 判定も residual で行う。
  arity split 関数の *外側* 本体にある `try`（residual が
  `Int -> Result _ e` で Result でない）は正しく拒否される
  ことを確認する負例を追加。最内側の完全 arity 関数内の `try`
  が引き続き受理されることも正例で固定。

### `go{}` 内ラムダの `try` を誤って拒否していた（must-fix ②）

`v.spawn` は `Spawn` 本体で +1 されるが Func 境界でリセット
されず、`go { (fn () -> try f ()) () }`（spec 10.3 認可形 —
ラムダは真の関数境界）まで拒否していた。`withFunc` で
`v.spawn` を save/reset/restore するようにした。生の `go{}`
本体の `try`（`To` が外側 Func を指す）は外側の spawn>0 で
引き続き拒否される。`TailCall` は `fnTop`（FuncDecl 限定）で
既に守られており影響なし。

### その他（nice-to-have 適用）

- nil 耐性: `*Body`・`Atom`・`DCase.Test`・各 sealed family の
  typed-nil pointer が panic せず Violation になるよう、
  `nilExpr`/`nilAtom`/`nilDecl`/`nilTypeBody`/`nilDTree`/
  `nilDTest`/`nilSelArm` ヘルパーを追加（いずれも go-check-sumtype
  で網羅検査される exhaustive switch）。
- 追加チェック: `Spawn.Typ == Unit`、TestDecl の params 空、
  TSum variant sym の重複登録、`FieldGet.Index >= 0`（常時）、
  SwitchList subj が List 型、SelRecv/SelSend の chan 型、
  ネストした DSwitch/DGuard の Typ が親と一致（`dtreeChild`）。
- 正例フィクスチャ `okFile` を全ノード族に拡張:
  SwitchTag/SwitchValue/SwitchList、DLitTest/DListTest/DGuard、
  TailCall、SelSend、ResultFromGo/OptionFromGo/TupleFromGo、
  Conv、EscapeResult、VarDecl、Unary、TRecord/TAlias/TAbstract、
  arity-split FuncDecl（mk_add）+ arity-split Closure、
  `go{}`+lambda+`try` の認可形を追加。
