# module 外 `gom run` / `gom init`

- 状態: 採用（prototype 検証済み）
- 日付: 2026-11-27
- Supersedes: [gom-run](2026-11-18-gom-run.md) のうち module 外では scratch module を仮組みしても import が解決できないという判断

## 状況

`gom run` は生成 `.go` をソースの隣へ書き、`go build` でリンクする。生成コードが `github.com/podhmo/gom/rt` と stdlib を module 解決するため、go.mod のない directory では compile はできても link できなかった。また新規 project では `go.mod` と最初の `main.gom` を手で作る必要があった。

以前は「scratch module を仮組みしてもユーザー側の別 Gom package import が解決できない」として module 外実行を退けたが、module path を固定し、その配下の import だけを module-local として扱えば、少なくとも単独 project と子 directory への依存は解決できる。

## 決定

### `gom run` の scratch module

go.mod が見つからない run package directory では、`gom run` がその directory に一時的な `go.mod` を作る。

- module path は `gom.local/scratch` に固定する。run package 本人は `gom.local/scratch`、その子 directory は `gom.local/scratch/<relative>` として import できる
- scratch root は run package directory である。親 directory を project root だと推測しないため、run package の外にある sibling package は module-local import として扱わない。その規模になったら `gom init` で明示的な module を作る
- 寿命は 1 回の `gom run` invocation に限る。compile、link、実行が終わったら削除する。異常終了で残った場合でも、次回は既存 go.mod を上書きしないため利用者の判断で削除できる
- cache は gom 側では持たない。毎回同じ内容の go.mod を合成し、Go 本体の build/module cache に任せる。export cache が再作成ごとの mtime に反応しないよう、scratch 管理ファイルの mtime は内容 hash から決める。同じ内容では cache を再利用し、toolchain の require/replace が変われば key も変わる。生成 `.go` は従来どおりソースの隣へ残る
- 既存 `go.mod` は絶対に上書きしない。作成は `O_EXCL` で行い、既にあれば scratch ではなく通常の module context として扱う
- toolchain 本体の `github.com/podhmo/gom` 依存は、release build では build info の module version を `require` に使う。開発版・test binary では source checkout を `replace` に使い、`GOM_TOOLCHAIN_ROOT` で明示 override できる。空白を含む path は go.mod の quoted string として書く
- checksum は合成直後に `go mod download github.com/podhmo/gom` で解決する。release module の初回実行でも read-only な `go build` が失敗しないようにするためで、結果は Go module cache に任せる
- 親 directory に `go.work` がある場合は、scratch invocation の Go subprocess では `GOWORK=off` にして workspace 解決を無効化する。scratch module は外部依存を持たない bootstrap なので、parent workspace の解決は使わない。run package directory 本人に既存 `go.work` がある場合は、それを上書きも削除もできないため module 外 run を拒否する
- 外部 dependency を追加する project では scratch を恒常化せず `gom init` を使う。scratch module は zero-config な起動経路であり、依存管理ファイルの代替ではない

### `gom init`

`gom init [-dir DIR] [MODULE-PATH]` を新設し、`go.mod` と `main.gom` の雛形を作る。

- `MODULE-PATH` 省略時は `example.com/<directory 名を sanitize したもの>` を使う
- `go.mod` / `main.gom` は既存ファイルを上書きしない。片方だけ存在するときは残っている側を保ち、欠けている側だけを作る。両方あるときは何もせず使用エラーにする
- 既存 `go.mod` が Gom 依存を持たないときは、生成 template が link できるよう `require github.com/podhmo/gom`（必要なら開発用 `replace`）を末尾へ追加する。module 行や既存 require は書き換えず、追加後に checksum を解決する（wave52 lane B 以降、この解決は [init-standalone-checksum](2026-11-27-init-standalone-checksum.md) により init 先 module だけで行う）
- `main.gom` は `fmt.Println("hello")` を実行する package main で、`gom run` ですぐ動く最小形にする

## 検証

検証記録: [sketch/2026-11-27-wave51-lane-a-run-init-verify](../sketch/2026-11-27-wave51-lane-a-run-init-verify.md)。

`cmd/gom/run_test.go` が次を固定する。

- module 外で実行でき、scratch `go.mod` が残らない
- `gom.local/scratch/lib` のような固定 module path 配下の import が module-local dependency として compile/link される
- `gom init` が指定 module path の `go.mod` と `main.gom` を作る
- 既存 `main.gom` が上書きされない
- init 直後の template が `gom run` で実行できる
