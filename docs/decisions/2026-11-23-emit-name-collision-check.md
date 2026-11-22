# emit 名衝突は codegen 台帳で検出し W0601/W0602 で警告する

- 状態: adopted
- 日付: 2026-11-23
- 問い: E0607 が止めるのは `.gomi` メンバの衝突までで、生成 suffix への
  追い出し（非公開 decl が先取りして公開 decl がずれる等）と暗黙 member
  名（sum variant struct・marker・test 名）の衝突は素通しする —
  検出器の形（validator / lint / `--strict` 系）は調査して決める
  （wave35 lane C、[go-abi-stability](2026-11-22-go-abi-stability.md) の
  案 B の実装）

## 決定

(a) codegen 内部の emit 名台帳 + 警告診断 — 検出器を新しいフェーズや
`--strict` フラグにせず、`indexDecls` の名請求と同じ場所に置く。

- `gen.emitLog`/`emitOwner` が全パッケージスコープ emit 名（型・バリアント
  struct・marker・fn・let・内部ヘルパ・テスト）を記録する
- 2 つの衝突形状で警告: verbatim 二重請求（型・バリアントは suffix 機構を
  持たず両方 emit される → 生成 Go がコンパイル不可）は **W0601**、
  予測名からの数値 suffix 追い出し（`myFn1`、`TestX2`）は **W0602**
- 警告しないもの: `gomEqN`/`gomShowN` など内部名（go-abi-stability で
  「内部名は約束の外」）、同名テストどうしの dedup、pkgReserved/
  `safeIdent` の `_` マングル（spec 化された退避自体）
- 一覧化は `Output.EmitNames` + `gom dump -phase=emit`（kind・Gom 名・
  emit 名、`(base X)` 付きで追い出しを表示）
- 警告は `Output.Diags` → `build.Result.Diagnostics` に流れる。
  `gom check` は codegen に達しないので出ない — check-only の発見性は
  `dump -phase=emit` が担う

## なぜこの形か

衝突検出は claim の並び（正準順・verbatim 先取り）を知っている場所でしか
正確にできない。resolve/check 段で Gom 名の衝突を探す validator にすると
emit 規則（`exportName`・`declBase`・suffix 機構）を再実装することになり、
規則が変わるたびにずれる。codegen 内なら請求の結果をそのまま観測できる。

lint や `--strict` オプトインにしないのは、verbatim 二重請求は生成 Go が
コンパイルできない壊れ方で、発見されないべきでないため。エラーではなく
警告なのは、現状合法的に通っている Gom ソースをビルド失敗に変えない
互換性のため（suffix 追い出しは生成 Go が動くので尚更）。

## 検証

- `fn my_fn`/`fn myFn` → W0602（`my_fn` emits `myFn1`）
- `type Foo_Bar`/`type FooBar` → W0601（`type FooBar` ×2）
- `| Some_Opt | SomeOpt` → W0601（`T_SomeOpt` ×2）
- `type TestX` + `test "x"` → `gom test` 時に W0602（`TestX2`）
- spec fixtures `testdata/spec/W0601_*.gom`/`W0602_*.gom` と
  `internal/build` の `TestCompileFilesEmitNameCollisions` が固定する

## 残件

- 兄弟ファイルの型は `Unit.Types` が名前だけを持つので、兄弟↔ローカルの
  型衝突はローカル側 span に警告が付く（両方ローカルの場合は後の claimant）
- marker（`isT`）はメソッドスコープなので一覧のみ、衝突警告なし
  （型名の衝突が根本原因として警告される）
