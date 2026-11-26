# record 型名前置 literal

- 状態: 採用（実装・検証した — [検証](../sketch/2026-11-27-wave49-lane-b-record-type-literal-verify.md)）
- 日付: 2026-11-27

## 決定

record literal は型名を前置して書ける。

```gom
ValidatedOrder { id = id, total = total }
pkg.ValidatedOrder { id = id, total = total }
```

前置名は record 型を直接選び、同名フィールドを持つ複数 record による E0407 を回避する。型パラメータを持つ record では型引数を書かず、フィールド値または期待型から推論する。record への transparent alias も前置できる — alias は注釈と同じく展開してから record かどうかを見る。期待型は展開後の型と整合するときだけ型引数の推論に使い、alias が固定・再利用する型パラメータの制約を期待型で上書きしない。適合判定は通常の unification を speculative に replay し、カリー化された関数 tail や共有 meta の伝播を通常の型検査と同じ意味にする。展開結果に現れない alias パラメータは構築型を未定にしない。`{ base with x = v }` 更新や匿名 record の文脈推論一般化は導入しない。

頭の名前がコンストラクタや関数などの値として解決できる場合は従来どおり適用と読む。したがって `Wrap { value = x }` は `Wrap` コンストラクタへ record 引数を渡す式のままで、後方互換性を壊さない。型名が値として解決できず、record literal 引数 1 つだけに前置されたときだけ型名前置 literal と読む。

## 背景

段階型（`UnvalidatedOrder` / `ValidatedOrder` / `PricedOrder`）では同名フィールドが自然に並び、field-set だけの record literal 解決は曖昧になりやすい。型注釈で解ける場合もあるが、コンストラクタ payload やパイプライン途中では注釈の置き場が悪く、DMMF 型の validation では頻出する。

expected type / field type を使う推論強化は型推論の作用範囲が広い。wave49 の採択どおり、まず決定的な型名前置 literal だけを入れる。

## 実装

構文上は従来どおり適用 `T { ... }` として読み、resolve が「record literal 1 つだけを取り、頭が値として解決できない」形を型名へ束ねる。check はその型シンボルから record 型を確定し、型パラメータを fresh meta で instantiate して既存の record literal checker に渡す。typed AST は通常の record construction であり、generated Go は従来の literal と同じである。
