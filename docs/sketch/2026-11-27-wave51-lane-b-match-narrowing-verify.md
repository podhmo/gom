# wave51 lane B: same-scrutinee narrowing 検証

## 実施

- red-first: `E0301_nested_match_selfish_action` / `E0301_nested_match_dmmf_line` / `E0301_nested_match_positive_context` の期待を `diagnostics: []` へ先に更新し、実装前の `go test ./internal/build -run 'TestSpec/E0301_nested_match' -count=1` で 3 件とも現行の E0301 が出て fail することを確認した
- `internal/pattern` のファイル walker に、match 腕本体の lexical scope で有効な root constructor domain を追加した。同じ安定なローカル変数、または root の素朴な変数束縛 alias を scrutinee に持つ nested match では、E0301 の root column の constructor signature だけを絞る
- negative narrowing は guard のない前腕のうち、root constructor を payload も含めて 1 つの alternative が確実に覆うものだけを使う。guard 付き腕は本体では positive narrowing を使えるが、後続腕の negative narrowing には使わない
- E0302 の判定と、root column より深い payload / recursive column の signature は変更しなかった
- 回帰 fixture を追加した:
  - `ok_nested_match_or_narrowing`: or-pattern の positive union と negative folding
  - `E0301_nested_match_partial_payload`: `Add 0` のような partial payload は constructor 全体を negative narrowing から外さない境界
  - `E0301_nested_match_root_only`: narrowing が recursive payload へ波及しない境界

## 結果

- red-first 後、実装では `go test ./internal/build -run 'TestSpec/E0301_nested_match' -count=1` が pass し、selfish/DMMF/positive の 3 fixture は green になった
- `E0301_nested_match_guard_boundary` は decision どおり E0301 `Add _` のまま残った
- `go test ./internal/build ./internal/pattern ./internal/tool/decisionsdoc -count=1`: pass
- `go test ./...`: pass
- `go vet ./...`: pass
- `gofmt -l .`: 出力なし
- `sh scripts/check-sumtype.sh` / `sh scripts/check-staticcheck.sh`: pass

## 境界・残件

- contextual E0302 は入れていない。narrowing は E0301 の網羅性判定だけに使う
- guard 条件の意味解析、リテラル範囲、フィールド/path narrowing、interprocedural narrowing、`as` / or-pattern 内変数の alias 解釈は入れていない
- 到達不能な腕本体では domain が空になり得るが、到達不能自体は従来どおり uncontextual な E0302 側が報告する
- 複数の partial payload を組み合わせて constructor 全体を覆う場合（例: 有限 payload を複数腕で分担）は今回の negative narrowing に使わない。1 つの guard なし alternative が constructor payload をすべて覆う場合だけを根拠にする

## review 対応の追加検証

- `@extern let` は backing Go storage を外から更新しうるため、stable subject から外した。`E0301_nested_match_extern_boundary` は inner match が `Remove _` を欠く E0301 を保持する
- positive narrowing の root 判定は payload の or-pattern を cartesian product へ展開しない root-shape walk にした。16 個の payload がそれぞれ 3 択の or-pattern を持つ guard 付き腕でも、root constructor の判定だけでは payload alternatives を展開しないことを、helper の unit test と `CheckFile` 全体を通す回帰 test で固定した
