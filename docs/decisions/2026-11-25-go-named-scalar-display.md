# named Go scalar の表示は underlying scalar に揃える

- 状態: 採用
- 日付: 2026-11-25

## 文脈

`"${expr}"` と `assert_eq` の表示は、Go 由来の値を `GoError` 以外
`fmt` の `%v` に素通ししていた。しかし named Go scalar（例:
`time.Duration`）は Gom の式では underlying の数値として算術・比較に
参加し、evaluator も scalar payload として保持する。結果、`let d :
time.Duration = 1` の splice は evaluator が `1`、compiled binary が
Go の `String()` を通して `1ns` を返し、design.md 7 節の同一意味 surface
でずれた。

## 決定

named Go type の underlying が numeric / bool / string の basic type の
とき、表示はその underlying scalar に揃える。compiled の ShowPlan は
`ShowGoScalar` を持ち、fmt の type-specific な振る舞い（`String()` を
含む）ではなく underlying への conversion 後の strconv 表示を使う。
float32 underlying は bitSize 32 で描く。

- `time.Duration(1)` は `1ns` ではなく `1`
- `type SmallRatio float32` の `0.1` は `0.1`
- named string は comparison display では quote、splice では裸 — Gom の
  `String` と同じ使い分け
- struct / interface / map / slice / channel / function / complex など
  scalar underlying を持たない Go 由来値は従来どおり `%v`
- composite 内で Go scalar を括弧で包むかどうかは別問題（wave46 lane A）
  であり、この決定は括弧 canonical を変えない

## 却下した案

- `%v` を正とし evaluator 側で `String()` を再現する → evaluator は
  任意の Go メソッドを実行しない設計で、全 named type を faithfully に
  再現できない
- scalar でも `%v` を維持し「実装依存」と明記する → 言語が scalar と
  して扱う値の表示だけが type の Go メソッド有無で変わり、evaluator と
  compiled のずれを残す

## 検証

- `splice_go_named_scalar_underlying` — `time.Duration` が `1`、
  float32 underlying の `SmallRatio` が `0.1` で evaluator / compiled
  一致（[compare_test.go](../../internal/build/compare_test.go)）
