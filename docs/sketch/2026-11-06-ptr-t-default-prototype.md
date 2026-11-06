# Prototype 検証: `*T` 既定 — `Ptr T` 派生ビュー vs `Option (Ptr T)` 宣言ビューの呼び出し側比較

- 日付: 2026-11-06
- 決定: [decisions/2026-11-06-ptr-t-default](../decisions/2026-11-06-ptr-t-default.md)
- 関連: [ptr-ops-surface](../decisions/2026-11-05-ptr-ops-surface.md)、[nilable-iface-pair-ok-payload](../decisions/2026-11-06-nilable-iface-pair-ok-payload.md)

## 目的

自動 `*T` マッピングの既定を `Ptr T` にするか `Option (Ptr T)`
にするかを、実 Go API の呼び出し側の書き味で比較する。対象は
`(*T, error)` の Result 経路ではなく lone `*T`・`(*T, bool)`・
never-nil コンストラクタ形（`(*T, error)` は既存の Result 経路が
カバー済みだが、比較のため `(*T,error)` も含めた）。

## prototype の場所

scratch ディレクトリ（未コミット）:

- `/tmp/gom-w12/go.mod` — `module example.com/gomw12`、
  `replace github.com/podhmo/gom => <worktree>`
- `/tmp/gom-w12/probeapi/probeapi.go` — scratch Go API:
  `LonePtr(filled bool) *Config`（nil 返し得る lone `*T`）、
  `NewConfig() *Config`（never-nil コンストラクタ形）、
  `PtrOr(n int64) (*Config, error)`（n==0 で `(nil,nil)` の
  smuggle ケース）、`MaybeBox(n int64) (*int64, bool)`（comma-ok）
- `/tmp/gom-w12/ptrA/main.gom` — 派生 `Ptr T` ビュー側
- `/tmp/gom-w12/ptrB/main.gom` — `Option (Ptr T)` 宣言ビュー側
  （extern で全 4 API を Option 宣言 — `nilablePresence` の受理は
  既存実装そのまま。既定が Option だった世界を外挿で試す）

実 API: `flag.Lookup`（`*Flag` — nil 返し得る lone `*T`、
既存 golden `extern_flag.gom` と同じ対象）。コンパイラは未変更の
`/tmp/gom`（worktree ビルド、prototype パッチ無し — `Ptr` 既定は
現行実装そのもの）。

## 実行コマンド

```
cd /tmp/gom-w12/ptrA && /tmp/gom build main.gom && go build -o main main.go && ./main
cd /tmp/gom-w12/ptrB && /tmp/gom build main.gom && go build -o main main.go && ./main
```

## 観測

ptrA（派生 `Ptr T`）出力:

```
<absent>        // flag.Lookup + Ptr.is_nil
{}              // Ptr.to_option → None（Option の生表示）
nil             // LonePtr(false) + is_nil
config          // LonePtr(true)
false           // NewConfig().Verbose — 1 式、分解不要
miss:nil        // MaybeBox(0): ok=false 側で v が nil と観測できる
boxed           // MaybeBox(5)
fatal           // PtrOr(-1)
ok:nil-ptr      // PtrOr(0): (nil,nil) → Ok <nil Ptr>、is_nil で観測
ok:cfg          // PtrOr(3)
```

ptrB（`Option (Ptr T)` 宣言）出力:

```
<absent>
nil
config
false           // NewConfig() — match + dead `None` arm
miss            // MaybeBox(0): ok チャネル。nil の情報は消える
boxed
fatal
ok:none         // PtrOr(0): (nil,nil) → Ok None
ok:cfg          // PtrOr(3)
{1 absent <nil>} // pair_err_try 0: try が Option (Ptr Config) を yield
```

## 書き味の比較（`main.gom` 両版）

- **never-nil コンストラクタ**（`NewConfig`/`bytes.NewBufferString`
  形）— `Ptr` 既定: `(probeapi.NewConfig()).Verbose` の 1 式。
  `Option` 既定: `match` に `None -> false` の dead arm を要する。
  `*T` を返す Go API の多数派はこの形で、Option 既定は全呼び出し
  に unreachable arm を強いる。
- **二重アンラップ** — `Option (Ptr T)` の `Some` の中身は依然
  `Ptr`（nil 許容）。`fresh_write` は `match` の内側でさらに
  `Ptr.get` が要る。Option を既定にしても `Ptr` 操作の必要性は
  消えず、その上に層が乗るだけ。
- **comma-ok** — `(nil, true)` を返す病的 API は Option 宣言でも
  `Some <nil>` を通す（`ok` チャネルを読むので）。Option は
  「中身が non-nil」を保証しない — absence テストを 1 回するだけ。
- **nil の観測** — `Ptr` 既定でも `(nil,false)` の miss 側で
  `v` が nil と分かる。`Option` 既定では `None` しか見えない
  （comma-ok ではそれが API の契約なので害はないが、情報は減る）。
- **on-demand の安全経路** — `Ptr.to_option` で必要な場所だけ
  Option 読み出しが得られる。既定が `Ptr` でも安全経路は届く。
- **`try`** — Option 宣言で `try` は `Option (Ptr Config)` を
  yield（`pair_err_try` 動作確認）。既定 `Result (Ptr T) GoError`
  では `Ptr T` を yield — どちらも動く。

## `ResNilable` との関係（設計読み取り — コード確認）

`lower.GoSig.ResNilable` は raw 第 1 結果がメソッド持ち
interface のときだけ立つ（`internal/goimport/extern.go` の
`mapSignature`/`BindExtern`）。`*T` には立たない — verbatim
`Result (Ptr T) GoError` が既定そのものなので「宣言された
strict 契約」と「既定の繰り返し」を区別できず、拡げると派生
ビューにも boundary panic が付いて `(nil,nil)` の正当な返しを
壊す。`*T` の nil は `is_nil` で観測できるので検査宣言は不要。
決定の根拠を確認済み。

## 結論

`Ptr T` 既定を採用。`Option (Ptr T)` は nil = absence の契約を
持つ API への extern 宣言上書きとして残す（現行の受理規則の
まま）。`ResNilable` は `*T` に拡げない。
