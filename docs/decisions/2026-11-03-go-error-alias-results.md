# Go の error alias は error 戻り値規約を引き継ぐ

- 状態: 採用
- 日付: 2026-11-03

## 背景

P3-1 の監査で `type Failure = error` を末尾に返す Go 関数が、型自体は
`GoError` に対応するのに、戻り値の形だけタプルへ分類されることを再現した。
Go の alias は元の型と同一であり、spec 8.3 の `(T, error)` → `Result T GoError`
の規則を alias の有無で変える理由はない。

## 案

戻り値の error 判定に先立って `types.Unalias` する。名前付きの新しい型を
alias と混同せず、既存の名前付き型の対応は変えない。Go パッケージの読み込みは
[既存の採用済み方式](2026-11-03-import-go-loading-mechanics.md)を維持する。

## 検証

別モジュールの `replace` 依存から error alias を読み込み、単一セッションの
named identity、参照時の unsupported 診断、Go→Gom→Go の実行をテストする。

検証は [P3-1 session audit](../sketch/2026-11-03-go-import-session-audit.md) を参照。alias chain と単一 error / `(T, error)` の両方が実行まで通る。spec 8.3 の既存の型同一性・error 戻り値規約をそのまま実装するため、新しい構文や規則の追加はない。
