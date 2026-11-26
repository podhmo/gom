# `gom build` / `gom run` は module-local の `.gom` dependency を推移的に emit する

- 状態: 採用（prototype 検証済み）
- 日付: 2026-11-26

## 状況

`gom check` は import 先の `.gomi` だけを読むため、main package が同一 module 内の Gom package に依存していても検査は通る。一方 `gom run` / `gom build` は入力に列挙された dir の `.gom` しか emit しないため、依存先の生成 `.go` がなければリンク時に初めて失敗する。`gom run` は利用者入力を単一 package directory に制限しているので、依存を先に `gom build` する 2 段運用になっていた。

## 決定

`gom build` / `gom run` は、利用者が指定した `.gom` 入力の import を起点に、同一 main module に属する `.gom` source dependency を推移的に compile 対象へ加える。

- module-local の判定は importing file の nearest `go.mod` に対し、canonical import path（stdlib 短名を展開したもの）が module path 本人かその下位にあることで行う。workspace sibling・module cache・replace 先など、別 module に属する package は対象外で、そこへ生成ファイルを書かない
- toolchain stdlib（`list` / `option` / `result` / `cmp`）は常に対象外とする。gom リポジトリ自身の中では import path が main module に収まるが、これらの生成 artifact は stdlib workflow が管理するもので、利用者プログラムの build/run から勝手に再生成しない
- dependency dir を加えるときは direct child の `.gom` をすべて加える。Gom の compile unit は package directory なので、import 宣言を持つ 1 ファイルだけを切り出すと incomplete package になる
- 利用者が既に入力した dir は dependency expansion で広げない。file 単位入力の意味を変えない
- dependency からさらに辿れる module-local import も同じ規則で辿る。cycle があっても収集は停止し、E0606 は従来どおり compile が報告する
- 自動追加された dependency の生成 `.go` は常にソースの隣へ書く。Go package identity は directory identity なので、`-o` で dependency だけを別 dir へ flatten するとリンクできない。`-o` は利用者が明示した入力の出力先を変える指定として残す。明示入力の `-o` 先と自動 dependency の output が同じファイルに衝突する場合は build を拒否する
- `gom run` の「入力は単一 package directory」制約は利用者が指定した入力に適用する。自動追加された dependency dir はこの制約に含めない

`gom check` / `gom test` / `gom info` の入力解釈は変えない。`gom install` や module cache 内 package への emit は別問題とする。`import go` した Go adapter がさらに生成 Gom package を必要とする export-data 依存も、この lane の Gom import closure には含めない — 依存 output を型検査中の Go package loading へ供給するには compile/write の段階構造を変える必要がある。

## 検証

prototype 検証を [sketch](../sketch/2026-11-26-wave48-lane-c-transitive-emit-verify.md) に残す。
