# wave57 lane D2: spec fixture `-update` preservation 検証

- 日付: 2026-11-28
- lane: D2（小さい実装修正）

## 実施内容

spec fixture `-update` の 2 つの preservation bug を直した。

1. absolute foreign span file: dependency `.gomi` のような machine-local absolute path を `span.file` へ書き戻さない。position (`line`/`col`/`offset`) は残し、`file` key だけ落とす。foreign diagnostic path の portable な正規化は open question のままにする
2. stale `data_absent`: actual `data` に存在する key と矛盾する harness-only `data_absent` entry を preserve しない。actual に still absent な entry だけ残す

## red-first

`TestSpecUpdateDropsStaleDataAbsent` と `TestSpecUpdateOmitsAbsoluteForeignSpanFile` 追加後、実装前は前者が `data` と `data_absent` の矛盾した fixture を書き、後者が tempdir 以下の absolute foreign path を書いて red だった。実装後は同じテストが pass した。

## fixture

- `TestSpecUpdateDropsStaleDataAbsent`: actual `data.candidates` があるとき stale な `data_absent: ["candidates"]` を残さない
- `TestSpecUpdateOmitsAbsoluteForeignSpanFile`: foreign `.gomi` label span から absolute `file` を落とし、position を残す。全角スペースを含む path でも wire JSON の exact bytes を decode して判定し、同じ path を持つ machine-facing `data.file` は落とさない
- `TestSpecUpdatePreservesExplicitEmptyAssertions`: still valid な `data_absent` と empty notes/labels/fixes の既存 preserve 非退行

## 残件

dependency `.gomi` diagnostic path を module-relative など portable な形で出すかどうかは open question のまま。今回は fixture が環境依存 path を取り込まない harness 修正だけを行った。
