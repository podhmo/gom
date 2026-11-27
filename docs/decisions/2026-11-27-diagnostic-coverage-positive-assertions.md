# 診断 coverage positive assertion 規約

- 状態: 採用（実装・検証した — [検証](../sketch/2026-11-27-wave52-lane-c-positive-assertions-verify.md)）
- 日付: 2026-11-27

## 決定

診断 coverage の例外表が unit integration / checker recovery evidence として指名した test では、diagnostic code literal が test 本体に存在するだけでは足りない。`requireCodes(t, diags, "E0502")` のように、承認済み positive assertion helper への直接引数として code を渡す形だけを evidence とする。

承認形は現時点では `requireCodes` のみとする。複数 code を同じ呼出しで確認してよい。data の追加検査や入れ子の `t.Run` は evidence の周りに書いてよいが、code literal は test 本体から見える direct call に置く。negative assertion、comment、無関係な literal、別 helper 経由の literal は evidence にしない。

## 理由

coverage 例外表は「その診断を positive に確認した test」を台帳化する仕組みであり、literal の所在だけを見ると負例・回復用 fixture・コメントを誤って登録できる。一方、すべての test assertion を意味解析するのは保守対象が大きい。例外表が指名した少数の evidence test へ direct positive assertion 形だけを要求すれば、規約は小さく、事故は編集時に検出できる。

## 検証

scanner fixture で positive helper、nested `t.Run`、negative comparison、negative helper、comment、無関係 literal を固定し、E0502/E0606 の evidence test を承認形へ揃えた。
