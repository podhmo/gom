# wave51 lane C: 診断 coverage 再分類と meta-test 強化

## 実施

- unit-only だった例外を意味確認し、E0502/E0704 は checker recovery、E0606 は unit integration、E0901 は compiler-internal、coverage exempt へ再分類した。E0204/E0503 は現行どおり production emitter のない reserved とする
- positive unit / recovery coverage は test function 名まで例外表へ書く形に強化した。coverage scanner は top-level `Test*` function ごとに diagnostic-code literal を対応づけ、例外表に挙げた test が実際にそのコードを含むことだけを根拠にする
- E0606 の evidence から `TestLoadReturnsTypedCycleError` を外した。強化後の meta-test は、この test が E0606 literal をコメントに持つだけで positive assertion ではないことを検出した
- design.md に coverage 分類軸と positive evidence の規則を追加した

## Red / green

- red: 強化した scanner で `TestLoadReturnsTypedCycleError does not name it` の failure を確認した
- green: false positive evidence を外し、`go test ./internal/diag -run 'TestLiveDiagnosticCoverageClassification|TestReservedDiagnosticsHaveNoEmitter' -count=1` pass

## 分類

| code | lane C の分類 | 根拠 |
| --- | --- | --- |
| E0204 | reserved | parser が先に E0101 を出し、production emitter がない。方向決定は保留 |
| E0502 | checker recovery unit test | resolver が value method selection を未解決のまま残し、通常 source からは到達しにくい checker recovery |
| E0503 | reserved | implicit numeric conversion の production emitter がない。方向決定は保留 |
| E0606 | unit integration test | package import cycle は multi-module layout が必要で、gomimport integration test が positive coverage |
| E0704 | checker recovery unit test | parser が destructuring receive pattern を E0101 で拒否するため checker recovery。方向決定は保留 |
| E0901 | compiler-internal, coverage exempt | lowering invariant の violation は利用者 diagnostic ではなく compiler contract |

## 境界

- E0204/E0503/E0704 を実装側へ寄せるか文書・code table 側へ寄せるかは lane C で決めない。open question のまま残す
- explain fragment の coverage 方針もこの lane に入れない
- compiler 内部の coverage 分類変更なので spec fixture の追加は不要
