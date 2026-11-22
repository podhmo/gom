# E0404 は Go 呼出し専用とし、E0401 に未適用ラベルの文言を載せる

- 状態: 採択（2026-11-22 wave32 prompts インタビュー — wave32 lane B で実装）
- 日付: 2026-11-22

`docs/problems/2026-11-02-gom-missing-label-is-partial.md` と
`docs/problems/2026-11-03-overapply-labeled-params.md` が共有する
同根の未決事項: 「ラベル引数を埋め忘れた Gom 呼出し」に E0404 は
発火しない — Gom 呼出しは部分適用として合法な関数値になるためで、
失敗時の診断は E0401（`expected Unit, got ~amount: Int -> Unit`）
になる。この整理は 2 問ある: (1) E0404 の適用域を Gom 呼出しへ
広げるか、(2) E0401 の文言を改善するか。

## 採る案

**(1) E0404 は Go 呼出し専用のままとする。** Gom 呼出しの部分適用は
言語の機能（spec §2 — ラベルの一部だけ渡して残りを関数型に残す）で、
「ラベルを埋め忘れた」と「意図した部分適用」は呼出し位置では区別
できない。全位置引数が埋まりかつ文脈が非関数を要求するとき未充填
ラベルを禁止する案は言語意味を変えるのでこの lane の範囲外とする
（別 decision が要る）。コード表の例は既に Go 呼出し
（`io.Copy ~src: f`）に直っている。

**(2) E0401 の文言に未適用ラベルを添える** — 推奨。部分適用で残った
関数型が非関数の期待型と照合できないとき、E0401 の診断に「callee が
まだ `~x:` を要求している」ことを `data` または注記として載せる。
意味は変えずメッセージだけ改善する小粒変更。位置特定には「この関数型
が部分適用の残りである」ことの追跡が要るため、wave32 以降の小粒
実装として残す。

overapply-labeled-params 側の残件（positional extras がラベル
パラメータを埋めたときの E0401 wording — `expected ~x: Int -> Int,
got Int -> ?n`）も同じ枠組みで扱う: 発火コードは E0401 のまま、
文言改善が入ればそちらの読みやすさも一緒に解決する。

## 考えなかった案

- E0404 を Gom 呼出しにも発火させる: 上記の区別不能性のため棄却。
- 部分適用そのものを限定する: 言語変更、別途検討。

## 関連

- [gom-missing-label-is-partial](../problems/2026-11-02-gom-missing-label-is-partial.md)
- [overapply-labeled-params](../problems/2026-11-03-overapply-labeled-params.md)
- internal/diag/codes.go E0401/E0404、internal/check/apply.go
  `checkGomApply` (CallPartial) / `checkGoApply`
- spec §2、decisions/2026-11-01-labeled-required-arguments.md
