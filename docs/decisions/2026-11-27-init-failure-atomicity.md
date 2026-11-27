# `gom init` の failure atomicity

- 状態: 採用（実装・検証した — [検証](../sketch/2026-11-27-wave53-lane-b-init-atomicity-verify.md)）
- 日付: 2026-11-27

## 決定

`gom init` が失敗したとき、init が作成・変更した project file はすべて rollback する。対象は `go.mod`、`go.sum`、`main.gom` とし、init 自身が作成した file は削除し、init が変更した既存 file は開始前の内容へ戻す。init 開始後に外部から `main.gom` が作られた場合は、init がまだその file を作っていないので削除しない。`go.mod` / `go.sum` については、init 開始時の backup を正とし、validation 中に外部から concurrent に作成・置換された file まで attribution を保証しない。

- standalone toolchain validation の失敗だけでなく、validation 成功後の `main.gom` write failure も同じ失敗として扱う
- 新規 init で作成した `go.mod` / `go.sum`、既存 module への additive edit、init が書いた `go.sum` 変更を区別せず rollback する
- `main.gom` は既存 file を上書きせず、`writeNewFile` 自身が partial write を消す。rollback 対象には含めず、validation 中に外部から作られた `main.gom` を init が消さないようにする。`go.mod` / `go.sum` の backup 判定は symlink を含む directory entry 単位で行い、読めない既存 entry を rollback が誤って消さないようにする
- directory 作成自体は rollback 対象にしない。project file の atomicity を決めるものであり、directory 構成を init が巻き戻すものではない
- 成功時だけ `created` / `kept existing` を stdout に出す。失敗時に一時的へ作った file を created と表示しない

## 背景

wave52 lane B で standalone checksum を必須にしたとき、既存 module への additive edit と init-written `go.sum` の rollback は入れた。一方、新規 init では validation 前に書いた `go.mod` が失敗後に残り、validation 後の `main.gom` write failure では既存 module への変更が残った。利用者から見ると `gom init` が失敗したのに project file だけが残る状態は再実行や手動 cleanup の判断を難しくする。

## 検証

`cmd/gom/scratch_test.go` で次を固定する。

- 新規 init で `go mod download` が `go.sum` を書いた後に toolchain package validation が失敗しても、`go.mod` / `go.sum` / `main.gom` が残らない
- 同じ validation failure でも、ユーザー既存の `main.gom` は内容を保ったまま残る
- `main.gom` write failure では、新規 init が作った `go.mod` / `go.sum` が残らない
- `main.gom` write failure では、既存 `go.mod` への additive edit が元へ戻り、既存の空 `go.sum` は checksum 追加なしの状態へ戻る
- dangling symlink の `go.mod` は init が作った file ではないので、失敗時 rollback で消えない
- validation 後に外部から作られた `main.gom` は init が作った file ではないので、失敗時 rollback で消えない
- dangling symlink の `main.gom` は keep して成功扱いにせず、write failure で init を失敗させつつ symlink 自体は残す
