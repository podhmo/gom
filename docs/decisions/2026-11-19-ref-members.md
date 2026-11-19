# `Ref a` のメンバー面: `Ref.new`/`get`/`set`（rt-backed builtin module）

- 状態: 採用
- 日付: 2026-11-19
- Supersedes: なし

## Context

`Ref a` は v1 の型として spec 3.1 から存在する（design.md の OCaml `ref` 相当
の限定採用、value restriction の例 `let r = Ref.new []` もそれを前提に書かれて
いる）が、member table を持たず `Ref.*` はすべて E0201
"exposes no members yet" だった。wave25 lane A として最小 API を実装する。

## Decision

`Ptr`/`Array`/`View` と同じ機構 — コンパイラ組み込みの `Ref` モジュールを
internal/stdlib の rt-backed member table として配線する。言語構文（代入形
`r := v` や dereference 演算子）は足さない。

| member | Gom signature | Go 意味 |
| --- | --- | --- |
| `Ref.new` | `a -> Ref a` | `new(v)` — 初期値を持つセル。OCaml `ref x` 相当 |
| `Ref.get` | `Ref a -> a` | `*r` — 読み出し |
| `Ref.set` | `Ref a -> a -> Unit` | `*r = v` — 書き込み |

- `Ref a` の Go 表現は `*T`（codegen の既存の `conPtr` 写像）。`Ptr a` と同じ
  表現だが nominal に別型 — `Ref` は常に初期化済みで nil を持たないので
  `is_nil`/`nil`/`to_option` は置かない。
- `Ref` は Gom 内部の可変セル。Go 境界の `*T` の読みは `Ptr a` が担い、
  `boundaryType`/`gomOnly` は `Ref` を Gom-only のままにする — `Ref a` を
  `*T` パラメータへ渡す変換も `Ptr a` からの逆変換も足さない。Go 側と状態を
  共有したいときは `Ptr` を使う。
- `Ref.new` は引数に初期値を取る（`Ref.new : a -> Ref a`）— spec 3.3 の
  value restriction の例 `let r = Ref.new []` が既にこの形を前提にしている。
  `Ptr.new : Unit -> Ptr a`（zero 値確保）とは対照的で、`Ref` には
  「未初期化」の状態がないので Unit 形は採らない。

## Rejected alternatives

- **代入構文 `r := v` / dereference 演算子**（prompt の「set 相当の代入形」案）:
  新しい左辺形式は E0511 の家族を増やし、`ptr.Field = v`（wave24 lane B）の
  限定と矛盾する。v1 は module member で足りる。
- **`Ref.modify` / `Ref.swap` 等の便利メンバー**: `Ref.get`/`Ref.set` で書ける
  ものを最小面から外す。要望が具体化したら足せる。
- **`.gomi` パッケージとして実装**: `stdlib/cmp` と違い `ref` という Gom
  パッケージを新設する理由がなく、`Array`/`View` と同じくぶら下げ先がない。
  手書き table の方が boundary fact（`IsGo` → rt 転送）と一致する。
- **境界で `Ref a` ↔ `*T` を暗黙変換**: `Ptr` の役割を食い、Gom 側の可変
  セルを Go 側の書き込みに無制約に曝す — 両者の役割を分ける。

## 結果へ影響する範囲

- `rt/ref.go`: `RefNew`/`RefGet`/`RefSet`
- `internal/stdlib/builtin.go`: `refMembers` + `builtinModules` 登録
- `internal/resolve/scope.go`: `stdlibModules` に `Ref`（ついでに漏れていた
  `Array` も登録 — unwired compile の E0201 note が正しく wiring gap を指す）
- `docs/spec.md` 8.3: `Ref` モジュール表
- `testdata/spec/ok_ref_ops.gom`、`E0201_ref_member.gom`、
  `testdata/golden/ref_ops.gom`
- `skills/gom-agent-guide`: 未実装の記述を更新
- open-questions 可変性項: `Ref` メンバー実装済みの注記
