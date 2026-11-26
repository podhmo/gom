# wave48 lane B: build cache dependency race 修正の検証

対象: `gom build` incremental cache が dependency fingerprint を compile 時観測ではなく commit 時再観測していた問題。Quint witness は [buildcache-quint](ja/2026-11-26-buildcache-quint.md)。

## 原因

`.gomi` / Go dep dir / module file は compile 中に読まれるが、manifest へ入れる fingerprint は commit 時に読み直していた。compile が old version を見たあと dependency が new version へ変わると、new version の fingerprint を持つ manifest が old version 由来の output に付き、後続 run が stale output を hit しえた。

## 修正

- `gomimport.Config.OnLoadGomi` を `(path, src)` へ変え、loader が compile へ渡した exact bytes を記録する
- Go dep dir は importer resolve 時に stat fingerprint を記録する
- module file は dir compile 開始前の stat fingerprint を記録する
- 同じ dependency を複数 version で観測した compile は conflict として commit しない
- commit 時には記録済み fingerprint と現在値を再検証し、変わっていたら manifest を書かない。manifest には compile 時観測値を入れる

cache 全体の再設計や新しい並列化は入れていない。

## 検証

- `TestBuildCache_CommitRejectsPostCompileGomiEdit`: compile 時の `.gomi` bytes を記録したあと commit 前に dependency を書き換える Quint witness 形。manifest が作られないことを確認する
- `go test ./cmd/gom -run TestBuildCache -count=1`: pass
- `go test ./cmd/gom ./internal/gomimport`: pass
- 完了チェック: `gofmt -l .` / `go vet ./...` / `sh scripts/check-sumtype.sh` / `sh scripts/check-staticcheck.sh` / `go test -count=1 ./...`

## 残る境界

dependency の変更と commit 時再検証の間に入る極小の race まではファイルシステムだけでは atomic に閉じられない。この lane は「compile 中観測と commit 時再観測が大きくずれる」 witness を閉じる修正であり、外部 writer との transactional serialization は scope 外。
