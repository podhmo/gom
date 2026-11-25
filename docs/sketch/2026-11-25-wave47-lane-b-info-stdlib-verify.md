# wave47 lane B stdlib inspection verify

- 日付: 2026-11-25
- 対象: `gom info` の stdlib 短名 read-only inspection

## prototype

`go run ./cmd/gom info list` と `go run ./cmd/gom info cmp` を実行し、stdout が既存 `.gomi`（`package list` / `package cmp`）で、stderr が `ok:true` summary であることを確認した。cwd に `list.gomi` / `cmp.gomi` は生成されない。

## 実装確認

- `internal/gomimport.Loader.GomiPath` は `CanonicalPath` を通してから `dirOf` + `oneGomi` を使うため、短名解決だけでなく module/replace/workspace/vendor 解決も import と同じ規則に載る
- `cmd/gom` 側は inspection 時に `iface.Draft` を呼ばず、stdout へ既存 `.gomi` を写すだけなので module cache や cwd に書き込まない
- 同名ファイルまたはディレクトリが存在する引数は従来の file/dir draft generation に残す

## テスト

`go test ./cmd/gom ./internal/gomimport -count=1` — pass。`TestInfoStdlibShortNameInspection` は embedded table 外の `cmp`、read-only 性、複数短名拒否、`-o <path>` 拒否を確認する。
