# wave57 lane A: E0401 record field provenance 検証

- 日付: 2026-11-28
- lane: A（実装）
- 対応 decision: [../decisions/2026-11-28-e0401-provenance-expansion.md](../decisions/2026-11-28-e0401-provenance-expansion.md)

## 実施内容

`checkRecordLit` が field 値を調べるとき、field declaration の型が declaring type の type parameter を含まない場合だけ `fieldInfo.sp` を `checkExprAt` へ渡すようにした。`usesTypeParam` は declaration 側の型を見るため、generic record の monomorphic field（`type Count a = { n: Int, items: List a }` の `n`）は対象、`v: a` は対象外になる。

## red-first

fixture 追加後、実装前の `go test ./internal/build -run 'TestSpec/E0401_record_field' -count=1` は 4 件の positive case で `diagnostics[0].labels[0]: no remaining actual label` となり red を確認した。実装後は同じテストが pass した。

## fixture

- `E0401_record_field_provenance`: local monomorphic field の direct mismatch で label positive
- `E0401_record_field_branch_provenance`: field 値の if 腕へ同じ provenance が伝わる positive
- `E0401_record_field_generic_mono`: generic record の monomorphic field は positive
- `E0401_record_field_foreign`: import `.gomi` の field declaration span へ label が付く positive。環境依存の `span.file` は assertion から外し、offset/line/col を固定
- `E0401_record_field_generic_negative`: `Box a` の `v: a` では label negative
- `E0401_record_field_constraint_negative`: field 内の独立 constraint mismatch では label negative
- `TestCheckE0401RecordFieldProvenanceText`: text output に `error[E0401]` と `expected type declared here` が出ることを固定

## 文書同期

- `docs/spec.md` §12: record field provenance の対象/除外を更新
- `docs/design.md` 2.3: `checkRecordLit` の provenance 伝播を更新
- `internal/diag/codes.go` / `docs/error-codes.md`: E0401 explanation を更新

## 残件

なし。call argument provenance と type parameter を含む record field の複合 provenance は既存の open question のまま。
