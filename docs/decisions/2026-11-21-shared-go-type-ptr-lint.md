# 共有前提の Go 型を指す `Ptr` は可変共有 lint の対象外

- 状態: 採用（実装・検証した — testdata/spec/ok_shared_go_ptr で固定。spec.md §10 へ反映済み）
- 日付: 2026-11-21
- 問い: wave28 lane D — 内部同期済みの Go 型（`Ptr sync.WaitGroup` 等）に W0701/W0702 が鳴るノイズ

## 文脈

可変共有 lint（spec 10 節の W0701/W0702）は `Ptr T` を無条件に可変コンストラクタとして扱う。しかし `Ptr sync.WaitGroup` や `Ptr errgroup.Group` のような、内部で同期が取れた Go の型を指すポインタは、goroutine 間で受け渡すことが型の本来の使い方であり、rgrep/errgroup の probe で警告がノイズになっていた。

## 選択肢

- **(a) builtin allowlist**: `Ptr` が指す先が内部同期済みの Go 名前付き型なら `containsMutable` から外す — lint の閉じた builtin テーブルと同じ設計
- **(b) `@shared` 属性**: 利用者が型ごとに抑制を注記する — 一覧の合意形成を先送りできるが、よくある型（sync.Mutex）ごとに注記を要求する
- **(c) 何もしない**: 警告は advisory なので素通り — ただしノイズは残る

## 決定

(a)。警告の目的は「共有が偶然でないか確かめる」ことであり、型の契約自体が共有を要求する場合は確認する意味がない。閉じたリストにすることで閾値も明確: 自分のドキュメントが並行使用を保証する型だけが入る。

対象は `sync` の同期プリミティブ（WaitGroup, Mutex, RWMutex, Map, Once, Pool, Cond）、`sync/atomic` の語型（Bool, Int32/64, Uint32/64, Uintptr, Value, Pointer）、`errgroup.Group`。

スコープは `Ptr X`（X が許可リストの `*types.Go` handle）に限定する。`Ref` や `Array` のペイロード、record のフィールドなどの位置では、外側のコンストラクタ自身が書き込み可能なので警告を維持する。

## 実装

- `internal/check/lint.go`: `sharedGoTypes` テーブル（`types.NamedRef` キー）+ `sharedGoTypePtr` 判定。`containsMutable` の `Ptr` 分岐が shadowed でなく exempt でないときだけ true を返す
- `docs/spec.md` §10: 例外の記述を追加
- `testdata/spec/ok_shared_go_ptr.gom`: `go { }` キャプチャと `Chan.make` 要素の両方で警告が出ない fixture
