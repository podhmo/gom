# `gom list` の外部 Gom パッケージ列挙 — 列挙対象の候補集合

- 日付: 2026-11-24
- lane: wave40 lane F（調査。sketch のみ、実装なし）
- 問い: 「何を列挙対象にしうるか」— module cache・workspace・vendor
  ディレクトリ・replace 解決後の候補集合。`import "path"` が解決される
  ものと列挙が一致していることが正しさの条件。

## 現状の解決経路（正となる挙動）

`gom list` は現状 `gomimport.StdlibShortNames()`（`list`/`option`/`result`/`cmp`、
埋め込みではなくパステーブルが正 — wave38 lane A で修正済み）を出すだけ。
外部 Gom パッケージの *参照* は wave36 以降実装済みで、解決は
`internal/gomimport/loader.go` の `dirOf` → `pkgDir` が担う:

1. `l.module()` — `Config.Dir` から上に辿った最寄りの `go.mod` が
   **main module**。`path == modPath` または `modPath + "/"` 前置なら
   `modDir + 残り`。
2. `vendorEnabled()` なら `<modRoot>/vendor/<path>` を先に見る
   （`GOFLAGS` の `-mod` を最後の指定まで読む、`vendor/` ありが条件）。
3. `extModules()` — `go list -m -f '{{.Path}}|{{.Dir}}' all` を
   `Config.Dir`（= importing file の module 文脈）で実行。
   vendoring module は `go list -m all` が拒否されるため
   `vendor/modules.txt` があれば `-mod=mod` で retry。
   最長 prefix 一致で module を選び、残りを dir に join。
4. `Dir` が空（module cache 未ダウンロード）なら
   `go mod download -json` で取りに行く。
5. `oneGomi(dir)` — dir 直下に `.gomi` がちょうど 1 つあれば
   Gom パッケージ。0/複数は not-a-package。

## 列挙対象になりうる集合

| 集合 | 取得方法 | コスト | 列挙としての意味 |
| --- | --- | --- | --- |
| stdlib 短名 | `stdlibPaths` テーブル | 0 | 現在の `gom list` |
| main module 内 | `modDir` 以下を walk し `oneGomi` で判定 | O(repo) | `import` できる自分のパッケージ群 |
| build list（post-replace） | `go list -m all` の Dir を各 module で walk | O(全依存の FS) | `require` から見える Gom パッケージ |
| vendor ディレクトリ | `<modRoot>/vendor/**` walk or `modules.txt` | O(vendor) | vendor mode で唯一見える集合 |
| workspace `use` modules | `go list -m all` が既に含む（Dir=member dir） | 同上 | workspace 結合 build list |
| module cache 全体 | `$GOMODCACHE` walk | 巨大 | **不正** — build list に無いものは import 不能 |

## 観察

- **`go list -m all` は replace 解決後を返す**。`Dir` は replacement の
  dir（ローカルパス or ダウンロード先）なので、build list walk = そのまま
  「`import` が見る候補集合」。post-replace であることは追加処理なしで
  得られる。
- **workspace は `go list -m all` に既に含まれる**。`go.work` の `use`
  module は workspace build list の main modules として Dir=member dir で
  出る。別途 `go work edit -json` を読む必要はない（`-mod` retry 経路も
  workspace では `-mod` 自体が禁止されるので、vendor retry が走るのは
  vendor/modules.txt 存在時のみ — 衝突しない）。
- **vendor は module 側であり workspace root ではない**（調査の指定どおり）。
  loader は `l.module()`（= importing file の最寄り go.mod）の
  `vendor/` のみを見る。Go の vendoring 規則自体も module 単位 —
  workspace mode では workspace 単位の vendoring（Go 1.22+ の
  `go work vendor`、`<workroot>/vendor`）が存在するが **loader は未対応**
  （module vendor のみ）。このずれは implementation-gaps に記録する。
- **vendor 下の列挙は `modules.txt` が速い**: パッケージ行の集合を読んで
  `.gomi` の存在だけ確認すれば walk 不要。`go list -m all` は vendor
  mode で空を返すので vendor は列挙上も別経路が必須。
- **`Dir` 空の module は `go mod download` までしないと判定不能**。
  列挙（一覧目的）で全 build-list module のソースを fetch するのは重い。
  現実的な線: Dir 空の module は「未確認」として skip するか、
  `gom list` の外部モードを opt-in（`-external` フラグ等）にして
  コストを明示する。
- **module cache 全 walk は候補にしない**。GOMODCACHE には build list に
  無い module も溜まる — import できないものを出すと列挙の意味が崩れる。

## 将来の実装メモ（実施は未採択）

- 候補集合 = main module walk + `go list -m all` の Dir walk
  （Dir 空は skip）+ vendorEnabled 時は `<modRoot>/vendor` walk
  （または modules.txt 読み）。`oneGomi` でフィルタ、import path は
  module path + 相対 dir で復元。
- 出力形は `gom list` の default（stdlib のみ）を保ち、外部を `-external`
  等の opt-in にする案が自然 — 全 build list の FS walk は default に
  置けない。
- `dirOf` と同じ Config.Dir 文脈で走るべき（importing file の
  require/replace/workspace が列挙の文脈 = 「そのファイルから import
  できるもの」）。

## 関連

- `internal/gomimport/loader.go` — `dirOf`/`pkgDir`/`extModules`/
  `moduleDir`/`vendorEnabled`/`module`/`oneGomi`
- `cmd/gom/list.go` — 現状の stdlib 列挙
- wave38 lane A — `gom list`（stdlib 短名）実装
