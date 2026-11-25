# @method `Result T GoError` unwrap — 検証（wave44 lane C）

decisions/2026-11-21-go-iface-impl の「2段目」として実装した `@method` の
`Result` 返り許容の検証記録。

## 変更

- check: E0420 を `Result T GoError`（payload が `GoError`）に限り緩和。
  `Result T String` などは引き続き E0420（message を payload 限定に修正）。
- codegen: メソッドは境界の生の綴りで emit する — `Result T GoError` →
  `(T', error)`、`Result Unit GoError` → `error`、`Result (Tuple ...) GoError`
  → `(T', U', ..., error)`、`Result (Option T) GoError` → `(T', error)`（`None`→nil）。
  unwrap は `result.ToGo`、payload が変換の要る形（`List T`→`[]T`、`Int`→`int`、
  `Option`→nilable）なら `[]T(v)` / `int(v)` / `option.GetOrElse(T(nil), v)` を差す。
  パラメータも境界綴り（`List Byte`→`[]byte`、`Int`→`int`）で emit し、内側呼出しで
  `list.List[T](p)` / `int64(p)` に戻す。

## 境界綴りが要る理由

interface のメソッド集合は Gom ビュー（`List Byte`、`Int`）で比較するが、
Go のメソッドシグネチャは生の型（`[]byte`、`int`）で一致する必要がある。
`list.List[byte]` は `[]byte` と同一ではなく、`int64` は `int` と同一ではない。

## 発見・判断

- `[]T` の import mapping は `List T` を生む — メソッドは `List T` を宣言し、
  emit で `[]T` に戻す往復変換になる（`Go.Slice` は boundary のスペルとして
  出ないので conformance も `List` で通る）。
- `int` と `int64` はどちらも `Int` に畳まれる — emit は `int` を選ぶ
  （decision doc の `Write` 例に倣う）。`int64` を要求する interface
  （`io.Seeker` 等）は check を通っても生成コードが不一致になる。
  台帳（implementation-gaps）に未解消として記録。
- `fs.ReadDirFS`/`ReadFileFS` は `fs.FS`（`Open`）を埋め込む — メソッド集合は
  埋め込み分も要求される（fixture で `open` + `read_dir` の両方を宣言）。

## 検証

- gocall `testdata/gocall/methodres`: `io.Writer`（`Write([]byte)(int,error)`）、
  `encoding.TextUnmarshaler`（`error` のみ）、`fs.ReadDirFS`（`Open` の
  `Result (Option fs.File) GoError` → nil、`ReadDir` の `Result (List
  fs.DirEntry) GoError` → `[]fs.DirEntry` + 非 nil error）を Go 側の
  `_test.go` が呼んで検証 — `go build`/`go test` 緑。
- golden `testdata/golden/method_result`: emit 形（`Write(p []byte)
  (int, error)` / `UnmarshalText(p []byte) error` + `result.ToGo` unwrap）
  を固定。
- spec `ok_method_result`（positive）、`E0420_method_result_payload`
  （非 `GoError` payload 拒否）。

## spec 反映

§8.9 の E0420 節を「`Result` のペイロードが `GoError` でない `Result` 返り不可」に
更新し、unwrap 規則と境界綴り（`List`→`[]T`、`Int`→`int`）を追記。
