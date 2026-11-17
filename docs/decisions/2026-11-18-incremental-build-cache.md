# インクリメンタルビルドキャッシュ

- 状態: 採用
- 日付: 2026-11-18

## 決定

`gom build` にプロセスをまたぐ増分キャッシュを入れた — design.md
5 章のキー設計をそのまま実装する。パッケージ（dir 単位の
`build.CompileFiles` グループ）ごとに 1 つの JSON manifest を
`os.UserCacheDir()/gom/build/<sha256(dir)>.json` に書く。キーが
変わらなければコンパイルも書き出しも走らせない。

### キーの構成

- **自分のソース**: 各 `.gom` input の canonical token stream ハッシュ
  （`syntax.Canonical`）。コメント・空行・位置情報は lexer の
  token 化で落ちるので、書式だけの編集はハッシュを動かさない。
  `test` / `@test` 宣言は `syntax.TestDeclSpans` で span を除いて
  ハッシュする — テストコードは `gom build` の成果物に入らない。
- **依存先のインターフェース**: パッケージ自身の `*.gomi` と、
  compile 中に import loader が読んだ全 `.gomi`（推移閉包を含む）
  を同じ canonical ハッシュで記録する。gomimport.Config に
  `OnLoadGomi` フックを足して観測する。実装のみの依存編集は
  `.gomi` を動かさないので downstream は hit し、ctor 追加などの
  意味的変更はハッシュを動かして再生成する — 5 章の非網羅
  `match` 保護が効くのはこの経路。
- **import した Go パッケージ**: `goimport.Config.OnLoad` フック
  で importer が解決した各パッケージのソース dir と
  `go list -export` の dep dir を観測し、各 dir の
  `*.go`/`*.gomi` 統計（`statTree` と同じ折りたたみ）を記録する。
  Go API の変更・dep の編集は stat を動かして再生成する。
- **モジュール解決の文脈**: 最寄りの go.mod/go.sum と最寄りの
  go.work/go.work.sum の (path, size, mtime) を manifest に記録
  し、hit 時に ancestor を再探索して集合ごと比較する — replace
  の追加・変更や、後から現れた go.mod/go.work も miss になる。
- **コンパイラ方式バージョン**: `genVersion` 定数
  （codegen/lower/extern の出力規則を変えたら bump）に、実行
  バイナリの (path, size, mtime) を併記する。
- **rt/stdlib ABI**: `go list -f {{.Dir}}` で解決した
  `github.com/podhmo/gom/rt` と `.../stdlib/...` 各 dir の
  `*.go`/`*.gomi` の (name, size, mtime)。replace された
  toolchain は別の dir に解決され別キーになる。
- **Go バージョンとビルド条件**: `runtime.Version()` +
  `go env GOOS GOARCH GOFLAGS GOWORK`。

### 失敗時の退化

- hit 検証は manifest の `dir`・`-o` 値・key・input 数と各ハッシュ・
  記録された全 gomi の再ハッシュ・記録された全出力ファイルの存在。
  一つでもずれれば素通りして compile する。cache が読めない・
  書けないときも常に素通り（exportCache と同じ方針）。
- manifest は `-o` 値込みで記録する。出力先を変えた run は miss
  して書き直す — recorded 出力パスをそのまま検証できるので
  判定が単純になる。
- `-go` 指定時はキャッシュしない。hit は compile を走らせない
  ので `go build` に渡す生成物が得られない。
- `GOM_BUILD_NOCACHE` で全体を無効化（`GOM_GOIMPORT_NOCACHE` の
  先例）、`GOM_BUILD_CACHE_DIR` で格納先を差し替え（テスト用）。
- 環境材料（`go list` + `go env`）は遅延評価 — manifest の検証・
  書き込みが一度も要らない run では subprocess を起こさない。

### write-if-different

`writeFile` は内容が同一なら書き込まない — 依存編集で
cache miss したパッケージでも、生成物が変わらなければ mtime は
動かず `go build` のキャッシュが効く（5 章ルール 5）。

## 計測 pin

multifile fixture（shapes + gomapp の 2 パッケージ）は小さすぎて
差が出ない（両者 ~20ms）。repo 自身の stdlib 3 パッケージ
（list/option/result）で `gom build -o`: nocache ~30ms、
cache hit ~20ms（`go list`/`go env` の subprocess が下限）。
節約分はパッケージの compile 全体なので、実コードベースでは
1 パッケージあたりの型検査＋lowering＋codegen が丸ごと消える。

## 未実装（5 章の残件）

- ルール 6「依存関係のないパッケージどうしは並列にコンパイル
  する」はこの lane では扱わない。dir グループの並列化は
  CompileFiles 呼び出しのスケジューリング問題で、cache と直交。

## 却下した案

- **`.gomi` の曖昧 hash（src stat / byte hash）**: コメント・書式
  だけの変更でも依存側が再生成される。canonical token stream
  は lexer 共有で実装コストが小さい。
- **`internal/build` 側にキャッシュを置く**: `build.CompileFiles`
  の内部で差し替える設計は Result への合成（diagnostics や
  interface 検査との整合）が複雑になる。driver (cmd/gom) 側で
  input/output の観測点に置く方が、checkInterfaces・write
  経路・`-o` の解決と自然に噛み合う。
