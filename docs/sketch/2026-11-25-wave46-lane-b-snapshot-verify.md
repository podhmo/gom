# wave46 lane B: snapshot / go-cmp 検証記録

2026-11-25。`docs/decisions/2026-11-25-test-snapshots-go-cmp.md` の
prototype 検証メモ。

## 確認したケース

- `snapshot [1, 2] "list"` — snapshot 未作成時は `gom test --update` を
  案内して失敗、`gom test --update` で `[1, 2]` を書き、通常実行は pass。
  値を `[1, 3]` に変えると mismatch と `diff (-expected +actual)` で失敗
  （`cmd/gom.TestTestSnapshotUpdate`）
- record の `assert_eq` — 既存の actual / expected に加えて go-cmp の
  構造 diff が出る（`cmd/gom.TestTestEmitsStream`）
- `GOM_TEST_UPDATE_SNAPSHOTS=1` を親環境に持つ通常実行では更新しないよう、
  child env は常に 0/1 を明示する（`snapshotUpdateEnv`）
- snapshot 書き込みは sibling temp + rename。失敗しても旧 baseline を
  truncate しない（`rt/testutil.TestWriteSnapshotAtomicPreservesBaselineOnRenameFailure`）
- test 内で `chdir` しても test binary 起動時に capture した package cwd を
  使う（`rt/testutil.TestSnapshotRootDoesNotFollowChdir`）
- 長い Unicode key を rune 境界で短縮し、source test name を namespace に
  使う（`rt/testutil.TestSnapshotNameUnicodeBoundaryAndStableNamespace`）
- production build は `rt/testutil` を import しないため go-cmp を load しない
  （`internal/goimport` の既存 module テストを `-mod=mod` 補助なしで通過）

## 残る運用上の注意

同一ファイル内で source test name まで同じ test 複数個は、テスト出力上も
区別できない。snapshot key は test 名の namespace 内で一意に選ぶ。
