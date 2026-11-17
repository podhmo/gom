# lint の callee 照合は let の値エイリアスを辿る

- 状態: 採用
- 日付: 2026-11-18

Supersedes: [mutable-sharing-lint](2026-11-04-mutable-sharing-lint.md)
の "An aliased callee ... is not recognized ... accepted as a v1
limitation" — callee 同一性で警告を決める lint について、その範囲外
扱いを撤回する。

## 背景

`let mk = Chan.make; mk 0` のように builtin や Go 関数を let で値として
束ねると、`Apply.Call` は `CallUnknown`（callee なし）に分類され、
`ap.Call.Callee` のシンボル一致に掛からない。callee 同一性を根拠にする
lint — W0701（`Chan.make` の可変要素）、W0501（in-place 変更契約の
`[]T` コピー）、W0502（`errors.As` の target）— はすべてその経路を
警告なしに通り抜けていた（implementation-gaps「W0701/W0702 lint の
捕捉範囲外」）。

## 候補

- **何もしない（v1 scope-out の維持）**: エイリアス経由で警告が出ない
  ことを文書で認め続ける。`let mk = Chan.make` は自然な書き方で、
  警告が出ないことに気付かないまま抜け道になる。
- **callee 照合の前に let の値エイリアスを辿る（採用）**: `let x = f`
  で値がシンボル参照（`Var`/`MemberExpr`）そのものの束縛を map 化
  し、lint の callee 解決で chain を追う。

## 決定

`lintAliases` が全宣言（トップレベル `LetDecl` と本体中の `Let` 式）を
1 回走査して `let <var> = <symbol reference>` の対応表を作り、
`lintCallee` が `Apply` の callee（`Call.Callee`、なければ `Fn` の
`Var`/`MemberExpr`）をその表で辿る。W0701・W0501・W0502 の 3 箇所が
同じ helper を使う。

- chain は `let mk2 = mk` の多段も辿る — let は毎回新しいシンボルを
  束ねるので cycle は作れない。
- W0501/W0502 のゲートは `Call.Kind == CallGo` から「解決した callee が
  `IsGo`」に置き換わる — CallGo 分類そのものが Go シンボル由来なので
  同じ意味の、直接呼出しとエイリアスに通じる条件になる。
- 値がシンボル参照でない束縛（`let mk = fn n -> Chan.make n` のような
  ラッパー）は辿らない — 実際に別の関数経路を通るので callee 同一性の
  範囲外として正しい。
- W0702 の thunk・クロージャ捕捉は範囲外のまま — あれは callee 同一性
  ではなく値の可変性が型に現れない問題で、本決定とは別件。

## 検証

`testdata/spec/W0701_chan_mutable_elem.gom` に `mk`・`mk2`（多段）の
alias 経由警告を追加、`W0502_errors_as_target.gom` に `as2` alias を
追加して期待診断を pin。ローカル関数の `takes` では IsGo ゲートにより
誤爆しないことも spec fixture（`ok_go_any_positions.gom`）で確認。
