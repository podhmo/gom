# テストスナップショットと go-cmp 差分

- 状態: 採用
- 日付: 2026-11-25

## 文脈

`test` ブロックには `assert` / `assert_eq` だけがあり、一覧や描画結果の
期待値はテスト名やコメントに依存する手書きのアサーションか、
tutorial / realworld では `expected.txt` の手同期に分かれていた。
失敗時の `assert_eq` も actual / expected の 2 行だけで、構造のどこが
違ったかは利用者が読み取る必要があった。

## 決定

`snapshot value key` を test-only builtin として追加する。`value` は
`assert_eq` と同じ型駆動の gomShow/display recipe で文字列化し、生成
`_test.go` のあるディレクトリの `__snapshot__/` に key ごとの `.snap`
ファイルとして比較する。`gom test --update`（短縮形 `-u`）のときだけ
欠損・ stale を問わず書き込む。evaluator oracle は runner state を模倣
せず no-op で `Unit` を返す。

`assert_eq actual expected` の失敗表示には actual / expected の描画に
加えて、raw operand を `github.com/google/go-cmp/cmp` で比較した
`diff (-expected +actual)` を付ける。go-cmp が inspect できない Go 値
（unexported fields など）では panic を回収して diff だけを省略し、
既存の 2 行表示に落ちる。等価判定自体は従来どおり Gom の equality
recipe であり、go-cmp の判定には置き換えない。

スナップショットファイル名は `Test名_key` を filesystem-safe に折り畳み、
常に短い hash を suffix に付けて test/key pair の衝突を避ける。長すぎる
名前は短縮する。key は test 内で一意にする運用を前提とし、コンパイラは重複を
検査しない。

## 却下した案

- テスト全体の stdout を自動記録する MoonBit そのままの形 → Gom の
  test block は任意の出力を持たない純粋な assertion 列なので、記録対象を
  明示する builtin のほうが生成物を小さく保てる
- `expected.txt` を gom が集約管理する → 1 テスト内に複数の期待値を
  置けず、失敗箇所の対応付けが弱い
- equality 判定も go-cmp に寄せる → 関数・Go named type・`GoError` の
  意味論は言語側の equality recipe が正であり、diff は診断の補助に
  限定する

## 検証

- `TestTestSnapshotUpdate` — missing snapshot は `--update` 誘導付きで
  失敗、`gom test --update` が `[1, 2]` を書き、stale 値は diff 付きで
  失敗（`cmd/gom/test_test.go`）
- `TestTestEmitsStream` — record の `assert_eq` 失敗に go-cmp diff が
  出る（同ファイル）
- `testdata/golden/test_flat_test.golden.go` — 生成 `_test.go` の
  `rt.Snapshot` / `rt.AssertEq` 形状を pin
