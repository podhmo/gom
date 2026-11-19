# Wave 27 orchestration prompt drafts

State as of 2026-11-19: wave26 の 4 lane（#79–#82）を PR 済み・
チェーンでマージ待ち。A は `Ptr ?m` 遅延拡張、B は fixture+
台帳解消、C は `stdlib/cmp` 合成関数、D はラベル引数調査
（省略可能引数は defaults record + `with` で代替する方針に
収束 — `docs/sketch/2026-11-19-labeled-args.md` 結論節）。
実行モデルは wave26 と同じ前提（単独実行・Devin Review・
podhmo がマージ）。lane 順は下からチェーンする。

## 候補（採否は podhmo が lane ごとに決める）

**採択（podhmo、2026-11-19）: A〜D の 4 lane 全部。lane A は (b)
codegen 退避 rename。wave25 残件の追加 lane なし。**

### A. トップレベル束縛名の Go 予約名衝突 — `devin/wave27-reserved-names`

```
[impl] implementation-gaps の唯一の open 項: `let init`/
`fn init`/`fn main`/`let main` が型検査を通るが生成コードが
そのまま `func init(w ...)`/`func main(...)` を吐き、
`go build` が "func init must have no arguments and no return
values" / "main redeclared" で失敗する（FuzzCompile seed
検出）。対応方針は2案: (a) check で予約名を E0xxx として拒否
（診断が早く明快・既存コードの互換影響なし — 現状そもそも
build できないため）、(b) codegen で束縛名を退避（`init` →
`init_` など、参照を全部 rename）。どちらが spec らしいか
判断して実装 — `main` パッケージの `fn main` 入口は正当な
ので、衝突するのは引数を持つ/非 main パッケージでの同名のみ
という整理に注意。testdata/spec に診断ケース or golden に
退避出力を固定。
Ownership: internal/check or internal/codegen + lower,
docs/implementation-gaps.md, testdata/spec|golden.
```

### B. `fn` ラムダのパラメータ注釈 `fn (x: T) ->` — `devin/wave27-lambda-param-ann`

```
[impl] `fn` ラムダのパラメータに注釈を書けない（`fn (x: T)`
は E0101）— wave26 lane B で明示的にスコープ外とした残件。
これがあれば `Go.funcN` の lambda や高階関数の引数で、外側の
want に頼らずローカルに型を供給できる（遅延経路の万能化では
なく明示オプション）。syntax の `fn` パラメータ節に `(` の
あと `name: Type` を許すよう拡張、check の lambda パラメータ
束縛に ascription を流す。`fn` 宣言側の既存 `(name: T)`
仮引数との規則を揃える。testdata/spec にケース。
Ownership: internal/syntax, internal/check, testdata/spec.
```

### C. dead-work 診断の quiet 化仕上げ — `devin/wave27-dead-work-quiet`

```
[impl] wave26 中盤の残件: 遅延 work が死んだとき
`quietMetasIn(span)` はノード span 内で mint されたメタだけ
quiet にするが、apply/assign の standing meta（結果 `?r`・
select の `?m`）は span 外で mint されるため loud のまま残り、
`let y = x.iter ()` のように束縛が生きると「型注釈が必要」
E0407 が本来の失敗診断に重なって出る。dead work 系（dropped
apply・dropped assign・失敗 member の standing meta）の
standing meta を drop 時に quiet にする — wave26 の
iterFailedOn/dropped/failed フラグの流儀に揃え、
`for` 以外の dead work でも同じ二重重診断を潰す。
`for v in y` の y が未解決だと更に for の iterable 診断が
出る点にも注意（E0407 が正しいか dead と見なすか）。
testdata/spec に「失敗した遅延呼出しの結果を束縛する」
ケースで E0401 のみを固定。
Ownership: internal/check (dropDeferredOn, quietMetasIn,
revisitSelects), testdata/spec.
```

### D. record→ラベル引数の spread 調査 — `devin/wave27-arg-spread`

```
[investigate] 省略可能引数の代替が defaults record + `with`
に収束したあとの残件: `f ~..opts` / `f ..opts`（record を
labeled 仮引数に `**` 展開する TS `...`/Python `**` 相当）が
欲しくなる条件を調査。labeled 呼び出し規約（spec §2）に
record 展開を足す場合の構文候補・arity/重複判定・型検査への
影響範囲（apply.go のスロット解決・ClassMappedCall）を測り、
「record 引数1個で受ける今の方式で足りる」境界線を整理する。
導入が本当に要るかの判断材料を sketch に残す。
Ownership: docs 調査 →（採用なら）internal/syntax,
internal/check/apply.go.
```

（wave25 調査 lane の残り採否 — composite literal (a) と Go
interface 実装 — は引き続き未確定。採択されたら別 lane として
追加する。）

## 申し送り

- CI は push-to-main / PR-to-main synchronize でしか走らない —
  チェーン PR は Devin Review のみ。マージは podhmo が
  retarget → `gh pr merge --merge` を下から順に。
- 遅延 iter の claim 分類（wave26 の知見）: iterable メタは
  (a) pending work の RESULT → iterPendingOn で既定待ち、
  (b) pending work の RECEIVER → 早めに List 既定、
  (c) 失敗/dropped/failed work の standing meta →
  iterFailedOn で quiet、
  (d) 無主 → `List` 既定。
- `q.done` は replay 成功と dead-work drop の両方で立つ —
  「失敗かどうか」は `dropped`/`failed` フラグで見る
  （replayApply が replay 中に新しい診断を出したら failed）。
- `for`/`let` の未注釈束縛の E0407 は quiet meta を踏むと
  消える — dead work をさらに増やす lane C の土台。
- spec fixture の再生: `go test ./internal/build -update -run
  TestSpec`。golden の再生: 同コマンド -run TestGolden。
  gocall は `cmd/gom/gocall_test.go` のパッケージ列挙に登録。
- scratch チェック環境: `/tmp/gomfix2/`（go.mod `module tmpfix2`
  + `replace github.com/podhmo/gom => ~/repos/gom`、
  `/tmp/gom check .`）。`.gom` は `package` 節から始める。
- コミットメッセージにバッククォート/`$()` を入れない —
  `-m` を複数回に分けて書く（shell が喰う）。
