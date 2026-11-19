# メンバー選択の遅延を「受け手に未解決メタを含む型」まで拡張（`Ptr ?m`）

- 状態: adopted
- 日付: 2026-11-19
- 問い: `let b = Ptr.new ()` の `b : Ptr ?m` に対する field 選択・
  field 代入が即エラーになる — wave26 lane A の採否

## 決定

`checkSelect`（と `checkAssign` の provisional 受け入れ）の遅延条件を
「受け手が裸メタ」から「受け手の型が未解決メタを含む」へ拡げる。
`Ptr ?m` の field 選択は `pendingSels` に載り、use サイトが `?m` を
束縛したあと `revisitSelects` の fixpoint で member が解く。

これで `let client : Ptr http.Client = { let c = Ptr.new ();
c.Timeout = ...; c }` が内側注釈なしで通る。`b.Fn = Go.func1 (fn ...)`
の RHS は member 解決まで check を遅らせ、`Go.FuncN` の want が
seed として届く（遅延しないと `?1` のまま E0510 になる）。

メタが最後まで解けないときは member の E0401 を出さず、内側メタの
正直な E0407 に任せる（裸メタ受け手と同じ扱い）。

## 実装メモ

- `hasUnsolvedMeta` = `metasIn` で未束縛メタを1つでも見つける判定。
- `revisitSelects` の未解決判定も同じ条件に揃えないと、`Ptr ?m` が
  メタ未束縛のまま member 検索して E0401 を吐く。
- deferred な field 代入の RHS は `assignTarget.ValueSyn` に surface
  を持ち、member 解決時に real want で check する。RHS は literal の
  default より後に生まれるので、finalize は defaults ↔ revisitSelects
  をループにした（旧コードは「retry が mint するメタに default は
  要らない」前提の単発パスだった）。
- done マップ（`selDone`/`assignDone`）は checker に持つ — ループの
  各 round で revisitSelects を呼んでも二重解決・二重診断しない。
- member が最後まで解けない write の RHS は `finishAssigns` が
  want=nil で一度だけ check する（dead meta を want にすると無垢な
  literal まで E0407 になる）。

## 影響

- 既存の診断順序は変わらない — deferred RHS の診断は member 解決の
  位置に集まる。E0511_assign_deferred の期待診断はそのまま通過。
- spec 8.3 節のユーザー向け規則は不変（推論の内部改善のため spec 更新なし）。
- fixture: `testdata/spec/ok_ptr_meta_defer.gom`。
