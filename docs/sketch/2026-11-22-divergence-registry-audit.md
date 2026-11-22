# divergence registry 監査 (wave31 lane E)

- 日付: 2026-11-22
- 目的: eval↔codegen 差分検査・コンパイルバグの登録台帳（known* レジストリ）を全件再検証し、退役済みエントリの掃除と台帳（implementation-gaps.md / TODO.md）との照合を行う。

## レジストリ一覧（全件空を確認）

| レジストリ | 場所 | エントリ数 | 管轄 |
| --- | --- | --- | --- |
| `knownParseBugs` | internal/syntax/fuzz_test.go | 0 | パーサ fuzz |
| `knownFormatBugs` | internal/format/fuzz_test.go | 0 | formatter fuzz |
| `knownGenFormatBugs` | internal/format/gen_test.go | 0 | formatter gen |
| `knownCompileBugs` | internal/build/fuzz_test.go | 0 | コンパイル契約（クラッシュ・不正診断・不正生成 Go） |
| `knownDivergences` | internal/build/diff_test.go | 0 | eval↔codegen の意味差分 |
| `knownInferBugs` | internal/build/infer_test.go | 0 | infer fuzz（cap 16、knownCompileBugs と同じ運用） |

- 各 `fuzzutil.CheckRegistry` テストは PASS（cap・TODO.md 参照の帳簿検査）
- 退役対象のエントリは存在しない — 過去の登録バグはすべて修正済みでレジストリから取り除かれている

## 台帳照合

- `docs/implementation-gaps.md`: 未チェック `[ ]` の divergence 項目なし（line 401 が「末尾の `[ ]` 2 件」を指すが、該当エントリは既に解消・除去済み — ポインタのみ stale）
- `TODO.md` の `[divergence]` 項目: 全 3 件 `[x]` 済み（Float 定数畳み込み・バリアント `.f`・Int 幅）
- 新規に台帳へ足すべきずれ: 本監査では発見なし

## 手順（次回の監査用）

1. `grep -rn 'var known' internal/` で全レジストリを列挙（初版の `knownBugs|…` パターンは knownParseBugs 等 4 件を取りこぼした — `known` 後ろが直結しない命名があるため）
2. エントリごとに repro seed がまず再現するか確認 — 再現しないものは退役。修復検出は fuzz ループ側の `fuzzutil.Report`（登録入力が通るようになったら報告）が担う。`CheckRegistry` は cap・TODO.md 参照の帳簿検査のみで stale エントリは検出しない
3. `docs/implementation-gaps.md`・`TODO.md` と相互参照 — 台帳にあるが登録漏れ、登録済みだが台帳未記載の両方向を確認
