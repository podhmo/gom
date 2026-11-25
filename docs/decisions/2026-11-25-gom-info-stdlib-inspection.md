# `gom info` の stdlib 短名 inspection

- 状態: 採用
- 日付: 2026-11-25

## 文脈

`gom list` は `list` / `option` / `result` / `cmp` を import 可能な短名として出すが、`gom info list` は実装ファイルパス `list` として扱い、stdlib の公開シグネチャをすぐには見られなかった。

## 決定

`gom info` の位置引数が stdlib 短名と一致し、同じ綴りのファイルまたはディレクトリが存在しないときは、既存の `.gomi` を read-only inspection として stdout に出す。解決は import と同じ canonical path・module/replace/workspace/vendor 規則を使う。draft は生成せず、module cache や cwd に書き込まない。

inspection は stdout に 1 つの interface artifact を出す形なので、短名は 1 件だけを受け付け、ファイル・ディレクトリ入力や出力先 `-o <path>` との混合は使用法エラーにする。`-o -` は省略と同じ意味として許す。同名のファイルまたはディレクトリが存在するときは従来どおりファイルシステム入力を優先する。

external Gom package 全般の inspection や `gom info` の viewer/drafter 分離は行わない。

## 却下した案

- 短名を embedded stdlib source だけから出す → `cmp` は embedded table にない上、replace/workspace/vendor で利用者が実際に import する interface とずれる
- 複数短名を連結して出す → stdout が単一の `.gomi` artifact にならない
- `.gom` 実装から draft 再生成する → read-only inspection の用途に不要で、module cache への書き込み動機が生まれる

## 検証

prototype 記録: [wave47 lane B stdlib inspection verify](../sketch/2026-11-25-wave47-lane-b-info-stdlib-verify.md)。

- `TestInfoStdlibShortNameInspection` — `list` と embedded table 外の `cmp` が既存 `.gomi` を stdout に出し、cwd へ書かないこと、複数短名と `-o <path>` を拒否することを確認（`cmd/gom/cli_test.go`）
