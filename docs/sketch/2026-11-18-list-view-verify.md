# List 由来 View の検証メモ（2026-11-18, wave21 lane4）

- `/tmp/w21dep`（module example.com/w21）で E2E: `List.slice xs 1 3` → `list.ListSub`、`View.get` → `list.ViewGet` (T,bool)→Option、`for x in w` → `range` over `list.View[A]`、`View.to_list` → `list.ViewToList`。`go vet` で生成コードがコンパイル通過。
- boundary test `TestListViewSub`（stdlib/list）: length/get/sub/fold/to_list の値検証。
- `go test ./...` 全 pass、`gofmt -l` 空、`go vet` クリーン。

初回 `gom build` で core validator が `View Int is not List/Array/Go.Slice`（E0901）を出した — ForList の iterable 集合に `View` を追加して解消。
