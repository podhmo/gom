# wave51 lane F: agent skill の inventory 外部化

## 実施

- `SKILL.md` の workflow に「`gom list` / `gom info` で API inventory を調べてから書く」手順を追加した
- stdlib member 名の列挙を削除し、data-last convention だけを `SKILL.md` に残した
- `gom list` の出力そのものだった short name の静的転写をやめ、コマンドへ問い合わせる記述へ変更した
- compiler-registered な `View`/`Array`/`Ref` は `.gomi` を持たず `gom info` で列挙できないため、規範・落とし穴を `references/language-fundamentals.md` へ残した。`Chan`/`Task` は既存の `references/concurrency.md` を指す
- 移動時に `references/language-fundamentals.md` にあった「`Ref.*`/`Array.*` は member table が空」という古い記述を、現在の compiler-registered helper の説明へ修正した
- 方針は [decision](../decisions/2026-11-27-agent-skill-tool-inventory.md) に記録した

## 分類

- ツールへ委譲: stdlib short name 一覧、stdlib package/member inventory、member directive/doc
- guide に残す規範: data-last convention、missing は `Option`、compiler-registered helper の freeze/non-nil/concurrency 上の意味制約
- guide に残す落とし穴: memorized inventory は最初に腐る、compiler-registered surface は `gom info` で出ない

## 確認

- `go run ./cmd/gom list`: `cmp` / `list` / `option` / `result` / `str` を列挙
- `go run ./cmd/gom info list`: 現在の `stdlib/list/list.gomi` を表示
- `go test ./skills`: pass

## 最終検証

- `go vet ./...`: pass
- `go test ./...`: pass
- `sh scripts/check-sumtype.sh`: pass
- `sh scripts/check-staticcheck.sh`: pass
- `gofmt -l .`: empty
