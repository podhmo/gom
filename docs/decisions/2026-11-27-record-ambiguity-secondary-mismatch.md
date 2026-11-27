# record 未解決時の二次 mismatch suppression

- 状態: 採用（調査済み・実装は未実施）
- 日付: 2026-11-27

## 決定

record literal の field-set resolution が失敗し、その位置ですでに E0407/E0401 を報告したときは、recovery type を `Unit` にせず quiet meta へ寄せる。これにより `let seed = { ... }` のような backward inference が必要な形で、未解決 record の後続使用へ出る `got Unit` の二次 E0401 を抑制する。

これは言語の backward inference を導入しない。field-set resolution 失敗時点でソースはすでにエラーなので、quiet meta が後続使用から仮に解かれても受理されるプログラムは増えない。valid program の型推論規則、型名前置 literal 優先、callback result propagation の scope は変えない。

## 理由

- 現状の `Unit` recovery は root cause ではない mismatch を作る。例: `E0407_record_context_no_backward` は literal の E0407 に加えて、後続使用に `expected Acc, got Unit` を出す。`Unit` はユーザーが書いた型ではなく compiler の回復用 artifact なので、利用者を誤導する
- checker の設計は「一つの root cause に一つの診断」を優先し、quiet meta は「他の診断がすでに失敗を説明している recovery artifact」の既存仕組みである。専用の taint/error type や診断 suppression map を新設しなくてよい
- 調査用 prototype では、ambiguous record の recovery を quiet meta へ変えるだけで同 fixture が E0407 1 件になった。`go test ./internal/check ./internal/build` では既存 fixture の期待件数だけが fail し、他の固定済み挙動への影響は観測されなかった
- E0407 に候補 record 名を出す改善は有益だが、`got Unit` の二次診断は残る。現状維持は二次 E0401 がたまたま期待型を示す一方で、回復 artifact を見せる noise を残す

## 実装候補の境界

- `recordByFields` が ambiguity/no-match をすでに報告した経路でだけ recovery を quiet にする。field-set resolution が成功する通常の推論には影響させない
- Unit へ bind しない。quiet meta は後続 checking を続けるための recovery type であり、未解決のまま残っても E0407 を増やさない
- quiet meta が後続使用でいったん具体型へ解けたあと、別の使用と矛盾する場合は、その独立した mismatch は従来どおり報告する。例えば `seed` が `Other` と `Acc` の両方へ使われる形では `expected Acc, got Other` を残す
- 未解決 recovery 値への downstream constraint mismatch は、root diagnostic が未修正の間は追わない。注釈後に残る場合は通常どおり報告される
- E0407 の候補 record 表示改善は今回の実装候補に含めない。二次 mismatch suppression と独立の診断改善として open question に残す

## red-first fixture 方針

1. 既存 `E0407_record_context_no_backward` を更新し、`let seed = { count = 0 }` の E0407 だけを期待する。実装前は actual 2 / expected 1 で red になる
2. `seed` が `Other` へ注釈された変数へ流れたあと `Acc` を返す経路では、E0407 に加えて独立した `expected Acc, got Other` が残る negative fixture を足す
3. ambiguity だけでなく no matching record の経路でも recovery type が quiet になり、root diagnostic のみで終わる fixture を足す
4. 実装後に `go test ./internal/build -run 'TestSpec/E0407_record_context_no_backward' -count=1` と `go test ./internal/check ./internal/build -count=1` を確認する

検証記録: [sketch/2026-11-27-wave54-lane-c-secondary-e0401-verify](../sketch/2026-11-27-wave54-lane-c-secondary-e0401-verify.md)。
