# トランスパイラーパイプライン読解: 文書のずれと改善候補

- 目的: AST生成後のトランスパイラー実装の読解で得た検証事項を記録する。
- 確認日: 2026-11-20。
- 対象: `podhmo/gom`、`main`コミット`e14baa034cf716ec6f1964ce6d1f481535a77722`。
- 関連: [解説Markdown](../../ja/transpiler-pipeline.md)、[解説HTML](../../ja/transpiler-pipeline.html)。
- 調査方法: GitHubのファイル閲覧による静的コード読解。**Goツールチェーンでの再現実験やテストの実行は未実施**。

## 1. 確認済みのずれ: `docs/design.md`がGo ASTを主たる出力IRとして説明

[設計文書](https://github.com/podhmo/gom/blob/main/docs/design.md)の2節は「Core IR → go/ast」の中間表現を記載し、「`go/printer`で整形」としている。2.4節やディレクトリ図もGo AST主体の生成という記述になっている。

現行の[`codegen/codegen.go`](https://github.com/podhmo/gom/blob/main/internal/codegen/codegen.go)は`render`で生成バッファを組み立て、`go/format.Source`で整形する。[`codegen/gen.go`](https://github.com/podhmo/gom/blob/main/internal/codegen/gen.go)は一部の式・型に`go/ast`と`go/printer`を使用するが、[`codegen/emit.go`](https://github.com/podhmo/gom/blob/main/internal/codegen/emit.go)の文の生成や[`codegen/decl.go`](https://github.com/podhmo/gom/blob/main/internal/codegen/decl.go)の宣言生成は文字列バッファを直接構築する。`Output.ParseFile`は出力テキストを後から解析する補助機能である。

したがって、現在の実装はGoソーステキスト生成と部分的な`go/ast`の併用であり、「Goファイル全体をASTとして組み立ててから一度に印字する」という読み方は適合しない。改善案は、設計文書の説明を「Core IR → Goソース（式・型の一部は`go/ast`/`go/printer`）→ `go/format`」に更新すること。将来フルAST生成へ変更する意図があるなら別の設計課題として分離する。これは文書と実装のずれであり、動作バグと断定しない。

## 2. 改善候補: `CompileFiles`のコード生成をパッケージ横断検査の後へ

[`build.CompileFiles`](https://github.com/podhmo/gom/blob/main/internal/build/build.go)は各ファイルのloweringの直後に`codegen.File`を呼び、その後で全Coreファイルについて`CheckInitCycles`と`CheckTailRecMutual`を実行する。失敗時には全ての`Result.Output`を`nil`へ戻すため、部分出力は呼び出し側に返らない。

改善候補は「全ファイルlowering → パッケージ横断検査 → 全ファイルcodegen」とすること。後段で失敗するパッケージの不要なコード生成・整形を避け、エラー時の経路を明瞭にできる可能性がある。ただし性能計測や挙動差の再現は未実施である。変更時は診断順、dumpの戻り値、失敗時の`Core`/`Output`契約をテストする。

## 3. 改善候補: 「各フェーズは純粋関数」の説明を精密化

[設計文書](https://github.com/podhmo/gom/blob/main/docs/design.md)の2節には「各フェーズは純粋関数として実装」とある。一方、`check`は型メタ変数を束縛し、シンボルの分類を更新し、ローダーはセッションに基づくキャッシュを持ち、コード生成も内部バッファを更新する。

「各フェーズの入出力・責務を分離する」という意図なら妥当だが、数学的な参照透明性をすべての関数が満たすという意味だと不正確である。改善案は、フェーズ間の依存の一方向性を示しつつ、内部の可変状態を許すと明記すること。動作上の不具合とは判定しない。

## 4. 検証提案: 静的パターン検査と実行木の性質を別々にテストする

[`pattern/matrix.go`](https://github.com/podhmo/gom/blob/main/internal/pattern/matrix.go)はMarangetのusefulnessに基づく被覆検査、[`lower/pat.go`](https://github.com/podhmo/gom/blob/main/internal/lower/pat.go)は実行用の義務キュー方式の決定木生成である。両者は別アルゴリズムであり、静的な網羅性があることと生成した分岐が同じ値を処理することは異なる検証対象となる。

検証対象として、再帰ADT、入れ子の`or`パターン、guard、重複腕、複数ファイルから輸入されたADTを挙げる。既存のテストは充実しているため、追加する前に具体的な未被覆条件を確認する。これは未確認の改善提案であり、バグ報告ではない。

## 5. 検証提案: 遅延型解決の順序独立性

`check.finalize`は保留メンバー・適用・型デフォルトの反復で確定する。前方参照・ファイル順・独立した型制約の出現順を入れ替えた場合に、同じ型と呼び出し分類が得られるかを回帰テストにすると、固定点計算の性質を確認しやすい。こちらも現時点で再現した不具合はない。

## 6. 台帳への記録

Goコード生成の中間表現と、フェーズの純粋性に関する記述を[差分台帳](../../implementation-gaps.md)に登録した。

## 7. 検証範囲

本調査は静的なコード読解に基づく。Goによるビルドやテスト、再現実験は実施していない。
