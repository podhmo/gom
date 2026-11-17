# Cons buffer 化の検証（wave18 lane1）

日付: 2026-11-18。対象: [decision](../decisions/2026-11-18-cons-buffer-array-extern-impl.md) の
prototype 検証 — `Array` モジュール（a）と `@extern fn` 機構（b）。

## 実装

- `rt/array.go`: `ArrayMake`/`ArrayLen`/`ArrayGet`/`ArraySet` — `[]T` への素直な forward。
- `internal/stdlib/builtin.go`: `arrayMembers` 6 件の member table。`to_list`/`from_list` は
  `pkg` フィールドで stdlib/list 側の `ArrayFreeze`/`ArrayFromList` に向ける
  （rt → stdlib/list の逆向き import は cycle のため不可）。
- `@extern fn`: `syntax` で属性を受理（body 禁止・`fn` 限定・結果型必須）、`check` は
  Body=nil を許容、`pattern` は bodyless を skip、`lower` は Body==nil の FnDecl を
  `Extern: true` の `core.FuncDecl` として出す（コードは emit しないが名前は claim する
  — 同パッケージ参照が `Map` を正しく出力するために必要）、`codegen` は emit を skip、
  `core` validator は Extern の nil Fn を許容。
- `stdlib/list`: `map`/`filter`/`append`/`rev`/`range` を `@extern fn` 化し、実体は
  hand-written `stdlib/list/impl.go` の buffer loop。`concat_go`/`range_go` の内部参照は
  宣言経路で解決されるためそのまま（`range_go` は不要になり削除）。
- `stdlib/list/list.gom` に `Array` 間の変換 helper は置かず、`Array.to_list`/`from_list`
  は stdlib/list 側の Go 関数が担う（`.gom` から `Array` member として呼べる）。

## 検証

1. `gom test stdlib/list` 全 11 テスト通過 — `concat`・`rev_append`・test 内の
   `range`/`map`/`filter`/`append`/`rev` 呼出しが生成コードから impl.go の関数へ解決。
2. `/tmp/artest` で `Array.make`/`set`/`get`/`length`/`from_list`/`to_list` を含む
   `test "array round trip"` が通過 — 全 member が Gom から正しく emit される。
3. spec fixtures: `ok_extern_fn`（宣言+呼出し）、`ok_array_ops`、
   `E0101_extern_on_let`/`E0101_extern_with_body`/`E0101_extern_no_ret`。
4. `gom fmt` の円等性: `@extern` を lead 行として print（format/decl.go）。

## 計測（`go test -bench=. -benchmem -run='^$' ./stdlib/list/`, VM）

| bench | before (n=2048) | after (n=2048) | allocs |
| --- | --- | --- | --- |
| Map | 4.01 ms / 17.7 MB | 5.9 µs / 16 KB | 4095 → 1 |
| Filter | 1.11 ms / 4.48 MB | 4.7 µs / 16 KB | 2047 → 1 |
| Append | 4.00 ms / 17.9 MB | 2.4 µs / 18 KB | 4096 → 1 |
| Rev | 3.13 ms / 17.7 MB | 3.3 µs / 16 KB | 4095 → 1 |
| Range | 6.46 ms / 35.5 MB | 3.5 µs / 16 KB | 8190 → 1 |
| Concat k=512 | 14.1 ms / 70.7 MB | 1.83 ms / 8.8 MB | — → 512 |

O(n²) → O(n) を確認（n=2048 で ~3 桁の改善、allocation も 1 回に）。

## 残件

- `Fold` の要素ごとクロージャ確保（curried arity-one 値 repr）— 別 lane 候補。
- ユーザーの cons 蓄積を自動で救う lower の cons-recursion 特殊化 — future work。
