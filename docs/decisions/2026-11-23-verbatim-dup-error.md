# emit 名の verbatim 二重請求はエラー E0608 で出力を withhold する

- 状態: adopted
- 日付: 2026-11-23
- Supersedes: emit-name-collision-check のうち「verbatim 二重請求は W0601 警告」の部分（[emit-name-collision-check](2026-11-23-emit-name-collision-check.md)。suffix 追い出し W0602 の警告化は変わらない）
- 問い: W0601 が警告を出しても codegen は両方の宣言を emit し続け、
  生成 Go はコンパイル不可のまま出力される — 壊れた成果物を書くか、
  エラーで止めるか（wave36 lane A、knownCompileBugs 2 件の解消）

## 決定

verbatim 二重請求は警告からエラーに格上げする。emit ledger の
`recordEmit` が重複 claim を検出したら **E0608** をエラーとして報告し、
build 層は emit 診断のエラーを `unitFailed` に接続してパッケージの
全 member の出力を withhold する。W0601 は retired（再割り当てしない）。

## なぜこの形か

wave35 lane C では「合法的に通っている Gom ソースをビルド失敗に変え
ない互換性のため」警告を選んだが、実際には壊れた Go を書き出す警告は
緩い — 警告を見落として `go build` で初めて気付く流れは、E0202（Gom
名の重複）や E0607（`.gomi` メンバの衝突）が既にエラーで拒否している
系統とずれる。生成物がコンパイル不可と確定しているのだから、Gom 側で
拒否する方が原因箇所を示せて速い。

suffix 退避（負け側を `FooBar1` に退避）は不採用: 型名と variant
struct 名は外部公開の Go ABI なので、黙った改名は `.gomi` と生成 API
面の両方で追跡不能なずれを生む。改名するならユーザーが決めるべき。

## 報告の形

- 同一ファイル内のペア: 後の claimant の span に 1 件
- ファイルをまたぐペア（`a.gom` の `Foo_Bar` × `b.gom` の `FooBar`）:
  各ファイルの codegen 走査が自分側の宣言を local claimant として見る
  ので、両ファイルの decl に 1 件ずつ — 合計 2 件。sibling↔sibling の
  抑制は「無関係な第三ファイル」だけを沈黙させ、ペア自身が沈黙する
  経路はない（`TestCompileFilesEmitNameCollisionsCrossFile`）
- sibling sum の variant ペア（`Unit.Sums` 経由）は span を持たないので
  owner ファイル側の報告に集約し、重複は `emitDiagKey` dedup で 1 件

## 検証

- `testdata/spec/E0608_type_name_dup.gom`・`E0608_variant_struct_dup.gom`
  がエラーの診断を固定（wave35 の W0601 fixture の改名）
- `internal/build` の `TestCompileFilesEmitNameCollisions*` が
  withhold・dedup・cross-file 報告を固定
- knownCompileBugs の 2 エントリは解消で退役（種子は "rejected is
  fine" 経路を通る）

## 残件

- `gom check` は codegen に到達しないので E0608 は出ない — check-only
  の発見性は `gom dump -phase=emit` が担う（W0601 時代と同じ制約）
- W0602 の suffix 追い出しは引き続き警告（生成 Go は動く）
