# `*T` の既定マッピング — 派生ビュー・extern ともに `Ptr T`、`Option (Ptr T)` は宣言による上書き

- 状態: 採用（検証済み — [検証](../sketch/2026-11-06-ptr-t-default-prototype.md)。spec.md §8.3/§8.6 へ反映済み）
- 日付: 2026-11-06
- 関連: [ptr-ops-surface](2026-11-05-ptr-ops-surface.md)（`Ptr` 操作面の採用）、[nilable-iface-pair-ok-payload](2026-11-06-nilable-iface-pair-ok-payload.md)（nil-able 宣言ゲート）、[problems/ptr-t-no-deref](../problems/2026-11-05-ptr-t-no-deref.md)

## Context

`*T` が Gom の型へ写るときの既定が未決だった
（open-questions.md「`*T` の既定」）。選択肢は:

- `Ptr T` — Go と同じく nil の参照・メソッド・field 読み出しが
  panic する生のポインタ。`Ptr.is_nil`/`Ptr.to_option`/`Ptr.nil`
  で nil の観測・安全な読み出し・nil の供給ができる
  （[ptr-ops-surface](2026-11-05-ptr-ops-surface.md) で採用済み）。
- `Option (Ptr T)` — nil を `None` に写す。安全だが、すべての
  `*T` API に match 分解を強いる。

`Ptr` モジュール採用前は `Option (Ptr T)` が唯一の nil 安全経路
だった。今は生の `Ptr` でも同じ安全操作が使えるので、「既定を
どちらにするか」が残る問いになった。問いは派生ビュー（型由来の
自動対応）と extern 宣言の両方、結果とパラメータの両方について
答える必要がある。

関連する原理は nil-able 族の決定で確立済み — **型由来ビューは
正直に、strict な主張は宣言されて境界で検査される**。`Ptr` の
nil は Gom から観測できる（`is_nil`）ので、verbatim の `Ptr T`
は nil を隠さない — メソッド持ち interface（nil だと呼び出しが
panic して観測不能）と違って smuggle ではない。

## Decision

### 派生ビューの既定は `Ptr T`

- lone `*T` 結果 → `Ptr T`（`flag.Lookup` は `Ptr Flag`）
- `(*T, error)` → `Result (Ptr T) GoError` — `(nil, nil)` は
  `Ok <nil Ptr>` として入るが、`Ptr.is_nil` で観測できるので
  正直なビュー
- `(*T, bool)` → `(Ptr T, Bool)` タプル（`(T,bool)` → タプルの
  一般則どおり）
- `*T` パラメータ → `Ptr T`。nil は `Ptr.nil ()` で渡す

### extern 宣言の規則は現行のまま — `Option` は結果側の上書きのみ

`nilablePresence` が既に受理している宣言ビューをそのまま採用する:

- `*T` 結果に `Option (Ptr T)` を宣言できる（nil を `None` と
  読む — `flag.Lookup` 形）。物質化は `option.FromGo(v, v != nil)`
- `(*T, error)` に `Result (Option (Ptr T)) GoError` を宣言できる
- `(*T, bool)` に `Option (Ptr T)` を宣言できる（`ok` チャネルを
  読む — nil ではない）
- パラメータ側に `Option` は認めない — `Ptr.nil ()` で nil を
  渡せるので param 側の nil チャネルは要らない

既定が `Ptr T` なので、nil = absence の契約を持つ API だけ
`extern` で `Option` を宣言する — spec の例 `flag.Lookup` の形。

### `ResNilable` は `*T` に拡げない — `*T` は別の仕組みも要らない

`ResNilable` が運ぶのは raw-type の事実だけ — 「raw 第 1 結果が
メソッド持ち interface である」。それが「verbatim 宣言は
non-nil 契約」という意味を持つのは、`payloadNilGuard` が宣言
ペイロードと raw 型の verbatim-Equal まで確認したときだけ。
`*T` には適用できない:

- `*T` では verbatim `Result (Ptr T) GoError` が**既定そのもの**
  なので、「ユーザーが non-nil を主張した」のと「既定を繰り返し
  た」だけなのを区別できない。`ResNilable` を `*T` に拡げると
  派生ビューにも boundary panic が付き、`(nil, nil)` を正当に
  返す API を壊す。
- メソッド持ち interface と違って `Ptr` の nil は `is_nil` で
  観測できる — smuggle を検査する宣言は要らない。
- 「non-nil `*T`」を宣言したい場合は `Ptr T` に狭い綴りがない
  （interface の `Option I` 対 `I` のような区別がない）。必要
  になれば新しい綴り（`NonNull` 等）として future work に回す —
  `ResNilable` の再利用ではない。

## 検証まとめ

実 API（`flag.Lookup`、probe パッケージの lone `*T` /
`(*T,bool)` / `(*T,error)`）で両ビューを書き比べた
（[sketch](../sketch/2026-11-06-ptr-t-default-prototype.md)）:

- never-nil コンストラクタ形（`bytes.NewBufferString` 型）で
  `Option` 既定は dead `None` arm を強いる。`Ptr` 既定は
  `(probeapi.NewConfig()).Verbose` の1式
- `Option (Ptr T)` は二重のアンラップ — `Some` の中身は依然
  `Ptr`（nil 許容）で、`(nil, true)` を返す comma-ok API は
  `Some <nil>` を通す。Option は `Ptr` 操作の必要性を消さず、
  その上に層を足すだけ
- `Ptr` 既定でも `Ptr.to_option` で必要な場所だけ Option の
  安全読み出しが得られる — 安全経路は既定を選ばなくても届く
- `try` は `Option` 宣言で `Option (Ptr T)` を、既定では
  `Ptr T` を yield する — 両方動く

## Alternatives

- **`Option (Ptr T)` を既定にする** — 不採用: `*T` を返す Go API
  の多数派は non-nil コンストラクタ（`New*` 形）で、dead arm を
  全呼び出しに強いる。nil = absence の API だけ extern で
  Option にすればよく、既定で全員に match を課す理由がない。
  `Some` の中身が依然 nil-able `Ptr` なので「安全」でもない。
- **`ResNilable` を `*T` に拡げる** — 不採用: 上記。verbatim が
  既定である型に declared-strict の意味を持たせられない。
- **現状を decision 化しない** — 不採用: open question が残ると
  後続レーンが自動マッピングを触るたびに既定を再検討する。

## Consequences

- spec §8.3 の `*T` 行は `Ptr T` のまま確定。§8.6 の「`*T` の
  既定は open-questions.md」は本決定への参照に変わる。
- open-questions.md の「`*T` の既定」項目は解決済みとして消す。
- 実装変更は要らない — 現行の自動マッピングと extern 受理が
  そのまま採用内容になる。
- `Option (Ptr T)` の extern 宣言は `flag.Lookup` 形の nil-able
  API に引き続き使える。
