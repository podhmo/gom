# wave55 lane E: E0201 typo/import 候補の最小面調査

## 現状

次の 3 形はいずれも E0201 の主 message と主 span だけで、候補は出ない。

```gom
package main

fn area (x: Int) -> Int = x
fn use (x: Int) -> Int = aera x              // undefined name `aera`
fn member (xs: List Int) -> Int = List.lenght xs  // `List` has no member `lenght`
fn missing (s: String) -> Int = str.length s      // undefined name `str`（import 忘れ）
```

text output で確認すると、`aera`・`List.lenght`・未 import `str` のどれにも note/data/fix がない。

## 候補の比較

| 案 | 候補集合 | 実装しやすさ | 誤候補リスク | 判断 |
| --- | --- | --- | --- | --- |
| bare name typo | lexical scope chain の visible term binding | scope 走査・shadowing 重複除去・kind filter が要る | local/fn/builtin/module が混ざり、距離だけでは意味がぶれやすい | 後送り |
| module member typo | 解決済み module の member table | `selectMember` が table を持ち、診断点に近い | table が閉じていて小さく、最も低い | 採用 |
| 未 import stdlib module | toolchain 同梱 stdlib の module path | resolve 側に stdlib index がなく、loader probe か registry 共有が要る | import 追加は意味を変えるが note だけなら低い | 後送り |

## 編集距離と上限

stdlib member 名を確認すると、距離 2 には `max`/`min`、`find`/`fold`、`tail`/`take`、`filter`/`iter` のような別操作が多い。最初の実装は optimal string alignment 距離 1 に絞るのが適当。これで `lenght` → `length` の隣接転置も 1 編集として拾える。候補は距離昇順・名前昇順で最大 3 件にし、note と deterministic な `data.candidates` に出す。

## 採用する最小実装形

1. `namespace: "member"` の E0201 で、lookup に使った member table があるものだけ対象にする
2. table 内 exported member へ距離 1 の候補を付ける
3. 1 件なら ``did you mean `length`?``、複数なら ``did you mean one of ...?`` の note
4. `data.candidates = [{"name": ..., "edit_distance": 1}]`
5. `fixes` は付けない

## 実装 lane で固定すべき fixture

- `List.lenght` に `length` の候補 note/data が出る
- import alias 付き stdlib/Gom module で同じ候補が出る
- Go package member typo（例: `fmt.Printlnn`）で同じ規則が効く
- `Go.Slice.lenght` のような operation member typo でも `length` の候補が出る（constructor fallback 経路でも member 診断として扱う）
- 距離 2 になる typo（例: `List.fndd`）や遠い名前に候補が出ない
- 候補が 4 件以上作れる人工 module では deterministic に 3 件へ truncate される
- `fixes` が空であること、text/JSON 両方の出力を確認する

## 結論

module member typo を最初の実装面とする decision を記録した。bare name typo、未 import stdlib module 候補、module/type fallback constructor 候補は後送り。実装は TODO E0201-1。
