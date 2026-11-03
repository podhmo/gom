# P4-1 `.gomi` 検査と `gom info` — 実装・検証メモ

## 実装の構成

- `internal/iface/`: `Package`/`Member`/`Surface`（check.File から公開面を抽出、@test 除外）、`Check`（interface vs impl 照合 → E0603）、`Draft`（型推論結果から .gomi 下書き生成）、`Hash`（正規化構文木の semantic hash — 消費者は P4-7 キャッシュ側、現状未配線）
- `internal/syntax`: `p.gomi` フラグで interface 文法を強制 — signature の `= body` / 非単純パターン / 式を E0602 で拒否（回復のため parse は続行）
- `cmd/gom/iface.go`: ディレクトリ census で .gomi を検出。非 main パッケージに `<name>.gomi` が無ければ E0601、パッケージ名と合わない .gomi は迷子 E0604、実装は `iface.Check`（impl にエラーがある群は照合 skip で二次ノイズ抑止）
- `gom info`: 型推論済み decl から下書き生成して write-if-different。`gom gomi` はリネーム通知に変更

## 検証

- E2E: `gom info` → .gomi 生成 → `gom check` 受理。型不一致 `Shape -> Float` vs `Int` → E0603、未実装メンバー → E0603、.gomi 無し非 main → E0601（cmd/gom/cli_test.go TestInfo*/TestCheckMissing/TestCheckInterfaceMismatch/TestCheckInterfaceFile/TestBuildRejectsInterfaceFile）
- spec 7.5 の `gom gomi` 表記を `gom info` に統一（CLI sketch の決定どおり。gaps 台帳の CLI usage エントリを解消）

## 残課題

- `iface.Hash` は実装済みだが消費者は P4-7（増分キャッシュ）側。単体テストなし
- `internal/iface` のユニットテストが薄い（E2E のみ）。`@test` decl が surface から外れることの回帰は TestBuildSkipsTestCode が兼ねる
- 複数ファイルパッケージの scope 共有は open-questions の未決（V-6 と連動）
