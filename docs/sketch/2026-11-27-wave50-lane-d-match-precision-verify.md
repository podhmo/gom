# wave50 lane D: match 精度調査 検証

## 実施

- implementation-gaps の「catch-all の内側で sub-match すると網羅性の恩恵が薄れる」を、selfish の action dispatch 形と DMMF の `ProductLine` 形に分解して再現した
- `internal/pattern` の現行実装を確認し、各 `match` が独立に Maranget matrix へ入り、外側の腕文脈を渡していないことを確認した
- positive narrowing、negative narrowing、guard、alias、到達不能 arm の適用範囲を [decision](../decisions/2026-11-27-match-nested-narrowing.md) として確定し、利用者に見える規則を spec §4 へ記述した
- 現行挙動を固定する spec fixture を 4 件追加した。いずれも現行では E0301 を期待し、将来の実装 lane で red-first の入力になる

## 観測

- `E0301_nested_match_selfish_action`: 外側で `Add n` を処理したあとの catch-all alias 内で `Remove` / `Archive` を挙げても、現行は `Add _` が不足していると報告する
- `E0301_nested_match_dmmf_line`: `CommentLine` を外側で処理したあとの nested match でも、現行は `CommentLine _` を要求する
- `E0301_nested_match_positive_context`: `Add n` の腕本体で `Add` だけを nested match しても、現行は `Remove _` / `Archive` を要求する
- `E0301_nested_match_guard_boundary`: `Add n if n > 0` では guard 付き腕を negative narrowing に使えないため、将来実装後も `Add _` は残余として扱うべき境界

## 結果

- `go test ./internal/build -run 'Spec/E0301_nested_match' -count=1`: pass
- `go test ./internal/pattern ./internal/check ./internal/build -count=1`: pass
- `go test ./...` / `go vet ./...` / `gofmt -l .` / `scripts/check-staticcheck.sh` / `scripts/check-sumtype.sh`: pass

## 境界

- lane D は調査 + decision + fixture に限定し、pattern/checker 実装は行わない。wave50 closeout で TODO 登録すべき実作業: same-scrutinee root constructor narrowing を `internal/pattern` に実装し、selfish/DMMF/positive の 3 fixture の期待を red-first で更新する。guard boundary fixture は guard 付き腕を negative narrowing に使わない境界なので、実装後も E0301 を期待するまま維持する
- context を考慮した E0302 は移行影響が大きいため第一段階に入れない。wave50 closeout で open-questions 登録すべき decision 候補: contextual unreachable arm を warning/error として有効にする移行時期と診断コード
- ガード条件の意味解析、リテラル範囲、フィールド/path narrowing、interprocedural narrowing、`as` / or-pattern の alias 解釈は対象外。採用する場合は別 decision が必要
- DMMF の「広い `_` があると新ケース追加を検知できない」問題は narrowing では解けない。wave50 closeout で open-questions 登録すべき decision 候補: 進化検知を重視する match スタイルを guide へ書くか、構文/診断で支援するか
