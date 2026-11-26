# type-erased composite 表示は runtime representation canonical にする

- 状態: 採用
- 日付: 2026-11-25

## 文脈

型パラメータを残す composite（例: generic helper の中の `[x]`）は、package
レベルの `gomShowN` が要素型を書けないため `ShowRuntime` に落ちる。
従来の compiled 側はそこを `fmt.Sprint`、つまり Go `%v` で描き、list は
空白区切り、tuple/record/variant は Go struct 綴りになった。evaluator oracle
は同じ位置を Gom comparison display に近い綴りで描くため、wave46 lane A が
括弧 canonical と fallback 境界を揃えたあとも内容表示がずれていた
（implementation-gaps 登録済み、wave47 lane A）。

## 決定

`ShowRuntime` は `rt.ShowRuntime`（comparison 表示）と root splice 用の `rt.ShowDisplay` に変え、runtime representation が識別できる
Gom 値を Gom comparison display 側へ寄せる。

- scalar は machine representation の幅で描く（符号付き/符号なし、float32 /
  float64、bool、string の quoted comparison 表示）
- `rt.TupleN` は `(a, b)`、slice/array representation は `[a, b]`、builtin
  Option / Result は `Some v` / `None` / `Ok v` / `Err e` で描く。`Err` の
  payload が静的に error interface のときは `Error()` の文字列を優先し、
  concrete な payload 型が error を実装するだけのときは record metadata を
  残す
- ユーザーの record / variant は generated code が登録する show metadata を
  reflection が引き、record は `{ field = v }`、variant は
  `Ctor ~name: v` で描く。metadata は field/constructor の Gom 綴りだけを持ち、
  generated な `gomShowInit_<Type>()` registration function が
  `rt.RegisterShowRecord` / `rt.RegisterShowVariant` へ登録し、runtime display を
  持つ generic function は描画前に unit の registration function を呼ぶ。
  registration function は同 package の package-level variable を参照しないため、
  この呼び出しは Go の初期化依存を増やさない。import された package 側は
  package 初期化でも同じ関数を呼び、importer は unexported function を直接
  呼べなくても初期化完了時点で metadata を見られる
- 一フィールド record の defined representation（`type X F`）でも metadata を
  登録し、`{ field = v }` に戻す。`F` が型パラメータを含まないときは
  `rt.RegisterShowUnwrap` が field representation の reflect.Type も登録し、
  composite field へ再帰する。slice/array representation は conversion metadata
  がなくても representation から list canonical に描く
- 登録のない Go struct・pointer・map・function など opaque 値は従来どおり
  `fmt` の `%v` に留める。Go `%v` の細則を再実装しない
- nested operand の括弧 canonical と runtime fallback に落ちる境界は wave46
  lane A の決定を変えない。runtime renderer の内部では evaluator と同じく値形で
  record / variant / opaque 値を括弧に入れる

`Char` は runtime representation 上 `rune`、すなわち Int32 と区別できない。
型パラメータで erase されたあと compiled が `'y'` を復元する方法はないため、
runtime fallback の canonical では数値として描く。evaluator oracle も
`runtimeFallback` の中では `VChar` を同じ数値表示に倒し、representation に
残らない区別を oracle 側だけ復元しない。

## 却下した案

- Go `%v` を完全再現する → wave47 lane A の採択範囲外。Gom 表示へ寄せる目的と
  逆であり、opaque 値の fmt 細則まで compiler runtime が追うことになる
- 静的型情報を type parameter ごとに別途 runtime へ渡す → 呼び出し規約を変える
  大規模な表現変更になり、今回は representation が保持する情報で canonical 化
  する範囲に留める
- record/variant を field 名の逆変換で推測する → `foo_bar` / initialism の逆写像は
  一意でなく、opaque Go struct との区別も付かない。生成側が metadata を登録する
  方が確実

## 検証

prototype 記録: [wave47 lane A runtime display verify](../sketch/2026-11-25-wave47-lane-a-runtime-display-verify.md)。
