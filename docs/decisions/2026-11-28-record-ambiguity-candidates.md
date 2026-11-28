# ambiguous record literal の候補表示

- 状態: 採用（実装・検証した — [検証](../sketch/2026-11-28-wave56-lane-a-record-candidates-verify.md)）
- 日付: 2026-11-28

## 決定

E0407 ambiguous record literal では、主 message は現行形を保ち、候補 record の宣言位置を secondary label で示す。あわせて候補名を structured data に入れる。候補名だけを主 message へ埋め込む形は採らない。

- label は候補ごとに `candidate record <name> declared here` とし、宣言 span を指す。5 件までとし、超過分は `and N more candidate records` の note で数だけ示す
- `data.candidates` には全候補を deterministic な qualified name 順で入れる。要素は少なくとも `name` と `qual` を持ち、text renderer は data を解釈しない
- 候補収集は map iteration に依存せず sort する。foreign record では qualified name を使い、local record と同名でも区別できるようにする

## 比較

| 案 | 利点 | 問題 | 判断 |
| --- | --- | --- | --- |
| 候補名を主 message へ入れる | 小さい。text/JSON で同じ名前が読める | 候補数や qualified name で 1 行 message が長くなる。宣言位置は分からない | 不採用 |
| 宣言位置 label を付ける | text で候補宣言へ直接誘導できる。JSON でも副 span として扱える | 表示件数の上限と foreign declaration の扱いが要る | 採用 |
| structured data だけに入れる | emitter は小さい。機械処理には十分 | text renderer は data を表示しないため、CLI 利用者には見えない | 単独では不採用。label と併用する |

## 実装候補の境界

- 実装は別 lane。wave55 lane D は調査だけで compiler 変更を入れない
- `recordByFields` は ambiguity 時に候補を count するだけでなく deterministic に収集する。no matching record の E0401 には候補がないので対象外
- lane A の quiet recovery は変えない。E0407 の情報量改善と二次 mismatch suppression を混ぜない
- import した `.gomi` の declaration span は、実装 lane で diagnostics resolver へ供給する。供給できない source form がある場合は unresolvable span を出さず、その候補の label だけを省略し `data.candidates` は残す。local cross-file と import `.gomi` を text/JSON 両方で fixture 化する
- E0407 の `gom explain` / error-codes 解説も実装 lane で更新し、候補 declaration label と `data.candidates` を説明する

検証記録: [sketch/2026-11-28-wave55-lane-d-record-candidates-verify](../sketch/2026-11-28-wave55-lane-d-record-candidates-verify.md)。
