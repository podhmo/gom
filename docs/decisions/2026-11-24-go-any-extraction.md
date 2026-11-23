# `Go.Any` からの取り出し — comma-ok の `Option` 返りを唯一の面とする

- 状態: 採択（wave39 lane A として実施）
- 日付: 2026-11-24
- 関連: [go-any-spellable](2026-11-06-go-any-spellable.md)、
  [go-any-conformance-materialize](2026-11-06-go-any-conformance-materialize.md)

## Context

`Go.Any` へのボックスは spec 8.3（`*` 節）で全面対応済み — want が
`Go.Any` の位置にはどの Gom 値も置け、境界で `any(x)` が生成される。
逆向き — `Go.Any` から具象型への取り出し — は spec が明示的に
拒否する（`Go.conv` では E0507、「Go の型アサーションに相当」）。

問題は代替の面がないこと: `json.Unmarshal` で `any` に decode した
値、`Go.Map String Go.Any` の読み出し、interface 経由で返る値は、
Gom 側から一切触れない（open-questions の記載どおり、回避は
`json.RawMessage`/`Go.Map` の他型/reflect 経由に限る）。

Go 側のプリミティブは `x.(T)`（panic 形）と `v, ok := x.(T)`
（comma-ok 形）の 2 形態。提供形の候補は open-questions の 3 案:

- **panic 式** — `Go.as x : T` で `x.(T)`、失敗時 panic
- **`Option` 返り** — `Go.try_as x : Option T` で comma-ok、
  失敗時 `None`
- **`match` 拡張** — `Go.Any` に型テスト腕を許す

## 調査

- ボックス方向は convBox/`ConvBoxAny` が既存で、数値の物質化
  （`any(int64(42))`）まで片付いている。取り出し側に既存の Core
  ノードはなく、新しい emit が必要。
- `Option` は `option.Option[T]` に写る（codegen/types.go）。
  comma-ok → Option の橋渡しは `option.FromGo(v, ok)` が既存なので、
  `v, ok := x.(T); option.FromGo(v, ok)` の小さな block emit で
  足りる — 専用の rt helper は要らない。
- `Option.get` が既に「Some を剥がし、None で diverge」する panic
  経路を持つ。**Option 返りを採れば panic 形は
  `Option.get (Go.try_as x : Option T)` で導出できる** — panic
  プリミティブを別途足す必要がない。
- `match` 拡張は最も表現力が高い（複数の動的型への分岐を 1 形で
  書ける）が、Gom の `match` は sum 上の構造的網羅検査で、
  `Go.Any` は開いた型 — 網羅性の意味論（default 腕必須化）から
  新設計が要る。json.Unmarshal のような単一型取り出しには
  過剰で、lane 単位の変更としては最も重い。

## Decision

**`Option` 返りを唯一の取り出し面として採用する。**

- `Go.try_as x : Option T` — `x : Go.Any` を Go の comma-ok 型
  アサーション `v, ok := x.(T)` で取り出し、`ok` なら `Some v`、
  失敗なら `None`。`T` は Gom の具象型式（宣言型・builtin・
  `Go.Slice`/`Go.Map`/named Go 型など `x.(T)` が合法な全て）。
  `Go.Any` や type param への取り出しは設計上拒否する — `x.(any)`
  は nil interface で失敗するため恒等ではなく、Go は型パラメータ
  への断言を許すが、Gom 側では取り出し面の意味論を単純に保つため
  具象型式に限定する。
- panic 形は導出 (`Option.get`) — panic プリミティブは足さない。
  `Go.as` 短形を将来欲しければ `Option.get` 糖衣として後日検討。
- `match` 拡張は今回採らない — 需要が分岐形（単一取り出しではなく
  多型ディスパッチ）として出た時点で open-questions に戻す。
- emit は comma-ok の 2 値束縛が文位置を要るため、文 block（
  `func() option.Option[T] { v, ok := x.(T); return
  option.FromGo(v, ok) }()`）または `option` に近い generic helper
  で生成する。lower が Conv 系ノードで生成。eval は `VUnit` 系の
  実値に対し同じ断言を内部で行う。

## Rejected alternatives

- **panic プリミティブ `Go.as`** — `Option.get` で導出可能で、
  Gom の失敗を型で表す方針（`|?>`/Result）に逆行する単独形。
- **`match` 型テスト腕** — 開いた型への網羅性設計が新規に要り、
  単一取り出し需要に対し重すぎる。
- **`Go.conv` の受理に倒す** — spec が明示的に拒否した方向を
  conv に混ぜると「`Go.conv` は Go の変換規則」の一意の読みが壊れる。

## 採択時の実装メモ

- check: `Go.try_as` を `Go.conv` と同系の組み込みフォームとして
  `x : Go.Any` と目標 `T` を検査（`x.(T)` が合法な T のみ受理）。
- emit: comma-ok は文位置が要るため block emit または
  `option.FromGo` 経由。`option.Option` の `Some`/`None` 構築は
  既存の sum emit を再利用。
- spec: 8.3 の「取り出しは `Go.conv` では認めない」の段落に
  `Go.try_as` を追記。spec fixture は受理/拒否（`Go.Any` ・メタへ
  の取り出し・`Option` ネスト）を両方向で。
- eval: `E9001` 相当の未実装拒否ではなく内部断言を実装する。
