# write 失敗は E0003 診断（exit 2 → 1）

- 日付: 2026-11-25
- 状態: 採用
- Supersedes: [build-driver](2026-11-02-build-driver.md) のうち write 失敗の扱い（stderr + exit 2）

## 決定

生成物の write 失敗（clobber ガードの拒否・パーミッション・disk full 等）はすべて位置を持たない E0003 診断としてレポートストリームに出す。終了コードは診断エラーの 1 で、内部エラーの 2 にはしない。

## 理由

build-driver decision は write 失敗を stderr + exit 2 としたが、これは2点で不整合だった:

1. 終了コード 2 は spec 12 節で「コンパイラの内部エラー」。disk full やガード拒否はコンパイラのバグではなく、ユーザーが直せる環境の問題
2. ストリームに診断が載らないと JSON summary が `ok: true` のまま終了し、機械 consumer が出力欠損を検出できない（wave45 lane D のレビューで指摘）

診断化すれば summary の `ok` が常に正直になり、clobber 拒否（ユーザーアクションが要る通常ケース）と filesystem エラーを同一の規則で扱える。
