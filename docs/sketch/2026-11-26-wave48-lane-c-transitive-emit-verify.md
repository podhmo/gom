# wave48 lane C: module-local transitive emit 検証

日付: 2026-11-26

## 確認したこと

- `testdata/multifile` を生成 `.go` なしでコピーし、`gom build gomapp` だけで `shapes/ops.go` / `shapes/shapes.go` / `gomapp/main.go` が生成されることを確認した。summary は `ok: true` で、relative input から辿った dependency の files/outputs も relative 表示に揃った
- 同じ fixture で `gom run gomapp` が 2 段ビルドなしにリンク・実行され、stdout が `12.56636\ncircle 4\n2\n6.28318\n[]\n[]\n7\n` になることを確認した
- `main -> a -> b` の import 連鎖で、途中 dir `a` の direct child `.gom`（`a.go` 相当と sibling）と終端 `b` が closure 収集されることを `TestExpandModuleLocalDepsTransitivelyAddsPackageFiles` で確認した
- `gom build -o <file> main.gom` では明示入力だけが `-o` の出力へ行き、自動追加 dependency は `dep/dep.go` に書かれることを `TestBuildModuleLocalDependencyKeepsOutputBesideSource` で確認した
- `TestRunModuleLocalDependency` は fixture の `gom run` を end-to-end で実行する
- `TestExpandModuleLocalDepsSkipsToolchainStdlib` は main module path が `github.com/podhmo/gom` のときでも stdlib short name を closure 収集しないことを固定する
- 専用 `GOM_BUILD_CACHE_DIR` で `gom build gomapp` を 2 回続けて実行し、2 回目も dependency outputs を含む `ok: true` summary になることを確認した（auto input の cache output mode がずれない）

## 実装上の確認点

- auto input は input 解決時に `auto` 標識を持ち、`-o` の output mode から外れる。cache manifest の output mode も同じ判定を使うので、依存 dir が `-o` 付き build の explicit package と混ざらない
- `gom run` の単一 package directory 制約は auto expansion 前の利用者入力だけに適用する。`hasMain` も利用者入力側の生成物だけを見るため、依存 package 名に引きずられない
- import の解釈自体は変えていない。typecheck は従来どおり `.gomi` を読むため、この lane が解決するのは「`.gomi` はあるが生成 `.go` がまだない同一 module の依存を 2 段で build しなければならない」運用上の切断である

## 境界

- workspace sibling / module cache / replace 先など別 module の package には生成ファイルを書かない。main module path の prefix 判定で拾えるのは同一 module 内に限定する
- toolchain stdlib (`list` / `option` / `result` / `cmp`) は gom リポジトリ内では物理的に同一 module に見えるが、生成 artifact は stdlib workflow が管理するので auto emit から除外する。これを外さないと examples/tutorial のテストが `stdlib/cmp/cmp.go` を呼び出し cwd 依存の `//line` で再生成し、コミット済み artifact を汚すことを確認した
- 利用者が dependency dir の一部ファイルだけを明示入力した場合、その dir は auto expansion で広げない。file 単位入力の意味を保持するため、incomplete package になる場合の診断は従来どおり compile が返す
- 依存が import されるには `.gomi` が必要という公開面の規則は変わらない。`.gomi` のない source-only package を暗黙に公開する仕組みはこの lane の scope 外
