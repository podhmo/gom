# wave57 lane B: bare value name typo candidates 検証

- 日付: 2026-11-28
- lane: B（実装）
- 対応 decision: [../decisions/2026-11-28-e0201-bare-name-typo-candidates.md](../decisions/2026-11-28-e0201-bare-name-typo-candidates.md)

## 実施内容

`undefinedTerm` が出す E0201 `namespace: "value"` に、lexical scope chain から集めた OSA 距離 1 の候補を最大 3 件追加した。候補 kind は var/func/constructor/builtin に限定し、shadowing dedup を filter より先に行う。Hidden、non-test から使えない TestOnly、test-only builtin（`assert` / `assert_eq` / `snapshot`）、3 文字未満の入力は除外する。`fixes` は付けない。

## red-first

fixture 追加後、実装前の `go test ./internal/build -run 'TestSpec/E0201_bare_typo' -count=1` は positive/shadowing/truncate で `data.candidates` と note がないために red だった。実装後は同じテストが pass した。

## fixture

- `E0201_bare_typo_positive`: local variable `total` に対する `tota` positive
- `E0201_bare_typo_negative`: `totx` は `total`/`todo` とも距離 2 で negative
- `E0201_bare_typo_truncate`: `foo1`-`foo4` から 3 件へ truncate
- `E0201_bare_typo_shadowing`: top-level と parameter の同名 `total` を 1 件だけ出す
- `E0201_bare_typo_kind_filter`: module/type 名 `List` に近い `Lis` を出さない
- `E0201_bare_typo_short_name`: 2 文字の `to` には候補を出さない
- `E0201_bare_typo_testonly_builtin`: `asser` に `assert` を出さない
- `TestValueTypoCandidates`: Hidden/TestOnly/kind/shadowing/short-name filter の unit regression
- `TestCheckE0201BareTypoText`: text output に `error[E0201]` と `did you mean` note が出ることを固定

## 文書同期

- `docs/spec.md` §12: bare value typo candidate の対象・filter・上限を更新
- `docs/design.md` 2.3: member/bare value typo の実装済み構造を更新
- `internal/diag/codes.go` / `docs/error-codes.md`: E0201 explanation を更新

## 残件

なし。member typo の context-aware kind filter は wave57 lane C、未 import Gom stdlib module candidate は lane E の調査対象。
