# flat partial eval — 検証メモ

- 対象: wave22 lane3 `devin/wave22-flat-partial`
- 決定: [decision](../decisions/2026-11-19-flat-partial-eval.md)
- 検証日: 2026-11-19

## 機構

`internal/eval/call.go`:

- `evalFlatSig` — lower `flatSig` と同じ規則で矢印チェーンを 1 つの
  `types.Func` に平坦化
- `flatSlot` — 束縛先スロットが `fn` 宣言由来の flatArity マーカーを
  持つとき（lambda・builtin 由来のスロットは対象外 — lower の `l.flat`
  と同じ区分）、束縛値の VFunc を `fnBuiltin` facade で包む。facade の
  sig = flat 型全体なので `applyArgs` は flat arity 分まで partial を
  返し、飽和時に builtin が内側へ `applyOne` を繰り返す
- VFunc.flatArity — `fnValue` が `fn` 宣言の >=2-arrow パラメータごとに
  arity を記録する per-slot マーカー
- 束縛点 2 箇所: `applyArgs` の `nf.bound[rem[pos]]` と、ラベル適用の
  eta 残尾部 `nf.bound[rem2[k]]`

## 確認

- `TestEagerFnChunkTiming`（internal/build/eager_test.go）を新仕様に
  更新して通過:
  - eval `answer`: `let p = stage make` は `f 20` の部分適用 — residual
    未適用のまま `"survived"` を返す（旧仕様ではここで todo が発火）
  - eval `probe`: `(stage make) 0` で flat arity 充足 → E9003 panic
    （make の第 1 チャンクが飽和時に発火）
  - 生成バイナリ: 従来どおり `(stage make) 0` の時点で `gom: todo` —
    "after" に到達しない（回帰なし、eval と同じ発火位置）
- lambda パラメータ経路の回帰確認（review 指摘 BUG_0001）: `probeLam`
  が `let stg = fn f -> f 20` の canonical 経路で引き続き eager に
  panic — eval/生成コードとも発火位置一致
- spec fixture `testdata/spec/ok_flat_partial.gom`（部分適用・飽和適用の
  合法形、診断なし）
- `go test ./...` 全緑、`go vet ./...` ・`gofmt -l .` クリーン

## divergence の残存確認

発火タイミング差は解消。知っている残差:

- 副作用の観測順序だけが一致対象 — flat 適用内で eval が 1 引数ずつ
  `applyOne` する中間過程はユーザーから観測不能（applyOne 自体に
  副作用はない）
- 深い arrow チェーン（`A -> B -> C -> D`）も同じ規則 — flat arity は
  チェーン全長、facade は 1 段のみ
