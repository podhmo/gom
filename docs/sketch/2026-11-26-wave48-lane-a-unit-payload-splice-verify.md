# wave48 lane A: Unit payload splice verification

- 日付: 2026-11-26
- lane: wave48 lane A（`Unit` payload 束縛の生成 Go 未使用 bug）

## 再現

`Result Unit String` の `Ok n` を文字列補間だけで使う最小 case:

```gom
fn run () -> Result Unit String = Ok ()
fn answer () -> String = match (run ()) {
  Ok n -> "ok ${n}"
  Err e -> "err ${e}"
}
```

修正前の focused run:

```text
go test ./internal/build -run 'TestEvalMatchesCompiled/unit_payload_splice_binding' -count=1
../cases/c001/unit_payload_splice_binding.gom:7: declared and not used: tmp3
```

原因は production splice の Unit 表示が Go 式として `"()"` リテラルへ降り、operand を参照しないこと。lower/core 側では `n` が使われているため payload 束縛は残るが、codegen 後には使用が消えていた。comparison display の scalar recipe も同じ形だった。

## 対応

- `rt.ShowUnit(struct{}) string` を追加し、結果は常に `"()"` だが引数を参照する
- production splice の Unit と comparison scalar の Unit を両方 `rt.ShowUnit(x)` 経由にした
- `unit_payload_splice_binding` を compare suite に追加
- `testdata/golden/unit_payload_splice.gom/.golden.go` で `tmp3` が `rt.ShowUnit(tmp3)` に使われる生成形を固定

## 検証

```text
go test ./internal/build -run 'TestEvalMatchesCompiled/unit_payload_splice_binding' -count=1  # pass
go test ./internal/build -run 'TestGolden/unit_payload_splice' -update -count=1             # golden 生成
go test ./internal/codegen ./internal/build -count=1                                        # pass
```

eval/compiled の観測出力はどちらも `ok ()`。`Unit` 意味論は変えず、生成 Go が operand を消さないようにしただけなので新規 decision はなし。design §4.8 に implementation note を追記した。
