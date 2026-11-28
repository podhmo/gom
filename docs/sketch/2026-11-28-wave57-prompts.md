# wave57 prompts 草案

状態: 保留（採否インタビュー済み、wave57 開始は見送り）

入力は wave56 round、TODO、open-questions。wave56 は診断候補表示の実装と次段方針の決定を進めたので、wave57 は decision 済みの 2 件を実装しつつ、review で見つかった小さい follow-up と残る候補 source の設計を分ける案を置く。

## lane A: E0401 record field provenance の実装

TODO E0401-1。type parameter を含まない exact declaration type に限り、record field 値と block tail/if/match 腕の最終 mismatch へ `expected type declared here` label を付ける。local/import `.gomi`、positive/negative、generic field、constraint mismatch、text/JSON fixture を含める。decision 済みで実装のみ。

## lane B: bare value name typo candidates の実装

TODO E0201-2。`undefinedTerm` が出す E0201 `namespace: "value"` に限定し、lexical scope chain の value kind だけを対象に、OSA 距離 1 の候補を最大 3 件 note と `data.candidates` に出す。shadowing dedup、TestOnly/test-only builtin/Hidden 除外、3 文字未満 suppression、positive/negative/truncate、kind filter、text/JSON fixture を含める。decision 済みで実装のみ。

## lane C: member typo candidate の context-aware kind filter

wave56 lane B review の follow-up。型注釈や constructor pattern で使えない member を候補から外すため、value/type/pattern の参照位置に応じた kind policy を決めて実装する。bare-name typo の filter と共通化できるなら共有する。`fmt.Printlm` の型注釈 negative、通常 value positive、`Go` operation table の非退行 fixture を含める。実装 lane。

## lane D: diagnostic rendering follow-up の小さい修正

wave56 lane A/B review の follow-up。成功した `gom run` が dependency `.gomi` bytes を保持する問題、spec fixture `-update` が foreign span の絶対パスを書き戻す問題、同じく `-update` が actual data と矛盾する `data_absent` assertion を残す問題、text renderer の source snippet 内制御文字 sanitize を扱う。いずれも診断出力の安全性・再現性に関する小さい修正で、意味変更は入れない。

## lane E: 未 import Gom stdlib module candidate の調査

調査のみ。`list`/`option`/`result`/`cmp`/`str` に限定し、stdlib short-name index を resolver へ注入する所有関係、note に出す import alias/path、`lst` のような typo と import 忘れの区別を fixture で比較する。goimports 的な Go package 探索と safe fix は対象外。採否を decision へ落とし、実装は lane に入れない。

## 候補外

- module/type fallback constructor candidate: `Option.Some` 系は有用だが、wave56 lane E の scope 外で引き続き利用頻度と false positive の評価が足りない
- generic record field / call argument provenance: 複合 label や parameter span ownership の設計が大きく、lane A の結果を見てから分ける
- snippet 統合: wave56 lane D で現時点見送り。頻出ノイズの実例が集まるまで戻さない
- match narrowing の残り精度化: 重要だが診断候補の wave と混ぜると scope が広い

## 採択結果

保留。2026-11-28 の採否インタビューで podhmo は「closeout だけ先に進め、wave57 は保留」を選択した。個別 lane の採否は確定していないため、wave57 再開時にこの草案を入力として採否と順序をやり直す。
