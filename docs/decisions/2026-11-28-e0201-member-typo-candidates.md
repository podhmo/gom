# E0201 module member typo 候補表示

- 状態: 採用（調査済み・実装候補の決定。実装は未実施）
- 日付: 2026-11-28

## 決定

E0201 typo/import 補完の最初の実装面は **module member typo の候補表示**とする。`mod.name` の `mod` が解決済みで member table があるのに `name` がない診断について、table 内の近い member 名を note と structured data で示す。bare name typo と未 import stdlib module の候補表示は今回の実装面にしない。

## 候補表示の規則

- 対象は E0201 の `namespace: "member"` で、member lookup に使った table が存在するもの。builtin module、import した Gom package、import した Go package を含む
- 候補は同じ table の exported member 名だけとする。`Option.Some` のような module/type 同名 fallback の constructor table、bare name、type 名、record field は最初の実装に含めない
- 編集距離は optimal string alignment（1 文字の挿入・削除・置換・隣接転置を 1 とする）で、距離 1 のものだけを候補にする。`lenght` → `length` のような転置を拾いつつ、距離 2 で出る `max`/`min`、`find`/`fold` のような false positive を避ける
- 候補は距離昇順・名前昇順で最大 3 件。現行規則では距離はすべて 1 だが、将来 threshold を広げたときも順序を固定する
- 1 件なら note を ``did you mean `length`?``、複数なら ``did you mean one of `a`, `b`, or `c`?`` とする
- `data.candidates` に `{ "name": ..., "edit_distance": 1 }` の配列を同じ順で入れる。`fixes` は付けず、safe fix / unsafe fix / import 自動追加は行わない

## 理由

- member table は `selectMember` が既に持ち、診断生成点から同じ namespace の閉じた候補集合を取れる。実装が小さく、候補の意味も利用者に伝わりやすい
- bare name typo は利用者価値が高いが、lexical scope chain の走査・shadowing 後の visible binding の重複除去・module/type/builtin の kind filtering が要る。候補集合が大きく、同じ距離規則でも false positive の評価が必要になる
- 未 import stdlib module は `str.length` のような実害があるが、undefined qualifier から toolchain 同梱 stdlib を引く registry が resolve 側にない。loader probe や stdlib index の所有を決める必要があり、module member typo より実装面が広い
- note は text 利用者に見え、`data.candidates` は JSON consumer が使える。どちらも候補提示なので typo 修正の意味をコンパイラが確定せず、safe fix の誤適用を避けられる

## 却下・後送り

- bare name typo: 後送り。`aera` → `area` は価値があるが、visible scope の正しい候補集合と kind filter を別途設計する
- 未 import stdlib module 候補: 後送り。Gom stdlib に限定しても、toolchain stdlib index の所有と import alias 表示を決める必要がある。goimports 的な Go package 探索は対象外のまま
- module/type fallback constructor の候補: 後送り。まず真の member table に限定し、constructor fallback を同じ候補へ混ぜるかは利用頻度を見て決める

検証記録: [sketch/2026-11-28-wave55-lane-e-typo-candidates-verify](../sketch/2026-11-28-wave55-lane-e-typo-candidates-verify.md)。
