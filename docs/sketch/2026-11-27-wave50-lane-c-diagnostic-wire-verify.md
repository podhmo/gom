# wave50 lane C: diagnostic labels/details wire 検証

## 実施

- `diag.Diagnostic` に汎用 `labels`（副 span + message）と `details`（label + lines）を追加し、JSON wire・validation・text rendererへ接続した
- `gom schema` の diagnostic object に optional な `labels` / `details` を追加した。後方互換な optional field 追加なので schema version は 1 のまま
- text renderer は `data` の鍵を診断コードごとに解釈せず、`details` を generic に `<label>:` block、`labels` を副 span snippet として描く
- E0301 は `data.missing_patterns` に加えて同じ反例を `patterns not covered` detail として出す。wave49 の E0301 限定 renderer 例外は除去した
- E0401 の代表利用として、let 注釈と関数 return 注釈が直接 supply する expected type との mismatch に、注釈位置を指す `expected type declared here` label を付けた。deferred interface conformance で最終確定まで遅れた mismatch でも同じ label を保持し、numeric constraint のような注釈型そのものではない constraint failure には付けない
- spec §12、decision、open-questions、problem、spec harness の `labels` / `details` 比較を同期した

## 結果

- `go test ./internal/diag -count=1`: pass
- `go test ./internal/check -run TestExpectedTypeLabel -count=1`: pass
- `go test ./internal/build -run Spec/E0401_expected_type_label -count=1`: pass
- `go test ./internal/build -run Spec -count=1`: pass
- `go test ./...`: pass
- `go vet ./...` / `gofmt -l .` / `scripts/check-sumtype.sh`: pass
- `go run ./cmd/gom check --format json testdata/spec/E0401_expected_type_label.gom`: let 注釈と関数 return 注釈の両方で `labels[].span` が注釈位置、`message` が `expected type declared here` になることを確認
- `go run ./cmd/gom check --format text` の E0401 出力で副 span snippet、E0301 出力で `patterns not covered` detail block を確認

## 境界

- expected type の由来追跡は、let 注釈と関数 return 注釈に直接起因する mismatch に限る。if/match/select の腕、block 内伝播、呼出し引数、constructor payload、record field、imported signature まで provenance を広げる設計は lane C に入れていない。wave50 closeout で open-questions 登録すべき decision 候補: expected type provenance を型推論のどの単位で運ぶか。方針確定後の実装は TODO 化する
- E0303 など `missing_patterns` を既に持つ他診断への `details` 適用は lane C に入れていない。wave50 closeout で open-questions 登録すべき decision 候補: どの診断の data を人間向け detail として昇格するかの基準。採用対象が決まれば TODO 化する
- labels の text 表示は、主 snippet と統合した rustc 風 multi-span layout ではなく、副 span ごとの独立 snippet とした。wave50 closeout で open-questions 登録すべき decision 候補: 同一ファイル内で近い span を 1 つの snippet に統合するか。統合する場合の実装は TODO 化する
- typo 候補、error recovery 拡大、継続行誤パース診断は prompts どおり対象外

## 運用メモ

- lane C は red-first 運用が明文化される前に実装先行で進んだ。最終的には spec fixture と checker test を伴うが、fixture を先に red 確認してから実装した順序ではない。lane D/E では診断・fixture を変える変更を先に red で固定する
