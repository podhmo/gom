# E0301 の text 出力へ不足パターンを出す

- 状態: 採用（実装・検証した — [検証](../sketch/2026-11-27-wave49-lane-d-e0301-text-verify.md)）
- 日付: 2026-11-27
- Supersedes: [diag-output-formats](2026-11-02-diag-output-formats.md) のうち structured `data` は text に描かないという一律ルール（E0301 の `missing_patterns` だけを例外にする）

## 決定

`--format text` の E0301 では、診断 `data.missing_patterns` を構造的な反例として `patterns not covered:` ブロックに描く。他の診断コードの `data` はこれまでどおり text へ一般的には描かない。

これは汎用 `labels` / `details` wire の導入ではない。E0301 だけが、すでに JSON wire と spec §4 の人間向け例の両方に存在する不足パターンを text emitter へ接続する限定例外とする。

## 背景

spec §4 の E0301 例は `patterns not covered:` を示すが、text emitter は structured data を一切描かなかったため、実装では再現できなかった。DMMF の進化テストでは、追加 variant のどれが未カバーかを人間が compiler output から読む必要があり、この欠落が実用上の摩擦だった。

汎用詳細 wire を先に設計すると、JSON schema、LSP 的な副 span、文言表現の責務まで一気に決める必要がある。一方 E0301 の反例は pattern checker がすでに `missing_patterns` として構造化しており、spec 例もある。小さい例外で実害を解消し、汎用 wire は引き続き open question とする。

## 実装

- `internal/diag.emit_text` は `Code == "E0301"` のときだけ `Data["missing_patterns"]` を読み、snippet のあと・notes/fixes の前に `patterns not covered:` を出す
- JSON schema と診断生成側は変えない。`missing_patterns` は従来どおり machine-facing data でもある
- 他のコードの `data` key を template 的に解釈する仕組みは入れない

## 検証

- `internal/diag.TestWriteTextE0301MissingPatterns`: snippet・不足パターン・note の配置を固定
